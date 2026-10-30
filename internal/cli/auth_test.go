package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"sshgo/internal/credential"
	"sshgo/internal/host"
)

// fakeStore is the credential surface the auth strategies consult. It stands in
// for the encrypted store so per-hop lookup and the single unlock are observable
// without a master password.
type fakeStore struct {
	passwords map[string]string
	sudo      map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{passwords: map[string]string{}, sudo: map[string]string{}}
}

func (f *fakeStore) Get(key string) (string, error) {
	if pwd, ok := f.passwords[key]; ok {
		return pwd, nil
	}
	return "", errors.New("password not found")
}

func (f *fakeStore) GetSudoPassword(key string) (string, error) {
	if pwd, ok := f.sudo[key]; ok {
		return pwd, nil
	}
	return "", errors.New("sudo password not found")
}

func (f *fakeStore) Set(key, password string) error {
	f.passwords[key] = password
	return nil
}

func (f *fakeStore) SetSudoPassword(key, password string) error {
	f.sudo[key] = password
	return nil
}

// writeTestKey writes an unencrypted private key that tryLoadKey can parse, so
// "this hop has a key" is observable without a real credential.
func writeTestKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return path
}

// hop builds a Resolved the way host.Resolve would, including the ordered
// Credential Keys the lookup depends on.
func hop(name string, keys ...string) host.Resolved {
	if len(keys) == 0 {
		keys = []string{name}
	}
	return host.Resolved{Name: name, Addressed: name, Hostname: "10.0.0.1", Port: "22", User: "root", CredKeys: keys}
}

// ---------------------------------------------------------------------------
// Per-hop authentication
// ---------------------------------------------------------------------------

// Every hop offers its own saved password, even when a key is available. The
// old code skipped the password lookup whenever any key existed, which silently
// broke the most common jump-host combination: agent keys plus a bastion that
// only accepts passwords.
func TestExecAuthOffersPasswordAlongsideKeys(t *testing.T) {
	keyPath := writeTestKey(t)

	store := newFakeStore()
	store.passwords["bastion"] = "bastion-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}
	jump := hop("bastion")
	jump.IdentityFile = keyPath

	cred, err := a.HopAuth(jump)
	if err != nil {
		t.Fatalf("HopAuth() failed: %v", err)
	}
	// publickey for the identity file, password for the bastion's own entry.
	if len(cred.Methods) != 2 {
		t.Fatalf("HopAuth() offered %d methods, want a key method and a password method", len(cred.Methods))
	}
}

// A hop with keys but no saved password offers the key alone; nothing is
// borrowed from another hop.
func TestExecAuthKeysOnlyWhenHopHasNoPassword(t *testing.T) {
	keyPath := writeTestKey(t)

	store := newFakeStore()
	store.passwords["db1"] = "target-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}
	bastion := hop("bastion")
	bastion.IdentityFile = keyPath

	cred, err := a.HopAuth(bastion)
	if err != nil {
		t.Fatalf("HopAuth() failed: %v", err)
	}
	if len(cred.Methods) != 1 {
		t.Errorf("HopAuth() offered %d methods, want the key only", len(cred.Methods))
	}
}

// The password offered to a hop is that hop's own. Sharing it across hops would
// hand the target's credential to the bastion.
func TestExecAuthPasswordIsPerHop(t *testing.T) {
	store := newFakeStore()
	store.passwords["bastion"] = "bastion-pw"
	store.passwords["db1"] = "target-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}

	pwd, err := a.savedPassword(hop("bastion"))
	if err != nil {
		t.Fatalf("savedPassword(bastion) failed: %v", err)
	}
	if pwd != "bastion-pw" {
		t.Errorf("savedPassword(bastion) = %q, want the bastion's own password", pwd)
	}

	pwd, err = a.savedPassword(hop("db1"))
	if err != nil {
		t.Fatalf("savedPassword(db1) failed: %v", err)
	}
	if pwd != "target-pw" {
		t.Errorf("savedPassword(db1) = %q, want the target's own password", pwd)
	}
}

// A hop with no entry of its own gets nothing: the lookup never borrows another
// hop's password.
func TestExecAuthPasswordMissingForHop(t *testing.T) {
	store := newFakeStore()
	store.passwords["db1"] = "target-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}

	if _, err := a.savedPassword(hop("bastion")); err == nil {
		t.Error("savedPassword(bastion) found a password, want none: credentials never cross hops")
	}
}

// An Alias is addressed first: a password saved under the alias must be found
// even though the canonical Host name differs.
func TestExecAuthPasswordUsesCredentialKeys(t *testing.T) {
	store := newFakeStore()
	store.passwords["database"] = "alias-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}

	pwd, err := a.savedPassword(hop("db1", "database", "db1", "10.0.0.1"))
	if err != nil {
		t.Fatalf("savedPassword() failed: %v", err)
	}
	if pwd != "alias-pw" {
		t.Errorf("savedPassword() = %q, want the alias entry", pwd)
	}
}

// ---------------------------------------------------------------------------
// Sudo boundary
// ---------------------------------------------------------------------------

// A sudo password is never an SSH authentication method: it only reaches the
// target session's stdin.
func TestExecAuthSudoPasswordIsNotAnAuthMethod(t *testing.T) {
	store := newFakeStore()
	store.sudo["db1"] = "sudo-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}, needSudo: true}

	if _, err := a.savedPassword(hop("db1")); err == nil {
		t.Error("savedPassword() returned the sudo password, want none")
	}

	pwd, ok := a.SudoPassword(hop("db1"))
	if !ok || pwd != "sudo-pw" {
		t.Errorf("SudoPassword() = (%q, %v), want the saved sudo password", pwd, ok)
	}
}

// Sudo Password Priority: saved sudo password, then the saved SSH password.
func TestExecAuthSudoFallsBackToSavedSSHPassword(t *testing.T) {
	store := newFakeStore()
	store.passwords["db1"] = "ssh-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}, needSudo: true}

	pwd, ok := a.SudoPassword(hop("db1"))
	if !ok || pwd != "ssh-pw" {
		t.Errorf("SudoPassword() = (%q, %v), want the saved SSH password reused", pwd, ok)
	}
}

// Without --sudo no sudo password is resolved at all.
func TestExecAuthSudoNotResolvedWithoutFlag(t *testing.T) {
	store := newFakeStore()
	store.sudo["db1"] = "sudo-pw"

	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}

	if pwd, ok := a.SudoPassword(hop("db1")); ok {
		t.Errorf("SudoPassword() = %q, want nothing without --sudo", pwd)
	}
}

// ---------------------------------------------------------------------------
// Single password-store unlock
// ---------------------------------------------------------------------------

// One invocation unlocks the store once, no matter how many hops ask for a
// password and no matter whether the password is then saved. Two hops used to
// mean two master-password prompts.
func TestPasswordStoreUnlocksOnce(t *testing.T) {
	setupTempHome(t)

	// A store file has to exist, otherwise there is nothing to unlock.
	seed, err := credential.Load("master")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := seed.Set("bastion", "bastion-pw"); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	prompts := 0
	c := &CLI{Out: io.Discard, Err: io.Discard, masterPassword: func(string) (string, error) {
		prompts++
		return "master", nil
	}}

	first := c.openStore()
	if first == nil {
		t.Fatal("openStore() returned nil for an existing store")
	}
	if _, ok := lookupSavedPassword(first, []string{"bastion"}); !ok {
		t.Error("the seeded password was not found")
	}

	// A second hop and a save both reuse the unlocked store.
	if second := c.openStore(); second == nil {
		t.Fatal("openStore() returned nil on the second call")
	}
	if store, err := c.saveStore(); err != nil || store == nil {
		t.Fatalf("saveStore() = (%v, %v), want the already unlocked store", store, err)
	}

	if prompts != 1 {
		t.Errorf("master password prompted %d times, want exactly 1", prompts)
	}
}

// A missing store file is not an unlock: `exec` on a key-only machine must not
// ask for a master password it does not need.
func TestPasswordStoreAbsentDoesNotPrompt(t *testing.T) {
	setupTempHome(t)

	c := &CLI{Out: io.Discard, Err: io.Discard, masterPassword: func(string) (string, error) {
		t.Error("the master password was requested although no store exists")
		return "", nil
	}}

	if store := c.openStore(); store != nil {
		t.Errorf("openStore() = %v, want nil when no store exists", store)
	}
	if _, ok := lookupSavedPassword(c.openStore(), []string{"bastion"}); ok {
		t.Error("lookupSavedPassword() found a password in a store that does not exist")
	}
}

// A cancelled master password degrades to key-only auth instead of hanging or
// aborting the connection.
func TestPasswordStoreCancelledIsTreatedAsEmpty(t *testing.T) {
	setupTempHome(t)

	seed, err := credential.Load("master")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := seed.Set("bastion", "bastion-pw"); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	c := &CLI{Out: io.Discard, Err: io.Discard, masterPassword: func(string) (string, error) {
		return "", fmt.Errorf("not a terminal")
	}}

	if store := c.openStore(); store != nil {
		t.Errorf("openStore() = %v, want nil after a cancelled prompt", store)
	}
}

// ---------------------------------------------------------------------------
// Non-interactive contract
// ---------------------------------------------------------------------------

// exec never prompts for an SSH password, on the target or on a hop: the prompt
// would hang a CI job. The strategy must therefore produce nothing rather than
// reach for the terminal.
func TestExecAuthNeverPrompts(t *testing.T) {
	// A developer's agent or ~/.ssh default keys would otherwise supply a
	// credential and hide the point of the test.
	t.Setenv("SSH_AUTH_SOCK", "")
	setupTempHome(t)

	store := newFakeStore()
	a := &execAuth{cli: &CLI{Err: io.Discard, store: store, storeOpened: true}}

	cred, err := a.HopAuth(hop("bastion"))
	if err != nil {
		t.Fatalf("HopAuth() failed: %v", err)
	}
	if len(cred.Methods) != 0 {
		t.Errorf("HopAuth() offered %d methods, want none without keys or a saved password", len(cred.Methods))
	}
	if pwd, ok := a.SudoPassword(hop("bastion")); ok {
		t.Errorf("SudoPassword() = %q, want nothing without --sudo", pwd)
	}
}

// sudoInput is the only path a sudo password takes to the target: the session's
// stdin, and only with --sudo.
func TestSudoInputOnlyWithSudo(t *testing.T) {
	if got := sudoInput(false, "pw", true); got != "" {
		t.Errorf("sudoInput(false) = %q, want empty", got)
	}
	if got := sudoInput(true, "pw", false); got != "" {
		t.Errorf("sudoInput(true, no password) = %q, want empty", got)
	}
	if got := sudoInput(true, "pw", true); got != "pw" {
		t.Errorf("sudoInput(true) = %q, want the password", got)
	}
}
