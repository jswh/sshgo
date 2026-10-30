package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

// ---------------------------------------------------------------------------
// Jump-chain wording. A broken chain is reported before anything is dialled, so
// the full text is assertable in-process: in the exec JSON `stderr` field, and
// on stderr for the interactive path.
// ---------------------------------------------------------------------------

func TestChainErrorText(t *testing.T) {
	const notFound = `Jump host "bastion" for "db1" not found.
  Import your SSH config:  sshgo config import
  Add it manually:         sshgo config set bastion hostname <address>
  Or connect directly:     sshgo user@host or sshgo host:port`
	const noDetails = `Jump host "bastion" for "db1" has no connection details.
  Set one: sshgo config set bastion hostname <address>`
	const unsupported = `Jump host "ssh://bastion:22" for "db1" is not an sshgo address.
  Use a Host name, an Alias, or a Direct Address (user@host:port)`

	tests := []struct {
		name string
		seed map[string]string
		want string
	}{
		{name: "jump host not found", seed: map[string]string{"proxy-jump": "bastion"}, want: notFound},
		{name: "jump host has no connection details", seed: map[string]string{"proxy-jump": "bastion", "bastion": "manual"}, want: noDetails},
		{name: "unsupported jump value", seed: map[string]string{"proxy-jump": "ssh://bastion:22"}, want: unsupported},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupTempHome(t)
			seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": tc.seed["proxy-jump"]})
			if tc.seed["bastion"] == "manual" {
				seedHost(t, "bastion", map[string]string{"notes": "created by hand"})
			}

			// exec reports through its JSON envelope.
			code, stdout, _ := run(t, "exec", "db1", "true")
			if code != 1 {
				t.Errorf("exec exit code = %d, want 1", code)
			}
			var result ExecResult
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatalf("exec output is not JSON: %v", err)
			}
			if result.Stderr != tc.want {
				t.Errorf("exec stderr =\n%q\nwant\n%q", result.Stderr, tc.want)
			}
			if result.ExitCode != 1 {
				t.Errorf("exec JSON exit_code = %d, want 1", result.ExitCode)
			}

			// The interactive path reports on stderr with exit code 1.
			code, _, stderr := run(t, "db1")
			if code != 1 {
				t.Errorf("legacy exit code = %d, want 1", code)
			}
			if stderr != "Error: "+tc.want+"\n" {
				t.Errorf("legacy stderr =\n%q\nwant\n%q", stderr, "Error: "+tc.want+"\n")
			}
		})
	}
}

// The jump chain is reported in both output shapes, and a single-hop Host is
// byte-for-byte unchanged: no `Jump:` line, no `jump` field.
func TestInfoReportsJumpChain(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": "inner"})
	seedHost(t, "inner", map[string]string{"hostname": "10.0.0.2", "proxy-jump": "bastion"})
	seedHost(t, "bastion", map[string]string{"hostname": "10.0.0.9", "user": "ops"})

	t.Run("human readable", func(t *testing.T) {
		code, stdout, stderr := run(t, "info", "db1")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
		}
		want := "  Jump:     bastion -> inner\n"
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout =\n%s\nwant it to contain %q", stdout, want)
		}
		// The Jump line sits between the connection details and Auth.
		if strings.Index(stdout, "Jump:") > strings.Index(stdout, "Auth:") {
			t.Error("the Jump line appears after Auth; it belongs with the connection details")
		}
	})

	t.Run("json", func(t *testing.T) {
		// Give the target an identity file and the hop none, so "top-level
		// auth_methods describes the target" is observable.
		keyPath := filepath.Join(setupTempHome(t), "target_key")
		if err := os.WriteFile(keyPath, []byte("not a key"), 0600); err != nil {
			t.Fatalf("write key file: %v", err)
		}
		seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": "inner", "identity-file": keyPath})
		seedHost(t, "inner", map[string]string{"hostname": "10.0.0.2", "proxy-jump": "bastion"})
		seedHost(t, "bastion", map[string]string{"hostname": "10.0.0.9", "user": "ops"})

		code, stdout, _ := run(t, "info", "--json", "db1")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}

		var result struct {
			AuthMethods []string `json:"auth_methods"`
			Jump        []struct {
				Name        string   `json:"name"`
				Hostname    string   `json:"hostname"`
				Port        string   `json:"port"`
				User        string   `json:"user"`
				AuthMethods []string `json:"auth_methods"`
			} `json:"jump"`
		}
		if err := json.Unmarshal([]byte(stdout), &result); err != nil {
			t.Fatalf("info --json is not JSON: %v", err)
		}

		var names []string
		for _, hop := range result.Jump {
			names = append(names, hop.Name)
		}
		if want := []string{"bastion", "inner"}; !slices.Equal(names, want) {
			t.Errorf("jump = %v, want %v (outermost first)", names, want)
		}
		if len(result.Jump) == 2 && result.Jump[0].User != "ops" {
			t.Errorf("jump[0].user = %q, want the hop's own user", result.Jump[0].User)
		}
		if len(result.Jump) == 2 && result.Jump[1].Port != "22" {
			t.Errorf("jump[1].port = %q, want the default port", result.Jump[1].Port)
		}
		// The top-level auth_methods still describes the target only.
		if !slices.Contains(result.AuthMethods, "IdentityFile (key)") {
			t.Errorf("target auth_methods = %v, want the target's identity file", result.AuthMethods)
		}
		for _, hop := range result.Jump {
			if slices.Contains(hop.AuthMethods, "IdentityFile (key)") {
				t.Errorf("hop %q auth_methods = %v, want the hop's own methods", hop.Name, hop.AuthMethods)
			}
		}
	})

	t.Run("single hop is unchanged", func(t *testing.T) {
		seedHost(t, "plain", map[string]string{"hostname": "10.0.0.7"})

		_, human, _ := run(t, "info", "plain")
		if strings.Contains(human, "Jump") {
			t.Errorf("stdout =\n%s\nwant no Jump line for a direct Host", human)
		}

		code, stdout, _ := run(t, "info", "--json", "plain")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
			t.Fatalf("info --json is not JSON: %v", err)
		}
		if _, ok := raw["jump"]; ok {
			t.Error("the jump field appeared for a Host with no jump chain")
		}
		want := []string{"auth_methods", "hostname", "meta", "name", "port", "source", "user"}
		if got := sortedKeys(raw); !slices.Equal(got, want) {
			t.Errorf("info --json keys = %v, want %v", got, want)
		}
	})
}

// A jump host may be an Alias or a Direct Address: one addressing grammar for
// the whole project, so a value that resolves is never rejected.
func TestProxyJumpAcceptsAliasAndDirectAddress(t *testing.T) {
	for _, value := range []string{"bastion-alias", "root@10.0.0.9:2222"} {
		t.Run(value, func(t *testing.T) {
			setupTempHome(t)
			seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": value})
			seedHost(t, "bastion", map[string]string{"hostname": "10.0.0.9", "alias": "bastion-alias"})

			if code, _, stderr := run(t, "info", "db1"); code != 0 {
				t.Errorf("info db1 exit code = %d, want 0 (stderr %q)", code, stderr)
			}
		})
	}
}

// A loop is a parse-time failure, so `info` reports it without dialling and the
// path itself is the diagnosis.
func TestChainLoopText(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": "bastion"})
	seedHost(t, "bastion", map[string]string{"hostname": "10.0.0.9", "proxy-jump": "db1"})

	code, stdout, _ := run(t, "exec", "db1", "true")
	if code != 1 {
		t.Errorf("exec exit code = %d, want 1", code)
	}
	var result ExecResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("exec output is not JSON: %v", err)
	}
	want := `ProxyJump chain for "db1" loops: db1 -> bastion -> db1`
	if result.Stderr != want {
		t.Errorf("exec stderr = %q, want %q", result.Stderr, want)
	}

	code, _, stderr := run(t, "db1")
	if code != 1 {
		t.Errorf("legacy exit code = %d, want 1", code)
	}
	if stderr != "Error: "+want+"\n" {
		t.Errorf("legacy stderr = %q, want the loop text", stderr)
	}
}

// A deeper chain resolves through every hop, and a break in the middle names the
// hop that is missing and the Host the user addressed.
func TestChainDepthIsUnlimited(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": "inner"})
	seedHost(t, "inner", map[string]string{"hostname": "10.0.0.2", "proxy-jump": "edge"})
	seedHost(t, "edge", map[string]string{"hostname": "10.0.0.1"})

	if code, _, stderr := run(t, "info", "db1"); code != 0 {
		t.Fatalf("info db1 exit code = %d, want 0 (stderr %q)", code, stderr)
	}

	seedHost(t, "inner", map[string]string{"proxy-jump": "ghost"})
	code, stdout, _ := run(t, "exec", "db1", "true")
	if code != 1 {
		t.Errorf("exec exit code = %d, want 1", code)
	}
	var result ExecResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("exec output is not JSON: %v", err)
	}
	if !strings.Contains(result.Stderr, `Jump host "ghost" for "db1" not found.`) {
		t.Errorf("exec stderr = %q, want the break named by hop and target", result.Stderr)
	}
}

// `exec` never reads stdin, not even to answer an authentication prompt: a CI
// job must fail fast instead of hanging. A chain hop with no credential is the
// case that used to be silent.
func TestExecDoesNotReadStdinOnMissingHopAuth(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": "bastion"})
	seedHost(t, "bastion", map[string]string{"hostname": "127.0.0.1", "port": "1"})

	// A closed stdin makes any read fail immediately, so a prompt would show up
	// as a different error than the expected one.
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	writer.Close()
	oldStdin := os.Stdin
	os.Stdin = reader
	defer func() { os.Stdin = oldStdin; reader.Close() }()

	code, stdout, _ := run(t, "exec", "db1", "true")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}

	var result ExecResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("exec output is not JSON: %v", err)
	}
	if !strings.Contains(result.Stderr, `Jump host "bastion"`) {
		t.Errorf("exec stderr = %q, want the jump failure named", result.Stderr)
	}
}

// A hop with no authentication method at all gets a message naming it and a
// command that fixes it, because "no authentication method" alone does not say
// which end of the chain to look at.
func TestExecNamesTheHopWithoutAuth(t *testing.T) {
	setupTempHome(t)
	seedHost(t, "db1", map[string]string{"hostname": "10.0.0.5", "proxy-jump": "bastion"})
	// The bastion resolves and is dialled; nothing can authenticate to it.
	seedHost(t, "bastion", map[string]string{"hostname": "127.0.0.1", "port": "1"})

	code, stdout, _ := run(t, "exec", "db1", "true")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	var result ExecResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("exec output is not JSON: %v", err)
	}
	// Either wording is acceptable here as long as the hop is named: the bastion
	// may fail to connect before authentication is even attempted.
	if !strings.Contains(result.Stderr, "bastion") {
		t.Errorf("exec stderr = %q, want the bastion named", result.Stderr)
	}
}
