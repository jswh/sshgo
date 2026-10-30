package main

import (
	"errors"
	"fmt"
	"os"
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

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		interactiveSelect()
		return
	}

	// Check for help flags before subcommand dispatch
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		printHelp()
		os.Exit(0)
	}

	// Subcommand dispatch
	switch args[0] {
	case "exec":
		handleExec(args[1:])
	case "info":
		handleInfo(args[1:])
	case "config":
		handleConfig(args[1:])
	case "update":
		os.Exit(update.Run(version, args[1:], os.Stdout, os.Stderr))
	default:
		// Legacy mode: sshgo <host> [command]
		var command string
		if len(args) > 1 {
			command = strings.Join(args[1:], " ")
		}

		target, err := host.Resolve(args[0])
		if err != nil {
			exitResolveError(err)
		}

		connectToHost(target, command)
	}
}

func printHelp() {
	fmt.Println("Usage: sshgo [command] [args]")
	fmt.Println()
	fmt.Println("SSH client with features:")
	fmt.Println("  - Imports ~/.ssh/config once (via `config import`), then owns its host list")
	fmt.Println("  - Never reads ~/.ssh/config at runtime")
	fmt.Println("  - SSH key authentication")
	fmt.Println("  - Encrypted password storage")
	fmt.Println("  - Interactive host selection")
	fmt.Println("  - Execute remote commands")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  exec    Execute a remote command with structured output")
	fmt.Println("  info    Show connection information for a host")
	fmt.Println("  config  Manage local host metadata and import SSH config")
	fmt.Println("  update  Update sshgo to the latest version from GitHub")
	fmt.Println()
	fmt.Println("Legacy usage:")
	fmt.Println("  sshgo <host> [command]        # <host> must be in the local host list")
	fmt.Println("  sshgo user@host[:port]        # Direct Address, always dialed as given")
	fmt.Println("  sshgo                         # Interactive selection")
	fmt.Println()
	fmt.Println("First run:")
	fmt.Println("  sshgo config import           # Import hosts from ~/.ssh/config")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  sshgo exec db-staging \"SELECT count(*) FROM users\"")
	fmt.Println("  sshgo info --json db-staging")
	fmt.Println("  sshgo config set db-staging tags prod,db")
	fmt.Println("  sshgo zg                         # Login to a host from the local list")
	fmt.Println("  sshgo root@1.2.3.4:2222          # Direct Address")
	fmt.Println("  sshgo zg \"ls -la\"                # Execute command on remote host")
}

func interactiveSelect() {
	hosts := buildHostList()

	if len(hosts) == 0 {
		fmt.Println("No hosts found. Run `sshgo config import` to import from ~/.ssh/config.")
		os.Exit(1)
	}

	fmt.Println("=== SSH Hosts ===")
	fmt.Println()

	var items []string
	for i, h := range hosts {
		items = append(items, fmt.Sprintf("%2d. %-20s %-8s", i+1, h.Name, h.Source))
	}

	idx, _, err := ui.Select("Select host to connect", items)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Selection failed: %v\n", err)
		os.Exit(1)
	}

	target, err := host.Resolve(hosts[idx].Name)
	if err != nil {
		exitResolveError(err)
	}
	connectToHost(target, "")
}

// exitResolveError reports a resolution failure on stderr and exits 1. Every
// entry point shares this so the three of them cannot drift apart again.
func exitResolveError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func initPasswordStore() (*credential.Store, error) {
	path := credential.Path()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("First time setup: Please set a master password for encryption")
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

// hostEntry is one row of the interactive host list.
type hostEntry struct {
	Name   string
	Source string
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

func connectToHost(target host.Resolved, command string) {
	client, err := connect.Dial(target, interactiveAuth{}, connect.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", dialFailureLabel(err), connect.Cause(err))
		os.Exit(1)
	}
	defer client.Close()

	session, err := connect.NewSession(client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to create session: %v\n", connect.Cause(err))
		os.Exit(1)
	}

	width, height := terminalSize()
	code, err := connect.RunInteractive(session, command, connect.Streams{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	}, width, height)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", sessionFailureLabel(err), connect.Cause(err))
	}
	os.Exit(code)
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

func loadPrivateKey(path string) (ssh.Signer, error) {
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

func loadSavedPasswords() *credential.Store {
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
		fmt.Fprintf(os.Stderr, "Error: Failed to load password store: %v\n", err)
		return &credential.Store{Passwords: make(map[string]string)}
	}
	return store
}
