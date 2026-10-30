package connect

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Op names the session step that failed. Callers map it to their own frozen
// wording, so the same failure can read differently for an interactive login
// and for `exec` without duplicating the step logic.
type Op string

const (
	OpNewSession Op = "new session"
	OpPTY        Op = "request pty"
	OpShell      Op = "start shell"
	OpRun        Op = "run command"
)

// Error is a failed session step. The cause is kept verbatim so callers can
// print it with their own prefix.
type Error struct {
	Op  Op
	Err error
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %v", e.Op, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// Cause returns the underlying failure, unwrapping a session Error so callers
// can print it with their own frozen wording.
func Cause(err error) error {
	var sessionErr *Error
	if errors.As(err, &sessionErr) {
		return sessionErr.Err
	}
	return err
}

// Streams are the three standard streams an interactive session is wired to.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// NewSession opens a session on an established client.
func NewSession(client *ssh.Client) (*ssh.Session, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, &Error{Op: OpNewSession, Err: err}
	}
	return session, nil
}

// RunInteractive executes command on session, or opens an interactive shell when
// command is empty. It returns the process exit code.
//
// A shell that ends with an error is reported as exit code 0: the remote side
// closed the session, which is not sshgo's failure to report.
func RunInteractive(session *ssh.Session, command string, streams Streams, width, height int) (int, error) {
	defer session.Close()

	if command == "" {
		if err := RequestPTY(session, width, height); err != nil {
			return 1, &Error{Op: OpPTY, Err: err}
		}
	}

	session.Stdin = streams.In
	session.Stdout = streams.Out
	session.Stderr = streams.Err

	if command != "" {
		err := session.Run(command)
		if err == nil {
			return 0, nil
		}
		if status, ok := ExitStatus(err); ok {
			return status, nil
		}
		return 1, nil
	}

	if err := session.Shell(); err != nil {
		return 1, &Error{Op: OpShell, Err: err}
	}
	_ = session.Wait()
	return 0, nil
}

// Captured is the result of a non-interactive command run.
type Captured struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCapture executes command on session with captured output. sudoPassword,
// when non-empty, is written to the session's stdin so `sudo -S` can read it;
// it is never part of an authentication method.
//
// A remote exit status is reported through ExitCode; any other failure is
// returned so the caller can attach its own message.
func RunCapture(session *ssh.Session, command, sudoPassword string) (Captured, error) {
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf
	if sudoPassword != "" {
		session.Stdin = strings.NewReader(sudoPassword + "\n")
	}

	err := session.Run(command)

	captured := Captured{Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}
	if err == nil {
		return captured, nil
	}
	if status, ok := ExitStatus(err); ok {
		captured.ExitCode = status
		return captured, nil
	}
	captured.ExitCode = 1
	return captured, &Error{Op: OpRun, Err: err}
}
