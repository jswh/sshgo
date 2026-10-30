package main

import (
	"errors"
	"fmt"
	"strings"
)

// Source records where a Host came from. It answers "where did this entry
// originate", not "who edited it last".
const (
	sourceImported = "imported"
	sourceDirect   = "direct"
	sourceManual   = "manual"
)

// Typed resolution failures. Callers branch on these with errors.Is and never
// on message text.
var (
	errHostNotFound        = errors.New("host not found")
	errNoConnectionDetails = errors.New("host has no connection details")
	errIPv6Unsupported     = errors.New("IPv6 address unsupported")
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
	case errHostNotFound:
		return fmt.Sprintf("Host %q not found.\n"+
			"  Import your SSH config:  sshgo config import\n"+
			"  Add it manually:         sshgo config set %s hostname <address>\n"+
			"  Or connect directly:     sshgo user@host or sshgo host:port",
			e.addressed, e.addressed)
	case errNoConnectionDetails:
		return fmt.Sprintf("Host %q has no connection details.\n"+
			"  Set one: sshgo config set %s hostname <address>",
			e.addressed, e.addressed)
	case errIPv6Unsupported:
		return fmt.Sprintf("%q looks like an IPv6 address, which sshgo does not support.\n"+
			"  Create a Host entry with the address instead: sshgo config set <name> hostname <address>",
			e.addressed)
	}
	return e.addressed
}

func (e *resolveError) Unwrap() error { return e.kind }

// resolvedTarget is a dialable Host plus the credential lookup keys for it and
// the local metadata it was resolved from (absent for Direct Addresses).
type resolvedTarget struct {
	Info hostInfo
	Cred credentialTarget
	Meta HostMeta
}

// resolveTarget is the single resolution entry point shared by legacy mode,
// `exec`, and `info`.
//
// Input containing "@" or ":" is a Direct Address: sshgo's own syntax, dialed
// as given, never reported as not found. A bare name is resolved against local
// metadata only and never falls back to dialing the name itself.
func resolveTarget(addressed string) (resolvedTarget, error) {
	if strings.ContainsAny(addressed, "@:") {
		return resolveDirectAddress(addressed)
	}
	return resolveNamedHost(addressed)
}

func resolveDirectAddress(addressed string) (resolvedTarget, error) {
	// sshgo speaks IPv4 only. Splitting "root@::1" on its last colon used to
	// yield host "root@" and port "1", so reject anything with several colons.
	if strings.Count(addressed, ":") > 1 {
		return resolvedTarget{}, &resolveError{kind: errIPv6Unsupported, addressed: addressed}
	}

	info := hostInfo{Name: addressed, Source: sourceDirect}
	hostname, _, user, _ := resolveConnection(info)
	return newResolvedTarget(addressed, info, hostname, user, HostMeta{}), nil
}

func resolveNamedHost(addressed string) (resolvedTarget, error) {
	lc, err := LoadLocalConfig()
	if err != nil {
		return resolvedTarget{}, err
	}

	name := addressed
	meta, ok := lc.Hosts[name]
	if !ok {
		if name, meta, ok = findByAlias(lc, addressed); !ok {
			return resolvedTarget{}, &resolveError{kind: errHostNotFound, addressed: addressed}
		}
	}

	// Decide this before any dial attempt: an entry without an address is a
	// configuration error, not a connection error.
	if meta.Hostname == "" {
		return resolvedTarget{}, &resolveError{kind: errNoConnectionDetails, addressed: addressed}
	}

	info := hostInfoFromMeta(name, meta)
	hostname, _, user, _ := resolveConnection(info)
	return newResolvedTarget(addressed, info, hostname, user, meta), nil
}

// newResolvedTarget keeps the addressed name as the primary Credential Key so
// credentials saved by `config set-sudo-password <host>` are found.
func newResolvedTarget(addressed string, info hostInfo, hostname, user string, meta HostMeta) resolvedTarget {
	return resolvedTarget{
		Info: info,
		Cred: credentialTarget{
			Keys: credentialKeys(addressed, info.Name, hostname),
			Host: hostname,
			User: user,
		},
		Meta: meta,
	}
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

// hostInfoFromMeta projects stored metadata into the resolved Host view.
func hostInfoFromMeta(name string, meta HostMeta) hostInfo {
	source := sourceManual
	if meta.Hostname != "" || meta.User != "" || meta.Port != "" || meta.IdentityFile != "" {
		source = sourceImported
	}
	return hostInfo{
		Name:         name,
		Source:       source,
		Hostname:     meta.Hostname,
		Port:         meta.Port,
		User:         meta.User,
		IdentityFile: meta.IdentityFile,
	}
}

// parseDirectAddress splits "user@host:port" into its parts. Callers must have
// rejected IPv6 input first, otherwise the port split is meaningless.
func parseDirectAddress(s string) (user, host, port string) {
	if idx := strings.Index(s, "@"); idx >= 0 {
		user = s[:idx]
		s = s[idx+1:]
	}

	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		return user, s[:idx], s[idx+1:]
	}
	return user, s, ""
}

// resolveConnection fills in defaults for a Host's connection details.
func resolveConnection(info hostInfo) (hostname, port, user, identityFile string) {
	hostname, port, user = info.Hostname, info.Port, info.User

	// Direct Addresses carry their details in the name string itself.
	if info.Source == sourceDirect {
		user, hostname, port = parseDirectAddress(info.Name)
	}

	if hostname == "" {
		hostname = info.Name
	}
	if port == "" {
		port = "22"
	}
	if user == "" {
		user = "root"
	}
	return hostname, port, user, info.IdentityFile
}
