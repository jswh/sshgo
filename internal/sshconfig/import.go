// Package sshconfig turns a parsed ~/.ssh/config into a snapshot of Host
// metadata. Import is a snapshot, not a sync: the pure function decides what
// gets stored, the adapter only reads the file.
package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kevinburke/ssh_config"

	"sshgo/internal/host"
)

// SkippedPattern records a Host pattern that import refused to store, and why.
type SkippedPattern struct {
	Pattern string
	Reason  string
}

// Result is what a snapshot of a parsed SSH config yields.
type Result struct {
	Imported int
	Hosts    map[string]host.HostMeta
	Skipped  []SkippedPattern
	Warnings []string
}

// Source is the read side of an SSH config: only name lookups are needed, which
// keeps the pure import function independent of file parsing.
type Source interface {
	// Get returns the effective value of key for name, including defaults
	// inherited from Host * blocks.
	Get(name, key string) (string, error)
	// Names lists every Host pattern in the file, in file order.
	Names() []string
}

// BuildSnapshot merges the SSH config into existing local metadata.
//
// Import is a snapshot, not a sync: connection details are refreshed from the
// config, user-owned fields (alias, notes, tags, priority) are preserved, and
// entries deleted from ~/.ssh/config are never removed from sshgo.
func BuildSnapshot(src Source, existing map[string]host.HostMeta) Result {
	result := Result{Hosts: make(map[string]host.HostMeta, len(existing))}
	for name, meta := range existing {
		result.Hosts[name] = meta
	}

	seen := make(map[string]bool)
	for _, name := range src.Names() {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true

		if reason := skipReason(name); reason != "" {
			result.Skipped = append(result.Skipped, SkippedPattern{Pattern: name, Reason: reason})
			continue
		}

		meta := result.Hosts[name]
		meta.Source = host.SourceImported
		meta.Hostname, _ = src.Get(name, "Hostname")
		meta.Port, _ = src.Get(name, "Port")
		meta.User, _ = src.Get(name, "User")
		meta.IdentityFile, _ = src.Get(name, "IdentityFile")
		meta.ProxyJump, _ = src.Get(name, "ProxyJump")
		result.Hosts[name] = meta
		result.Imported++

		// ProxyJump is imported as-is and validated when a connection is
		// resolved, never here: a forward reference (target first, jump host
		// later) is a legal order, so a warning would be noise.
	}

	sort.Slice(result.Skipped, func(i, j int) bool { return result.Skipped[i].Pattern < result.Skipped[j].Pattern })
	return result
}

// skipReason reports why a Host pattern cannot be a Host, or "" if it can.
// sshgo connects to concrete hosts only; patterns that match other patterns
// would otherwise become fake host entries with misleading metadata.
func skipReason(pattern string) string {
	if pattern == "*" {
		return "wildcard, applies to all hosts"
	}
	if strings.HasPrefix(pattern, "!") {
		return "negated pattern, not a host"
	}
	if strings.ContainsAny(pattern, "*?[") {
		return "wildcard pattern, not a concrete host"
	}
	return ""
}

// FormatSkipped renders "pattern (reason), pattern (reason)" for one summary line.
func FormatSkipped(skipped []SkippedPattern) string {
	parts := make([]string, 0, len(skipped))
	for _, s := range skipped {
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Pattern, s.Reason))
	}
	return strings.Join(parts, ", ")
}

// adapter adapts a parsed *ssh_config.Config to Source.
type adapter struct {
	cfg *ssh_config.Config
}

// NewSource wraps an already parsed SSH config.
func NewSource(cfg *ssh_config.Config) Source {
	return adapter{cfg: cfg}
}

func (a adapter) Get(name, key string) (string, error) {
	return a.cfg.Get(name, key)
}

func (a adapter) Names() []string {
	var names []string
	for _, h := range a.cfg.Hosts {
		for _, pattern := range h.Patterns {
			names = append(names, strings.TrimSpace(pattern.String()))
		}
	}
	return names
}

// LoadConfig reads and parses ~/.ssh/config.
func LoadConfig() (*ssh_config.Config, error) {
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
