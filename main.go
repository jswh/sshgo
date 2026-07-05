package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

type hostInfo struct {
	Name         string
	Source       string
	HasPassword  bool
	Hostname     string
	Port         string
	User         string
	IdentityFile string
}

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
		handleUpdate(args[1:])
	default:
		// Legacy mode: sshgo <host> [command]
		passwords := &PasswordStore{Passwords: make(map[string]string)}
		hosts := buildHostList(passwords)

		var command string
		if len(args) > 1 {
			command = strings.Join(args[1:], " ")
		}

		hostName := args[0]
		user, hostname, port := parseUserHost(hostName)

		// 1. Try local config lookup
		info, found := findHost(hosts, hostName)

		if !found {
			// 2. Try alias resolution
			aliasInfo := resolveAlias(hostName)
			if aliasInfo.Name != "" {
				info = aliasInfo
				found = true
			}
		}

		if !found {
			// 3. Fall through to direct connection
			if hostname == "" {
				fmt.Printf("Host %s not found. Run `sshgo config import` to import from ~/.ssh/config.\n", hostName)
				os.Exit(1)
			}
			info = hostInfo{
				Name:   hostname,
				Source: "direct",
			}
			if user != "" {
				info.Name = user + "@" + hostname
			}
			if port != "" {
				info.Name = info.Name + ":" + port
			}
		}

		connectToHost(info, passwords, command)
	}
}

func printHelp() {
	fmt.Println("Usage: sshgo [command] [args]")
	fmt.Println()
	fmt.Println("SSH client with features:")
	fmt.Println("  - Import from ~/.ssh/config (via `config import`)")
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
	fmt.Println("  sshgo <host> [command]")
	fmt.Println("  sshgo root@host:2222")
	fmt.Println("  sshgo                        # Interactive selection")
	fmt.Println()
	fmt.Println("First run:")
	fmt.Println("  sshgo config import           # Import hosts from ~/.ssh/config")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  sshgo exec db-staging \"SELECT count(*) FROM users\"")
	fmt.Println("  sshgo info --json db-staging")
	fmt.Println("  sshgo config set db-staging tags prod,db")
	fmt.Println("  sshgo zg                        # Login using ssh config")
	fmt.Println("  sshgo zg \"ls -la\"               # Execute command on remote host")
}

func interactiveSelect() {
	passwords := &PasswordStore{Passwords: make(map[string]string)}
	hosts := buildHostList(passwords)

	if len(hosts) == 0 {
		fmt.Println("No hosts found. Run `sshgo config import` to import from ~/.ssh/config.")
		os.Exit(1)
	}

	fmt.Println("=== SSH Hosts ===")
	fmt.Println()

	var items []string
	for i, h := range hosts {
		status := ""
		if h.HasPassword {
			status = " [saved]"
		}
		items = append(items, fmt.Sprintf("%2d. %-20s %-8s%s", i+1, h.Name, h.Source, status))
	}

	prompt := promptui.Select{
		Label: "Select host to connect",
		Items: items,
		Size:  20,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		fmt.Printf("Selection failed: %v\n", err)
		os.Exit(1)
	}

	connectToHost(hosts[idx], passwords, "")
}

func parseUserHost(s string) (user, host, port string) {
	if idx := strings.Index(s, "@"); idx >= 0 {
		user = s[:idx]
		s = s[idx+1:]
	}

	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		host = s[:idx]
		port = s[idx+1:]
	} else {
		host = s
	}

	return user, host, port
}

func initPasswordStore() (*PasswordStore, error) {
	path := getPasswordStorePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("First time setup: Please set a master password for encryption")
		prompt := promptui.Prompt{
			Label: "Set master password",
			Mask:  '*',
		}
		masterPwd, err := prompt.Run()
		if err != nil {
			return nil, fmt.Errorf("failed to set master password: %w", err)
		}
		prompt2 := promptui.Prompt{
			Label: "Confirm master password",
			Mask:  '*',
		}
		confirmPwd, err := prompt2.Run()
		if err != nil {
			return nil, fmt.Errorf("failed to confirm master password: %w", err)
		}
		if masterPwd != confirmPwd {
			return nil, fmt.Errorf("passwords do not match")
		}
		return LoadPasswordStore(masterPwd)
	}

	prompt := promptui.Prompt{
		Label: "Enter master password",
		Mask:  '*',
	}
	masterPwd, err := prompt.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to enter master password: %w", err)
	}
	return LoadPasswordStore(masterPwd)
}

func buildHostList(passwords *PasswordStore) []hostInfo {
	var result []hostInfo

	lc, err := LoadLocalConfig()
	if err != nil {
		return result
	}

	for name, meta := range lc.Hosts {
		source := "manual"
		if meta.Hostname != "" || meta.User != "" || meta.Port != "" || meta.IdentityFile != "" {
			source = "imported"
		}
		result = append(result, hostInfo{
			Name:         name,
			Source:       source,
			Hostname:     meta.Hostname,
			Port:         meta.Port,
			User:         meta.User,
			IdentityFile: meta.IdentityFile,
		})
	}

	return result
}

func findHost(hosts []hostInfo, name string) (hostInfo, bool) {
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return hostInfo{}, false
}

func connectToHost(info hostInfo, passwords *PasswordStore, command string) {
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

	// 1. Collect all key signers (agent + file) for a single publickey method
	var allSigners []ssh.Signer
	var agentConn net.Conn
	if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentConn = conn
		sshAgent := agent.NewClient(conn)
		if signers, err := sshAgent.Signers(); err == nil && len(signers) > 0 {
			allSigners = append(allSigners, signers...)
			fmt.Fprintln(os.Stderr, "Using SSH agent keys")
		}
	}

	// 2. Build non-agent auth methods (file signers + passwords)
	fileSigners, extraMethods, err := buildAuthMethods(hostname, user, identityFile, passwords)
	if err != nil {
		if agentConn != nil {
			agentConn.Close()
		}
		fmt.Printf("Authentication failed: %v\n", err)
		os.Exit(1)
	}
	allSigners = append(allSigners, fileSigners...)

	// Combine all public key signers into ONE method
	var methods []ssh.AuthMethod
	if len(allSigners) > 0 {
		methods = append(methods, ssh.PublicKeys(allSigners...))
	}
	methods = append(methods, extraMethods...)

	clientConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", hostname+":"+port, clientConfig)
	if agentConn != nil {
		agentConn.Close() // safe to close after auth completes
	}
	if err != nil {
		fmt.Printf("Connection failed: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		fmt.Printf("Failed to create session: %v\n", err)
		os.Exit(1)
	}
	defer session.Close()

	if command == "" {
		// Interactive shell - request PTY
		modes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}

		fd := int(os.Stdin.Fd())
		width, height, _ := term.GetSize(fd)
		if width == 0 {
			width = 80
		}
		if height == 0 {
			height = 24
		}

		if err := session.RequestPty("xterm-256color", height, width, modes); err != nil {
			fmt.Printf("Failed to request PTY: %v\n", err)
			os.Exit(1)
		}
	}

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if command != "" {
		// Execute remote command
		if err := session.Run(command); err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				os.Exit(exitErr.ExitStatus())
			}
			os.Exit(1)
		}
	} else {
		// Start interactive shell
		if err := session.Shell(); err != nil {
			fmt.Printf("Failed to start shell: %v\n", err)
			os.Exit(1)
		}

		if err := session.Wait(); err != nil {
			os.Exit(0)
		}
	}
}

func buildAuthMethods(hostName, user, identityFile string, passwords *PasswordStore) ([]ssh.Signer, []ssh.AuthMethod, error) {
	var fileSigners []ssh.Signer
	var extraMethods []ssh.AuthMethod

	// 1. Try configured IdentityFile
	if identityFile != "" {
		identityFile = expandPath(identityFile)
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

	// 3. Check saved passwords
	savedPasswords := loadSavedPasswords()
	if savedPasswords.Has(hostName) {
		savedPwd, err := savedPasswords.Get(hostName)
		if err == nil {
			extraMethods = append(extraMethods, ssh.Password(savedPwd))
			fmt.Fprintln(os.Stderr, "Using saved password")
			return fileSigners, extraMethods, nil
		}
	}

	// 4. Return if we have any auth methods
	if len(fileSigners) > 0 || len(extraMethods) > 0 {
		return fileSigners, extraMethods, nil
	}

	// 5. Prompt for password
	prompt := promptui.Prompt{
		Label: fmt.Sprintf("Enter password for %s@%s", user, hostName),
		Mask:  '*',
	}

	password, err := prompt.Run()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to enter password: %w", err)
	}

	extraMethods = append(extraMethods, ssh.Password(password))

	// Ask to save password
	savePrompt := promptui.Prompt{
		Label:     "Save password? (y/n)",
		Default:   "n",
		AllowEdit: true,
	}
	saveAnswer, _ := savePrompt.Run()
	if strings.ToLower(strings.TrimSpace(saveAnswer)) == "y" {
		store, err := initPasswordStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize password store: %v\n", err)
		} else if err := store.Set(hostName, password); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to save password: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "Password saved")
		}
	}

	return fileSigners, extraMethods, nil
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
	passphrasePrompt := promptui.Prompt{
		Label: fmt.Sprintf("Enter passphrase for %s", path),
		Mask:  '*',
	}
	passphrase, _ := passphrasePrompt.Run()
	if passphrase == "" {
		return nil, fmt.Errorf("passphrase required")
	}

	return ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
}

func loadSavedPasswords() *PasswordStore {
	path := getPasswordStorePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &PasswordStore{Passwords: make(map[string]string)}
	}
	prompt := promptui.Prompt{
		Label: "Enter master password",
		Mask:  '*',
	}
	masterPwd, err := prompt.Run()
	if err != nil {
		return &PasswordStore{Passwords: make(map[string]string)}
	}
	store, err := LoadPasswordStore(masterPwd)
	if err != nil {
		fmt.Printf("Failed to load password store: %v\n", err)
		return &PasswordStore{Passwords: make(map[string]string)}
	}
	return store
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[1:])
	}
	return path
}
