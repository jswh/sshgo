package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"

	"sshgo/internal/connect"
	"sshgo/internal/credential"
	"sshgo/internal/host"
	"sshgo/internal/ui"
)

// interactiveAuth is the authentication strategy for the interactive login
// path: it may prompt for a passphrase, a password, and whether to save it.
type interactiveAuth struct{}

// HopAuth collects agent keys, key files, and a saved or prompted password.
func (interactiveAuth) HopAuth(hop host.Resolved) (connect.HopCredentials, error) {
	var allSigners []ssh.Signer
	var cleanup func()

	// 1. Agent keys, shared as one publickey method.
	if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		cleanup = func() { conn.Close() }
		sshAgent := agent.NewClient(conn)
		if signers, err := sshAgent.Signers(); err == nil && len(signers) > 0 {
			allSigners = append(allSigners, signers...)
			fmt.Fprintln(os.Stderr, "Using SSH agent keys")
		}
	}

	// 2. Non-agent methods: key files plus a password.
	fileSigners, extraMethods, err := buildAuthMethods(hop)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return connect.HopCredentials{}, err
	}
	allSigners = append(allSigners, fileSigners...)

	var methods []ssh.AuthMethod
	if len(allSigners) > 0 {
		methods = append(methods, ssh.PublicKeys(allSigners...))
	}
	methods = append(methods, extraMethods...)

	return connect.HopCredentials{Methods: methods, Cleanup: cleanup}, nil
}

// SudoPassword is unused on the interactive path: sudo passwords only exist for
// `exec --sudo`.
func (interactiveAuth) SudoPassword(host.Resolved) (string, bool) { return "", false }

// buildAuthMethods collects file-based key signers plus password methods for an
// interactive login. Passwords are looked up and saved by Credential Key, so an
// entry saved for the addressed name is found here as well as by `exec`.
func buildAuthMethods(target host.Resolved) ([]ssh.Signer, []ssh.AuthMethod, error) {
	var fileSigners []ssh.Signer
	var extraMethods []ssh.AuthMethod
	identityFile := target.IdentityFile

	// 1. Try configured IdentityFile
	if identityFile != "" {
		identityFile = host.ExpandPath(identityFile)
		if signer, err := loadPrivateKey(identityFile); err == nil {
			fileSigners = append(fileSigners, signer)
			fmt.Fprintf(os.Stderr, "Using key: %s\n", identityFile)
		}
	}

	// 2. Try default keys (only if no IdentityFile configured)
	if identityFile == "" {
		homeDir, _ := os.UserHomeDir()
		for _, keyName := range []string{"id_rsa", "id_ed25519", "id_ecdsa"} {
			keyPath := filepath.Join(homeDir, ".ssh", keyName)
			if signer, err := loadPrivateKey(keyPath); err == nil {
				fileSigners = append(fileSigners, signer)
			}
		}
	}

	// 3. Check saved passwords. The store is loaded here rather than passed in
	// because loading may prompt for the master password, which should only
	// happen when key auth is not enough.
	if savedPwd, ok := credential.LookupSavedPassword(loadSavedPasswords(), target.CredKeys); ok {
		extraMethods = append(extraMethods, ssh.Password(savedPwd))
		fmt.Fprintln(os.Stderr, "Using saved password")
		return fileSigners, extraMethods, nil
	}

	// 4. Return if we have any auth methods
	if len(fileSigners) > 0 || len(extraMethods) > 0 {
		return fileSigners, extraMethods, nil
	}

	// 5. Prompt for password
	password, err := ui.Password(fmt.Sprintf("Enter password for %s@%s", target.User, target.Hostname))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to enter password: %w", err)
	}

	extraMethods = append(extraMethods, ssh.Password(password))

	// Ask to save password
	if ui.YesNo("Save password?", "n") {
		store, err := initPasswordStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize password store: %v\n", err)
		} else if err := store.Set(target.CredKeys[0], password); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save password: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "Password saved")
		}
	}

	return fileSigners, extraMethods, nil
}

// execAuth is the non-interactive strategy used by `exec`: it never asks for an
// SSH password, and only the sudo password may be prompted for on a TTY.
//
// The sudo password is collected during HopAuth so the prompt order stays
// identical to the pre-refactor code, and is then handed out through the
// separate SudoPassword exit instead of being mixed into the auth methods.
type execAuth struct {
	needSudo bool
	sudoPwd  string
}

// HopAuth collects key signers and, when needed, the sudo password.
func (a *execAuth) HopAuth(hop host.Resolved) (connect.HopCredentials, error) {
	var allSigners []ssh.Signer
	var cleanup func()

	if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		cleanup = func() { conn.Close() }
		sshAgent := agent.NewClient(conn)
		if signers, err := sshAgent.Signers(); err == nil && len(signers) > 0 {
			allSigners = append(allSigners, signers...)
			fmt.Fprintln(os.Stderr, "exec: using SSH agent keys")
		}
	}

	fileSigners, password, passwordIsAuthMethod := buildAuthMethodsNonInteractive(hop, a.needSudo)
	allSigners = append(allSigners, fileSigners...)

	var methods []ssh.AuthMethod
	if len(allSigners) > 0 {
		methods = append(methods, ssh.PublicKeys(allSigners...))
	}
	if passwordIsAuthMethod {
		// Password method is separate (different method name "password")
		methods = append(methods, ssh.Password(password))
	}
	// A password found only for sudo still reaches the target session through
	// SudoPassword; it must never be offered as an authentication method.
	a.sudoPwd = password

	return connect.HopCredentials{Methods: methods, Cleanup: cleanup}, nil
}

// SudoPassword returns the password for the target session's sudo -S stdin.
func (a *execAuth) SudoPassword(host.Resolved) (string, bool) {
	return a.sudoPwd, a.sudoPwd != ""
}

// buildAuthMethodsNonInteractive collects file-based key signers and returns
// them along with a password and whether that password is an SSH auth method.
// The password is returned for sudo use even when it is not an auth method, so
// "key auth + saved sudo password" keeps working.
// Priority for sudo password: saved sudo password > saved SSH password > interactive prompt.
func buildAuthMethodsNonInteractive(target host.Resolved, needSudo bool) ([]ssh.Signer, string, bool) {
	var fileSigners []ssh.Signer
	identityFile := target.IdentityFile

	// Try configured IdentityFile (skip passphrase-protected keys non-interactively)
	if identityFile != "" {
		identityFile = host.ExpandPath(identityFile)
		if signer := tryLoadKey(identityFile); signer != nil {
			fileSigners = append(fileSigners, signer)
			fmt.Fprintln(os.Stderr, "exec: using identity file")
		}
	}

	// Try default keys
	if identityFile == "" {
		homeDir, _ := os.UserHomeDir()
		for _, keyName := range []string{"id_rsa", "id_ed25519", "id_ecdsa"} {
			keyPath := filepath.Join(homeDir, ".ssh", keyName)
			if signer := tryLoadKey(keyPath); signer != nil {
				fileSigners = append(fileSigners, signer)
			}
		}
	}

	hasKeyAuth := len(fileSigners) > 0

	// If sudo is needed or no key auth, try saved passwords (may prompt for master password)
	if needSudo || !hasKeyAuth {
		store := tryLoadPasswordStoreInteractive()
		if store != nil {
			// First try saved sudo password
			if needSudo {
				if pwd, ok := credential.LookupSudoPassword(store, target.CredKeys); ok {
					fmt.Fprintln(os.Stderr, "exec: using saved sudo password")
					return fileSigners, pwd, !hasKeyAuth
				}
			}
			// Then try saved SSH password
			if pwd, ok := credential.LookupSavedPassword(store, target.CredKeys); ok {
				fmt.Fprintln(os.Stderr, "exec: using saved password")
				return fileSigners, pwd, !hasKeyAuth
			}
		}
	}

	// Key auth + sudo + no saved passwords → prompt for sudo password
	if needSudo && hasKeyAuth {
		if pwd := promptSudoPassword(target); pwd != "" {
			return fileSigners, pwd, false
		}
	}

	return fileSigners, "", false
}

// promptSudoPassword interactively prompts for a sudo password with option to save.
// Uses stderr for prompts and term.ReadPassword to avoid corrupting stdout.
// In non-interactive terminals, prints a hint and returns "".
// The password is saved under the addressed host key, matching
// `config set-sudo-password <host>`.
func promptSudoPassword(target host.Resolved) string {
	hostKey := target.CredKeys[0]
	if !isInteractive() {
		fmt.Fprintf(os.Stderr, "sudo password required for %s@%s. Use `sshgo config set-sudo-password %s` to set one.\n", target.User, target.Hostname, hostKey)
		return ""
	}
	fmt.Fprintf(os.Stderr, "sudo password for %s@%s (empty = NOPASSWD): ", target.User, target.Hostname)
	pwdBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return ""
	}
	pwd := string(pwdBytes)
	if pwd == "" {
		return ""
	}

	// Ask to save
	fmt.Fprintf(os.Stderr, "Save sudo password? (y/n): ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if strings.ToLower(answer) == "y" {
		store, err := initPasswordStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to init password store: %v\n", err)
		} else if err := store.SetSudoPassword(hostKey, pwd); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save sudo password: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "sudo password saved")
		}
	}

	return pwd
}

// isInteractive checks whether stdout is a terminal (TTY).
// Uses stdout instead of stdin so that subshells (e.g. test scripts with $())
// are correctly detected as non-interactive, avoiding hangs on prompts.
// Uses term.IsTerminal instead of ModeCharDevice because /dev/null is also
// a character device on some systems but is not a TTY.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// tryLoadKey tries to load a private key without passphrase prompts. Returns nil on failure.
func tryLoadKey(path string) ssh.Signer {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil // skip passphrase-protected keys
	}
	return signer
}

// tryLoadPasswordStoreInteractive loads the password store, prompting for master password.
// Returns nil if the file doesn't exist or the user cancels.
func tryLoadPasswordStoreInteractive() *credential.Store {
	path := credential.Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return loadSavedPasswords()
}
