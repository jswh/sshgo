// Package connect is sshgo's only dial point: it turns a resolved Host plus an
// injected authentication strategy into an ssh.Client and a session.
//
// It deliberately depends on neither the terminal UI nor os/term: everything
// that needs a human (progress lines, password prompts, "save password?") lives
// in the strategy the caller injects, which also makes the whole path testable
// with a fake Authenticator and no sshd.
package connect

import (
	"errors"
	"time"

	"golang.org/x/crypto/ssh"

	"sshgo/internal/host"
)

// Authenticator is the narrow seam between dialling and authentication policy.
// sshgo injects two implementations: an interactive one that may prompt, and a
// non-interactive one that never does.
//
// The two exits are deliberately separate. Authentication methods are offered
// to the hop being dialled; the sudo password only ever reaches the target
// session's stdin, never any hop's ssh.ClientConfig.Auth.
type Authenticator interface {
	// HopAuth returns the authentication methods to offer hop. An empty method
	// list with a nil error means the hop has no authentication method at all.
	HopAuth(hop host.Resolved) (HopCredentials, error)
	// SudoPassword returns the password to feed the target session's
	// `sudo -S` stdin, if one is available.
	SudoPassword(target host.Resolved) (string, bool)
}

// HopCredentials is one hop's authentication methods plus the release of any
// connection acquired while collecting them (typically the SSH agent socket).
// Dial runs Cleanup once authentication has completed.
type HopCredentials struct {
	Methods []ssh.AuthMethod
	Cleanup func()
}

// ErrNoAuthMethod is reported when a hop has no authentication method to offer.
// The message is user-facing and frozen.
var ErrNoAuthMethod = errors.New("No authentication method available (no SSH agent, keys, or saved passwords)")

// AuthError wraps a failure reported by the injected Authenticator. Callers use
// it to tell "could not authenticate" from "could not connect".
type AuthError struct{ Err error }

func (e *AuthError) Error() string { return e.Err.Error() }
func (e *AuthError) Unwrap() error { return e.Err }

// DialError wraps a failed TCP connection or SSH handshake.
type DialError struct{ Err error }

func (e *DialError) Error() string { return e.Err.Error() }
func (e *DialError) Unwrap() error { return e.Err }

// Options carries the dial knobs the two callers disagree on today.
type Options struct {
	// Timeout bounds the TCP and handshake phase; zero means no timeout.
	Timeout time.Duration
	// Resolve resolves one addressed string into a Host. Nil means the real
	// resolution entry point; tests inject a fake to drive chain semantics
	// without a metadata file.
	Resolve Resolver
}

// Dial opens a single-hop client to hop, ignoring any proxy-jump on it. Use
// Chain when the Host may have a jump chain.
func Dial(hop host.Resolved, auth Authenticator, opts Options) (*ssh.Client, error) {
	return dialHop(nil, hop, auth, opts)
}

// PTYName is the terminal type requested for interactive shells.
const PTYName = "xterm-256color"

// ptyModes are the terminal modes sshgo has always requested.
var ptyModes = ssh.TerminalModes{
	ssh.ECHO:          1,
	ssh.TTY_OP_ISPEED: 14400,
	ssh.TTY_OP_OSPEED: 14400,
}

// RequestPTY asks for a pseudo-terminal of the given size on an interactive
// session.
func RequestPTY(session *ssh.Session, width, height int) error {
	return session.RequestPty(PTYName, height, width, ptyModes)
}

// ExitStatus reports the remote exit status carried by err. The boolean is
// false when err is not a remote exit status (or is nil).
func ExitStatus(err error) (int, bool) {
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus(), true
	}
	return 0, false
}
