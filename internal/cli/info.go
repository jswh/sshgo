package cli

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh/agent"

	"sshgo/internal/connect"
	"sshgo/internal/credential"
	"sshgo/internal/host"
)

func (c *CLI) runInfo(args []string) int {
	jsonFlag := false
	var filtered []string

	for _, a := range args {
		switch a {
		case "--json":
			jsonFlag = true
		case "--help", "-h":
			fmt.Fprintln(c.Out, "Usage: sshgo info [--json] <host>")
			fmt.Fprintln(c.Out)
			fmt.Fprintln(c.Out, "Display connection information for a host.")
			fmt.Fprintln(c.Out)
			fmt.Fprintln(c.Out, "Flags:")
			fmt.Fprintln(c.Out, "  --json    Output as JSON")
			return 0
		default:
			filtered = append(filtered, a)
		}
	}

	if len(filtered) == 0 {
		fmt.Fprintln(c.Err, "Usage: sshgo info [--json] <host>")
		return 1
	}

	hostName := filtered[0]

	target, err := host.Resolve(hostName)
	if err != nil {
		return c.resolveError(err)
	}

	// Validate the chain without dialling: `info` describes whether the Host can
	// be reached, so a broken or looping chain must fail here rather than be
	// reported as a healthy Host. The wording is the connection wording, since
	// the user is looking at the same problem.
	hops, err := connect.ResolveChain(target, connect.Options{})
	if err != nil {
		return c.resolveError(err)
	}

	result := buildInfoResult(target, hops)

	// Local metadata shown alongside the connection details: user-defined fields
	// only, never the connection values already surfaced above.
	if target.Source != host.SourceDirect {
		userMeta := stripConnectionFields(target.Meta)
		result.Meta = &userMeta
	}

	if jsonFlag {
		fmt.Fprintln(c.Out, result.JSON())
	} else {
		printInfoHuman(c.Out, result)
	}
	return 0
}

func buildInfoResult(target host.Resolved, hops []host.Resolved) InfoResult {
	// Detect available auth methods (non-interactive, non-sensitive)
	authMethods := detectAuthMethods(target.IdentityFile)

	return InfoResult{
		Name:        target.Name,
		Hostname:    target.Hostname,
		Port:        target.Port,
		User:        target.User,
		Source:      target.Source,
		AuthMethods: authMethods,
		Jump:        buildJumpHops(hops),
	}
}

// buildJumpHops describes each hop the way the target is described: connection
// details plus the auth methods available for that hop, with no secrets. It
// returns nil for an empty chain so the JSON field stays absent.
func buildJumpHops(hops []host.Resolved) []JumpHop {
	if len(hops) == 0 {
		return nil
	}
	result := make([]JumpHop, 0, len(hops))
	for _, hop := range hops {
		result = append(result, JumpHop{
			Name:        hop.Name,
			Hostname:    hop.Hostname,
			Port:        hop.Port,
			User:        hop.User,
			AuthMethods: detectAuthMethods(hop.IdentityFile),
		})
	}
	return result
}

// detectAuthMethods checks which auth methods are available without connecting.
// Returns human-readable descriptions without exposing secrets.
func detectAuthMethods(identityFile string) []string {
	var methods []string

	// Check SSH agent
	if conn, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		sshAgent := agent.NewClient(conn)
		if keys, err := sshAgent.List(); err == nil {
			if len(keys) > 0 {
				methods = append(methods, fmt.Sprintf("SSH Agent (%d keys)", len(keys)))
			}
		}
		conn.Close()
	}

	// Check identity file exists
	if identityFile != "" {
		identityFile = host.ExpandPath(identityFile)
		if _, err := os.Stat(identityFile); err == nil {
			methods = append(methods, "IdentityFile (key)")
		}
	}

	// Check default keys
	homeDir, _ := os.UserHomeDir()
	for _, keyName := range defaultKeyNames {
		keyPath := filepath.Join(homeDir, ".ssh", keyName)
		if _, err := os.Stat(keyPath); err == nil {
			methods = append(methods, "Default key ("+keyName+")")
			break
		}
	}

	// Check saved passwords (without decrypting, just check file existence)
	passPath := credential.Path()
	if _, err := os.Stat(passPath); err == nil {
		// File exists; may or may not have a password for this host
		methods = append(methods, "Password store (encrypted)")
	}

	return methods
}

func printInfoHuman(out interface{ Write([]byte) (int, error) }, result InfoResult) {
	fmt.Fprintln(out, "Host:", result.Name)
	fmt.Fprintln(out, "  Hostname:", result.Hostname)
	fmt.Fprintln(out, "  Port:    ", result.Port)
	fmt.Fprintln(out, "  User:    ", result.User)
	fmt.Fprintln(out, "  Source:  ", result.Source)
	// The Jump line sits with the connection details, and only when a chain
	// exists: a direct Host's output stays byte-for-byte what it was.
	if len(result.Jump) > 0 {
		fmt.Fprintln(out, "  Jump:    ", jumpPath(result.Jump))
	}
	fmt.Fprintln(out, "  Auth:")
	for _, m := range result.AuthMethods {
		fmt.Fprintln(out, "    -", m)
	}
	if result.Meta != nil {
		fmt.Fprintln(out, "  Metadata:")
		if result.Meta.Alias != "" {
			fmt.Fprintln(out, "    Alias:", result.Meta.Alias)
		}
		if result.Meta.Notes != "" {
			fmt.Fprintln(out, "    Notes:", result.Meta.Notes)
		}
		if len(result.Meta.Tags) > 0 {
			fmt.Fprintln(out, "    Tags: ", strings.Join(result.Meta.Tags, ", "))
		}
		if len(result.Meta.ConnectionPriority) > 0 {
			fmt.Fprintln(out, "    Priority:", strings.Join(result.Meta.ConnectionPriority, ", "))
		}
	}
}

// jumpPath renders the chain as "outermost -> ... -> innermost".
func jumpPath(hops []JumpHop) string {
	names := make([]string, 0, len(hops))
	for _, hop := range hops {
		names = append(names, hop.Name)
	}
	return strings.Join(names, " -> ")
}

// stripConnectionFields returns a copy of HostMeta with only user-defined metadata fields.
// Connection details (hostname, port, user, identity-file, proxy-jump) are excluded
// since they are already shown as top-level fields in InfoResult.
func stripConnectionFields(meta host.HostMeta) host.HostMeta {
	return host.HostMeta{
		Alias:              meta.Alias,
		Notes:              meta.Notes,
		Tags:               meta.Tags,
		ConnectionPriority: meta.ConnectionPriority,
	}
}
