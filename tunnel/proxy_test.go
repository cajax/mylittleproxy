package tunnel

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/proto"
)

// readAll drains conn until it is closed, so a test can tell "closed without a
// reply" from "replied".
//
// The deadline is best effort: net.Pipe refuses to set one once either end is
// closed, and these tests are about the proxy closing its end, so losing that
// race is not a failure. Callers set a deadline when they create the pipe.
func readAll(t *testing.T, conn net.Conn) string {
	t.Helper()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("SetDeadline: %v", err)
	}

	b, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

// deadlinedPipe returns a pipe whose client end already has a deadline, set
// before anything can close the other end.
func deadlinedPipe(t *testing.T) (client, remote net.Conn) {
	t.Helper()

	client, remote = net.Pipe()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	return client, remote
}

func TestProxyClosesConnectionForUnknownProtocol(t *testing.T) {
	client, remote := deadlinedPipe(t)
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// No ProxyFunc exists for this protocol; it must be refused, not
		// dispatched to a nil function.
		Proxy(ProxyFuncs{})(remote, &proto.ControlMessage{
			Action:   proto.RequestClientSession,
			Protocol: proto.Type(99),
		})
	}()

	if got := readAll(t, client); got != "" {
		t.Errorf("got %q on the wire, want the connection closed with no reply", got)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Proxy did not return")
	}
}

func TestProxyForTCPWithoutImplementationDoesNotPanic(t *testing.T) {
	client, remote := deadlinedPipe(t)
	defer client.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// DefaultProxyFuncs.TCP is nil.
		Proxy(ProxyFuncs{})(remote, &proto.ControlMessage{
			Action:   proto.RequestClientSession,
			Protocol: proto.TCP,
		})
	}()

	readAll(t, client)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Proxy did not return")
	}
}

func TestHTTPProxyWithoutLoggerDoesNotPanic(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer local.Close()

	// HTTPProxy is exported: a caller that leaves Log unset must not crash the
	// process on the first request.
	p := &HTTPProxy{TargetHost: local.URL}

	client, remote := net.Pipe()
	defer client.Close()

	go p.Proxy(remote, &proto.ControlMessage{Action: proto.RequestClientSession, Protocol: proto.HTTP})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	if _, err := io.WriteString(client, "GET /ok HTTP/1.1\r\nHost: \r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestClientWebsocketProxyHasALogger(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer local.Close()

	c, err := NewClient(&ClientConfig{
		Identifier: "1234",
		ServerAddr: "127.0.0.1:0",
		ConnectionConfig: proto.ConnectionConfig{
			Http: proto.HTTPConfig{Domain: "app.example.com", Target: local.URL},
		},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	client, remote := net.Pipe()
	defer client.Close()

	// The websocket branch used to be built without a logger and panicked here.
	go c.proxy(remote, &proto.ControlMessage{Action: proto.RequestClientSession, Protocol: proto.WS})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	if _, err := io.WriteString(client, "GET /ok HTTP/1.1\r\nHost: \r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(client), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHandleWSConnNeedsAHijackableWriter(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	// httptest.ResponseRecorder does not implement http.Hijacker.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://app.example.com/socket", nil)

	err := s.handleWSConn(w, req, "1234", 0)
	if err == nil {
		t.Fatal("no error although the response writer cannot be hijacked")
	}
	if !strings.Contains(err.Error(), "hijack") {
		t.Errorf("error = %v, want it to mention hijacking", err)
	}
}
