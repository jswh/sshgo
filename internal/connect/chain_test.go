package connect

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"sshgo/internal/host"
)

// ---------------------------------------------------------------------------
// Jump-chain seam
//
// Chain is the only place a jump chain exists. These tests drive it with a fake
// Authenticator and a fake Resolver against an in-process SSH server that also
// accepts direct-tcpip channels, so a whole chain runs without sshd and
// without a second network namespace.
// ---------------------------------------------------------------------------

// hopAuthFunc is a fake strategy that answers per hop, which is what makes the
// credential matrix observable: every call records the hop it was asked about.
type hopAuthFunc struct {
	auth func(hop host.Resolved) (HopCredentials, error)
	sudo func(target host.Resolved) (string, bool)
}

func (f *hopAuthFunc) HopAuth(hop host.Resolved) (HopCredentials, error) { return f.auth(hop) }

func (f *hopAuthFunc) SudoPassword(target host.Resolved) (string, bool) {
	if f.sudo == nil {
		return "", false
	}
	return f.sudo(target)
}

// recorder returns a strategy that offers methods to every hop and records the
// hops in the order they were authenticated.
func recorder(methods []ssh.AuthMethod) (*[]host.Resolved, *hopAuthFunc) {
	hops := new([]host.Resolved)
	return hops, &hopAuthFunc{auth: func(hop host.Resolved) (HopCredentials, error) {
		*hops = append(*hops, hop)
		return HopCredentials{Methods: methods}, nil
	}}
}

// fakeResolver resolves the addressed names the tests use, so chain semantics
// can be tested without a metadata file.
type fakeResolver map[string]host.Resolved

func (f fakeResolver) resolve(addressed string) (host.Resolved, error) {
	if hop, ok := f[addressed]; ok {
		return hop, nil
	}
	// Mirror host.Resolve: a miss is a typed error, which is what the chain
	// wrapper branches on.
	return host.Resolved{}, &fakeResolveError{kind: host.ErrHostNotFound}
}

// notFoundResolver mimics host.Resolve for a missing bare name, including the
// typed error callers branch on.
func notFoundResolver(string) (host.Resolved, error) {
	return host.Resolved{}, &fakeResolveError{kind: host.ErrHostNotFound}
}

type fakeResolveError struct{ kind error }

func (e *fakeResolveError) Error() string { return e.kind.Error() }
func (e *fakeResolveError) Unwrap() error { return e.kind }

// TestChainSingleJumpDialsBothHops is the tracer bullet: a target reached
// through one jump host must authenticate to the jump host first and to the
// target second, in that order.
func TestChainSingleJumpDialsBothHops(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, target.Name = "bastion", "db1"
	target.ProxyJump = "bastion"

	hops, auth := recorder(passwordAuth("pw"))
	opts := Options{Resolve: fakeResolver{"bastion": jump}.resolve}

	client, cleanup, err := Chain(target, auth, opts)
	if err != nil {
		t.Fatalf("Chain() failed: %v", err)
	}
	defer cleanup()

	if client == nil {
		t.Fatal("Chain() returned a nil client")
	}
	if len(*hops) != 2 {
		t.Fatalf("HopAuth called for %d hops, want 2", len(*hops))
	}
	if (*hops)[0].Name != "bastion" || (*hops)[1].Name != "db1" {
		t.Errorf("hop order = [%s %s], want the jump host before the target", (*hops)[0].Name, (*hops)[1].Name)
	}
}

// The chain must be usable: a session on the far side of the jump host proves
// the tunnel carries real traffic, not just a handshake.
func TestChainRunsCommandThroughJumpHost(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, target.Name = "bastion", "db1"
	target.ProxyJump = "bastion"

	_, auth := recorder(passwordAuth("pw"))
	opts := Options{Resolve: fakeResolver{"bastion": jump}.resolve}

	client, cleanup, err := Chain(target, auth, opts)
	if err != nil {
		t.Fatalf("Chain() failed: %v", err)
	}
	defer cleanup()

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() through the chain failed: %v", err)
	}
	captured, err := RunCapture(session, "hello", "")
	if err != nil {
		t.Fatalf("RunCapture() through the chain failed: %v", err)
	}
	if captured.Stdout != "ran:hello" {
		t.Errorf("Stdout = %q, want the remote output", captured.Stdout)
	}
}

// A target with no jump host behaves exactly like a single dial: one hop, and
// the chain must not invent an empty hop.
func TestChainWithoutJumpIsASingleDial(t *testing.T) {
	srv := startTestServer(t)
	target := srv.resolvedHost()

	hops, auth := recorder(passwordAuth("pw"))
	client, cleanup, err := Chain(target, auth, Options{})
	if err != nil {
		t.Fatalf("Chain() failed: %v", err)
	}
	defer cleanup()

	if len(*hops) != 1 {
		t.Errorf("HopAuth called %d times, want once for a single-hop target", len(*hops))
	}
	if _, err := NewSession(client); err != nil {
		t.Errorf("NewSession() on the target failed: %v", err)
	}
}

// A three-hop chain is a normal environment (bastion -> inner -> target), not an
// edge case: every hop authenticates as itself, outermost first, and the target
// is reached through all of them.
func TestChainThreeHopsDialsOutermostFirst(t *testing.T) {
	srv := startTestServer(t)
	edge, inner, target := srv.resolvedHost(), srv.resolvedHost(), srv.resolvedHost()
	edge.Name, inner.Name, target.Name = "edge", "inner", "db1"
	edge.User, inner.User, target.User = "edgeuser", "inneruser", "targetuser"
	edge.ProxyJump, inner.ProxyJump = "", "edge"
	inner.ProxyJump, target.ProxyJump = "edge", "inner"

	hops, auth := recorder(passwordAuth("pw"))
	opts := Options{Resolve: fakeResolver{"edge": edge, "inner": inner}.resolve}

	client, cleanup, err := Chain(target, auth, opts)
	if err != nil {
		t.Fatalf("Chain() failed: %v", err)
	}
	defer cleanup()

	var names []string
	for _, h := range *hops {
		names = append(names, h.Name)
	}
	if want := []string{"edge", "inner", "db1"}; !slices.Equal(names, want) {
		t.Errorf("dial order = %v, want %v (outermost hop first)", names, want)
	}

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() through a three-hop chain failed: %v", err)
	}
	captured, err := RunCapture(session, "deep", "")
	if err != nil {
		t.Fatalf("RunCapture() through a three-hop chain failed: %v", err)
	}
	if captured.Stdout != "ran:deep" {
		t.Errorf("Stdout = %q, want the remote output", captured.Stdout)
	}
}

// A loop is a parse-time failure: the path is printed and nothing is dialled.
func TestResolveChainDetectsLoop(t *testing.T) {
	db1 := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "bastion"}
	bastion := host.Resolved{Name: "bastion", Hostname: "10.0.0.9", Port: "22", User: "root", ProxyJump: "db1"}

	hops, err := ResolveChain(db1, Options{Resolve: fakeResolver{"bastion": bastion, "db1": db1}.resolve})
	if err == nil {
		t.Fatalf("ResolveChain() on a loop succeeded with hops %v", hops)
	}
	if !errors.Is(err, ErrChainLoop) {
		t.Fatalf("error = %v, want ErrChainLoop", err)
	}
	want := `ProxyJump chain for "db1" loops: db1 -> bastion -> db1`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// A host that jumps to itself is the shortest loop and must not recurse.
func TestResolveChainDetectsSelfLoop(t *testing.T) {
	db1 := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "db1"}

	_, err := ResolveChain(db1, Options{Resolve: fakeResolver{"db1": db1}.resolve})
	if !errors.Is(err, ErrChainLoop) {
		t.Fatalf("error = %v, want ErrChainLoop", err)
	}
	if want := `ProxyJump chain for "db1" loops: db1 -> db1`; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// A deeper loop prints only the cycle, not the tail leading into it.
func TestResolveChainLoopPrintsTheCycleOnly(t *testing.T) {
	edge := host.Resolved{Name: "edge", Hostname: "10.0.0.1", Port: "22", User: "root"}
	inner := host.Resolved{Name: "inner", Hostname: "10.0.0.2", Port: "22", User: "root", ProxyJump: "edge"}
	edge.ProxyJump = "inner"
	db1 := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "inner"}

	_, err := ResolveChain(db1, Options{Resolve: fakeResolver{"edge": edge, "inner": inner}.resolve})
	if !errors.Is(err, ErrChainLoop) {
		t.Fatalf("error = %v, want ErrChainLoop", err)
	}
	want := `ProxyJump chain for "db1" loops: inner -> edge -> inner`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// A break anywhere in the chain names the failing hop and the host the user
// addressed, not the intermediate one.
func TestResolveChainBreakNamesHopAndTarget(t *testing.T) {
	inner := host.Resolved{Name: "inner", Hostname: "10.0.0.2", Port: "22", User: "root", ProxyJump: "ghost"}
	db1 := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "inner"}

	_, err := ResolveChain(db1, Options{Resolve: fakeResolver{"inner": inner}.resolve})
	if !errors.Is(err, ErrJumpHostNotFound) {
		t.Fatalf("error = %v, want ErrJumpHostNotFound", err)
	}
	msg := err.Error()
	for _, want := range []string{`"ghost"`, `"db1"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %s", msg, want)
		}
	}
}

// The chain is built without any network I/O, which is what lets `info` report
// a broken chain without dialling.
func TestResolveChainDoesNotDial(t *testing.T) {
	edge := host.Resolved{Name: "edge", Hostname: "203.0.113.9", Port: "22", User: "root"}
	inner := host.Resolved{Name: "inner", Hostname: "203.0.113.8", Port: "22", User: "root", ProxyJump: "edge"}
	db1 := host.Resolved{Name: "db1", Hostname: "203.0.113.7", Port: "22", User: "root", ProxyJump: "inner"}

	hops, err := ResolveChain(db1, Options{Resolve: fakeResolver{"edge": edge, "inner": inner}.resolve})
	if err != nil {
		t.Fatalf("ResolveChain() failed: %v", err)
	}
	var names []string
	for _, h := range hops {
		names = append(names, h.Name)
	}
	if want := []string{"edge", "inner"}; !slices.Equal(names, want) {
		t.Errorf("hops = %v, want %v (outermost first)", names, want)
	}
}

// `none` and the empty value both mean "direct connection": ssh writes `none`,
// and import carries it over, so it must not become an error.
func TestResolveChainTreatsNoneAsDirect(t *testing.T) {
	for _, value := range []string{"", "none", "  none  "} {
		t.Run(fmt.Sprintf("value=%q", value), func(t *testing.T) {
			target := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: value}
			hops, err := ResolveChain(target, Options{Resolve: func(string) (host.Resolved, error) {
				t.Error("the resolver was called for a direct connection")
				return host.Resolved{}, nil
			}})
			if err != nil {
				t.Fatalf("ResolveChain(%q) failed: %v", value, err)
			}
			if len(hops) != 0 {
				t.Errorf("hops = %v, want none", hops)
			}
		})
	}
}

// A jump host is its own Host: it is dialled as itself, never with the target's
// user or port.
func TestChainUsesEachHopsOwnDetails(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, jump.User = "bastion", "jumpuser"
	target.Name, target.User = "db1", "targetuser"
	target.ProxyJump = "bastion"

	hops, auth := recorder(passwordAuth("pw"))
	opts := Options{Resolve: fakeResolver{"bastion": jump}.resolve}

	_, cleanup, err := Chain(target, auth, opts)
	if err != nil {
		t.Fatalf("Chain() failed: %v", err)
	}
	defer cleanup()

	if len(*hops) != 2 {
		t.Fatalf("HopAuth called %d times, want 2", len(*hops))
	}
	if (*hops)[0].User != "jumpuser" || (*hops)[1].User != "targetuser" {
		t.Errorf("hop users = [%s %s], want each hop dialled as itself", (*hops)[0].User, (*hops)[1].User)
	}
}

// ---------------------------------------------------------------------------
// Chain Errors: a broken chain is reported before anything is dialled.
// ---------------------------------------------------------------------------

// The jump host is missing from local metadata. The message names both the jump
// host and the target and offers the three ways out.
func TestChainMissingJumpHostIsReported(t *testing.T) {
	target := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "bastion"}

	auth := &hopAuthFunc{auth: func(host.Resolved) (HopCredentials, error) {
		t.Fatal("HopAuth was called for a chain that cannot be resolved")
		return HopCredentials{}, nil
	}}

	_, _, err := Chain(target, auth, Options{Resolve: notFoundResolver})
	if err == nil {
		t.Fatal("Chain() with a missing jump host succeeded")
	}

	want := `Jump host "bastion" for "db1" not found.
  Import your SSH config:  sshgo config import
  Add it manually:         sshgo config set bastion hostname <address>
  Or connect directly:     sshgo user@host or sshgo host:port`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
	if !errors.Is(err, ErrJumpHostNotFound) {
		t.Errorf("error = %v, want it to wrap ErrJumpHostNotFound", err)
	}
}

// An entry that exists but records no address is a configuration error, not a
// connection error, and must not be dialled as if the name were the address.
func TestChainJumpHostWithoutDetailsIsReported(t *testing.T) {
	target := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "bastion"}

	auth := &hopAuthFunc{auth: func(host.Resolved) (HopCredentials, error) {
		t.Fatal("HopAuth was called for a jump host with no connection details")
		return HopCredentials{}, nil
	}}

	resolve := func(string) (host.Resolved, error) {
		return host.Resolved{}, &fakeResolveError{kind: host.ErrNoConnectionDetails}
	}
	_, _, err := Chain(target, auth, Options{Resolve: resolve})
	if err == nil {
		t.Fatal("Chain() with a detail-less jump host succeeded")
	}

	want := `Jump host "bastion" for "db1" has no connection details.
  Set one: sshgo config set bastion hostname <address>`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
	if !errors.Is(err, ErrJumpHostNoDetails) {
		t.Errorf("error = %v, want it to wrap ErrJumpHostNoDetails", err)
	}
}

// ssh spellings that sshgo does not implement are refused instead of being
// dialled as if they were host names.
func TestChainRejectsUnsupportedJumpValues(t *testing.T) {
	for _, value := range []string{"ssh://bastion:22", "bastion,inner"} {
		t.Run(value, func(t *testing.T) {
			target := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: value}

			_, err := ResolveChain(target, Options{Resolve: func(string) (host.Resolved, error) {
				t.Error("the resolver was called for an unsupported jump value")
				return host.Resolved{}, nil
			}})
			if !errors.Is(err, ErrJumpValueUnsupported) {
				t.Fatalf("ResolveChain(%q) error = %v, want ErrJumpValueUnsupported", value, err)
			}
			if msg := err.Error(); !strings.Contains(msg, "db1") || !strings.Contains(msg, value) {
				t.Errorf("message %q does not name both the jump value and the target", msg)
			}
		})
	}
}

// A failing jump host must be distinguishable from a failing target: only the
// jump failure gets a prefix, and the prefix names the hop without a sequence
// number.
func TestChainJumpAuthFailureIsPrefixed(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, target.Name = "bastion", "db1"
	target.ProxyJump = "bastion"

	auth := &hopAuthFunc{auth: func(hop host.Resolved) (HopCredentials, error) {
		if hop.Name == "bastion" {
			return HopCredentials{}, nil // no methods: nothing to authenticate with
		}
		return HopCredentials{Methods: passwordAuth("pw")}, nil
	}}

	_, _, err := Chain(target, auth, Options{Resolve: fakeResolver{"bastion": jump}.resolve})
	if err == nil {
		t.Fatal("Chain() with an unauthenticated jump host succeeded")
	}

	var hopErr *JumpNoAuthError
	if !errors.As(err, &hopErr) {
		t.Fatalf("error = %v, want a JumpNoAuthError naming the hop", err)
	}
	if hopErr.Hop.Name != "bastion" {
		t.Errorf("error names %q, want the jump host", hopErr.Hop.Name)
	}
	want := `Jump host "bastion" has no authentication method. Save one: sshgo config set-password bastion`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// A jump host that cannot be reached is a jump failure too, with its address in
// the prefix so the user knows which end to look at.
func TestChainJumpDialFailureIsPrefixed(t *testing.T) {
	jump := host.Resolved{Name: "bastion", Hostname: "127.0.0.1", Port: "1", User: "root"}
	target := host.Resolved{Name: "db1", Hostname: "10.0.0.5", Port: "22", User: "root", ProxyJump: "bastion"}

	auth := &hopAuthFunc{auth: func(host.Resolved) (HopCredentials, error) {
		return HopCredentials{Methods: passwordAuth("pw")}, nil
	}}

	_, _, err := Chain(target, auth, Options{Resolve: fakeResolver{"bastion": jump}.resolve})
	if err == nil {
		t.Fatal("Chain() to an unreachable jump host succeeded")
	}
	var hopErr *JumpError
	if !errors.As(err, &hopErr) {
		t.Fatalf("error = %v, want a JumpError", err)
	}
	if !strings.HasPrefix(err.Error(), `Jump host "bastion" (127.0.0.1:1) failed: `) {
		t.Errorf("error = %q, want the jump prefix with the hop address", err)
	}
}

// A failing target keeps the target wording: no jump prefix, so the two ends of
// a chain stay distinguishable.
func TestChainTargetFailureIsNotPrefixed(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, target.Name = "bastion", "db1"
	target.ProxyJump = "bastion"

	auth := &hopAuthFunc{auth: func(hop host.Resolved) (HopCredentials, error) {
		if hop.Name == "bastion" {
			return HopCredentials{Methods: passwordAuth("pw")}, nil
		}
		return HopCredentials{}, nil // the target has nothing to offer
	}}

	_, _, err := Chain(target, auth, Options{Resolve: fakeResolver{"bastion": jump}.resolve})
	if err == nil {
		t.Fatal("Chain() with an unauthenticated target succeeded")
	}
	var hopErr *JumpError
	var noAuthErr *JumpNoAuthError
	if errors.As(err, &hopErr) || errors.As(err, &noAuthErr) {
		t.Errorf("error = %v, want a plain target failure with no jump prefix", err)
	}
	if !errors.Is(err, ErrNoAuthMethod) {
		t.Errorf("error = %v, want ErrNoAuthMethod for the target", err)
	}
}

// cleanup releases every connection the chain opened, in reverse order. The
// jump connection must not outlive the target: a leaked jump connection is a
// leaked authenticated session on the bastion.
func TestChainCleanupClosesEveryConnection(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, target.Name = "bastion", "db1"
	target.ProxyJump = "bastion"

	_, auth := recorder(passwordAuth("pw"))
	client, cleanup, err := Chain(target, auth, Options{Resolve: fakeResolver{"bastion": jump}.resolve})
	if err != nil {
		t.Fatalf("Chain() failed: %v", err)
	}
	if got := srv.active.Load(); got != 2 {
		t.Errorf("live connections = %d, want 2 (jump and target)", got)
	}

	cleanup()
	waitFor(t, func() bool { return srv.active.Load() == 0 }, "cleanup did not close every connection")
	if _, err := client.NewSession(); err == nil {
		t.Error("the target client is still usable after cleanup")
	}
}

// When a later hop fails, the connections already opened are released by Chain
// itself: the caller gets no client and nothing to clean up.
func TestChainReleasesEarlierHopsOnFailure(t *testing.T) {
	srv := startTestServer(t)
	jump := srv.resolvedHost()
	jump.Name = "bastion"

	// The jump host is up but the target is refused through the tunnel, so the
	// only connection to leak would be the jump host's.
	target := host.Resolved{Name: "db1", Hostname: "127.0.0.1", Port: "1", User: "root", ProxyJump: "bastion"}

	auth := &hopAuthFunc{auth: func(host.Resolved) (HopCredentials, error) {
		return HopCredentials{Methods: passwordAuth("pw")}, nil
	}}

	client, cleanup, err := Chain(target, auth, Options{Resolve: fakeResolver{"bastion": jump}.resolve})
	if err == nil {
		t.Fatal("Chain() to a refused target through a jump host succeeded")
	}
	if client != nil {
		t.Error("Chain() returned a client alongside an error")
	}
	if cleanup != nil {
		t.Error("Chain() returned a cleanup alongside an error")
	}
	waitFor(t, func() bool { return srv.active.Load() == 0 }, "the jump connection leaked after a failed target dial")
}

// waitFor polls cond until it holds, so a test can assert on a cleanup that
// completes asynchronously.
func waitFor(t *testing.T, cond func() bool, message string) {
	t.Helper()
	for range 200 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

// A target with no authentication method is named when it sits at the end of a
// chain: in a multi-hop setup "no authentication method" alone does not say
// which end to fix. A single-hop Host keeps its existing wording.
func TestChainTargetNoAuthNamesTheTargetInAChain(t *testing.T) {
	srv := startTestServer(t)
	jump, target := srv.resolvedHost(), srv.resolvedHost()
	jump.Name, target.Name = "bastion", "db1"
	target.ProxyJump = "bastion"

	auth := &hopAuthFunc{auth: func(hop host.Resolved) (HopCredentials, error) {
		if hop.Name == "bastion" {
			return HopCredentials{Methods: passwordAuth("pw")}, nil
		}
		return HopCredentials{}, nil
	}}

	_, _, err := Chain(target, auth, Options{Resolve: fakeResolver{"bastion": jump}.resolve})
	if err == nil {
		t.Fatal("Chain() with an unauthenticated target succeeded")
	}

	var targetErr *TargetNoAuthError
	if !errors.As(err, &targetErr) {
		t.Fatalf("error = %v, want a TargetNoAuthError naming the target", err)
	}
	want := `Host "db1" has no authentication method. Save one: sshgo config set-password db1`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// The single-hop wording is frozen: no host name, no hint.
func TestChainSingleHopNoAuthKeepsFrozenText(t *testing.T) {
	srv := startTestServer(t)
	target := srv.resolvedHost()

	auth := &hopAuthFunc{auth: func(host.Resolved) (HopCredentials, error) {
		return HopCredentials{}, nil
	}}

	_, _, err := Chain(target, auth, Options{})
	if !errors.Is(err, ErrNoAuthMethod) {
		t.Fatalf("error = %v, want the frozen ErrNoAuthMethod", err)
	}
	if err.Error() != ErrNoAuthMethod.Error() {
		t.Errorf("error = %q, want the frozen single-hop text", err)
	}
}
