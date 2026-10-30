package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// skippedPattern records a Host pattern that import refused to store, and why.
type skippedPattern struct {
	Pattern string
	Reason  string
}

// importResult is what a snapshot of a parsed SSH config yields. It is computed
// by a pure function so the import semantics can be tested without a real
// ~/.ssh/config.
type importResult struct {
	Imported int
	Hosts    map[string]HostMeta
	Skipped  []skippedPattern
	Warnings []string
}

// sshConfigSource is the read side of an SSH config: only name lookups are
// needed, which keeps the pure import function independent of file parsing.
type sshConfigSource interface {
	// Get returns the effective value of key for name, including defaults
	// inherited from Host * blocks.
	Get(name, key string) (string, error)
	// Names lists every Host pattern in the file, in file order.
	Names() []string
}

// buildImportSnapshot merges the SSH config into existing local metadata.
//
// Import is a snapshot, not a sync: connection details are refreshed from the
// config, user-owned fields (alias, notes, tags, priority) are preserved, and
// entries deleted from ~/.ssh/config are never removed from sshgo.
func buildImportSnapshot(src sshConfigSource, existing map[string]HostMeta) importResult {
	result := importResult{Hosts: make(map[string]HostMeta, len(existing))}
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
			result.Skipped = append(result.Skipped, skippedPattern{Pattern: name, Reason: reason})
			continue
		}

		meta := result.Hosts[name]
		meta.Source = sourceImported
		meta.Hostname, _ = src.Get(name, "Hostname")
		meta.Port, _ = src.Get(name, "Port")
		meta.User, _ = src.Get(name, "User")
		meta.IdentityFile, _ = src.Get(name, "IdentityFile")
		meta.ProxyJump, _ = src.Get(name, "ProxyJump")
		result.Hosts[name] = meta
		result.Imported++

		// ProxyJump is stored but not honoured yet; say so at import time
		// rather than letting the user discover it when a connection fails.
		if meta.ProxyJump != "" {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Host %q has ProxyJump %q: imported, but jump-host connections are not supported yet.",
					name, meta.ProxyJump))
		}
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

// sshConfigAdapter adapts a parsed *ssh_config.Config to sshConfigSource.
type sshConfigAdapter struct {
	cfg *ssh_config.Config
}

func (a sshConfigAdapter) Get(name, key string) (string, error) {
	return a.cfg.Get(name, key)
}

func (a sshConfigAdapter) Names() []string {
	var names []string
	for _, host := range a.cfg.Hosts {
		for _, pattern := range host.Patterns {
			names = append(names, strings.TrimSpace(pattern.String()))
		}
	}
	return names
}

// handleConfigImport reads ~/.ssh/config and stores the hosts it describes.
// The work itself lives in buildImportSnapshot; this is only the I/O shell.
func handleConfigImport() {
	sshCfg, err := loadSSHConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read ~/.ssh/config: %v\n", err)
		os.Exit(1)
	}

	lc, err := LoadLocalConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	result := buildImportSnapshot(sshConfigAdapter{cfg: sshCfg}, lc.Hosts)

	for _, warning := range result.Warnings {
		fmt.Fprintln(os.Stderr, "Warning:", warning)
	}

	lc.Hosts = result.Hosts
	if err := lc.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Imported %d hosts from ~/.ssh/config\n", result.Imported)
	if len(result.Skipped) > 0 {
		fmt.Printf("Skipped %d pattern(s): %s\n", len(result.Skipped), formatSkipped(result.Skipped))
	}
	fmt.Println("Run `sshgo config list` to see imported hosts.")
	fmt.Println("Re-running `sshgo config import` refreshes connection details from ~/.ssh/config;")
	fmt.Println("hosts deleted there are not removed here.")
}

// formatSkipped renders "pattern (reason), pattern (reason)" for one summary line.
func formatSkipped(skipped []skippedPattern) string {
	parts := make([]string, 0, len(skipped))
	for _, s := range skipped {
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Pattern, s.Reason))
	}
	return strings.Join(parts, ", ")
}

// loadSSHConfig reads and parses ~/.ssh/config.
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
