package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// execFlags holds parsed flags for the exec subcommand.
type execFlags struct {
	Raw  bool
	Sudo bool
}

func handleExec(args []string) {
	// Check for help before parsing flags
	if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: sshgo exec [--raw] [--sudo] <host> <command>")
		fmt.Println()
		fmt.Println("Execute a remote command with structured JSON output.")
		fmt.Println()
		fmt.Println("Flags:")
		fmt.Println("  --raw     Output raw stdout/stderr stream (no JSON wrapper)")
		fmt.Println("  --sudo    Execute command with sudo (reuses SSH password)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  sshgo exec db-staging \"SELECT count(*) FROM users\"")
		fmt.Println("  sshgo exec --raw web-01 \"tail -n 100 /var/log/nginx/access.log\"")
		fmt.Println("  sshgo exec --sudo db-staging \"systemctl restart postgresql\"")
		return
	}

	flags := parseExecFlags(&args)

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Error: missing host or command")
		fmt.Fprintln(os.Stderr, "Usage: sshgo exec [--raw] [--sudo] <host> <command>")
		os.Exit(1)
	}

	hostName := args[0]
	command := strings.Join(args[1:], " ")

	passwords := &PasswordStore{Passwords: make(map[string]string)}
	hosts := buildHostList(passwords)

	_, hostname, _ := parseUserHost(hostName)

	info, found := findHost(hosts, hostName)

	if !found {
		// Try alias resolution
		aliasInfo := resolveAlias(hostName)
		if aliasInfo.Name != "" {
			info = aliasInfo
			found = true
		}
	}

	if !found {
		if hostname == "" {
			fmt.Fprintf(os.Stderr, "Host %q not found. Run `sshgo config import` to import from ~/.ssh/config.\n", hostName)
			os.Exit(1)
		}
		info = hostInfo{
			Name:   hostName,
			Source: "direct",
		}
	}

	start := time.Now()
	result := executeRemote(info, command, flags)
	result.Duration = time.Since(start).Round(time.Millisecond).String()

	if flags.Raw {
		os.Stdout.Write([]byte(result.Stdout))
		os.Stderr.Write([]byte(result.Stderr))
		os.Exit(result.ExitCode)
	}

	fmt.Println(result.JSON())
	os.Exit(result.ExitCode)
}

// resolveAlias looks up a host by alias in local config.
func resolveAlias(name string) hostInfo {
	lc, err := LoadLocalConfig()
	if err != nil {
		return hostInfo{}
	}
	for host, meta := range lc.Hosts {
		if meta.Alias == name {
			info := hostInfo{
				Name:         host,
				Source:       "imported",
				Hostname:     meta.Hostname,
				Port:         meta.Port,
				User:         meta.User,
				IdentityFile: meta.IdentityFile,
			}
			if strings.Contains(host, "@") || strings.Contains(host, ":") {
				info.Source = "direct"
			}
			return info
		}
	}
	return hostInfo{}
}

func parseExecFlags(args *[]string) execFlags {
	var flags execFlags
	var filtered []string

	for _, a := range *args {
		switch {
		case a == "--raw":
			flags.Raw = true
		case a == "--sudo":
			flags.Sudo = true
		case strings.HasPrefix(a, "--"):
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", a)
			os.Exit(1)
		default:
			filtered = append(filtered, a)
		}
	}

	*args = filtered
	return flags
}

// executeRemote dials the host, runs a command, and returns structured output.
func executeRemote(info hostInfo, command string, flags execFlags) ExecResult {
	hostname := info.Hostname
	port := info.Port
	user := info.User
	identityFile := info.IdentityFile

	// For direct addresses, parse from the name string
	if info.Source == "direct" {
		name := info.Name
		if idx := strings.Index(name, "@"); idx >= 0 {
			user = name[:idx]
			name = name[idx+1:]
		}
		if idx := strings.LastIndex(name, ":"); idx >= 0 {
			hostname = name[:idx]
			port = name[idx+1:]
		} else {
			hostname = name
		}
	}

	if hostname == "" {
		hostname = info.Name
	}
	if port == "" {
		port = "22"
	}
	if user == "" {
		user = "root"
	}

	// 1. Agent: keep connection alive during auth
	var agentConn net.Conn
	var methods []ssh.AuthMethod
	if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentConn = conn
		sshAgent := agent.NewClient(conn)
		if signers, err := sshAgent.Signers(); err == nil && len(signers) > 0 {
			methods = append(methods, ssh.PublicKeys(signers...))
			fmt.Fprintln(os.Stderr, "exec: using SSH agent keys")
		}
	}

	// 2. Build non-agent auth methods
	passwordForSudo := buildAuthMethodsNonInteractive(hostname, user, identityFile, flags.Sudo, &methods)
	if len(methods) == 0 {
		if agentConn != nil {
			agentConn.Close()
		}
		return ExecResult{
			Command:  command,
			Stderr:   "No authentication method available (no SSH agent, keys, or saved passwords)",
			ExitCode: 1,
		}
	}

	clientConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	client, err := ssh.Dial("tcp", hostname+":"+port, clientConfig)
	if agentConn != nil {
		agentConn.Close()
	}
	if err != nil {
		return ExecResult{
			Command:  command,
			Stderr:   fmt.Sprintf("Connection failed: %v", err),
			ExitCode: 1,
		}
	}
	defer client.Close()

	if flags.Sudo {
		if passwordForSudo != "" {
			command = "sudo -S -p '' " + command
		} else {
			command = "sudo " + command
		}
	}

	session, err := client.NewSession()
	if err != nil {
		return ExecResult{
			Command:  command,
			Stderr:   fmt.Sprintf("Session creation failed: %v", err),
			ExitCode: 1,
		}
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if flags.Sudo && passwordForSudo != "" {
		session.Stdin = strings.NewReader(passwordForSudo + "\n")
	}

	err = session.Run(command)
	exitCode := 0
	if exitErr, ok := err.(*ssh.ExitError); ok {
		exitCode = exitErr.ExitStatus()
	} else if err != nil {
		stderrBuf.WriteString(fmt.Sprintf("\nSession error: %v", err))
		exitCode = 1
	}

	return ExecResult{
		Command:  command,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}
}

// buildAuthMethodsNonInteractive adds non-agent auth methods to the given slice.
// Returns the sudo password if available.
// Priority for sudo password: saved sudo password > saved SSH password > interactive prompt.
func buildAuthMethodsNonInteractive(hostName, user, identityFile string, needSudo bool, methods *[]ssh.AuthMethod) string {
	// Try configured IdentityFile (skip passphrase-protected keys non-interactively)
	if identityFile != "" {
		identityFile = expandPath(identityFile)
		if signer := tryLoadKey(identityFile); signer != nil {
			*methods = append(*methods, ssh.PublicKeys(signer))
			fmt.Fprintln(os.Stderr, "exec: using identity file")
		}
	}

	// Try default keys
	if identityFile == "" {
		homeDir, _ := os.UserHomeDir()
		for _, keyName := range []string{"id_rsa", "id_ed25519", "id_ecdsa"} {
			keyPath := filepath.Join(homeDir, ".ssh", keyName)
			if signer := tryLoadKey(keyPath); signer != nil {
				*methods = append(*methods, ssh.PublicKeys(signer))
				break
			}
		}
	}

	hasKeyAuth := len(*methods) > 0

	// If sudo is needed or no key auth, try saved passwords (may prompt for master password)
	if needSudo || !hasKeyAuth {
		store := tryLoadPasswordStoreInteractive()
		if store != nil {
			// First try saved sudo password
			if needSudo && store.HasSudoPassword(hostName) {
				if pwd, err := store.GetSudoPassword(hostName); err == nil {
					if !hasKeyAuth {
						*methods = append(*methods, ssh.Password(pwd))
					}
					fmt.Fprintln(os.Stderr, "exec: using saved sudo password")
					return pwd
				}
			}
			// Then try saved SSH password
			if store.Has(hostName) {
				if pwd, err := store.Get(hostName); err == nil {
					if !hasKeyAuth {
						*methods = append(*methods, ssh.Password(pwd))
					}
					fmt.Fprintln(os.Stderr, "exec: using saved password")
					return pwd
				}
			}
		}
	}

	// Key auth + sudo + no saved passwords → prompt for sudo password
	if needSudo && hasKeyAuth {
		if pwd := promptSudoPassword(hostName, user); pwd != "" {
			return pwd
		}
	}

	return ""
}

// promptSudoPassword interactively prompts for a sudo password with option to save.
// Uses stderr for prompts and term.ReadPassword to avoid corrupting stdout.
// In non-interactive terminals, prints a hint and returns "".
func promptSudoPassword(hostName, user string) string {
	if !isInteractive() {
		fmt.Fprintf(os.Stderr, "sudo password required for %s@%s. Use `sshgo config set-sudo-password %s` to set one.\n", user, hostName, hostName)
		return ""
	}
	fmt.Fprintf(os.Stderr, "sudo password for %s@%s (empty = NOPASSWD): ", user, hostName)
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
		} else if err := store.SetSudoPassword(hostName, pwd); err != nil {
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
func tryLoadPasswordStoreInteractive() *PasswordStore {
	path := getPasswordStorePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return loadSavedPasswords()
}
