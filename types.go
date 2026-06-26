package main

import "encoding/json"

// Subcommand identifiers for CLI routing.
type Subcommand string

const (
	SubExec   Subcommand = "exec"
	SubInfo   Subcommand = "info"
	SubConfig Subcommand = "config"
)

// ExecResult is the structured JSON output for a remote command.
type ExecResult struct {
	Command  string `json:"command"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration,omitempty"` // human-readable, only when --sudo or timing enabled
}

// JSON returns a pretty-printed JSON string.
func (e ExecResult) JSON() string {
	b, _ := json.MarshalIndent(e, "", "  ")
	return string(b)
}

// HostMeta is the per-host metadata stored in ~/.sshgo_config.
type HostMeta struct {
	Alias              string   `json:"alias,omitempty"`
	Notes              string   `json:"notes,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ConnectionPriority []string `json:"connection_priority,omitempty"` // ordered auth method names
}

// LocalConfig represents the full ~/.sshgo_config file.
type LocalConfig struct {
	Version string              `json:"version"`
	Hosts   map[string]HostMeta `json:"hosts"` // keyed by canonical host identifier
}

// InfoResult is the structured output for `sshgo info`.
type InfoResult struct {
	Name        string    `json:"name"`
	Hostname    string    `json:"hostname"`
	Port        string    `json:"port"`
	User        string    `json:"user"`
	Source      string    `json:"source"`
	AuthMethods []string  `json:"auth_methods"` // non-sensitive descriptions
	Meta        *HostMeta `json:"meta,omitempty"`
}

func (i InfoResult) JSON() string {
	b, _ := json.MarshalIndent(i, "", "  ")
	return string(b)
}
