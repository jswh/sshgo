package main

import "testing"

// The lookups take an ordered key list from the caller and return the first
// hit. Key ordering itself belongs to the host package; these tests only pin
// that the store is consulted in the given order.

func TestLookupSudoPasswordUsesHostKey(t *testing.T) {
	setupTempHome(t)

	store, err := LoadPasswordStore("master-pass")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.SetSudoPassword("acchost", "host-key-pw"); err != nil {
		t.Fatalf("set sudo password: %v", err)
	}

	keys := []string{"acchost", "127.0.0.1"}
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

	keys := []string{"acchost", "127.0.0.1"}
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

	keys := []string{"acchost", "127.0.0.1"}
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

	keys := []string{"acchost", "127.0.0.1"}
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

	keys := []string{"acchost", "127.0.0.1"}
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
