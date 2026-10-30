package main

import "testing"

// `config remove` is the discoverable way to delete a host that import will
// never delete. It is exactly `config unset <host>` with no key: no prompt, no
// flags, so it stays usable in scripts.
func TestLocalConfigRemoveEqualsUnset(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	for _, host := range []string{"removed", "unset"} {
		if err := cfg.Set(host, "hostname", "10.0.0.5"); err != nil {
			t.Fatalf("set hostname for %s: %v", host, err)
		}
	}

	if err := cfg.Remove("removed"); err != nil {
		t.Fatalf("remove host: %v", err)
	}
	if err := cfg.Unset("unset", ""); err != nil {
		t.Fatalf("unset host: %v", err)
	}

	reloaded, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if _, ok := reloaded.Hosts["removed"]; ok {
		t.Error("host entry survived Remove")
	}
	if _, ok := reloaded.Hosts["unset"]; ok {
		t.Error("host entry survived Unset with empty key")
	}
	if got := reloaded.List(); len(got) != 0 {
		t.Errorf("List() = %v, want no hosts left", got)
	}
}

// Removing a host that is not there is not an error: scripts may remove
// idempotently.
func TestLocalConfigRemoveMissingHost(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if err := cfg.Remove("never-existed"); err != nil {
		t.Errorf("Remove(never-existed) = %v, want no error", err)
	}
}

// A removed imported host must be gone from resolution too, not just from the
// metadata file.
func TestRemoveMakesHostUnresolvable(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if err := cfg.Set("myvm", "hostname", "10.0.0.5"); err != nil {
		t.Fatalf("set hostname: %v", err)
	}
	if _, err := resolveTarget("myvm"); err != nil {
		t.Fatalf("resolveTarget(myvm) before remove: %v", err)
	}

	if err := cfg.Remove("myvm"); err != nil {
		t.Fatalf("remove host: %v", err)
	}
	if _, err := resolveTarget("myvm"); err == nil {
		t.Error("resolveTarget(myvm) still succeeded after the host was removed")
	}
}
