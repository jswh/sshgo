package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTempHome points HOME (and USERPROFILE for Windows) at a fresh temp dir
// so the password store and local config resolve inside it.
func setupTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestPasswordStoreRoundtrip(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Set("example.com", "s3cret"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	reloaded, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got, err := reloaded.Get("example.com")
	if err != nil {
		t.Fatalf("get password: %v", err)
	}
	if got != "s3cret" {
		// Don't echo stored secrets into test output.
		t.Error("roundtripped password does not match the stored one")
	}
}

func TestPasswordStoreFileFormat(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Set("example.com", "s3cret"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	data, err := os.ReadFile(getPasswordStorePath())
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse store file: %v", err)
	}

	for _, key := range []string{"version", "kdf", "params", "salt", "passwords"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("store file missing %q field", key)
		}
	}

	var version int
	if err := json.Unmarshal(raw["version"], &version); err != nil || version != 2 {
		t.Errorf("version = %s, want 2", raw["version"])
	}
	var kdf string
	if err := json.Unmarshal(raw["kdf"], &kdf); err != nil || kdf != "argon2id" {
		t.Errorf("kdf = %s, want \"argon2id\"", raw["kdf"])
	}
	// Values fixed by the design decision: t=1, m=64 MiB, p=4.
	var params struct {
		Time    uint32 `json:"time"`
		Memory  uint32 `json:"memory"`
		Threads uint8  `json:"threads"`
	}
	if err := json.Unmarshal(raw["params"], &params); err != nil {
		t.Fatalf("parse params: %v", err)
	}
	if params.Time != 1 || params.Memory != 64*1024 || params.Threads != 4 {
		t.Errorf("params = %+v, want time=1 memory=%d threads=4", params, 64*1024)
	}

	var salt string
	if err := json.Unmarshal(raw["salt"], &salt); err != nil {
		t.Fatalf("parse salt: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		t.Fatalf("salt is not base64: %v", err)
	}
	if len(decoded) < 16 {
		t.Errorf("salt length = %d, want >= 16", len(decoded))
	}
}

func TestPasswordStoreWrongMasterPassword(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("correct")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Set("example.com", "s3cret"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	if _, err := LoadPasswordStore("wrong"); err == nil {
		t.Fatal("load with wrong master password succeeded, want error")
	}
}

func TestPasswordStoreTamperedCiphertext(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Set("example.com", "s3cret"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	path := getPasswordStorePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}

	// Patch the ciphertext in place so every other field (version, kdf, params,
	// salt) survives and the failure can only come from the GCM tag.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse store file: %v", err)
	}
	var passwords map[string]string
	if err := json.Unmarshal(raw["passwords"], &passwords); err != nil {
		t.Fatalf("parse passwords: %v", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(passwords["example.com"])
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	ciphertext[len(ciphertext)-1] ^= 0xFF
	passwords["example.com"] = base64.StdEncoding.EncodeToString(ciphertext)

	encoded, err := json.Marshal(passwords)
	if err != nil {
		t.Fatalf("marshal tampered passwords: %v", err)
	}
	raw["passwords"] = encoded

	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal tampered store: %v", err)
	}
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatalf("write tampered store: %v", err)
	}

	if _, err := LoadPasswordStore("master-pass"); err == nil {
		t.Fatal("load of tampered store succeeded, want error")
	}
}

func TestPasswordStoreRejectsOldFormat(t *testing.T) {
	home := setupTempHome(t)

	// Shape of the pre-argon2id store: no version/kdf/params fields.
	old := `{
  "passwords": {"example.com": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
  "salt": "c2FsdHNhbHRzYWx0c2E="
}`
	if err := os.WriteFile(filepath.Join(home, ".sshgo_passwords"), []byte(old), 0600); err != nil {
		t.Fatalf("write old store: %v", err)
	}

	_, err := LoadPasswordStore("master-pass")
	if err == nil {
		t.Fatal("load of old-format store succeeded, want error")
	}
	want := "old format"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to mention %q", err, want)
	}
}

func TestPasswordStoreTamperedNonFirstEntry(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	for _, host := range []string{"a.example.com", "b.example.com", "c.example.com"} {
		if err := store.Set(host, "s3cret-"+host); err != nil {
			t.Fatalf("set password for %s: %v", host, err)
		}
	}

	// Tamper only b.example.com: map iteration order is random, so every entry
	// has to be verified for this to fail deterministically.
	path := getPasswordStorePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse store file: %v", err)
	}
	var passwords map[string]string
	if err := json.Unmarshal(raw["passwords"], &passwords); err != nil {
		t.Fatalf("parse passwords: %v", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(passwords["b.example.com"])
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	ciphertext[0] ^= 0xFF
	passwords["b.example.com"] = base64.StdEncoding.EncodeToString(ciphertext)
	encoded, err := json.Marshal(passwords)
	if err != nil {
		t.Fatalf("marshal tampered passwords: %v", err)
	}
	raw["passwords"] = encoded
	tampered, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal tampered store: %v", err)
	}
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatalf("write tampered store: %v", err)
	}

	if _, err := LoadPasswordStore("master-pass"); err == nil {
		t.Fatal("load of store with a tampered entry succeeded, want error")
	}
}

func TestPasswordStoreRejectsUnknownKDF(t *testing.T) {
	home := setupTempHome(t)

	unknown := `{
  "version": 2,
  "kdf": "sha256",
  "params": {"time": 1, "memory": 65536, "threads": 4},
  "passwords": {},
  "salt": "c2FsdHNhbHRzYWx0c2E="
}`
	if err := os.WriteFile(filepath.Join(home, ".sshgo_passwords"), []byte(unknown), 0600); err != nil {
		t.Fatalf("write store with unknown kdf: %v", err)
	}

	if _, err := LoadPasswordStore("master-pass"); err == nil {
		t.Fatal("load of store with unknown kdf succeeded, want error")
	}
}

func TestPasswordStoreRejectsInvalidKDFParams(t *testing.T) {
	home := setupTempHome(t)

	// Zero rounds would panic inside argon2.IDKey; the loader must reject it.
	corrupt := `{
  "version": 2,
  "kdf": "argon2id",
  "params": {"time": 0, "memory": 65536, "threads": 4},
  "passwords": {},
  "salt": "c2FsdHNhbHRzYWx0c2E="
}`
	if err := os.WriteFile(filepath.Join(home, ".sshgo_passwords"), []byte(corrupt), 0600); err != nil {
		t.Fatalf("write corrupt store: %v", err)
	}

	if _, err := LoadPasswordStore("master-pass"); err == nil {
		t.Fatal("load of store with zero rounds succeeded, want error")
	}
}

func TestPasswordStoreSaltIsRandomPerStore(t *testing.T) {
	home := setupTempHome(t)

	first, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create first store: %v", err)
	}
	if err := os.Remove(filepath.Join(home, ".sshgo_passwords")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove store file: %v", err)
	}

	second, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create second store: %v", err)
	}
	if first.Salt == second.Salt {
		t.Errorf("salt reused across stores: %q", first.Salt)
	}
}
