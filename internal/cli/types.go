package cli

import (
	"encoding/json"

	"sshgo/internal/host"
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
	Name        string   `json:"name"`
	Hostname    string   `json:"hostname"`
	Port        string   `json:"port"`
	User        string   `json:"user"`
	Source      string   `json:"source"`
	AuthMethods []string `json:"auth_methods"` // non-sensitive descriptions

	// Jump describes the hops dialled before the target, outermost first. It is
	// omitted entirely when the chain is empty, so consumers of a single-hop
	// Host see exactly the keys they saw before.
	Jump []JumpHop `json:"jump,omitempty"`

	Meta *host.HostMeta `json:"meta,omitempty"`
}

// JumpHop is one hop of the jump chain in `info --json`. AuthMethods keeps the
// existing promise: method names and counts only, never a key path, password,
// or passphrase.
type JumpHop struct {
	Name        string   `json:"name"`
	Hostname    string   `json:"hostname"`
	Port        string   `json:"port"`
	User        string   `json:"user"`
	AuthMethods []string `json:"auth_methods"`
}

func (i InfoResult) JSON() string {
	b, _ := json.MarshalIndent(i, "", "  ")
	return string(b)
}
