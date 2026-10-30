package main

import (
	"encoding/json"

	"sshgo/internal/host"
)

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

// InfoResult is the structured output for `sshgo info`.
type InfoResult struct {
	Name        string         `json:"name"`
	Hostname    string         `json:"hostname"`
	Port        string         `json:"port"`
	User        string         `json:"user"`
	Source      string         `json:"source"`
	AuthMethods []string       `json:"auth_methods"` // non-sensitive descriptions
	Meta        *host.HostMeta `json:"meta,omitempty"`
}

func (i InfoResult) JSON() string {
	b, _ := json.MarshalIndent(i, "", "  ")
	return string(b)
}
