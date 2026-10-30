package connect

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/crypto/ssh"

	"sshgo/internal/host"
)

// Parse-time chain failures. Callers branch on these with errors.Is and never
// on message text.
var (
	ErrJumpHostNotFound     = errors.New("jump host not found")
	ErrJumpHostNoDetails    = errors.New("jump host has no connection details")
	ErrJumpValueUnsupported = errors.New("jump host value not supported")
	ErrChainLoop            = errors.New("proxy jump chain loops")
)

// jumpNone is ssh's spelling for "no jump host". Import carries it over from
// ~/.ssh/config, so it must mean a direct connection rather than an error.
const jumpNone = "none"

// ChainError is a chain failure detected without dialling: a broken link, a
// jump host without connection details, a loop, or an unsupported spelling.
type ChainError struct {
	kind   error
	jump   string
	target string
	path   []string
}

func (e *ChainError) Error() string {
	switch e.kind {
	case ErrJumpHostNotFound:
		return fmt.Sprintf("Jump host %q for %q not found.\n"+
			"  Import your SSH config:  sshgo config import\n"+
			"  Add it manually:         sshgo config set %s hostname <address>\n"+
			"  Or connect directly:     sshgo user@host or sshgo host:port",
			e.jump, e.target, e.jump)
	case ErrJumpHostNoDetails:
		return fmt.Sprintf("Jump host %q for %q has no connection details.\n"+
			"  Set one: sshgo config set %s hostname <address>",
			e.jump, e.target, e.jump)
	case ErrChainLoop:
		// No way out is offered: the path itself is the diagnosis.
		return fmt.Sprintf("ProxyJump chain for %q loops: %s", e.target, strings.Join(e.path, " -> "))
	case ErrJumpValueUnsupported:
		return fmt.Sprintf("Jump host %q for %q is not an sshgo address.\n"+
			"  Use a Host name, an Alias, or a Direct Address (user@host:port)", e.jump, e.target)
	}
	return e.jump
}

func (e *ChainError) Unwrap() error { return e.kind }

// JumpError is a jump host that could not be authenticated or reached. Only
// jump failures carry this wrapper: the target keeps its existing wording, and
// the two ends stay distinguishable in the output.
type JumpError struct {
	Hop   host.Resolved
	Cause error
}

func (e *JumpError) Error() string {
	return fmt.Sprintf("Jump host %q (%s:%s) failed: %v", e.Hop.Name, e.Hop.Hostname, e.Hop.Port, e.Cause)
}

func (e *JumpError) Unwrap() error { return e.Cause }

// JumpNoAuthError reports a jump host with no authentication method at all. It
// is the jump equivalent of ErrNoAuthMethod, and names the command that fixes
// it so the message is actionable without the docs.
type JumpNoAuthError struct{ Hop host.Resolved }

func (e *JumpNoAuthError) Error() string {
	return fmt.Sprintf("Jump host %q has no authentication method. Save one: sshgo config set-password %s",
		e.Hop.Name, e.Hop.Name)
}

// Resolver resolves one addressed string into a dialable Host. host.Resolve is
// the production implementation; tests inject a fake so chain semantics can be
// exercised without touching the filesystem or a real sshd.
type Resolver func(addressed string) (host.Resolved, error)

// resolver reports the resolver to use, defaulting to the real one.
func (o Options) resolver() Resolver {
	if o.Resolve != nil {
		return o.Resolve
	}
	return host.Resolve
}

// ResolveChain expands target's proxy-jump into the ordered hops to dial before
// it, outermost first. Depth is unlimited: each hop is an ordinary Host that may
// itself have a jump host.
//
// It performs no network I/O, so every failure here is a parse-time failure:
// `info` reaches the same verdict as a connection without dialling, and a loop
// is reported as a path instead of as a hang.
func ResolveChain(target host.Resolved, opts Options) ([]host.Resolved, error) {
	resolve := opts.resolver()

	// hops collects the chain as it is walked outward from the target; it is
	// reversed before returning so callers dial the outermost hop first.
	var hops []host.Resolved
	// visited records the hosts already on the walk so a cycle is detected at
	// the hop that closes it. It is keyed by Name, which is what a proxy-jump
	// value addresses.
	visited := []string{target.Name}

	current := target
	for {
		value := strings.TrimSpace(current.ProxyJump)
		if isDirect(value) {
			break
		}

		hop, err := resolveJump(value, target.Name, resolve)
		if err != nil {
			return nil, err
		}

		if idx := slices.Index(visited, hop.Name); idx >= 0 {
			// Print only the cycle: the tail leading into it is not the problem.
			cycle := append(slices.Clone(visited[idx:]), hop.Name)
			return nil, &ChainError{kind: ErrChainLoop, target: target.Name, path: cycle}
		}

		hops = append(hops, hop)
		visited = append(visited, hop.Name)
		current = hop
	}

	slices.Reverse(hops)
	return hops, nil
}

// isDirect reports whether a proxy-jump value means "no jump host".
func isDirect(value string) bool {
	return value == "" || value == jumpNone
}

// resolveJump turns one proxy-jump value into a hop, wrapping resolution
// failures in chain wording that names both the hop and the target.
func resolveJump(value, target string, resolve Resolver) (host.Resolved, error) {
	if err := validateJumpValue(value, target); err != nil {
		return host.Resolved{}, err
	}

	hop, err := resolve(value)
	switch {
	case err == nil:
		return hop, nil
	case errors.Is(err, host.ErrHostNotFound):
		return host.Resolved{}, &ChainError{kind: ErrJumpHostNotFound, jump: value, target: target}
	case errors.Is(err, host.ErrNoConnectionDetails):
		return host.Resolved{}, &ChainError{kind: ErrJumpHostNoDetails, jump: value, target: target}
	default:
		return host.Resolved{}, err
	}
}

// validateJumpValue rejects the ssh spellings sshgo does not implement. A jump
// host is an sshgo Host, an Alias, or a Direct Address: one addressing grammar
// for the whole project.
func validateJumpValue(value, target string) error {
	if strings.Contains(value, "://") || strings.Contains(value, ",") {
		return &ChainError{kind: ErrJumpValueUnsupported, jump: value, target: target}
	}
	return nil
}

// Chain dials target through its jump chain and returns the target client plus
// a cleanup that releases every connection opened for it, in reverse order.
//
// Each hop is an ordinary Host: its own user, port, identity file, and saved
// password. Nothing is shared across hops except the SSH agent, which belongs
// to the user rather than to any host.
func Chain(target host.Resolved, auth Authenticator, opts Options) (*ssh.Client, func(), error) {
	hops, err := ResolveChain(target, opts)
	if err != nil {
		return nil, nil, err
	}

	clients := make([]*ssh.Client, 0, len(hops)+1)
	cleanup := func() {
		for i := len(clients) - 1; i >= 0; i-- {
			clients[i].Close()
		}
	}

	var previous *ssh.Client
	for _, hop := range hops {
		client, err := dialHop(previous, hop, auth, opts)
		if err != nil {
			cleanup()
			return nil, nil, wrapJumpFailure(err, hop)
		}
		clients = append(clients, client)
		previous = client
	}

	client, err := dialHop(previous, target, auth, opts)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	clients = append(clients, client)

	return client, cleanup, nil
}

// wrapJumpFailure re-labels a hop failure as a jump failure so the target's
// wording stays untouched and the two ends are distinguishable.
func wrapJumpFailure(err error, hop host.Resolved) error {
	if errors.Is(err, ErrNoAuthMethod) {
		return &JumpNoAuthError{Hop: hop}
	}
	return &JumpError{Hop: hop, Cause: Cause(err)}
}

// dialHop authenticates to hop, tunnelling through previous when the chain is
// deeper than one hop.
func dialHop(previous *ssh.Client, hop host.Resolved, auth Authenticator, opts Options) (*ssh.Client, error) {
	cred, err := auth.HopAuth(hop)
	if err != nil {
		return nil, &AuthError{Err: err}
	}
	if cred.Cleanup != nil {
		defer cred.Cleanup()
	}
	if len(cred.Methods) == 0 {
		return nil, ErrNoAuthMethod
	}

	clientConfig := &ssh.ClientConfig{
		User:            hop.User,
		Auth:            cred.Methods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         opts.Timeout,
	}

	// Direct string concatenation, not net.JoinHostPort: IPv6 input is rejected
	// during resolution, so the two forms are identical here.
	addr := hop.Hostname + ":" + hop.Port
	if previous == nil {
		client, err := ssh.Dial("tcp", addr, clientConfig)
		if err != nil {
			return nil, &DialError{Err: err}
		}
		return client, nil
	}

	// direct-tcpip: the previous hop opens a TCP connection to the next one on
	// our behalf, so the next handshake runs inside that tunnel.
	conn, err := previous.Dial("tcp", addr)
	if err != nil {
		return nil, &DialError{Err: err}
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		conn.Close()
		return nil, &DialError{Err: err}
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}
