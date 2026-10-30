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

// passwordStore is the surface the authentication strategies use. They depend on
// this rather than on *credential.Store, so per-hop lookup and the single unlock
// can be tested without a master password.
type passwordStore interface {
	Get(key string) (string, error)
	GetSudoPassword(key string) (string, error)
	Set(key, password string) error
	SetSudoPassword(key, password string) error
}

// lookupSavedPassword returns the first saved SSH password among keys.
func lookupSavedPassword(store passwordStore, keys []string) (string, bool) {
	if store == nil {
		return "", false
	}
	return credential.LookupPassword(keys, store.Get)
}

// lookupSudoPassword returns the first saved sudo password among keys.
func lookupSudoPassword(store passwordStore, keys []string) (string, bool) {
	if store == nil {
		return "", false
	}
	return credential.LookupPassword(keys, store.GetSudoPassword)
}

// interactiveAuth is the authentication strategy for the interactive login
// path: it may prompt for a passphrase, a password, and whether to save it.
type interactiveAuth struct {
	cli *CLI
}

// HopAuth collects agent keys, key files, and a saved or prompted password for
// one hop. Every hop is asked separately, so a bastion prompts for its own
// password rather than borrowing the target's.
func (a *interactiveAuth) HopAuth(hop host.Resolved) (connect.HopCredentials, error) {
	// 1. Agent keys, shared as one publickey method. The agent belongs to the
	// user, not to a host, so it is offered on every hop.
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
func keySigners(hop host.Resolved, load func(string) (ssh.Signer, error)) ([]ssh.Signer, string) {
	if hop.IdentityFile == "" {
		var signers []ssh.Signer
		for _, keyPath := range defaultKeyPaths() {
			if signer, err := load(keyPath); err == nil {
				signers = append(signers, signer)
			}
		}
		return signers, ""
	}

	path := host.ExpandPath(hop.IdentityFile)
	signer, err := load(path)
	if err != nil {
		return nil, path
	}
	return []ssh.Signer{signer}, path
}

// SudoPassword is unused on the interactive path: sudo passwords only exist for
// `exec --sudo`.
func (a *interactiveAuth) SudoPassword(host.Resolved) (string, bool) { return "", false }

// buildAuthMethods collects file-based key signers plus password methods for one
// hop. Passwords are looked up and saved by Credential Key, so an entry saved for
// the addressed name is found here as well as by `exec`.
func (a *interactiveAuth) buildAuthMethods(hop host.Resolved) ([]ssh.Signer, []ssh.AuthMethod, error) {
	var extraMethods []ssh.AuthMethod

	// 1-2. Key files: the configured IdentityFile, else the default keys.
	fileSigners, identityFile := keySigners(hop, a.cli.loadPrivateKey)
	if identityFile != "" && len(fileSigners) > 0 {
		fmt.Fprintf(a.cli.Err, "Using key: %s\n", identityFile)
	}

	// 3. Check saved passwords for this hop.
	if savedPwd, ok := lookupSavedPassword(a.cli.openStore(), hop.CredKeys); ok {
		extraMethods = append(extraMethods, ssh.Password(savedPwd))
		fmt.Fprintln(a.cli.Err, "Using saved password")
		return fileSigners, extraMethods, nil
	}

	// 4. Return if we have any auth methods
	if len(fileSigners) > 0 || len(extraMethods) > 0 {
		return fileSigners, extraMethods, nil
	}

	// 5. Prompt for this hop's password
	password, err := ui.Password(fmt.Sprintf("Enter password for %s@%s", hop.User, hop.Hostname))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to enter password: %w", err)
	}

	extraMethods = append(extraMethods, ssh.Password(password))

	// Ask to save password
	if ui.YesNo("Save password?", "n") {
		store, err := a.cli.saveStore()
		if err != nil {
			fmt.Fprintf(a.cli.Err, "Failed to initialize password store: %v\n", err)
		} else if err := store.Set(hop.CredKeys[0], password); err != nil {
			fmt.Fprintf(a.cli.Err, "Failed to save password: %v\n", err)
		} else {
			fmt.Fprintln(a.cli.Err, "Password saved")
		}
	}

	return fileSigners, extraMethods, nil
}

// execAuth is the non-interactive strategy used by `exec`: it never asks for an
// SSH password, on any hop. Only the sudo password may be prompted for on a TTY,
// and only for the target.
type execAuth struct {
	cli      *CLI
	needSudo bool
}

// HopAuth collects this hop's key signers and its own saved password. The two
// are independent: a key on one hop never suppresses another hop's password
// lookup, which is what made "agent keys plus a password-only bastion" fail.
func (a *execAuth) HopAuth(hop host.Resolved) (connect.HopCredentials, error) {
	agentKeys, cleanup := agentSigners(a.cli.Err, "exec: using SSH agent keys")

	fileSigners, identityFile := keySigners(hop, loadKeyWithoutPrompt)
	if identityFile != "" && len(fileSigners) > 0 {
		fmt.Fprintln(a.cli.Err, "exec: using identity file")
	}

	allSigners := append(agentKeys, fileSigners...)

	var methods []ssh.AuthMethod
	if len(allSigners) > 0 {
		methods = append(methods, ssh.PublicKeys(allSigners...))
	}

	// This hop's own saved password, regardless of whether it also has keys.
	if pwd, err := a.savedPassword(hop); err == nil {
		methods = append(methods, ssh.Password(pwd))
		fmt.Fprintln(a.cli.Err, "exec: using saved password")
	}

	return connect.HopCredentials{Methods: methods, Cleanup: cleanup}, nil
}

// savedPassword returns the SSH password saved for hop, if any. It is never the
// sudo password: that one only ever reaches the target session's stdin.
func (a *execAuth) savedPassword(hop host.Resolved) (string, error) {
	store := a.cli.openStore()
	if store == nil {
		return "", fmt.Errorf("no password store")
	}
	if pwd, ok := lookupSavedPassword(store, hop.CredKeys); ok {
		return pwd, nil
	}
	return "", fmt.Errorf("password not found")
}

// SudoPassword resolves the sudo password for the target session, after the
// chain is up. Sudo Password Priority: saved sudo password, then the saved SSH
// password, then an interactive prompt, then NOPASSWD.
func (a *execAuth) SudoPassword(target host.Resolved) (string, bool) {
	if !a.needSudo {
		return "", false
	}

	if store := a.cli.openStore(); store != nil {
		if pwd, ok := lookupSudoPassword(store, target.CredKeys); ok {
			fmt.Fprintln(a.cli.Err, "exec: using saved sudo password")
			return pwd, true
		}
		if pwd, ok := lookupSavedPassword(store, target.CredKeys); ok {
			return pwd, true
		}
	}

	if pwd := a.cli.promptSudoPassword(target); pwd != "" {
		return pwd, true
	}
	return "", false
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
		store, err := c.saveStore()
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
