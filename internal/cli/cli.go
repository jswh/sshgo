// Package cli holds sshgo's user-visible surface: subcommand dispatch, help
// text, exit codes, interactive selection, and the two authentication
// strategies injected into internal/connect.
//
// The entry point is Run, which returns an exit code instead of calling
// os.Exit, so error text and exit codes can be asserted in-process.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"sshgo/internal/connect"
	"sshgo/internal/credential"
	"sshgo/internal/host"
	"sshgo/internal/ui"
	"sshgo/internal/update"
)

// CLI is one invocation: the injected streams plus the build version.
type CLI struct {
	// Version is injected by the root package so `update` can compare it.
	Version string
	Out     io.Writer
	Err     io.Writer
}

// Run dispatches a subcommand and returns the process exit code.
func Run(version string, args []string, stdout, stderr io.Writer) int {
	c := &CLI{Version: version, Out: stdout, Err: stderr}
	return c.Run(args)
}

// Run dispatches on the first argument. Subcommand names win over Host names:
// that is deliberate, so a Host named "exec" is reachable only by Alias.
func (c *CLI) Run(args []string) int {
	if len(args) == 0 {
		return c.interactiveSelect()
	}

	// Check for help flags before subcommand dispatch
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		c.printHelp()
		return 0
	}

	switch args[0] {
	case "exec":
		return c.runExec(args[1:])
	case "info":
		return c.runInfo(args[1:])
	case "config":
		return c.runConfig(args[1:])
	case "update":
		return update.Run(c.Version, args[1:], c.Out, c.Err)
	}

	// Legacy mode: sshgo <host> [command]
	var command string
	if len(args) > 1 {
		command = strings.Join(args[1:], " ")
	}

	target, err := host.Resolve(args[0])
	if err != nil {
		return c.resolveError(err)
	}
	return c.connectToHost(target, command)
}

// resolveError reports a resolution failure. Every entry point shares this so
// the three of them cannot drift apart again.
func (c *CLI) resolveError(err error) int {
	fmt.Fprintf(c.Err, "Error: %v\n", err)
	return 1
}

func (c *CLI) printHelp() {
	fmt.Fprintln(c.Out, "Usage: sshgo [command] [args]")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "SSH client with features:")
	fmt.Fprintln(c.Out, "  - Imports ~/.ssh/config once (via `config import`), then owns its host list")
	fmt.Fprintln(c.Out, "  - Never reads ~/.ssh/config at runtime")
	fmt.Fprintln(c.Out, "  - SSH key authentication")
	fmt.Fprintln(c.Out, "  - Encrypted password storage")
	fmt.Fprintln(c.Out, "  - Interactive host selection")
	fmt.Fprintln(c.Out, "  - Execute remote commands")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "Subcommands:")
	fmt.Fprintln(c.Out, "  exec    Execute a remote command with structured output")
	fmt.Fprintln(c.Out, "  info    Show connection information for a host")
	fmt.Fprintln(c.Out, "  config  Manage local host metadata and import SSH config")
	fmt.Fprintln(c.Out, "  update  Update sshgo to the latest version from GitHub")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "Legacy usage:")
	fmt.Fprintln(c.Out, "  sshgo <host> [command]        # <host> must be in the local host list")
	fmt.Fprintln(c.Out, "  sshgo user@host[:port]        # Direct Address, always dialed as given")
	fmt.Fprintln(c.Out, "  sshgo                         # Interactive selection")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "First run:")
	fmt.Fprintln(c.Out, "  sshgo config import           # Import hosts from ~/.ssh/config")
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, "Examples:")
	fmt.Fprintln(c.Out, "  sshgo exec db-staging \"SELECT count(*) FROM users\"")
	fmt.Fprintln(c.Out, "  sshgo info --json db-staging")
	fmt.Fprintln(c.Out, "  sshgo config set db-staging tags prod,db")
	fmt.Fprintln(c.Out, "  sshgo zg                         # Login to a host from the local list")
	fmt.Fprintln(c.Out, "  sshgo root@1.2.3.4:2222          # Direct Address")
	fmt.Fprintln(c.Out, "  sshgo zg \"ls -la\"                # Execute command on remote host")
}

// hostEntry is one row of the interactive host list.
type hostEntry struct {
	Name   string
	Source string
}

func (c *CLI) interactiveSelect() int {
	hosts := buildHostList()

	if len(hosts) == 0 {
		fmt.Fprintln(c.Out, "No hosts found. Run `sshgo config import` to import from ~/.ssh/config.")
		return 1
	}

	fmt.Fprintln(c.Out, "=== SSH Hosts ===")
	fmt.Fprintln(c.Out)

	var items []string
	for i, h := range hosts {
		items = append(items, fmt.Sprintf("%2d. %-20s %-8s", i+1, h.Name, h.Source))
	}

	idx, _, err := ui.Select("Select host to connect", items)
	if err != nil {
		fmt.Fprintf(c.Err, "Error: Selection failed: %v\n", err)
		return 1
	}

	target, err := host.Resolve(hosts[idx].Name)
	if err != nil {
		return c.resolveError(err)
	}
	return c.connectToHost(target, "")
}

func buildHostList() []hostEntry {
	var result []hostEntry

	lc, err := host.LoadLocalConfig()
	if err != nil {
		return result
	}

	for name, meta := range lc.Hosts {
		result = append(result, hostEntry{Name: name, Source: meta.Source})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// connectToHost runs one interactive login: dial, then a shell or one command.
func (c *CLI) connectToHost(target host.Resolved, command string) int {
	auth := &interactiveAuth{cli: c}

	client, err := connect.Dial(target, auth, connect.Options{})
	if err != nil {
		fmt.Fprintf(c.Err, "Error: %s: %v\n", dialFailureLabel(err), connect.Cause(err))
		return 1
	}
	defer client.Close()

	session, err := connect.NewSession(client)
	if err != nil {
		fmt.Fprintf(c.Err, "Error: Failed to create session: %v\n", connect.Cause(err))
		return 1
	}

	width, height := terminalSize()
	code, err := connect.RunInteractive(session, command, connect.Streams{
		In:  os.Stdin,
		Out: c.Out,
		Err: c.Err,
	}, width, height)
	if err != nil {
		fmt.Fprintf(c.Err, "Error: %s: %v\n", sessionFailureLabel(err), connect.Cause(err))
	}
	return code
}

// dialFailureLabel names the phase a dial failure belongs to. Authentication
// and connection failures are distinct: they point at different fixes.
func dialFailureLabel(err error) string {
	var authErr *connect.AuthError
	if errors.As(err, &authErr) || errors.Is(err, connect.ErrNoAuthMethod) {
		return "Authentication failed"
	}
	return "Connection failed"
}

// sessionFailureLabel names the session step that failed.
func sessionFailureLabel(err error) string {
	var sessionErr *connect.Error
	if errors.As(err, &sessionErr) {
		switch sessionErr.Op {
		case connect.OpPTY:
			return "Failed to request PTY"
		case connect.OpShell:
			return "Failed to start shell"
		}
	}
	return "Session failed"
}

// terminalSize reports the local terminal size, defaulting to 80x24 when it
// cannot be determined (not a TTY, or a platform without a size query).
func terminalSize() (width, height int) {
	width, height, _ = term.GetSize(int(os.Stdin.Fd()))
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	return width, height
}

func (c *CLI) initPasswordStore() (*credential.Store, error) {
	path := credential.Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintln(c.Out, "First time setup: Please set a master password for encryption")
		masterPwd, err := ui.Password("Set master password")
		if err != nil {
			return nil, fmt.Errorf("failed to set master password: %w", err)
		}
		confirmPwd, err := ui.Password("Confirm master password")
		if err != nil {
			return nil, fmt.Errorf("failed to confirm master password: %w", err)
		}
		if masterPwd != confirmPwd {
			return nil, fmt.Errorf("passwords do not match")
		}
		return credential.Load(masterPwd)
	}

	masterPwd, err := ui.Password("Enter master password")
	if err != nil {
		return nil, fmt.Errorf("failed to enter master password: %w", err)
	}
	return credential.Load(masterPwd)
}

func (c *CLI) loadPrivateKey(path string) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try without passphrase
	if signer, err := ssh.ParsePrivateKey(key); err == nil {
		return signer, nil
	}

	// Try with passphrase
	passphrase, _ := ui.Password(fmt.Sprintf("Enter passphrase for %s", path))
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase required")
	}

	return ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
}

func (c *CLI) loadSavedPasswords() *credential.Store {
	path := credential.Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &credential.Store{Passwords: make(map[string]string)}
	}
	masterPwd, err := ui.Password("Enter master password")
	if err != nil {
		return &credential.Store{Passwords: make(map[string]string)}
	}
	store, err := credential.Load(masterPwd)
	if err != nil {
		fmt.Fprintf(c.Err, "Error: Failed to load password store: %v\n", err)
		return &credential.Store{Passwords: make(map[string]string)}
	}
	return store
}

// expandIdentityFile is host.ExpandPath, kept local so auth.go stays readable.
func expandIdentityFile(path string) string { return host.ExpandPath(path) }

// defaultKeyNames are the keys sshgo falls back to when no IdentityFile is set.
var defaultKeyNames = []string{"id_rsa", "id_ed25519", "id_ecdsa"}

// defaultKeyPaths lists the candidate default keys under the user's ~/.ssh.
func defaultKeyPaths() []string {
	homeDir, _ := os.UserHomeDir()
	paths := make([]string, 0, len(defaultKeyNames))
	for _, name := range defaultKeyNames {
		paths = append(paths, filepath.Join(homeDir, ".ssh", name))
	}
	return paths
}
