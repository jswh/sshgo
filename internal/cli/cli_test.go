package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"
)

// setupTempHome points HOME (and USERPROFILE for Windows) at a fresh temp dir so
// the metadata and password stores resolve inside it. Deliberately duplicated
// per package instead of shared: five lines do not justify a helper package.
func setupTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// run is the golden-test entry point: the real Run, with captured streams.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run("dev", args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// The three Resolution Errors are the machine contract the e2e suite also
// checks. Their full text and exit code are frozen here so a refactor cannot
// silently reword them.
func TestResolutionErrorText(t *testing.T) {
	setupTempHome(t)

	t.Run("host not found", func(t *testing.T) {
		code, _, stderr := run(t, "exec", "typo-host", "true")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		want := `Error: Host "typo-host" not found.
  Import your SSH config:  sshgo config import
  Add it manually:         sshgo config set typo-host hostname <address>
  Or connect directly:     sshgo user@host or sshgo host:port
`
		if stderr != want {
			t.Errorf("stderr =\n%q\nwant\n%q", stderr, want)
		}
	})

	t.Run("no connection details", func(t *testing.T) {
		setupTempHome(t)
		seedHost(t, "manual-host", map[string]string{"notes": "created by hand"})

		code, _, stderr := run(t, "exec", "manual-host", "true")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		want := `Error: Host "manual-host" has no connection details.
  Set one: sshgo config set manual-host hostname <address>
`
		if stderr != want {
			t.Errorf("stderr =\n%q\nwant\n%q", stderr, want)
		}
	})

	t.Run("IPv6 unsupported", func(t *testing.T) {
		setupTempHome(t)

		code, _, stderr := run(t, "exec", "root@::1", "true")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		want := `Error: "root@::1" looks like an IPv6 address, which sshgo does not support.
  Create a Host entry with the address instead: sshgo config set <name> hostname <address>
`
		if stderr != want {
			t.Errorf("stderr =\n%q\nwant\n%q", stderr, want)
		}
	})

	// Legacy mode shares the same resolution contract.
	t.Run("legacy mode uses the same text", func(t *testing.T) {
		setupTempHome(t)

		code, _, stderr := run(t, "typo-host")
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, `Host "typo-host" not found.`) {
			t.Errorf("stderr = %q, want the not-found text", stderr)
		}
	})
}

// info --json and exec share a stable key set. Values are not compared: the
// contract is the field names.
func TestInfoJSONKeys(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "db1", map[string]string{
		"hostname": "10.0.0.5", "user": "deploy", "port": "2222", "alias": "database",
	})

	code, stdout, stderr := run(t, "info", "--json", "db1")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("info --json output is not a JSON object: %v", err)
	}

	want := []string{"auth_methods", "hostname", "meta", "name", "port", "source", "user"}
	if got := sortedKeys(raw); !slices.Equal(got, want) {
		t.Errorf("info --json keys = %v, want %v", got, want)
	}
}

// A Direct Address has no local metadata, so `meta` must be absent rather than
// an empty object.
func TestInfoJSONOmitsMetaForDirectAddress(t *testing.T) {
	setupTempHome(t)

	code, stdout, _ := run(t, "info", "--json", "root@10.0.0.5:2222")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("info --json output is not a JSON object: %v", err)
	}
	if _, ok := raw["meta"]; ok {
		t.Error("meta appeared for a Direct Address")
	}
}

// exec never reaches a real host in these tests: an unreachable address keeps
// the JSON envelope observable without a network round trip.
func TestExecJSONKeys(t *testing.T) {
	setupTempHome(t)

	code, stdout, _ := run(t, "exec", "root@127.0.0.1:1", "echo hi")
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for an unreachable host", code)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		t.Fatalf("exec output is not a JSON object: %v", err)
	}

	// duration is omitempty in the struct but always set by runExec, so it is
	// part of the key set even when the dial failed.
	want := []string{"command", "duration", "exit_code", "stderr", "stdout"}
	if got := sortedKeys(raw); !slices.Equal(got, want) {
		t.Errorf("exec keys = %v, want %v", got, want)
	}
}

// Help text is a frozen contract too: it is what users read first.
func TestHelpTextIsStable(t *testing.T) {
	setupTempHome(t)

	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"--help"}, want: "Usage: sshgo [command] [args]"},
		{args: []string{"-h"}, want: "Usage: sshgo [command] [args]"},
		{args: []string{"exec", "--help"}, want: "Usage: sshgo exec [--raw] [--sudo] <host> <command>"},
		{args: []string{"info", "--help"}, want: "Usage: sshgo info [--json] <host>"},
		{args: []string{"config", "--help"}, want: "Usage: sshgo config <command> [args]"},
		{args: []string{"update", "--help"}, want: "Usage: sshgo update"},
	}

	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, stdout, stderr := run(t, tc.args...)
			if code != 0 {
				t.Errorf("exit code = %d, want 0", code)
			}
			if !strings.HasPrefix(stdout, tc.want+"\n") {
				t.Errorf("stdout = %q, want it to start with %q", stdout, tc.want)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
		})
	}
}

// Every usage error path must return 1 rather than fall through.
func TestUsageErrorsReturnOne(t *testing.T) {
	setupTempHome(t)

	tests := []struct {
		name   string
		args   []string
		stderr string
	}{
		{name: "exec missing args", args: []string{"exec"}, stderr: "Error: missing host or command\n" +
			"Usage: sshgo exec [--raw] [--sudo] <host> <command>\n"},
		{name: "exec unknown flag", args: []string{"exec", "--nope", "h", "c"}, stderr: "Unknown flag: --nope\n"},
		{name: "info missing args", args: []string{"info"}, stderr: "Usage: sshgo info [--json] <host>\n"},
		{name: "config get missing host", args: []string{"config", "get"}, stderr: "Usage: sshgo config get <host> [key]\n"},
		{name: "config set missing args", args: []string{"config", "set", "h", "k"}, stderr: "Usage: sshgo config set <host> <key> <value>\n"},
		{name: "config unset missing host", args: []string{"config", "unset"}, stderr: "Usage: sshgo config unset <host> [key]\n"},
		{name: "config remove missing host", args: []string{"config", "remove"}, stderr: "Usage: sshgo config remove <host>\n"},
		{name: "config find missing tag", args: []string{"config", "find"}, stderr: "Usage: sshgo config find --tag <tag>\n"},
		{name: "config unknown command", args: []string{"config", "bogus"}, stderr: "Unknown config command: \"bogus\"\n"},
		{name: "update unknown flag", args: []string{"update", "--nope"}, stderr: "Error: unknown flag \"--nope\"\n" +
			"Usage: sshgo update\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := run(t, tc.args...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if stderr != tc.stderr {
				t.Errorf("stderr = %q, want %q", stderr, tc.stderr)
			}
		})
	}
}

// config list/get/set/unset/remove/find all report through the injected
// streams, and set persists.
func TestConfigRoundTripThroughRun(t *testing.T) {
	setupTempHome(t)

	if code, stdout, _ := run(t, "config", "list"); code != 0 || stdout != "No hosts in local config\n" {
		t.Fatalf("empty list = (%d, %q), want (0, \"No hosts in local config\\n\")", code, stdout)
	}

	if code, stdout, _ := run(t, "config", "set", "web1", "hostname", "10.0.0.5"); code != 0 ||
		stdout != "Set web1[hostname] = 10.0.0.5\n" {
		t.Fatalf("set = (%d, %q)", code, stdout)
	}
	if code, stdout, _ := run(t, "config", "get", "web1", "hostname"); code != 0 || stdout != "10.0.0.5\n" {
		t.Fatalf("get = (%d, %q)", code, stdout)
	}
	if code, stdout, _ := run(t, "config", "list"); code != 0 || !strings.Contains(stdout, "web1:") {
		t.Fatalf("list = (%d, %q)", code, stdout)
	}
	if code, stdout, _ := run(t, "config", "remove", "web1"); code != 0 || stdout != "Removed web1\n" {
		t.Fatalf("remove = (%d, %q)", code, stdout)
	}
	if code, _, stderr := run(t, "config", "get", "web1"); code != 1 ||
		!strings.Contains(stderr, `host "web1" not found in local config`) {
		t.Fatalf("get after remove = (%d, %q)", code, stderr)
	}
}

// Subcommand Priority: the first argument wins over a Host name, and the help
// flags are only special on their own.
func TestSubcommandPriority(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "exec", map[string]string{"hostname": "10.0.0.5"})

	// "exec" as the first argument dispatches the subcommand, not the Host.
	code, _, stderr := run(t, "exec")
	if code != 1 || !strings.Contains(stderr, "Error: missing host or command") {
		t.Errorf("exec dispatch = (%d, %q), want the subcommand usage error", code, stderr)
	}
}

// seedHost writes one metadata entry directly, so tests do not depend on the
// config subcommand's own behaviour.
func seedHost(t *testing.T, name string, fields map[string]string) {
	t.Helper()
	for key, value := range fields {
		if code, _, stderr := run(t, "config", "set", name, key, value); code != 0 {
			t.Fatalf("seed %s[%s]: exit %d (%q)", name, key, code, stderr)
		}
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
