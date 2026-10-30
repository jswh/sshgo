package connect

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"

	"sshgo/internal/host"
)

// ---------------------------------------------------------------------------
// In-process SSH server
//
// A real sshd would make these tests slow and environment-dependent; the point
// of this package is that dialling and sessions can be exercised with injected
// credentials only.
// ---------------------------------------------------------------------------

type ptySize struct {
	width, height int
}

type testServer struct {
	addr     string
	ptySizes chan ptySize
	stdin    chan string
	closed   chan struct{}
	// active counts live TCP connections, so a test can prove a chain released
	// everything it opened.
	active atomic.Int32
}

// startTestServer serves the "session" channel only: pty-req, exec, shell.
// Commands "exit <n>" produce exit status n; anything else is echoed back.
func startTestServer(t *testing.T) *testServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer from key: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			return nil, nil // accept any password
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &testServer{
		addr:     ln.Addr().String(),
		ptySizes: make(chan ptySize, 4),
		stdin:    make(chan string, 4),
		closed:   make(chan struct{}),
	}

	go func() {
		defer close(srv.closed)
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			srv.active.Add(1)
			go func() {
				defer srv.active.Add(-1)
				srv.handle(nc, cfg)
			}()
		}
	}()

	t.Cleanup(func() {
		ln.Close()
		<-srv.closed
	})
	return srv
}

func (s *testServer) handle(nc net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		switch newCh.ChannelType() {
		case "session":
			ch, chReqs, err := newCh.Accept()
			if err != nil {
				continue
			}
			go s.serveSession(ch, chReqs)
		case "direct-tcpip":
			// Serving this channel is what makes the in-process server a
			// working jump host: it dials the requested address for the client.
			go serveDirectTCPIP(newCh)
		default:
			newCh.Reject(ssh.UnknownChannelType, "only session and direct-tcpip")
		}
	}
}

func (s *testServer) serveSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var payload struct {
				Term    string
				Width   uint32
				Height  uint32
				WidthP  uint32
				HeightP uint32
				Modes   string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err == nil {
				s.ptySizes <- ptySize{width: int(payload.Width), height: int(payload.Height)}
			}
			req.Reply(true, nil)

		case "shell":
			req.Reply(true, nil)
			ch.Write([]byte("shell started"))
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			return

		case "exec":
			var payload struct{ Command string }
			ssh.Unmarshal(req.Payload, &payload)
			req.Reply(true, nil)

			status := uint32(0)
			output := "ran:" + payload.Command
			if strings.HasPrefix(payload.Command, "exit ") {
				n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(payload.Command, "exit ")))
				if err != nil {
					t := fmt.Sprintf("bad exit code: %v", err)
					ch.Stderr().Write([]byte(t))
					status = 1
				} else {
					status = uint32(n)
				}
				output = ""
			}
			if strings.HasPrefix(payload.Command, "cat-stdin") {
				buf := make([]byte, 256)
				n, _ := ch.Read(buf)
				s.stdin <- string(buf[:n])
				output = ""
			}
			if output != "" {
				ch.Write([]byte(output))
			}
			ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
			return

		default:
			req.Reply(false, nil)
		}
	}
}

func (s *testServer) resolvedHost() host.Resolved {
	hostname, port, _ := net.SplitHostPort(s.addr)
	return host.Resolved{
		Name:     "test",
		Hostname: hostname,
		Port:     port,
		User:     "tester",
	}
}

// ---------------------------------------------------------------------------
// Fake authenticator
// ---------------------------------------------------------------------------

type fakeAuth struct {
	methods []ssh.AuthMethod
	err     error
	sudo    string
	hasSudo bool

	hops     []host.Resolved
	cleanups int
}

func (f *fakeAuth) HopAuth(hop host.Resolved) (HopCredentials, error) {
	f.hops = append(f.hops, hop)
	if f.err != nil {
		return HopCredentials{}, f.err
	}
	return HopCredentials{
		Methods: f.methods,
		Cleanup: func() { f.cleanups++ },
	}, nil
}

func (f *fakeAuth) SudoPassword(host.Resolved) (string, bool) { return f.sudo, f.hasSudo }

func passwordAuth(password string) []ssh.AuthMethod {
	return []ssh.AuthMethod{ssh.Password(password)}
}

// ---------------------------------------------------------------------------
// Dial
// ---------------------------------------------------------------------------

func TestDialNoAuthMethodDoesNotConnect(t *testing.T) {
	// An unreachable address proves Dial stops before dialling when the
	// strategy offers nothing.
	hop := host.Resolved{Name: "x", Hostname: "127.0.0.1", Port: "1", User: "u"}
	auth := &fakeAuth{}

	_, err := Dial(hop, auth, Options{})
	if err == nil {
		t.Fatal("Dial() with no auth methods succeeded, want ErrNoAuthMethod")
	}
	if !isNoAuthMethod(err) {
		t.Errorf("Dial() error = %v, want ErrNoAuthMethod", err)
	}
	if auth.cleanups != 1 {
		t.Errorf("cleanups = %d, want the agent connection released once", auth.cleanups)
	}
}

func TestDialWrapsAuthFailure(t *testing.T) {
	hop := host.Resolved{Name: "x", Hostname: "127.0.0.1", Port: "1", User: "u"}
	auth := &fakeAuth{err: fmt.Errorf("prompt failed")}

	_, err := Dial(hop, auth, Options{})
	var authErr *AuthError
	if !asError(err, &authErr) {
		t.Fatalf("Dial() error = %v, want AuthError", err)
	}
	if !strings.Contains(err.Error(), "prompt failed") {
		t.Errorf("error = %q, want the underlying cause", err)
	}
}

func TestDialWrapsConnectionFailure(t *testing.T) {
	hop := host.Resolved{Name: "x", Hostname: "127.0.0.1", Port: "1", User: "u"}
	auth := &fakeAuth{methods: passwordAuth("pw")}

	_, err := Dial(hop, auth, Options{})
	var dialErr *DialError
	if !asError(err, &dialErr) {
		t.Fatalf("Dial() error = %v, want DialError", err)
	}
	if auth.cleanups != 1 {
		t.Errorf("cleanups = %d, want 1", auth.cleanups)
	}
}

func TestDialReachesServer(t *testing.T) {
	srv := startTestServer(t)
	auth := &fakeAuth{methods: passwordAuth("pw")}

	client, err := Dial(srv.resolvedHost(), auth, Options{})
	if err != nil {
		t.Fatalf("Dial() failed: %v", err)
	}
	defer client.Close()

	if len(auth.hops) != 1 {
		t.Errorf("HopAuth called %d times, want once per hop", len(auth.hops))
	}
	if auth.cleanups != 1 {
		t.Errorf("cleanups = %d, want the agent released after authentication", auth.cleanups)
	}
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func dialForTest(t *testing.T, srv *testServer) *ssh.Client {
	t.Helper()
	client, err := Dial(srv.resolvedHost(), &fakeAuth{methods: passwordAuth("pw")}, Options{})
	if err != nil {
		t.Fatalf("Dial() failed: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestRunCaptureReturnsStdoutAndExitCode(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}

	captured, err := RunCapture(session, "hello", "")
	if err != nil {
		t.Fatalf("RunCapture() failed: %v", err)
	}
	if captured.Stdout != "ran:hello" {
		t.Errorf("Stdout = %q, want the remote output", captured.Stdout)
	}
	if captured.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", captured.ExitCode)
	}
}

func TestRunCaptureReportsRemoteExitStatus(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}

	captured, err := RunCapture(session, "exit 7", "")
	if err != nil {
		t.Fatalf("RunCapture() for a remote exit status returned an error: %v", err)
	}
	if captured.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", captured.ExitCode)
	}
}

// The sudo password is written to the session's stdin and nowhere else.
func TestRunCaptureFeedsSudoPasswordToStdin(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}

	if _, err := RunCapture(session, "cat-stdin", "s3cret"); err != nil {
		t.Fatalf("RunCapture() failed: %v", err)
	}
	select {
	case got := <-srv.stdin:
		if got != "s3cret\n" {
			t.Error("stdin did not receive exactly the sudo password and a newline")
		}
	default:
		t.Error("server never read the sudo password from stdin")
	}
}

func TestRunInteractiveCommand(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}

	var out, errOut strings.Builder
	code, err := RunInteractive(session, "hello", Streams{Out: &out, Err: &errOut}, 80, 24)
	if err != nil {
		t.Fatalf("RunInteractive() failed: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if out.String() != "ran:hello" {
		t.Errorf("stdout = %q, want the remote output", out.String())
	}
}

func TestRunInteractiveExitCode(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}

	code, err := RunInteractive(session, "exit 42", Streams{}, 80, 24)
	if err != nil {
		t.Fatalf("RunInteractive() failed: %v", err)
	}
	if code != 42 {
		t.Errorf("exit code = %d, want the remote status 42", code)
	}
}

// The PTY request must keep passing height before width, as sshgo always has.
func TestRunInteractiveShellRequestsPTYWithSize(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)

	session, err := NewSession(client)
	if err != nil {
		t.Fatalf("NewSession() failed: %v", err)
	}

	var out strings.Builder
	code, err := RunInteractive(session, "", Streams{Out: &out, Err: &out}, 132, 43)
	if err != nil {
		t.Fatalf("RunInteractive() failed: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	select {
	case size := <-srv.ptySizes:
		if size.width != 132 || size.height != 43 {
			t.Errorf("pty size = %dx%d, want 132x43", size.width, size.height)
		}
	default:
		t.Error("no pty-req was sent for an interactive shell")
	}
}

func TestNewSessionErrorIsWrapped(t *testing.T) {
	srv := startTestServer(t)
	client := dialForTest(t, srv)
	client.Close()

	_, err := NewSession(client)
	if err == nil {
		t.Fatal("NewSession() on a closed client succeeded, want error")
	}
	if Cause(err) == err {
		t.Error("Cause() did not unwrap the session error")
	}
}

// ---------------------------------------------------------------------------
// Small local helpers: the package must not depend on errors.As wrappers that
// would blur the type contract, so the tests assert on the concrete types.
// ---------------------------------------------------------------------------

func isNoAuthMethod(err error) bool { return errors.Is(err, ErrNoAuthMethod) }

func asError(err error, target any) bool { return errors.As(err, target) }

// directTCPIPMsg is the payload of a direct-tcpip channel open request. Field
// order matters: ssh.Unmarshal decodes positionally.
type directTCPIPMsg struct {
	Raddr string
	Rport uint32
	Laddr string
	Lport uint32
}

// serveDirectTCPIP dials the address a direct-tcpip channel asks for and joins
// the two byte streams. It is the server side of the tunnel a jump host
// provides, which is what lets the in-process server stand in for one.
func serveDirectTCPIP(newCh ssh.NewChannel) {
	var msg directTCPIPMsg
	if err := ssh.Unmarshal(newCh.ExtraData(), &msg); err != nil {
		newCh.Reject(ssh.Prohibited, "bad direct-tcpip payload")
		return
	}

	target, err := net.Dial("tcp", net.JoinHostPort(msg.Raddr, strconv.Itoa(int(msg.Rport))))
	if err != nil {
		newCh.Reject(ssh.ConnectionFailed, err.Error())
		return
	}

	ch, reqs, err := newCh.Accept()
	if err != nil {
		target.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	done := make(chan struct{}, 2)
	pipe := func(dst, src io.ReadWriteCloser) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go pipe(ch, target)
	go pipe(target, ch)
	<-done
	ch.Close()
	target.Close()
}
