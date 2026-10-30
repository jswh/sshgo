package cli

import (
	"errors"
	"fmt"
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

func (c *CLI) runExec(args []string) int {
	// Check for help before parsing flags
	if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(c.Out, "Usage: sshgo exec [--raw] [--sudo] <host> <command>")
		fmt.Fprintln(c.Out)
		fmt.Fprintln(c.Out, "Execute a remote command with structured JSON output.")
		fmt.Fprintln(c.Out)
		fmt.Fprintln(c.Out, "Flags:")
		fmt.Fprintln(c.Out, "  --raw     Output raw stdout/stderr stream (no JSON wrapper)")
		fmt.Fprintln(c.Out, "  --sudo    Execute command with sudo (reuses SSH password)")
		fmt.Fprintln(c.Out)
		fmt.Fprintln(c.Out, "Examples:")
		fmt.Fprintln(c.Out, "  sshgo exec db-staging \"SELECT count(*) FROM users\"")
		fmt.Fprintln(c.Out, "  sshgo exec --raw web-01 \"tail -n 100 /var/log/nginx/access.log\"")
		fmt.Fprintln(c.Out, "  sshgo exec --sudo db-staging \"systemctl restart postgresql\"")
		return 0
	}

	flags, args, code, ok := c.parseExecFlags(args)
	if !ok {
		return code
	}

	if len(args) < 2 {
		fmt.Fprintln(c.Err, "Error: missing host or command")
		fmt.Fprintln(c.Err, "Usage: sshgo exec [--raw] [--sudo] <host> <command>")
		return 1
	}

	hostName := args[0]
	command := strings.Join(args[1:], " ")

	target, err := host.Resolve(hostName)
	if err != nil {
		return c.resolveError(err)
	}

	start := time.Now()
	result := c.executeRemote(target, command, flags)
	result.Duration = time.Since(start).Round(time.Millisecond).String()

	if flags.Raw {
		c.Out.Write([]byte(result.Stdout))
		c.Err.Write([]byte(result.Stderr))
		return result.ExitCode
	}

	fmt.Fprintln(c.Out, result.JSON())
	return result.ExitCode
}

// parseExecFlags strips the recognised flags from args, returning the remaining
// positional arguments. The boolean is false when an unknown flag already failed
// the command, in which case code is the exit code to return.
func (c *CLI) parseExecFlags(args []string) (execFlags, []string, int, bool) {
	var flags execFlags
	filtered := make([]string, 0, len(args))

	for _, a := range args {
		switch {
		case a == "--raw":
			flags.Raw = true
		case a == "--sudo":
			flags.Sudo = true
		case strings.HasPrefix(a, "--"):
			fmt.Fprintf(c.Err, "Unknown flag: %s\n", a)
			return execFlags{}, nil, 1, false
		default:
			filtered = append(filtered, a)
		}
	}

	return flags, filtered, 0, true
}

// executeRemote dials the host, runs a command, and returns structured output.
// The sudo password reaches the session through a separate exit, never through
// the authentication methods.
func (c *CLI) executeRemote(target host.Resolved, command string, flags execFlags) ExecResult {
	auth := &execAuth{cli: c, needSudo: flags.Sudo}

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
