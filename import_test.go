package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/kevinburke/ssh_config"

	"sshgo/internal/host"
)

// fakeSSHConfig stands in for a parsed ~/.ssh/config so import semantics can be
// tested without touching the filesystem.
type fakeSSHConfig struct {
	names  []string
	values map[string]map[string]string
}

func newFakeSSHConfig() *fakeSSHConfig {
	return &fakeSSHConfig{values: make(map[string]map[string]string)}
}

func (f *fakeSSHConfig) add(name string, values map[string]string) *fakeSSHConfig {
	f.names = append(f.names, name)
	f.values[name] = values
	return f
}

func (f *fakeSSHConfig) Get(name, key string) (string, error) {
	return f.values[name][key], nil
}

func (f *fakeSSHConfig) Names() []string { return f.names }

// Patterns that match other patterns are not Hosts; storing them produces fake
// entries whose metadata is resolved from the wrong block.
func TestImportSkipsPatterns(t *testing.T) {
	src := newFakeSSHConfig().
		add("*", map[string]string{"User": "deploy"}).
		add("*.example.com", map[string]string{"User": "web"}).
		add("web?.example.com", map[string]string{"User": "web"}).
		add("db[0-9].example.com", map[string]string{"User": "db"}).
		add("!prod.example.com", map[string]string{"User": "nope"}).
		add("prod", map[string]string{"Hostname": "10.0.0.5", "User": "deploy"})

	result := buildImportSnapshot(src, nil)

	if _, ok := result.Hosts["*.example.com"]; ok {
		t.Error("glob pattern *.example.com was imported as a host")
	}
	if _, ok := result.Hosts["web?.example.com"]; ok {
		t.Error("glob pattern web?.example.com was imported as a host")
	}
	if _, ok := result.Hosts["db[0-9].example.com"]; ok {
		t.Error("glob pattern db[0-9].example.com was imported as a host")
	}
	if _, ok := result.Hosts["!prod.example.com"]; ok {
		t.Error("negated pattern !prod.example.com was imported as a host")
	}
	if _, ok := result.Hosts["*"]; ok {
		t.Error("Host * block was imported as a host")
	}
	if _, ok := result.Hosts["prod"]; !ok {
		t.Error("concrete host prod was not imported")
	}

	var skipped []string
	for _, s := range result.Skipped {
		skipped = append(skipped, s.Pattern)
		if s.Reason == "" {
			t.Errorf("skipped pattern %q has no reason", s.Pattern)
		}
	}
	slices.Sort(skipped)
	want := []string{"*", "!prod.example.com", "*.example.com", "db[0-9].example.com", "web?.example.com"}
	slices.Sort(want)
	if !slices.Equal(skipped, want) {
		t.Errorf("skipped = %v, want %v", skipped, want)
	}

	summary := formatSkipped(result.Skipped)
	if !strings.Contains(summary, "*.example.com") || !strings.Contains(summary, "wildcard") {
		t.Errorf("summary %q does not list the skipped pattern with its reason", summary)
	}
}

func TestImportRecordsSourceImported(t *testing.T) {
	src := newFakeSSHConfig().
		add("prod", map[string]string{"Hostname": "10.0.0.5", "User": "deploy", "Port": "2222"})

	result := buildImportSnapshot(src, nil)

	meta, ok := result.Hosts["prod"]
	if !ok {
		t.Fatal("concrete host prod was not imported")
	}
	if meta.Source != host.SourceImported {
		t.Errorf("Source = %q, want %q", meta.Source, host.SourceImported)
	}
	if meta.Hostname != "10.0.0.5" || meta.User != "deploy" || meta.Port != "2222" {
		t.Errorf("imported metadata = %+v, want hostname 10.0.0.5 user deploy port 2222", meta)
	}
}

// ProxyJump is stored but unused; import must say so instead of letting the
// connection fail later.
func TestImportWarnsAboutProxyJump(t *testing.T) {
	src := newFakeSSHConfig().
		add("behind-bastion", map[string]string{"Hostname": "10.0.0.9", "ProxyJump": "bastion"}).
		add("direct", map[string]string{"Hostname": "10.0.0.10"})

	result := buildImportSnapshot(src, nil)

	meta, ok := result.Hosts["behind-bastion"]
	if !ok {
		t.Fatal("host with ProxyJump was not imported")
	}
	if meta.ProxyJump != "bastion" {
		t.Errorf("ProxyJump = %q, want it stored as %q", meta.ProxyJump, "bastion")
	}

	if len(result.Warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one for the jump host", result.Warnings)
	}
	if w := result.Warnings[0]; !strings.Contains(w, "behind-bastion") || !strings.Contains(w, "not supported") {
		t.Errorf("warning %q does not name the host and the limitation", w)
	}
}

// Import is a snapshot: re-running refreshes connection details, keeps
// user-owned metadata, and never deletes entries the config no longer has.
func TestImportIsSnapshotNotSync(t *testing.T) {
	existing := map[string]host.HostMeta{
		"prod": {
			Source:   host.SourceImported,
			Alias:    "db-prod",
			Notes:    "primary",
			Tags:     []string{"prod"},
			Hostname: "10.0.0.5",
			User:     "old",
		},
		"gone": {Source: host.SourceImported, Hostname: "10.0.0.99"},
	}

	src := newFakeSSHConfig().
		add("prod", map[string]string{"Hostname": "10.0.0.6", "User": "deploy"})

	result := buildImportSnapshot(src, existing)

	prod := result.Hosts["prod"]
	if prod.Hostname != "10.0.0.6" || prod.User != "deploy" {
		t.Errorf("connection details = (%q,%q), want refreshed from the config", prod.Hostname, prod.User)
	}
	if prod.Alias != "db-prod" || prod.Notes != "primary" || !slices.Equal(prod.Tags, []string{"prod"}) {
		t.Errorf("user metadata was not preserved: %+v", prod)
	}
	if _, ok := result.Hosts["gone"]; !ok {
		t.Error("a host missing from the SSH config was dropped; import must not delete")
	}
}

// Host * defaults are inherited: the value imported is the effective one.
// ssh resolves first-obtained-value-wins, so the specific block must come
// before the catch-all one, as in a real ~/.ssh/config.
func TestImportKeepsHostStarDefaults(t *testing.T) {
	parsed, err := ssh_config.Decode(strings.NewReader(`
Host prod
  HostName prod.example.com
  User web

Host inherit
  HostName inherit.example.com

Host *
  User deploy
  Port 2200
`))
	if err != nil {
		t.Fatalf("decode ssh config: %v", err)
	}

	result := buildImportSnapshot(sshConfigAdapter{cfg: parsed}, nil)

	prod, ok := result.Hosts["prod"]
	if !ok {
		t.Fatalf("hosts = %v, want prod imported", result.Hosts)
	}
	if prod.Hostname != "prod.example.com" || prod.User != "web" {
		t.Errorf("prod = %+v, want its own HostName and User", prod)
	}

	// A host without its own User/Port carries the Host * values.
	inherited, ok := result.Hosts["inherit"]
	if !ok {
		t.Fatalf("hosts = %v, want inherit imported", result.Hosts)
	}
	if inherited.User != "deploy" || inherited.Port != "2200" {
		t.Errorf("inherit = %+v, want the Host * defaults", inherited)
	}
}
