package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

const sudoKeySuffix = "/sudo"

// Fixed KDF parameters for the v2 password store. Changing them makes existing
// stores unreadable, so they are constants rather than configuration.
const (
	passwordStoreVersion = 2
	passwordStoreKDF     = "argon2id"

	argon2Time    uint32 = 1
	argon2Memory  uint32 = 64 * 1024 // KiB
	argon2Threads uint8  = 4

	derivedKeyLength = 32
	saltLength       = 16
)

// KDFParams records the argon2id cost parameters used for a store.
type KDFParams struct {
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

// Store is the encrypted password store: one file, one master password, one
// AES-GCM key derived with argon2id.
type Store struct {
	Version   int               `json:"version"`
	KDF       string            `json:"kdf"`
	Params    KDFParams         `json:"params"`
	Passwords map[string]string `json:"passwords"`
	Salt      string            `json:"salt"`
	path      string
	key       []byte
}

func defaultKDFParams() KDFParams {
	return KDFParams{Time: argon2Time, Memory: argon2Memory, Threads: argon2Threads}
}

// validate guards against a corrupted or hostile store file whose parameters
// would make argon2.IDKey panic (zero rounds or zero parallelism).
func (p KDFParams) validate() error {
	if p.Time < 1 || p.Threads < 1 || p.Memory < 8*uint32(p.Threads) {
		return fmt.Errorf("invalid KDF parameters in password store (time=%d, memory=%d, threads=%d)", p.Time, p.Memory, p.Threads)
	}
	return nil
}

// Path returns the password store file path.
func Path() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".sshgo_passwords"
	}
	return filepath.Join(homeDir, ".sshgo_passwords")
}

func deriveKey(password string, salt []byte, params KDFParams) []byte {
	return argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Threads, derivedKeyLength)
}

func newStore(masterPassword, path string) (*Store, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	params := defaultKDFParams()
	store := &Store{
		Version:   passwordStoreVersion,
		KDF:       passwordStoreKDF,
		Params:    params,
		Passwords: make(map[string]string),
		Salt:      base64.StdEncoding.EncodeToString(salt),
		path:      path,
	}
	store.key = deriveKey(masterPassword, salt, params)
	return store, nil
}

// Load opens the encrypted store, creating a new one when the file is absent.
// Opening verifies the master password by decrypting every entry.
func Load(masterPassword string) (*Store, error) {
	path := Path()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newStore(masterPassword, path)
		}
		return nil, err
	}

	store := &Store{path: path}
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	// Pre-argon2id stores carry no version/kdf/params. There is no migration
	// path: refuse to open them instead of silently deriving the wrong key.
	if store.Version != passwordStoreVersion {
		return nil, fmt.Errorf("password store uses the old format and cannot be opened; please remove it and save your passwords again")
	}

	if store.KDF != passwordStoreKDF {
		return nil, fmt.Errorf("unsupported password store KDF %q", store.KDF)
	}

	if err := store.Params.validate(); err != nil {
		return nil, err
	}

	salt, err := base64.StdEncoding.DecodeString(store.Salt)
	if err != nil {
		return nil, fmt.Errorf("invalid password store salt: %w", err)
	}
	store.key = deriveKey(masterPassword, salt, store.Params)

	if store.Passwords == nil {
		store.Passwords = make(map[string]string)
	}

	if err := store.verifyKey(); err != nil {
		return nil, err
	}

	return store, nil
}

// verifyKey decrypts every entry to detect a wrong master password or a
// corrupted entry: the GCM tag fails as soon as the derived key is wrong.
// Empty stores skip the check.
func (s *Store) verifyKey() error {
	for _, v := range s.Passwords {
		if _, err := s.decryptValue(v); err != nil {
			return fmt.Errorf("invalid master password or corrupted data")
		}
	}
	return nil
}

// Save writes the store with 0600 permissions.
func (s *Store) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *Store) encryptValue(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *Store) decryptValue(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// Set stores the SSH password for the given key.
func (s *Store) Set(host, password string) error {
	encrypted, err := s.encryptValue(password)
	if err != nil {
		return err
	}
	s.Passwords[host] = encrypted
	return s.Save()
}

// Get retrieves the SSH password for the given key.
func (s *Store) Get(host string) (string, error) {
	encrypted, ok := s.Passwords[host]
	if !ok {
		return "", fmt.Errorf("password not found")
	}
	return s.decryptValue(encrypted)
}

// Has reports whether an SSH password is stored for the given key.
func (s *Store) Has(host string) bool {
	_, ok := s.Passwords[host]
	return ok
}

func (s *Store) sudoKey(host string) string {
	return host + sudoKeySuffix
}

// SetSudoPassword stores a sudo password for the given host.
func (s *Store) SetSudoPassword(host, password string) error {
	return s.Set(s.sudoKey(host), password)
}

// GetSudoPassword retrieves the saved sudo password for the given host.
func (s *Store) GetSudoPassword(host string) (string, error) {
	return s.Get(s.sudoKey(host))
}

// HasSudoPassword checks if a sudo password is saved for the given host.
func (s *Store) HasSudoPassword(host string) bool {
	return s.Has(s.sudoKey(host))
}
