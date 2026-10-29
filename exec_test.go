package main

import (
	"slices"
	"testing"
)

// The password store is keyed by the name the user addressed
// (`config set-sudo-password acchost` writes "acchost/sudo"), while the resolved
// hostname (127.0.0.1) is only a legacy fallback. exec must therefore look up
// credentials by the addressed key first.
func TestCredentialKeysPriority(t *testing.T) {
	tests := []struct {
		name      string
		addressed string
		canonical string
		hostname  string
		want      []string
	}{
		{
			name:      "addressed key first, hostname last",
			addressed: "acchost",
			canonical: "acchost",
			hostname:  "127.0.0.1",
			want:      []string{"acchost", "127.0.0.1"},
		},
		{
			name:      "alias addressed, canonical host name in the middle",
			addressed: "database",
			canonical: "db1",
			hostname:  "10.0.0.5",
			want:      []string{"database", "db1", "10.0.0.5"},
		},
		{
			name:      "direct address key plus parsed hostname",
			addressed: "root@10.0.0.5:2222",
			canonical: "root@10.0.0.5:2222",
			hostname:  "10.0.0.5",
			want:      []string{"root@10.0.0.5:2222", "10.0.0.5"},
		},
		{
			name:      "empty identifiers are dropped",
			addressed: "acchost",
			canonical: "",
			hostname:  "",
			want:      []string{"acchost"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := credentialKeys(tc.addressed, tc.canonical, tc.hostname)
			if !slices.Equal(got, tc.want) {
				t.Errorf("credentialKeys() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Regression: `exec --sudo acchost` used to pass the resolved hostname
// (127.0.0.1) to the credential lookup, so a password saved under "acchost"
// was never found. The resolved target must carry the addressed key first.
func TestResolveExecTargetUsesAddressedKey(t *testing.T) {
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

	target, ok := resolveExecTarget("acchost")
	if !ok {
		t.Fatal("resolveExecTarget(acchost) reported the host as not found")
	}
	if target.Info.Name != "acchost" {
		t.Errorf("Info.Name = %q, want %q", target.Info.Name, "acchost")
	}
	if len(target.Cred.Keys) == 0 || target.Cred.Keys[0] != "acchost" {
		t.Errorf("Cred.Keys = %v, want the addressed key %q first", target.Cred.Keys, "acchost")
	}
	if !slices.Contains(target.Cred.Keys, "127.0.0.1") {
		t.Errorf("Cred.Keys = %v, want the resolved hostname as a fallback", target.Cred.Keys)
	}
	if target.Cred.Host != "127.0.0.1" {
		t.Errorf("Cred.Host = %q, want %q", target.Cred.Host, "127.0.0.1")
	}
	if target.Cred.User != "tester" {
		t.Errorf("Cred.User = %q, want %q", target.Cred.User, "tester")
	}
}

func TestResolveExecTargetDirectAddress(t *testing.T) {
	setupTempHome(t)

	target, ok := resolveExecTarget("root@10.0.0.5:2222")
	if !ok {
		t.Fatal("resolveExecTarget() reported a direct address as not found")
	}
	if target.Info.Source != "direct" {
		t.Errorf("Info.Source = %q, want %q", target.Info.Source, "direct")
	}
	if target.Cred.Host != "10.0.0.5" || target.Cred.User != "root" {
		t.Errorf("Cred = %+v, want host 10.0.0.5 and user root", target.Cred)
	}
	if !slices.Contains(target.Cred.Keys, "10.0.0.5") {
		t.Errorf("Cred.Keys = %v, want the parsed hostname as a fallback", target.Cred.Keys)
	}
}

func TestResolveExecTargetUnknownName(t *testing.T) {
	setupTempHome(t)

	if _, ok := resolveExecTarget(""); ok {
		t.Error("resolveExecTarget(\"\") reported found, want not found")
	}
}

func TestLookupSudoPasswordUsesHostKey(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetSudoPassword("acchost", "host-key-pw"); err != nil {
		t.Fatalf("set sudo password: %v", err)
	}

	keys := credentialKeys("acchost", "acchost", "127.0.0.1")
	got, ok := lookupSudoPassword(store, keys)
	if !ok {
		t.Fatalf("lookupSudoPassword(%v) missed a password saved under the host key", keys)
	}
	if got != "host-key-pw" {
		t.Error("lookupSudoPassword returned a different password than the one stored")
	}
}

func TestLookupSudoPasswordFallsBackToHostname(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	// Legacy entry: saved under the resolved hostname before the key fix.
	if err := store.SetSudoPassword("127.0.0.1", "legacy-pw"); err != nil {
		t.Fatalf("set sudo password: %v", err)
	}

	keys := credentialKeys("acchost", "acchost", "127.0.0.1")
	got, ok := lookupSudoPassword(store, keys)
	if !ok {
		t.Fatalf("lookupSudoPassword(%v) missed a legacy hostname-keyed password", keys)
	}
	if got != "legacy-pw" {
		t.Error("lookupSudoPassword returned a different password than the one stored")
	}
}

func TestLookupSudoPasswordPrefersHostKeyOverHostname(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetSudoPassword("127.0.0.1", "legacy-pw"); err != nil {
		t.Fatalf("set legacy sudo password: %v", err)
	}
	if err := store.SetSudoPassword("acchost", "host-key-pw"); err != nil {
		t.Fatalf("set sudo password: %v", err)
	}

	keys := credentialKeys("acchost", "acchost", "127.0.0.1")
	got, ok := lookupSudoPassword(store, keys)
	if !ok {
		t.Fatalf("lookupSudoPassword(%v) missed the stored password", keys)
	}
	if got != "host-key-pw" {
		t.Error("lookupSudoPassword preferred the hostname key over the host key")
	}
}

// The interactive prompt saves under the addressed key; that choice must be
// readable by the lookup path, otherwise saving then re-running loses it.
func TestSudoPasswordSavedUnderAddressedKeyIsFound(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	keys := credentialKeys("acchost", "acchost", "127.0.0.1")
	if err := store.SetSudoPassword(keys[0], "prompted-pw"); err != nil {
		t.Fatalf("set sudo password: %v", err)
	}

	got, ok := lookupSudoPassword(store, keys)
	if !ok || got != "prompted-pw" {
		t.Fatal("a password saved under the first credential key was not found by the lookup")
	}
}

func TestLookupSavedPasswordUsesHostKey(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.Set("acchost", "ssh-pw"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	keys := credentialKeys("acchost", "acchost", "127.0.0.1")
	got, ok := lookupSavedPassword(store, keys)
	if !ok {
		t.Fatalf("lookupSavedPassword(%v) missed a password saved under the host key", keys)
	}
	if got != "ssh-pw" {
		t.Error("lookupSavedPassword returned a different password than the one stored")
	}

	if _, ok := lookupSavedPassword(store, []string{"127.0.0.1"}); ok {
		t.Error("lookupSavedPassword hit a key that was never stored")
	}
}
