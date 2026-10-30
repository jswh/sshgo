package main

import (
	"errors"
	"strings"
	"testing"
)

// A bare name is only resolved against local metadata. A miss must be a hard
// error with the three ways out spelled out, never a silent fallback that dials
// the name as a hostname.
func TestResolveTargetBareNameNotFound(t *testing.T) {
	setupTempHome(t)

	_, err := resolveTarget("typo-host")
	if !errors.Is(err, errHostNotFound) {
		t.Fatalf("resolveTarget(typo-host) error = %v, want errHostNotFound", err)
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
func TestResolveTargetDirectAddressNeverNotFound(t *testing.T) {
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
			tgt, err := resolveTarget(tc.addressed)
			if err != nil {
				t.Fatalf("resolveTarget(%q) failed: %v", tc.addressed, err)
			}
			if tgt.Info.Source != sourceDirect {
				t.Errorf("Info.Source = %q, want %q", tgt.Info.Source, sourceDirect)
			}
			if tgt.Info.Name != tc.addressed {
				t.Errorf("Info.Name = %q, want the addressed string %q", tgt.Info.Name, tc.addressed)
			}
			if tgt.Cred.Host != tc.host || tgt.Cred.User != tc.user {
				t.Errorf("Cred = %+v, want host %q user %q", tgt.Cred, tc.host, tc.user)
			}
			hostname, port, user, _ := resolveConnection(tgt.Info)
			if hostname != tc.host || port != tc.port || user != tc.user {
				t.Errorf("resolveConnection = (%q,%q,%q), want (%q,%q,%q)",
					hostname, port, user, tc.host, tc.port, tc.user)
			}
		})
	}
}

// An entry that exists but records no hostname cannot be dialed; the failure
// must surface before any connection attempt, pointing at `config set`.
func TestResolveTargetNoConnectionDetails(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if err := cfg.Set("manual-host", "notes", "created by hand"); err != nil {
		t.Fatalf("set notes: %v", err)
	}

	_, err = resolveTarget("manual-host")
	if !errors.Is(err, errNoConnectionDetails) {
		t.Fatalf("resolveTarget(manual-host) error = %v, want errNoConnectionDetails", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "config set") {
		t.Errorf("message %q does not point at `config set`", msg)
	}
}

// Multi-colon input is IPv6, which sshgo does not support. Splitting it on the
// last colon used to produce host="root@" and port="1".
func TestResolveTargetIPv6Unsupported(t *testing.T) {
	setupTempHome(t)

	for _, addressed := range []string{"root@::1", "::1", "2001:db8::1", "[::1]:22"} {
		t.Run(addressed, func(t *testing.T) {
			_, err := resolveTarget(addressed)
			if !errors.Is(err, errIPv6Unsupported) {
				t.Fatalf("resolveTarget(%q) error = %v, want errIPv6Unsupported", addressed, err)
			}
			if msg := err.Error(); !strings.Contains(msg, "hostname") {
				t.Errorf("message %q does not point at creating a Host entry", msg)
			}
		})
	}
}

// Resolution by Alias keeps the addressed name as the primary credential key.
func TestResolveTargetByAlias(t *testing.T) {
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

	tgt, err := resolveTarget("database")
	if err != nil {
		t.Fatalf("resolveTarget(database) failed: %v", err)
	}
	if tgt.Info.Name != "db1" {
		t.Errorf("Info.Name = %q, want the canonical host name %q", tgt.Info.Name, "db1")
	}
	if len(tgt.Cred.Keys) < 2 || tgt.Cred.Keys[0] != "database" || tgt.Cred.Keys[1] != "db1" {
		t.Errorf("Cred.Keys = %v, want the addressed alias first and the host name second", tgt.Cred.Keys)
	}
}
