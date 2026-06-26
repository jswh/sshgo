package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const localConfigVersion = "1"

func getLocalConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".sshgo_config"
	}
	return filepath.Join(homeDir, ".sshgo_config")
}

// LoadLocalConfig reads the local metadata file. Returns an empty config if the file doesn't exist.
func LoadLocalConfig() (*LocalConfig, error) {
	path := getLocalConfigPath()
	cfg := &LocalConfig{
		Version: localConfigVersion,
		Hosts:   make(map[string]HostMeta),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	// Ensure map is initialized
	if cfg.Hosts == nil {
		cfg.Hosts = make(map[string]HostMeta)
	}

	return cfg, nil
}

func (c *LocalConfig) Save() error {
	path := getLocalConfigPath()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ConfigSet sets a metadata field for a host.
// Supported keys: alias, notes, tags, connection-priority.
func (c *LocalConfig) Set(host, key, value string) error {
	meta := c.Hosts[host]

	switch strings.ToLower(key) {
	case "alias":
		meta.Alias = value
	case "notes":
		meta.Notes = value
	case "tags":
		meta.Tags = splitAndTrim(value)
	case "connection-priority", "priority":
		meta.ConnectionPriority = splitAndTrim(value)
	default:
		return fmt.Errorf("unknown key %q; supported: alias, notes, tags, connection-priority", key)
	}

	if c.Hosts == nil {
		c.Hosts = make(map[string]HostMeta)
	}
	c.Hosts[host] = meta
	return c.Save()
}

// ConfigGet retrieves a metadata field for a host.
func (c *LocalConfig) Get(host, key string) (string, error) {
	meta, ok := c.Hosts[host]
	if !ok {
		return "", fmt.Errorf("host %q not found in local config", host)
	}

	switch strings.ToLower(key) {
	case "alias":
		return meta.Alias, nil
	case "notes":
		return meta.Notes, nil
	case "tags":
		return strings.Join(meta.Tags, ", "), nil
	case "connection-priority", "priority":
		return strings.Join(meta.ConnectionPriority, ", "), nil
	case "":
		// Print all
		return c.formatMeta(meta), nil
	default:
		return "", fmt.Errorf("unknown key %q; supported: alias, notes, tags, connection-priority", key)
	}
}

// ConfigUnset removes a metadata field (or the entire host entry if no key given).
func (c *LocalConfig) Unset(host, key string) error {
	if key == "" {
		delete(c.Hosts, host)
		return c.Save()
	}

	meta, ok := c.Hosts[host]
	if !ok {
		return nil
	}

	switch strings.ToLower(key) {
	case "alias":
		meta.Alias = ""
	case "notes":
		meta.Notes = ""
	case "tags":
		meta.Tags = nil
	case "connection-priority", "priority":
		meta.ConnectionPriority = nil
	default:
		return fmt.Errorf("unknown key %q", key)
	}

	c.Hosts[host] = meta
	return c.Save()
}

// List returns all hosts with metadata, sorted by name.
func (c *LocalConfig) List() []string {
	var names []string
	for name := range c.Hosts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// FindByTag returns host names that have the given tag.
func (c *LocalConfig) FindByTag(tag string) []string {
	var result []string
	for name, meta := range c.Hosts {
		for _, t := range meta.Tags {
			if strings.EqualFold(t, tag) {
				result = append(result, name)
				break
			}
		}
	}
	sort.Strings(result)
	return result
}

func (c *LocalConfig) formatMeta(meta HostMeta) string {
	var parts []string
	if meta.Alias != "" {
		parts = append(parts, "alias: "+meta.Alias)
	}
	if meta.Notes != "" {
		parts = append(parts, "notes: "+meta.Notes)
	}
	if len(meta.Tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(meta.Tags, ", "))
	}
	if len(meta.ConnectionPriority) > 0 {
		parts = append(parts, "priority: "+strings.Join(meta.ConnectionPriority, ", "))
	}
	return strings.Join(parts, "\n")
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// handleConfig subcommand dispatcher.
func handleConfig(args []string) {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Println("Usage: sshgo config <command> [args]")
		fmt.Println()
		fmt.Println("Commands:")
		fmt.Println("  list                          List all hosts with metadata")
		fmt.Println("  get <host> [key]              Get metadata (key: alias, notes, tags, priority)")
		fmt.Println("  set <host> <key> <value>      Set metadata")
		fmt.Println("  unset <host> [key]            Remove metadata or entire host entry")
		fmt.Println("  find --tag <tag>              Find hosts by tag")
		return
	}

	cfg, err := LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	cmd := args[0]

	switch cmd {
	case "list":
		hosts := cfg.List()
		if len(hosts) == 0 {
			fmt.Println("No hosts in local config")
			return
		}
		for _, name := range hosts {
			meta := cfg.Hosts[name]
			fmt.Printf("%s:\n", name)
			fmt.Println("  " + strings.ReplaceAll(cfg.formatMeta(meta), "\n", "\n  "))
		}

	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config get <host> [key]")
			os.Exit(1)
		}
		host := args[1]
		key := ""
		if len(args) > 2 {
			key = args[2]
		}
		val, err := cfg.Get(host, key)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(val)

	case "set":
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config set <host> <key> <value>")
			os.Exit(1)
		}
		host, key, value := args[1], args[2], args[3]
		if err := cfg.Set(host, key, value); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Set %s[%s] = %s\n", host, key, value)

	case "unset":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config unset <host> [key]")
			os.Exit(1)
		}
		host := args[1]
		key := ""
		if len(args) > 2 {
			key = args[2]
		}
		if err := cfg.Unset(host, key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Unset %s[%s]\n", host, key)

	case "find":
		if len(args) < 3 || args[1] != "--tag" {
			fmt.Fprintln(os.Stderr, "Usage: sshgo config find --tag <tag>")
			os.Exit(1)
		}
		tag := args[2]
		hosts := cfg.FindByTag(tag)
		if len(hosts) == 0 {
			fmt.Printf("No hosts with tag %q\n", tag)
			return
		}
		for _, name := range hosts {
			fmt.Println(name)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown config command: %q\n", cmd)
		os.Exit(1)
	}
}
