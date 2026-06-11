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
		fmt.Println("用法: sshgo [主机名|user@host]")
		fmt.Println()
		fmt.Println("SSH 客户端工具，支持:")
		fmt.Println("  - 读取 ~/.ssh/config 配置")
		fmt.Println("  - SSH key 认证")
		fmt.Println("  - 密码加密保存")
		fmt.Println("  - 交互式主机选择")
		fmt.Println()
		fmt.Println("示例:")
		fmt.Println("  sshgo zg              # 使用 ssh config 中的 zg")
		fmt.Println("  sshgo root@1.2.3.4    # 直接登录指定地址")
		fmt.Println("  sshgo                 # 列出所有主机供选择")
		os.Exit(0)
	}

	sshConfig, err := loadSSHConfig()
	if err != nil {
		fmt.Printf("无法读取 SSH 配置文件: %v\n", err)
		os.Exit(1)
	}

	passwords := &PasswordStore{Passwords: make(map[string]string)}

	hosts := buildHostList(sshConfig, passwords)

	if len(args) > 0 {
		hostName := args[0]
		
		// 解析 user@host:port 格式
		user, hostname, port := parseUserHost(hostName)
		
		// 尝试在配置中查找
		info, found := findHost(hosts, hostName)
		if !found && hostname != "" {
			// 不在配置中，创建一个临时的 hostInfo
			info = hostInfo{
				Name:   hostname,
				Source: "direct",
			}
			if user != "" {
				info.Name = user + "@" + hostname
			}
			// 存储端口信息到额外字段
			if port != "" {
				info.Name = info.Name + ":" + port
			}
		} else if !found {
			fmt.Printf("主机 %s 未在 SSH 配置中找到\n", hostName)
			os.Exit(1)
		}
		
		connectToHost(info, sshConfig, passwords)
		return
	}

	if len(hosts) == 0 {
		fmt.Println("没有找到任何可用的 SSH 主机配置")
		os.Exit(1)
	}

	fmt.Println("=== SSH 主机列表 ===")
	fmt.Println()

	var items []string
	for i, h := range hosts {
		status := ""
		if h.HasPassword {
			status = " [已保存密码]"
		}
		items = append(items, fmt.Sprintf("%2d. %-20s %-8s%s", i+1, h.Name, h.Source, status))
	}

	prompt := promptui.Select{
		Label: "选择要登录的主机",
		Items: items,
		Size:  20,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		fmt.Printf("选择失败: %v\n", err)
		os.Exit(1)
	}

	connectToHost(hosts[idx], sshConfig, passwords)
}

func parseUserHost(s string) (user, host, port string) {
	// 解析 user@host:port 格式
	if idx := strings.Index(s, "@"); idx >= 0 {
		user = s[:idx]
		s = s[idx+1:]
	}
	
	// 检查端口
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
		fmt.Println("首次使用密码库，请设置主密码（用于加密保存的密码）")
		prompt := promptui.Prompt{
			Label: "设置主密码",
			Mask:  '*',
		}
		masterPwd, err := prompt.Run()
		if err != nil {
			return nil, fmt.Errorf("主密码设置失败: %w", err)
		}
		prompt2 := promptui.Prompt{
			Label: "确认主密码",
			Mask:  '*',
		}
		confirmPwd, err := prompt2.Run()
		if err != nil {
			return nil, fmt.Errorf("主密码确认失败: %w", err)
		}
		if masterPwd != confirmPwd {
			return nil, fmt.Errorf("两次输入的主密码不一致")
		}
		return LoadPasswordStore(masterPwd)
	}

	prompt := promptui.Prompt{
		Label: "输入主密码",
		Mask:  '*',
	}
	masterPwd, err := prompt.Run()
	if err != nil {
		return nil, fmt.Errorf("主密码输入失败: %w", err)
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
		// 解析 user@host:port 格式
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
		fmt.Printf("认证失败: %v\n", err)
		os.Exit(1)
	}

	clientConfig := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", hostname+":"+port, clientConfig)
	if err != nil {
		fmt.Printf("连接失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		fmt.Printf("创建会话失败: %v\n", err)
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
		fmt.Printf("请求伪终端失败: %v\n", err)
		os.Exit(1)
	}

	session.Stdin = os.Stdin
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	if err := session.Shell(); err != nil {
		fmt.Printf("启动 shell 失败: %v\n", err)
		os.Exit(1)
	}

	if err := session.Wait(); err != nil {
		os.Exit(0)
	}
}

func buildAuthMethods(hostName, user, identityFile string, passwords *PasswordStore) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// 1. 尝试从 SSH agent 获取密钥
	if sshAgentConn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		sshAgent := agent.NewClient(sshAgentConn)
		if keys, err := sshAgent.List(); err == nil && len(keys) > 0 {
			methods = append(methods, ssh.PublicKeysCallback(sshAgent.Signers))
			fmt.Println("使用 SSH agent 中的密钥")
		}
		sshAgentConn.Close()
	}

	// 2. 尝试使用配置的 IdentityFile
	if identityFile != "" {
		identityFile = expandPath(identityFile)
		if signer, err := loadPrivateKey(identityFile); err == nil {
			methods = append(methods, ssh.PublicKeys(signer))
			fmt.Printf("使用密钥: %s\n", identityFile)
		}
	}

	// 3. 尝试默认密钥文件
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

	// 4. 检查是否有保存的密码（按需加载）
	savedPasswords := loadSavedPasswords()
	if savedPasswords.Has(hostName) {
		savedPwd, err := savedPasswords.Get(hostName)
		if err == nil {
			methods = append(methods, ssh.Password(savedPwd))
			fmt.Println("使用已保存的密码")
			return methods, nil
		}
	}

	// 5. 如果已经有认证方法，直接返回
	if len(methods) > 0 {
		return methods, nil
	}

	// 6. 没有任何认证方式，提示输入密码
	prompt := promptui.Prompt{
		Label: fmt.Sprintf("输入 %s@%s 的密码", user, hostName),
		Mask:  '*',
	}

	password, err := prompt.Run()
	if err != nil {
		return nil, fmt.Errorf("密码输入失败: %w", err)
	}

	methods = append(methods, ssh.Password(password))

	// 询问是否保存密码
	savePrompt := promptui.Prompt{
		Label:     "是否保存此密码? (y/n)",
		Default:   "n",
		AllowEdit: true,
	}
	saveAnswer, _ := savePrompt.Run()
	if strings.ToLower(strings.TrimSpace(saveAnswer)) == "y" {
		store, err := initPasswordStore()
		if err != nil {
			fmt.Printf("密码库初始化失败: %v\n", err)
		} else if err := store.Set(hostName, password); err != nil {
			fmt.Printf("保存密码失败: %v\n", err)
		} else {
			fmt.Println("密码已保存")
		}
	}

	return methods, nil
}

func loadPrivateKey(path string) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// 尝试无密码
	if signer, err := ssh.ParsePrivateKey(key); err == nil {
		return signer, nil
	}

	// 尝试输入密码
	passphrasePrompt := promptui.Prompt{
		Label: fmt.Sprintf("输入 %s 的密钥密码", path),
		Mask:  '*',
	}
	passphrase, _ := passphrasePrompt.Run()
	if passphrase == "" {
		return nil, fmt.Errorf("需要密码")
	}

	return ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
}

func loadSavedPasswords() *PasswordStore {
	path := getPasswordStorePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &PasswordStore{Passwords: make(map[string]string)}
	}
	prompt := promptui.Prompt{
		Label: "输入主密码",
		Mask:  '*',
	}
	masterPwd, err := prompt.Run()
	if err != nil {
		return &PasswordStore{Passwords: make(map[string]string)}
	}
	store, err := LoadPasswordStore(masterPwd)
	if err != nil {
		fmt.Printf("密码库加载失败: %v\n", err)
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