package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevinburke/ssh_config"
	"github.com/manifoldco/promptui"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

type hostInfo struct {
	Name        string
	Source      string
	HasPassword bool
}

func main() {
	args := os.Args[1:]

	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: sshgo [host|user@host:port]")
		fmt.Println()
		fmt.Println("SSH client with features:")
		fmt.Println("  - Read ~/.ssh/config")
		fmt.Println("  - SSH key authentication")
		fmt.Println("  - Encrypted password storage")
		fmt.Println("  - Interactive host selection")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  sshgo zg              # Login using ssh config")
		fmt.Println("  sshgo root@1.2.3.4    # Direct login")
		fmt.Println("  sshgo root@host:2222  # Login with port")
		fmt.Println("  sshgo                 # Interactive selection")
		os.Exit(0)
	}

	sshConfig, err := loadSSHConfig()
	if err != nil {
		fmt.Printf("Failed to read SSH config: %v\n", err)
		os.Exit(1)
	}

	passwords := &PasswordStore{Passwords: make(map[string]string)}

	hosts := buildHostList(sshConfig, passwords)

	if len(args) > 0 {
		hostName := args[0]

		user, hostname, port := parseUserHost(hostName)

		info, found := findHost(hosts, hostName)
		if !found && hostname != "" {
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
		} else if !found {
			fmt.Printf("Host %s not found in SSH config\n", hostName)
			os.Exit(1)
		}

		connectToHost(info, sshConfig, passwords)
		return
	}

	if len(hosts) == 0 {
		fmt.Println("No SSH hosts found")
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

	connectToHost(hosts[idx], sshConfig, passwords)
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

func loadSSHConfig() (*ssh_config.Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(homeDir, ".ssh", "config")
	f, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ssh_config.Decode(f)
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

func buildHostList(cfg *ssh_config.Config, passwords *PasswordStore) []hostInfo {
	seen := make(map[string]bool)
	var result []hostInfo

	for _, host := range cfg.Hosts {
		for _, pattern := range host.Patterns {
			name := strings.TrimSpace(pattern.String())
			if name == "" || name == "*" {
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			result = append(result, hostInfo{
				Name:   name,
				Source: "config",
			})
		}
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

func connectToHost(info hostInfo, cfg *ssh_config.Config, passwords *PasswordStore) {
	var hostname, port, user, identityFile string

	if info.Source == "config" {
		hostname, _ = cfg.Get(info.Name, "Hostname")
		port, _ = cfg.Get(info.Name, "Port")
		user, _ = cfg.Get(info.Name, "User")
		identityFile, _ = cfg.Get(info.Name, "IdentityFile")
	} else if info.Source == "direct" {
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
	} else {
		hostname = info.Name
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

	authMethods, err := buildAuthMethods(hostname, user, identityFile, passwords)
	if err != nil {
		fmt.Printf("Authentication failed: %v\n", err)
		os.Exit(1)
	}

	clientConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", hostname+":"+port, clientConfig)
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

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		fmt.Printf("Failed to start shell: %v\n", err)
		os.Exit(1)
	}

	if err := session.Wait(); err != nil {
		os.Exit(0)
	}
}

func buildAuthMethods(hostName, user, identityFile string, passwords *PasswordStore) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// 1. Try SSH agent
	if sshAgentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		sshAgent := agent.NewClient(sshAgentConn)
		if keys, err := sshAgent.List(); err == nil && len(keys) > 0 {
			methods = append(methods, ssh.PublicKeysCallback(sshAgent.Signers))
			fmt.Println("Using SSH agent keys")
		}
		sshAgentConn.Close()
	}

	// 2. Try configured IdentityFile
	if identityFile != "" {
		identityFile = expandPath(identityFile)
		if signer, err := loadPrivateKey(identityFile); err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
			fmt.Printf("Using key: %s\n", identityFile)
		}
	}

	// 3. Try default keys
	if identityFile == "" {
		homeDir, _ := os.UserHomeDir()
		defaultKeys := []string{"id_rsa", "id_ed25519", "id_ecdsa"}
		for _, keyName := range defaultKeys {
			keyPath := filepath.Join(homeDir, ".ssh", keyName)
			if signer, err := loadPrivateKey(keyPath); err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}

	// 4. Check saved passwords
	savedPasswords := loadSavedPasswords()
	if savedPasswords.Has(hostName) {
		savedPwd, err := savedPasswords.Get(hostName)
		if err == nil {
			methods = append(methods, ssh.Password(savedPwd))
			fmt.Println("Using saved password")
			return methods, nil
		}
	}

	// 5. Return if we have auth methods
	if len(methods) > 0 {
		return methods, nil
	}

	// 6. Prompt for password
	prompt := promptui.Prompt{
		Label: fmt.Sprintf("Enter password for %s@%s", user, hostName),
		Mask:  '*',
	}

	password, err := prompt.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to enter password: %w", err)
	}

	methods = append(methods, ssh.Password(password))

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
			fmt.Printf("Failed to initialize password store: %v\n", err)
		} else if err := store.Set(hostName, password); err != nil {
			fmt.Printf("Failed to save password: %v\n", err)
		} else {
			fmt.Println("Password saved")
		}
	}

	return methods, nil
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
