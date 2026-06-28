package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const sudoKeySuffix = "/sudo"

type PasswordStore struct {
	Passwords map[string]string `json:"passwords"`
	Salt      string            `json:"salt"`
	path      string
	key       []byte
}

func getPasswordStorePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".sshgo_passwords"
	}
	return filepath.Join(homeDir, ".sshgo_passwords")
}

func deriveKey(password string, salt []byte) []byte {
	h := sha256.New()
	h.Write([]byte(password))
	h.Write(salt)
	return h.Sum(nil)
}

func LoadPasswordStore(masterPassword string) (*PasswordStore, error) {
	path := getPasswordStorePath()
	store := &PasswordStore{
		Passwords: make(map[string]string),
		path:      path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			salt := make([]byte, 16)
			rand.Read(salt)
			store.Salt = base64.StdEncoding.EncodeToString(salt)
			store.key = deriveKey(masterPassword, salt)
			return store, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	salt, _ := base64.StdEncoding.DecodeString(store.Salt)
	store.key = deriveKey(masterPassword, salt)

	if len(store.Passwords) > 0 {
		for k, v := range store.Passwords {
			if _, err := store.decryptValue(v); err != nil {
				return nil, fmt.Errorf("invalid master password or corrupted data")
			}
			_ = k
			break
		}
	}

	return store, nil
}

func (s *PasswordStore) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *PasswordStore) encryptValue(plaintext string) (string, error) {
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

func (s *PasswordStore) decryptValue(encoded string) (string, error) {
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

func (s *PasswordStore) Set(host, password string) error {
	encrypted, err := s.encryptValue(password)
	if err != nil {
		return err
	}
	s.Passwords[host] = encrypted
	return s.Save()
}

func (s *PasswordStore) Get(host string) (string, error) {
	encrypted, ok := s.Passwords[host]
	if !ok {
		return "", fmt.Errorf("password not found")
	}
	return s.decryptValue(encrypted)
}

func (s *PasswordStore) Has(host string) bool {
	_, ok := s.Passwords[host]
	return ok
}

func (s *PasswordStore) sudoKey(host string) string {
	return host + sudoKeySuffix
}

// SetSudoPassword stores a sudo password for the given host.
func (s *PasswordStore) SetSudoPassword(host, password string) error {
	return s.Set(s.sudoKey(host), password)
}

// GetSudoPassword retrieves the saved sudo password for the given host.
func (s *PasswordStore) GetSudoPassword(host string) (string, error) {
	return s.Get(s.sudoKey(host))
}

// HasSudoPassword checks if a sudo password is saved for the given host.
func (s *PasswordStore) HasSudoPassword(host string) bool {
	return s.Has(s.sudoKey(host))
}
