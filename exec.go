package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"sshgo/internal/connect"
	"sshgo/internal/host"
)

// execFlags holds parsed flags for the exec subcommand.
type execFlags struct {
	Raw  bool
	Sudo bool
}

func handleExec(args []string) {
	// Check for help before parsing flags
	if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: sshgo exec [--raw] [--sudo] <host> <command>")
		fmt.Println()
		fmt.Println("Execute a remote command with structured JSON output.")
		fmt.Println()
		fmt.Println("Flags:")
		fmt.Println("  --raw     Output raw stdout/stderr stream (no JSON wrapper)")
		fmt.Println("  --sudo    Execute command with sudo (reuses SSH password)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  sshgo exec db-staging \"SELECT count(*) FROM users\"")
		fmt.Println("  sshgo exec --raw web-01 \"tail -n 100 /var/log/nginx/access.log\"")
		fmt.Println("  sshgo exec --sudo db-staging \"systemctl restart postgresql\"")
		return
	}

	flags := parseExecFlags(&args)

	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Error: missing host or command")
		fmt.Fprintln(os.Stderr, "Usage: sshgo exec [--raw] [--sudo] <host> <command>")
		os.Exit(1)
	}

	hostName := args[0]
	command := strings.Join(args[1:], " ")

	target, err := host.Resolve(hostName)
	if err != nil {
		exitResolveError(err)
	}

	start := time.Now()
	result := executeRemote(target, command, flags)
	result.Duration = time.Since(start).Round(time.Millisecond).String()

	if flags.Raw {
		os.Stdout.Write([]byte(result.Stdout))
		os.Stderr.Write([]byte(result.Stderr))
		os.Exit(result.ExitCode)
	}

	fmt.Println(result.JSON())
	os.Exit(result.ExitCode)
}

func parseExecFlags(args *[]string) execFlags {
	var flags execFlags
	var filtered []string

	for _, a := range *args {
		switch {
		case a == "--raw":
			flags.Raw = true
		case a == "--sudo":
			flags.Sudo = true
		case strings.HasPrefix(a, "--"):
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", a)
			os.Exit(1)
		default:
			filtered = append(filtered, a)
		}
	}

	*args = filtered
	return flags
}

// executeRemote dials the host, runs a command, and returns structured output.
// The sudo password reaches the session through a separate exit, never through
// the authentication methods.
func executeRemote(target host.Resolved, command string, flags execFlags) ExecResult {
	auth := &execAuth{needSudo: flags.Sudo}

	client, err := connect.Dial(target, auth, connect.Options{Timeout: 30 * time.Second})
	if err != nil {
		return ExecResult{
			Command:  command,
			Stderr:   execDialError(err),
			ExitCode: 1,
		}
	}
	defer client.Close()

	sudoPwd, hasSudo := auth.SudoPassword(target)
	if flags.Sudo {
		if hasSudo && sudoPwd != "" {
			command = "sudo -S -p '' " + command
		} else {
			command = "sudo " + command
		}
	}

	session, err := connect.NewSession(client)
	if err != nil {
		return ExecResult{
			Command:  command,
			Stderr:   fmt.Sprintf("Session creation failed: %v", connect.Cause(err)),
			ExitCode: 1,
		}
	}

	captured, err := connect.RunCapture(session, command, sudoInput(flags.Sudo, sudoPwd, hasSudo))
	result := ExecResult{
		Command:  command,
		Stdout:   captured.Stdout,
		Stderr:   captured.Stderr,
		ExitCode: captured.ExitCode,
	}
	if err != nil {
		result.Stderr = fmt.Sprintf("%s\nSession error: %v", captured.Stderr, connect.Cause(err))
		result.ExitCode = 1
	}
	return result
}

// sudoInput returns the password to feed `sudo -S` on the target session.
func sudoInput(sudo bool, password string, has bool) string {
	if !sudo || !has {
		return ""
	}
	return password
}

// execDialError renders a dial failure for the exec JSON `stderr` field. The
// wording is the frozen pre-refactor wording: a missing authentication method
// stands alone, everything else is prefixed with the phase it failed in.
func execDialError(err error) string {
	if errors.Is(err, connect.ErrNoAuthMethod) {
		return connect.ErrNoAuthMethod.Error()
	}
	var authErr *connect.AuthError
	if errors.As(err, &authErr) {
		return fmt.Sprintf("Authentication failed: %v", connect.Cause(err))
	}
	return fmt.Sprintf("Connection failed: %v", connect.Cause(err))
}
