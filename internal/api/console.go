package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/runtime"
	"github.com/wstein/workharbor/internal/service"
	"github.com/wstein/workharbor/internal/termproto"
)

// ConsoleBackend is what the console routes need (design D43). The routes answer
// with a conflict when the supervisor has no console.
type ConsoleBackend interface {
	ConsoleOpen(ctx context.Context, readWrite []string) (service.ConsoleInfo, error)
	ConsoleStatus(ctx context.Context) (*service.ConsoleInfo, error)
	ConsoleClose(ctx context.Context) error
	ConsoleShell(ctx context.Context, req service.ShellRequest) (runtime.Terminal, error)
	ConsoleSSHCertificate(ctx context.Context, req service.SSHRequest) (service.SSHCertificate, error)
	ConsoleSSH(ctx context.Context, actor string) (service.SSHConn, error)
}

var errNoConsole = domain.NewConflict(domain.RuleEnvRunning, "this supervisor has no console")

// consoleOf returns the backend's console part, or one that says there is none.
func (s *Server) consoleOf() ConsoleBackend {
	if c, ok := s.be.(ConsoleBackend); ok {
		return c
	}
	return noConsole{}
}

type noConsole struct{}

func (noConsole) ConsoleOpen(context.Context, []string) (service.ConsoleInfo, error) {
	return service.ConsoleInfo{}, errNoConsole
}
func (noConsole) ConsoleStatus(context.Context) (*service.ConsoleInfo, error) { return nil, nil }
func (noConsole) ConsoleClose(context.Context) error                          { return errNoConsole }

func (noConsole) ConsoleShell(context.Context, service.ShellRequest) (runtime.Terminal, error) {
	return nil, errNoConsole
}

func (noConsole) ConsoleSSHCertificate(context.Context, service.SSHRequest) (service.SSHCertificate, error) {
	return service.SSHCertificate{}, errNoConsole
}

func (noConsole) ConsoleSSH(context.Context, string) (service.SSHConn, error) {
	return nil, errNoConsole
}

type consoleOpenBody struct {
	ReadWrite []string `json:"read_write"`
}

// consoleOpen opens the console, or returns the one that is open, with the
// workspaces named mounted read-write. The first open builds the image, which
// takes a while.
func (s *Server) consoleOpen(w http.ResponseWriter, r *http.Request) {
	var body consoleOpenBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(body.ReadWrite) > 32 {
		writeError(w, usageError{"read_write names more workspaces than a console can mount"})
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		info, err := s.consoleOf().ConsoleOpen(r.Context(), body.ReadWrite)
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, info, nil
	})
}

// consoleStatus says whether a console is open and what is writable in it.
func (s *Server) consoleStatus(w http.ResponseWriter, r *http.Request) {
	info, err := s.consoleOf().ConsoleStatus(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeOK(w, http.StatusOK, info) // null when none is open
}

// consoleClose stops and deletes the console, and keeps its home.
func (s *Server) consoleClose(w http.ResponseWriter, r *http.Request) {
	s.idempotent(w, r, nil, func() (int, any, error) {
		if err := s.consoleOf().ConsoleClose(r.Context()); err != nil {
			return 0, nil, err
		}
		return http.StatusOK, map[string]string{}, nil
	})
}

// consoleShell turns the connection into a terminal stream (termproto) after the
// client asked for the upgrade. Everything that can fail does so before the
// upgrade, as an ordinary error; after it, the stream carries the shell. It is
// behind the API token like every route, and the API is served only on its
// private unix socket (D29): a web session does not reach it.
func (s *Server) consoleShell(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), termproto.Upgrade) || !headerHasToken(r.Header, "Connection", "upgrade") {
		writeError(w, usageError{"a shell is a stream: ask for it with Connection: Upgrade and Upgrade: " + termproto.Upgrade})
		return
	}
	q := r.URL.Query()
	req := service.ShellRequest{Workspace: q.Get("workspace"), Term: q.Get("term"), Actor: s.actorOf(r)}
	for name, to := range map[string]*uint16{"cols": &req.Cols, "rows": &req.Rows} {
		if v := q.Get(name); v != "" {
			n, err := strconv.ParseUint(v, 10, 16)
			if err != nil || n == 0 {
				writeError(w, usageError{name + " must be a number from 1 to 65535"})
				return
			}
			*to = uint16(n)
		}
	}
	if len(req.Workspace) > 200 || strings.ContainsFunc(req.Workspace, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		writeError(w, usageError{"the workspace is not valid"})
		return
	}
	tm, err := s.consoleOf().ConsoleShell(r.Context(), req)
	if err != nil {
		s.fail(w, err)
		return
	}
	conn, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = tm.Close()
		s.internal(err)
		writeError(w, errors.New("the connection cannot be upgraded"))
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Time{}) // the server's timeouts end with the request; a shell is long
	pumpShell(conn, buf.Reader, tm)
}

// headerHasToken reports whether a comma-separated header has a token, in any case.
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// pumpShell answers the upgrade and carries the stream until it ends: the shell's
// output goes to the client as data frames, ending with the exit code; the
// client's data frames are the shell's input and its resize frames resize it. If
// the client goes away the terminal is closed, which ends the shell.
func pumpShell(conn net.Conn, in *bufio.Reader, tm runtime.Terminal) {
	defer func() { _ = tm.Close() }()
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+termproto.Upgrade+"\r\n\r\n"); err != nil {
		return
	}
	var wmu sync.Mutex
	send := func(f termproto.Frame) error {
		wmu.Lock()
		defer wmu.Unlock()
		return termproto.Write(conn, f)
	}
	clientGone := make(chan struct{})
	go func() { // the client's frames
		defer close(clientGone)
		for {
			f, err := termproto.Read(in)
			if err != nil {
				return
			}
			switch f.Type {
			case termproto.TypeData:
				if _, err := tm.Write(f.Data); err != nil {
					return
				}
			case termproto.TypeResize:
				if cols, rows, err := termproto.ParseResize(f); err == nil {
					_ = tm.Resize(cols, rows)
				}
			default:
				return // a frame the client may not send
			}
		}
	}()
	outDone := make(chan struct{})
	go func() { // the shell's output
		defer close(outDone)
		buf := make([]byte, 16<<10)
		for {
			n, err := tm.Read(buf)
			if n > 0 {
				if send(termproto.Frame{Type: termproto.TypeData, Data: append([]byte(nil), buf[:n]...)}) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-outDone: // the shell ended: say how
		code, err := tm.Wait()
		if err != nil {
			code = -1
		}
		_ = send(termproto.ExitFrame(code))
	case <-clientGone: // the client left: hang the terminal up
	}
}

type consoleSSHBody struct {
	PublicKey  string `json:"public_key"`
	Forwarding bool   `json:"forwarding"`
}

// consoleSSHCertificate signs the client's public key for one SSH session. The
// private key never reaches the supervisor.
func (s *Server) consoleSSHCertificate(w http.ResponseWriter, r *http.Request) {
	var body consoleSSHBody
	raw, err := readBody(w, r, &body)
	if err != nil {
		writeError(w, err)
		return
	}
	if len(body.PublicKey) == 0 || len(body.PublicKey) > 4096 {
		writeError(w, usageError{"public_key must be one public key line"})
		return
	}
	s.idempotent(w, r, raw, func() (int, any, error) {
		cert, err := s.consoleOf().ConsoleSSHCertificate(r.Context(), service.SSHRequest{PublicKey: body.PublicKey, Forwarding: body.Forwarding, Actor: s.actorOf(r)})
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, cert, nil
	})
}

// consoleSSH turns the connection into the SSH protocol, to and from an sshd that
// runs for it in the console, after the client asked for the upgrade. As for the
// shell, everything that can fail does so before the upgrade. The bytes are not
// framed: the client is an ssh with a ProxyCommand. What the sshd accepts is a
// certificate this supervisor signed, so the stream is no way in on its own.
func (s *Server) consoleSSH(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), termproto.SSHUpgrade) || !headerHasToken(r.Header, "Connection", "upgrade") {
		writeError(w, usageError{"an SSH connection is a stream: ask for it with Connection: Upgrade and Upgrade: " + termproto.SSHUpgrade})
		return
	}
	sc, err := s.consoleOf().ConsoleSSH(r.Context(), s.actorOf(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	conn, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = sc.Close()
		s.internal(err)
		writeError(w, errors.New("the connection cannot be upgraded"))
		return
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Time{})
	pumpSSH(conn, buf.Reader, sc)
}

// pumpSSH answers the upgrade and copies the bytes both ways until one side ends.
// Closing sc ends sshd.
func pumpSSH(conn net.Conn, in *bufio.Reader, sc service.SSHConn) {
	defer func() { _ = sc.Close() }()
	if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+termproto.SSHUpgrade+"\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { // the client to sshd
		_, _ = io.Copy(sc, in)
		done <- struct{}{}
	}()
	go func() { // sshd to the client
		_, _ = io.Copy(conn, sc)
		done <- struct{}{}
	}()
	<-done
}
