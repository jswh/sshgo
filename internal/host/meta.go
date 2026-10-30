// Package host owns what a Host is and how it is addressed: the local metadata
// file, its CRUD, provenance, and the single resolution entry point shared by
// every caller.
package host

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const localConfigVersion = "1"

// Source records where a Host came from. It answers "where did this entry
// originate", not "who edited it last".
const (
	SourceImported = "imported"
	SourceDirect   = "direct"
	SourceManual   = "manual"
)

// HostMeta is the per-host metadata stored in ~/.sshgo_config.
type HostMeta struct {
	Alias              string   `json:"alias,omitempty"`
	Notes              string   `json:"notes,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ConnectionPriority []string `json:"connection_priority,omitempty"` // ordered auth method names

	// Source records provenance (imported / direct / manual), not the last
	// editor. Empty in files written before it was persisted; see
	// deriveSource.
	Source string `json:"source,omitempty"`

	// SSH connection details (populated by `config import` or `config set`)
	Hostname     string `json:"hostname,omitempty"`
	Port         string `json:"port,omitempty"`
	User         string `json:"user,omitempty"`
	IdentityFile string `json:"identity_file,omitempty"`
	ProxyJump    string `json:"proxy_jump,omitempty"`
}

// LocalConfig represents the full ~/.sshgo_config file.
type LocalConfig struct {
	Version string              `json:"version"`
	Hosts   map[string]HostMeta `json:"hosts"` // keyed by canonical host identifier
}

// LocalConfigPath returns the metadata file path.
func LocalConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".sshgo_config"
	}
	return filepath.Join(homeDir, ".sshgo_config")
}

// LoadLocalConfig reads the local metadata file. Returns an empty config if the file doesn't exist.
func LoadLocalConfig() (*LocalConfig, error) {
	path := LocalConfigPath()
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

	// Files written before Source was persisted get it derived here, so callers
	// never see an empty provenance; the next Save writes it to disk.
	for name, meta := range cfg.Hosts {
		meta.Source = deriveSource(meta)
		cfg.Hosts[name] = meta
	}

	return cfg, nil
}

// deriveSource reports the stored provenance, falling back to the historical
// rule for entries written before the field existed: any connection detail set
// means the entry came from an import.
func deriveSource(meta HostMeta) string {
	if meta.Source != "" {
		return meta.Source
	}
	if meta.Hostname != "" || meta.User != "" || meta.Port != "" || meta.IdentityFile != "" {
		return SourceImported
	}
	return SourceManual
}

func (c *LocalConfig) Save() error {
	path := LocalConfigPath()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Set sets a metadata field for a host.
// Supported keys: alias, notes, tags, connection-priority.
// Creating an entry here makes it manual; editing one keeps its provenance.
func (c *LocalConfig) Set(host, key, value string) error {
	meta, existed := c.Hosts[host]
	if !existed {
		meta.Source = SourceManual
	}

	switch strings.ToLower(key) {
	case "alias":
		meta.Alias = value
	case "notes":
		meta.Notes = value
	case "tags":
		meta.Tags = splitAndTrim(value)
	case "connection-priority", "priority":
		meta.ConnectionPriority = splitAndTrim(value)
	case "hostname":
		meta.Hostname = value
	case "port":
		meta.Port = value
	case "user":
		meta.User = value
	case "identity-file", "identityfile":
		meta.IdentityFile = value
	case "proxy-jump", "proxyjump":
		meta.ProxyJump = value
	case "source":
		return fmt.Errorf("key %q is derived from where the host came from and cannot be set", key)
	default:
		return fmt.Errorf("unknown key %q; supported: alias, notes, tags, connection-priority, hostname, port, user, identity-file, proxy-jump", key)
	}

	if c.Hosts == nil {
		c.Hosts = make(map[string]HostMeta)
	}
	c.Hosts[host] = meta
	return c.Save()
}

// Get retrieves a metadata field for a host.
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
	case "hostname":
		return meta.Hostname, nil
	case "port":
		return meta.Port, nil
	case "user":
		return meta.User, nil
	case "identity-file", "identityfile":
		return meta.IdentityFile, nil
	case "proxy-jump", "proxyjump":
		return meta.ProxyJump, nil
	case "source":
		return meta.Source, nil
	case "":
		// Print all
		return c.FormatMeta(meta), nil
	default:
		return "", fmt.Errorf("unknown key %q; supported: alias, notes, tags, connection-priority, hostname, port, user, identity-file, proxy-jump", key)
	}
}

// Unset removes a metadata field (or the entire host entry if no key given).
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
	case "hostname":
		meta.Hostname = ""
	case "port":
		meta.Port = ""
	case "user":
		meta.User = ""
	case "identity-file", "identityfile":
		meta.IdentityFile = ""
	case "proxy-jump", "proxyjump":
		meta.ProxyJump = ""
	default:
		return fmt.Errorf("unknown key %q", key)
	}

	c.Hosts[host] = meta
	return c.Save()
}

// Remove deletes an entire host entry. It is `Unset` with no key, and exists as
// its own name because import is a snapshot: hosts deleted from ~/.ssh/config
// are not removed here, so users need a discoverable way to delete one.
func (c *LocalConfig) Remove(host string) error {
	return c.Unset(host, "")
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

// FormatMeta renders every set field of one entry as "key: value" lines.
func (c *LocalConfig) FormatMeta(meta HostMeta) string {
	var parts []string
	if meta.Source != "" {
		parts = append(parts, "source: "+meta.Source)
	}
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
	if meta.Hostname != "" {
		parts = append(parts, "hostname: "+meta.Hostname)
	}
	if meta.Port != "" {
		parts = append(parts, "port: "+meta.Port)
	}
	if meta.User != "" {
		parts = append(parts, "user: "+meta.User)
	}
	if meta.IdentityFile != "" {
		parts = append(parts, "identity-file: "+meta.IdentityFile)
	}
	if meta.ProxyJump != "" {
		parts = append(parts, "proxy-jump: "+meta.ProxyJump)
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
