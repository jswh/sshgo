package host

import (
	"errors"
	"strings"
	"testing"
)

// A bare name is only resolved against local metadata. A miss must be a hard
// error with the three ways out spelled out, never a silent fallback that dials
// the name as a hostname.
func TestResolveBareNameNotFound(t *testing.T) {
	setupTempHome(t)

	_, err := Resolve("typo-host")
	if !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("Resolve(typo-host) error = %v, want ErrHostNotFound", err)
	}

	msg := err.Error()
	for _, want := range []string{"typo-host", "config import", "config set", "user@host"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
}

// Direct Addresses are sshgo's own syntax: they are dialed as given even when
// no local metadata entry exists.
func TestResolveDirectAddressNeverNotFound(t *testing.T) {
	setupTempHome(t)

	tests := []struct {
		addressed string
		host      string
		port      string
		user      string
	}{
		{addressed: "root@10.0.0.5", host: "10.0.0.5", port: "22", user: "root"},
		{addressed: "10.0.0.5:2222", host: "10.0.0.5", port: "2222", user: "root"},
		{addressed: "deploy@10.0.0.5:2222", host: "10.0.0.5", port: "2222", user: "deploy"},
		{addressed: "deploy@db.example.com", host: "db.example.com", port: "22", user: "deploy"},
	}

	for _, tc := range tests {
		t.Run(tc.addressed, func(t *testing.T) {
			tgt, err := Resolve(tc.addressed)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", tc.addressed, err)
			}
			if tgt.Source != SourceDirect {
				t.Errorf("Source = %q, want %q", tgt.Source, SourceDirect)
			}
			if tgt.Name != tc.addressed {
				t.Errorf("Name = %q, want the addressed string %q", tgt.Name, tc.addressed)
			}
			if tgt.Hostname != tc.host || tgt.User != tc.user {
				t.Errorf("resolved = (%q, %q), want host %q user %q", tgt.Hostname, tgt.User, tc.host, tc.user)
			}
			if tgt.Hostname != tc.host || tgt.Port != tc.port || tgt.User != tc.user {
				t.Errorf("resolved = (%q,%q,%q), want (%q,%q,%q)",
					tgt.Hostname, tgt.Port, tgt.User, tc.host, tc.port, tc.user)
			}
		})
	}
}

// An entry that exists but records no hostname cannot be dialed; the failure
// must surface before any connection attempt, pointing at `config set`.
func TestResolveNoConnectionDetails(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if err := cfg.Set("manual-host", "notes", "created by hand"); err != nil {
		t.Fatalf("set notes: %v", err)
	}

	_, err = Resolve("manual-host")
	if !errors.Is(err, ErrNoConnectionDetails) {
		t.Fatalf("Resolve(manual-host) error = %v, want ErrNoConnectionDetails", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "config set") {
		t.Errorf("message %q does not point at `config set`", msg)
	}
}

// Multi-colon input is IPv6, which sshgo does not support. Splitting it on the
// last colon used to produce host="root@" and port="1".
func TestResolveIPv6Unsupported(t *testing.T) {
	setupTempHome(t)

	for _, addressed := range []string{"root@::1", "::1", "2001:db8::1", "[::1]:22"} {
		t.Run(addressed, func(t *testing.T) {
			_, err := Resolve(addressed)
			if !errors.Is(err, ErrIPv6Unsupported) {
				t.Fatalf("Resolve(%q) error = %v, want ErrIPv6Unsupported", addressed, err)
			}
			if msg := err.Error(); !strings.Contains(msg, "hostname") {
				t.Errorf("message %q does not point at creating a Host entry", msg)
			}
		})
	}
}

// Resolution by Alias keeps the addressed name as the primary credential key.
func TestResolveByAlias(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	for key, value := range map[string]string{
		"hostname": "127.0.0.1", "user": "tester", "port": "2222", "alias": "database",
	} {
		if err := cfg.Set("db1", key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	tgt, err := Resolve("database")
	if err != nil {
		t.Fatalf("Resolve(database) failed: %v", err)
	}
	if tgt.Name != "db1" {
		t.Errorf("Name = %q, want the canonical host name %q", tgt.Name, "db1")
	}
	if len(tgt.CredKeys) < 2 || tgt.CredKeys[0] != "database" || tgt.CredKeys[1] != "db1" {
		t.Errorf("CredKeys = %v, want the addressed alias first and the host name second", tgt.CredKeys)
	}
}

// Regression: `exec --sudo acchost` used to pass the resolved hostname
// (127.0.0.1) to the credential lookup, so a password saved under "acchost"
// was never found. The resolved target must carry the addressed key first.
func TestResolveUsesAddressedKey(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	for key, value := range map[string]string{"hostname": "127.0.0.1", "user": "tester", "port": "2222"} {
		if err := cfg.Set("acchost", key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	target, err := Resolve("acchost")
	if err != nil {
		t.Fatalf("Resolve(acchost) failed: %v", err)
	}
	if target.Name != "acchost" {
		t.Errorf("Name = %q, want %q", target.Name, "acchost")
	}
	if len(target.CredKeys) == 0 || target.CredKeys[0] != "acchost" {
		t.Errorf("CredKeys = %v, want the addressed key %q first", target.CredKeys, "acchost")
	}
	if !contains(target.CredKeys, "127.0.0.1") {
		t.Errorf("CredKeys = %v, want the resolved hostname as a fallback", target.CredKeys)
	}
	if target.Hostname != "127.0.0.1" {
		t.Errorf("Hostname = %q, want %q", target.Hostname, "127.0.0.1")
	}
	if target.User != "tester" {
		t.Errorf("User = %q, want %q", target.User, "tester")
	}
}

func TestResolveDirectAddressShape(t *testing.T) {
	setupTempHome(t)

	target, err := Resolve("root@10.0.0.5:2222")
	if err != nil {
		t.Fatalf("Resolve() rejected a direct address: %v", err)
	}
	if target.Source != SourceDirect {
		t.Errorf("Source = %q, want %q", target.Source, SourceDirect)
	}
	if target.Hostname != "10.0.0.5" || target.User != "root" {
		t.Errorf("resolved = %+v, want host 10.0.0.5 and user root", target)
	}
	if !contains(target.CredKeys, "10.0.0.5") {
		t.Errorf("CredKeys = %v, want the parsed hostname as a fallback", target.CredKeys)
	}
}

func TestResolveEmptyNameNotFound(t *testing.T) {
	setupTempHome(t)

	if _, err := Resolve(""); !errors.Is(err, ErrHostNotFound) {
		t.Errorf("Resolve(\"\") error = %v, want ErrHostNotFound", err)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
