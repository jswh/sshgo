package host

import (
	"os"
	"path/filepath"
	"testing"
)

// `config set` on a host that does not exist yet creates a manual entry: the
// user made it by hand, not by importing.
func TestSourceSetCreatesManualEntry(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	if err := cfg.Set("myvm", "hostname", "192.168.1.10"); err != nil {
		t.Fatalf("set hostname: %v", err)
	}

	reloaded, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := reloaded.Hosts["myvm"].Source; got != SourceManual {
		t.Errorf("Source = %q, want %q", got, SourceManual)
	}
}

// Editing an imported host must not rewrite its provenance: the source is where
// the entry came from, not who touched it last.
func TestSourceSurvivesEdits(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	cfg.Hosts["db1"] = HostMeta{Hostname: "10.0.0.5", Source: SourceImported}
	if err := cfg.Save(); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cfg, err = LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	for key, value := range map[string]string{
		"hostname": "10.0.0.6", "user": "deploy", "tags": "prod", "notes": "primary",
	} {
		if err := cfg.Set("db1", key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	reloaded, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := reloaded.Hosts["db1"].Source; got != SourceImported {
		t.Errorf("Source = %q after edits, want %q", got, SourceImported)
	}
}

// Files written before Source was persisted carry no source field. They are
// derived on read and written back on the next save, so no migration script is
// needed.
func TestSourceDerivedForLegacyFiles(t *testing.T) {
	home := setupTempHome(t)

	legacy := `{
  "version": "1",
  "hosts": {
    "legacy-imported": {"hostname": "10.0.0.5", "user": "deploy"},
    "legacy-manual": {"notes": "created by hand"}
  }
}`
	path := filepath.Join(home, ".sshgo_config")
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	if got := cfg.Hosts["legacy-imported"].Source; got != SourceImported {
		t.Errorf("legacy-imported Source = %q, want %q", got, SourceImported)
	}
	if got := cfg.Hosts["legacy-manual"].Source; got != SourceManual {
		t.Errorf("legacy-manual Source = %q, want %q", got, SourceManual)
	}

	// A save persists the derived values.
	if err := cfg.Set("legacy-manual", "notes", "still by hand"); err != nil {
		t.Fatalf("set notes: %v", err)
	}
	reloaded, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	for name, want := range map[string]string{
		"legacy-imported": SourceImported,
		"legacy-manual":   SourceManual,
	} {
		if got := reloaded.Hosts[name].Source; got != want {
			t.Errorf("%s Source after save = %q, want %q", name, got, want)
		}
	}
}

// The alias path reports the stored provenance instead of hardcoding
// "imported".
func TestSourceReportedThroughAlias(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load local config: %v", err)
	}
	for key, value := range map[string]string{
		"hostname": "192.168.1.10", "alias": "vm", "user": "admin",
	} {
		if err := cfg.Set("myvm", key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	target, err := Resolve("vm")
	if err != nil {
		t.Fatalf("Resolve(vm) failed: %v", err)
	}
	if got := target.Source; got != SourceManual {
		t.Errorf("Source = %q, want %q", got, SourceManual)
	}
}
