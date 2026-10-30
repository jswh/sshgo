package cli

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
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
type interactiveAuth struct {
	cli *CLI
}

// HopAuth collects agent keys, key files, and a saved or prompted password.
func (a *interactiveAuth) HopAuth(hop host.Resolved) (connect.HopCredentials, error) {
	// 1. Agent keys, shared as one publickey method.
	allSigners, cleanup := agentSigners(a.cli.Err, "Using SSH agent keys")

	// 2. Non-agent methods: key files plus a password.
	fileSigners, extraMethods, err := a.buildAuthMethods(hop)
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

// agentSigners connects to the SSH agent and lists its keys. The returned
// cleanup closes the connection and may be nil. The progress line is a
// parameter because the two paths word it differently on purpose.
func agentSigners(errOut io.Writer, progressLine string) ([]ssh.Signer, func()) {
	conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		return nil, nil
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil || len(signers) == 0 {
		conn.Close()
		return nil, nil
	}
	fmt.Fprintln(errOut, progressLine)
	return signers, func() { conn.Close() }
}

// keySigners collects the key files for one hop: the configured IdentityFile, or
// the default keys when none is configured. The second result is the expanded
// IdentityFile path, empty when the default keys were used, so each path can
// print its own progress line. load is the only difference between the two
// paths: the interactive one may prompt for a passphrase, `exec` never may.
func keySigners(target host.Resolved, load func(string) (ssh.Signer, error)) ([]ssh.Signer, string) {
	if target.IdentityFile == "" {
		var signers []ssh.Signer
		for _, keyPath := range defaultKeyPaths() {
			if signer, err := load(keyPath); err == nil {
				signers = append(signers, signer)
			}
		}
		return signers, ""
	}

	path := host.ExpandPath(target.IdentityFile)
	signer, err := load(path)
	if err != nil {
		return nil, path
	}
	return []ssh.Signer{signer}, path
}

// SudoPassword is unused on the interactive path: sudo passwords only exist for
// `exec --sudo`.
func (a *interactiveAuth) SudoPassword(host.Resolved) (string, bool) { return "", false }

// buildAuthMethods collects file-based key signers plus password methods for an
// interactive login. Passwords are looked up and saved by Credential Key, so an
// entry saved for the addressed name is found here as well as by `exec`.
func (a *interactiveAuth) buildAuthMethods(target host.Resolved) ([]ssh.Signer, []ssh.AuthMethod, error) {
	var extraMethods []ssh.AuthMethod

	// 1-2. Key files: the configured IdentityFile, else the default keys.
	fileSigners, identityFile := keySigners(target, a.cli.loadPrivateKey)
	if identityFile != "" && len(fileSigners) > 0 {
		fmt.Fprintf(a.cli.Err, "Using key: %s\n", identityFile)
	}

	// 3. Check saved passwords. The store is loaded here rather than passed in
	// because loading may prompt for the master password, which should only
	// happen when key auth is not enough.
	if savedPwd, ok := credential.LookupSavedPassword(a.cli.loadSavedPasswords(), target.CredKeys); ok {
		extraMethods = append(extraMethods, ssh.Password(savedPwd))
		fmt.Fprintln(a.cli.Err, "Using saved password")
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
		store, err := a.cli.initPasswordStore()
		if err != nil {
			fmt.Fprintf(a.cli.Err, "Failed to initialize password store: %v\n", err)
		} else if err := store.Set(target.CredKeys[0], password); err != nil {
			fmt.Fprintf(a.cli.Err, "Failed to save password: %v\n", err)
		} else {
			fmt.Fprintln(a.cli.Err, "Password saved")
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
	cli      *CLI
	needSudo bool
	sudoPwd  string
}

// HopAuth collects key signers and, when needed, the sudo password.
func (a *execAuth) HopAuth(hop host.Resolved) (connect.HopCredentials, error) {
	agentKeys, cleanup := agentSigners(a.cli.Err, "exec: using SSH agent keys")

	fileSigners, password, passwordIsAuthMethod := a.buildAuthMethodsNonInteractive(hop)
	allSigners := append(agentKeys, fileSigners...)

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
func (a *execAuth) buildAuthMethodsNonInteractive(target host.Resolved) ([]ssh.Signer, string, bool) {
	// Key files: the configured IdentityFile, else the default keys. This loader
	// skips passphrase-protected keys instead of prompting for one.
	fileSigners, identityFile := keySigners(target, func(path string) (ssh.Signer, error) {
		if signer := tryLoadKey(path); signer != nil {
			return signer, nil
		}
		return nil, fmt.Errorf("not loadable without a passphrase")
	})
	if identityFile != "" && len(fileSigners) > 0 {
		fmt.Fprintln(a.cli.Err, "exec: using identity file")
	}

	hasKeyAuth := len(fileSigners) > 0

	// If sudo is needed or no key auth, try saved passwords (may prompt for master password)
	if a.needSudo || !hasKeyAuth {
		store := a.cli.tryLoadPasswordStoreInteractive()
		if store != nil {
			// First try saved sudo password
			if a.needSudo {
				if pwd, ok := credential.LookupSudoPassword(store, target.CredKeys); ok {
					fmt.Fprintln(a.cli.Err, "exec: using saved sudo password")
					return fileSigners, pwd, !hasKeyAuth
				}
			}
			// Then try saved SSH password
			if pwd, ok := credential.LookupSavedPassword(store, target.CredKeys); ok {
				fmt.Fprintln(a.cli.Err, "exec: using saved password")
				return fileSigners, pwd, !hasKeyAuth
			}
		}
	}

	// Key auth + sudo + no saved passwords → prompt for sudo password
	if a.needSudo && hasKeyAuth {
		if pwd := a.cli.promptSudoPassword(target); pwd != "" {
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
func (c *CLI) promptSudoPassword(target host.Resolved) string {
	hostKey := target.CredKeys[0]
	if !isInteractive() {
		fmt.Fprintf(c.Err, "sudo password required for %s@%s. Use `sshgo config set-sudo-password %s` to set one.\n", target.User, target.Hostname, hostKey)
		return ""
	}
	fmt.Fprintf(c.Err, "sudo password for %s@%s (empty = NOPASSWD): ", target.User, target.Hostname)
	pwdBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(c.Err)
	if err != nil {
		return ""
	}
	pwd := string(pwdBytes)
	if pwd == "" {
		return ""
	}

	// Ask to save
	fmt.Fprintf(c.Err, "Save sudo password? (y/n): ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if strings.ToLower(answer) == "y" {
		store, err := c.initPasswordStore()
		if err != nil {
			fmt.Fprintf(c.Err, "Failed to init password store: %v\n", err)
		} else if err := store.SetSudoPassword(hostKey, pwd); err != nil {
			fmt.Fprintf(c.Err, "Failed to save sudo password: %v\n", err)
		} else {
			fmt.Fprintln(c.Err, "sudo password saved")
		}
	}

	return pwd
}

// isInteractive checks whether stdout is a terminal (TTY).
// Uses stdout instead of stdin so that subshells (e.g. test scripts with $())
// are correctly detected as non-interactive, avoiding hangs on prompts.
func isInteractive() bool {
	return ui.IsStdoutTerminal()
}

// loadKeyWithoutPrompt adapts tryLoadKey to the keySigners loader shape.
func loadKeyWithoutPrompt(path string) (ssh.Signer, error) {
	if signer := tryLoadKey(path); signer != nil {
		return signer, nil
	}
	return nil, fmt.Errorf("not loadable without a passphrase")
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
func (c *CLI) tryLoadPasswordStoreInteractive() *credential.Store {
	path := credential.Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return c.loadSavedPasswords()
}
