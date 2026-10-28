package main

import (
	"os"
	"testing"
)

func TestLocalConfigRoundtrip(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if len(cfg.Hosts) != 0 {
		t.Fatalf("expected empty config, got %d hosts", len(cfg.Hosts))
	}

	if err := cfg.Set("web1", "hostname", "web1.example.com"); err != nil {
		t.Fatalf("set hostname: %v", err)
	}
	if err := cfg.Set("web1", "port", "2222"); err != nil {
		t.Fatalf("set port: %v", err)
	}
	if err := cfg.Set("web1", "tags", "prod, web"); err != nil {
		t.Fatalf("set tags: %v", err)
	}
	if err := cfg.Set("db1", "tags", "prod"); err != nil {
		t.Fatalf("set tags: %v", err)
	}
	if err := cfg.Set("db1", "alias", "database"); err != nil {
		t.Fatalf("set alias: %v", err)
	}

	reloaded, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}

	if got, err := reloaded.Get("web1", "hostname"); err != nil || got != "web1.example.com" {
		t.Errorf("hostname = %q (err %v), want %q", got, err, "web1.example.com")
	}
	if got, err := reloaded.Get("web1", "port"); err != nil || got != "2222" {
		t.Errorf("port = %q (err %v), want %q", got, err, "2222")
	}
	if got, err := reloaded.Get("web1", "tags"); err != nil || got != "prod, web" {
		t.Errorf("tags = %q (err %v), want %q", got, err, "prod, web")
	}

	if got := reloaded.FindByTag("prod"); len(got) != 2 || got[0] != "db1" || got[1] != "web1" {
		t.Errorf("FindByTag(prod) = %v, want [db1 web1]", got)
	}
	if got := reloaded.FindByTag("web"); len(got) != 1 || got[0] != "web1" {
		t.Errorf("FindByTag(web) = %v, want [web1]", got)
	}
	if got := reloaded.FindByTag("missing"); len(got) != 0 {
		t.Errorf("FindByTag(missing) = %v, want empty", got)
	}

	if got := reloaded.List(); len(got) != 2 || got[0] != "db1" || got[1] != "web1" {
		t.Errorf("List() = %v, want [db1 web1]", got)
	}
}

func TestLocalConfigUnset(t *testing.T) {
	setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if err := cfg.Set("web1", "hostname", "web1.example.com"); err != nil {
		t.Fatalf("set hostname: %v", err)
	}
	if err := cfg.Set("web1", "notes", "primary"); err != nil {
		t.Fatalf("set notes: %v", err)
	}

	if err := cfg.Unset("web1", "hostname"); err != nil {
		t.Fatalf("unset hostname: %v", err)
	}
	reloaded, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got, err := reloaded.Get("web1", "hostname"); err != nil || got != "" {
		t.Errorf("hostname = %q (err %v), want empty", got, err)
	}
	if got, err := reloaded.Get("web1", "notes"); err != nil || got != "primary" {
		t.Errorf("notes = %q (err %v), want %q", got, err, "primary")
	}

	if err := reloaded.Unset("web1", ""); err != nil {
		t.Fatalf("unset host: %v", err)
	}
	after, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if _, ok := after.Hosts["web1"]; ok {
		t.Error("host entry survived Unset with empty key")
	}
}

func TestLocalConfigSaveUsesStablePath(t *testing.T) {
	home := setupTempHome(t)

	cfg, err := LoadLocalConfig()
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if err := cfg.Set("web1", "notes", "primary"); err != nil {
		t.Fatalf("set notes: %v", err)
	}

	path := getLocalConfigPath()
	if want := home + string(os.PathSeparator) + ".sshgo_config"; path != want {
		t.Errorf("config path = %q, want %q", path, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not written at %q: %v", path, err)
	}
}
