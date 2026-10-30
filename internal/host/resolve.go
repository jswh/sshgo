package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Typed resolution failures. Callers branch on these with errors.Is and never
// on message text.
var (
	ErrHostNotFound        = errors.New("host not found")
	ErrNoConnectionDetails = errors.New("host has no connection details")
	ErrIPv6Unsupported     = errors.New("IPv6 address unsupported")
)

// resolveError is a resolution failure that carries its user-facing message.
// The message always names the three ways out where they apply, so the user
// never has to open the docs to recover.
type resolveError struct {
	kind      error
	addressed string
}

func (e *resolveError) Error() string {
	switch e.kind {
	case ErrHostNotFound:
		return fmt.Sprintf("Host %q not found.\n"+
			"  Import your SSH config:  sshgo config import\n"+
			"  Add it manually:         sshgo config set %s hostname <address>\n"+
			"  Or connect directly:     sshgo user@host or sshgo host:port",
			e.addressed, e.addressed)
	case ErrNoConnectionDetails:
		return fmt.Sprintf("Host %q has no connection details.\n"+
			"  Set one: sshgo config set %s hostname <address>",
			e.addressed, e.addressed)
	case ErrIPv6Unsupported:
		return fmt.Sprintf("%q looks like an IPv6 address, which sshgo does not support.\n"+
			"  Create a Host entry with the address instead: sshgo config set <name> hostname <address>",
			e.addressed)
	}
	return e.addressed
}

func (e *resolveError) Unwrap() error { return e.kind }

// Resolved is a dialable Host: the resolved connection details, the ordered
// password-store keys for it, and the local metadata it came from (absent for
// Direct Addresses).
type Resolved struct {
	// Name is the canonical Host name, or the addressed string for a Direct
	// Address.
	Name string
	// Addressed is what the user typed; it leads CredKeys so an Alias lookup
	// still finds credentials saved under the alias.
	Addressed string

	Source       string
	Hostname     string
	Port         string
	User         string
	IdentityFile string
	ProxyJump    string

	// CredKeys is ordered: addressed key first, then the canonical Host name
	// (they differ when an Alias is used), then the resolved hostname as a
	// fallback for entries saved before this ordering was fixed. It is never
	// empty: the first element is the key a newly entered password is saved
	// under.
	CredKeys []string

	Meta HostMeta
}

// Resolve is the single resolution entry point shared by legacy mode, `exec`,
// and `info`.
//
// Input containing "@" or ":" is a Direct Address: sshgo's own syntax, dialed
// as given, never reported as not found. A bare name is resolved against local
// metadata only and never falls back to dialing the name itself.
func Resolve(addressed string) (Resolved, error) {
	if strings.ContainsAny(addressed, "@:") {
		return resolveDirectAddress(addressed)
	}
	return resolveNamedHost(addressed)
}

func resolveDirectAddress(addressed string) (Resolved, error) {
	// sshgo speaks IPv4 only. Splitting "root@::1" on its last colon used to
	// yield host "root@" and port "1", so reject anything with several colons.
	if strings.Count(addressed, ":") > 1 {
		return Resolved{}, &resolveError{kind: ErrIPv6Unsupported, addressed: addressed}
	}
	return newResolved(addressed, addressed, SourceDirect, HostMeta{}), nil
}

func resolveNamedHost(addressed string) (Resolved, error) {
	lc, err := LoadLocalConfig()
	if err != nil {
		return Resolved{}, err
	}

	name := addressed
	meta, ok := lc.Hosts[name]
	if !ok {
		if name, meta, ok = findByAlias(lc, addressed); !ok {
			return Resolved{}, &resolveError{kind: ErrHostNotFound, addressed: addressed}
		}
	}

	// Decide this before any dial attempt: an entry without an address is a
	// configuration error, not a connection error.
	if meta.Hostname == "" {
		return Resolved{}, &resolveError{kind: ErrNoConnectionDetails, addressed: addressed}
	}

	return newResolved(addressed, name, deriveSource(meta), meta), nil
}

// newResolved keeps the addressed name as the primary Credential Key so
// credentials saved by `config set-sudo-password <host>` are found, and fills
// in the defaults every dialer relies on.
func newResolved(addressed, canonical, source string, meta HostMeta) Resolved {
	resolved := Resolved{
		Name:         canonical,
		Addressed:    addressed,
		Source:       source,
		Hostname:     meta.Hostname,
		Port:         meta.Port,
		User:         meta.User,
		IdentityFile: meta.IdentityFile,
		ProxyJump:    meta.ProxyJump,
		Meta:         meta,
	}

	// Direct Addresses carry their details in the name string itself.
	if source == SourceDirect {
		resolved.User, resolved.Hostname, resolved.Port = ParseDirectAddress(addressed)
	}

	if resolved.Hostname == "" {
		resolved.Hostname = resolved.Name
	}
	if resolved.Port == "" {
		resolved.Port = "22"
	}
	if resolved.User == "" {
		resolved.User = "root"
	}

	resolved.CredKeys = credentialKeys(addressed, resolved.Name, resolved.Hostname)
	return resolved
}

// credentialKeys builds the ordered, de-duplicated lookup key list.
func credentialKeys(addressed, canonical, hostname string) []string {
	keys := make([]string, 0, 3)
	for _, k := range []string{addressed, canonical, hostname} {
		if k == "" || slices.Contains(keys, k) {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// findByAlias maps an Alias back to its canonical Host name.
func findByAlias(lc *LocalConfig, alias string) (string, HostMeta, bool) {
	for name, meta := range lc.Hosts {
		if meta.Alias == alias {
			return name, meta, true
		}
	}
	return "", HostMeta{}, false
}

// ParseDirectAddress splits "user@host:port" into its parts. Callers must have
// rejected IPv6 input first, otherwise the port split is meaningless.
func ParseDirectAddress(s string) (user, host, port string) {
	if idx := strings.Index(s, "@"); idx >= 0 {
		user = s[:idx]
		s = s[idx+1:]
	}

	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		return user, s[:idx], s[idx+1:]
	}
	return user, s, ""
}

// ExpandPath expands a leading "~" to the user's home directory.
func ExpandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[1:])
	}
	return path
}
