package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh/agent"
)

func handleInfo(args []string) {
	jsonFlag := false
	var filtered []string

	for _, a := range args {
		switch a {
		case "--json":
			jsonFlag = true
		case "--help", "-h":
			fmt.Println("Usage: sshgo info [--json] <host>")
			fmt.Println()
			fmt.Println("Display connection information for a host.")
			fmt.Println()
			fmt.Println("Flags:")
			fmt.Println("  --json    Output as JSON")
			return
		default:
			filtered = append(filtered, a)
		}
	}

	if len(filtered) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: sshgo info [--json] <host>")
		os.Exit(1)
	}

	hostName := filtered[0]

	target, err := resolveTarget(hostName)
	if err != nil {
		exitResolveError(err)
	}

	result := buildInfoResult(target)

	// Local metadata shown alongside the connection details: user-defined fields
	// only, never the connection values already surfaced above.
	if target.Info.Source != sourceDirect {
		userMeta := stripConnectionFields(target.Meta)
		result.Meta = &userMeta
	}

	if jsonFlag {
		fmt.Println(result.JSON())
	} else {
		printInfoHuman(result)
	}
}

func buildInfoResult(target resolvedTarget) InfoResult {
	info := target.Info
	hostname, port, user, identityFile := resolveConnection(info)

	// Detect available auth methods (non-interactive, non-sensitive)
	authMethods := detectAuthMethods(hostname, user, identityFile)

	return InfoResult{
		Name:        info.Name,
		Hostname:    hostname,
		Port:        port,
		User:        user,
		Source:      info.Source,
		AuthMethods: authMethods,
	}
}

// detectAuthMethods checks which auth methods are available without connecting.
// Returns human-readable descriptions without exposing secrets.
func detectAuthMethods(hostName, user, identityFile string) []string {
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
		identityFile = expandPath(identityFile)
		if _, err := os.Stat(identityFile); err == nil {
			methods = append(methods, "IdentityFile (key)")
		}
	}

	// Check default keys
	homeDir, _ := os.UserHomeDir()
	for _, keyName := range []string{"id_rsa", "id_ed25519", "id_ecdsa"} {
		keyPath := filepath.Join(homeDir, ".ssh", keyName)
		if _, err := os.Stat(keyPath); err == nil {
			methods = append(methods, "Default key ("+keyName+")")
			break
		}
	}

	// Check saved passwords (without decrypting, just check file existence)
	passPath := getPasswordStorePath()
	if _, err := os.Stat(passPath); err == nil {
		// File exists; may or may not have a password for this host
		methods = append(methods, "Password store (encrypted)")
	}

	return methods
}

func printInfoHuman(result InfoResult) {
	fmt.Println("Host:", result.Name)
	fmt.Println("  Hostname:", result.Hostname)
	fmt.Println("  Port:    ", result.Port)
	fmt.Println("  User:    ", result.User)
	fmt.Println("  Source:  ", result.Source)
	fmt.Println("  Auth:")
	for _, m := range result.AuthMethods {
		fmt.Println("    -", m)
	}
	if result.Meta != nil {
		fmt.Println("  Metadata:")
		if result.Meta.Alias != "" {
			fmt.Println("    Alias:", result.Meta.Alias)
		}
		if result.Meta.Notes != "" {
			fmt.Println("    Notes:", result.Meta.Notes)
		}
		if len(result.Meta.Tags) > 0 {
			fmt.Println("    Tags: ", strings.Join(result.Meta.Tags, ", "))
		}
		if len(result.Meta.ConnectionPriority) > 0 {
			fmt.Println("    Priority:", strings.Join(result.Meta.ConnectionPriority, ", "))
		}
	}
}

// stripConnectionFields returns a copy of HostMeta with only user-defined metadata fields.
// Connection details (hostname, port, user, identity-file, proxy-jump) are excluded
// since they are already shown as top-level fields in InfoResult.
func stripConnectionFields(meta HostMeta) HostMeta {
	return HostMeta{
		Alias:              meta.Alias,
		Notes:              meta.Notes,
		Tags:               meta.Tags,
		ConnectionPriority: meta.ConnectionPriority,
	}
}
