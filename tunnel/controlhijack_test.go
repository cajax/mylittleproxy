package tunnel

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/proto"
	"go.uber.org/zap"
)

// TestFailureAfterHijackClosesConnectionWithoutHTTPError drives the control
// endpoint over a real connection and then stalls, so the handshake times out
// after the hijack. The server cannot write an HTTP response at that point: it
// must close the connection instead of appending an error to the stream.
func TestFailureAfterHijackClosesConnectionWithoutHTTPError(t *testing.T) {
	restore := defaultTimeout
	defaultTimeout = 100 * time.Millisecond
	defer func() { defaultTimeout = restore }()

	s := testServer(t, &ServerConfig{
		Log:          zap.NewNop(),
		AllowedHosts: []string{`^.*\.example\.com$`},
	})

	srv := httptest.NewServer(s)
	defer srv.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	body, err := json.Marshal(httpConfig("app.example.com"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequest(s.controlMethod, srv.URL+s.controlPath, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set(proto.ClientIdentifierHeader, "alice")
	req.Header.Set(proto.ClientIdentifierSignature, signIdentifier("alice", s.signatureKey))
	if err := req.Write(conn); err != nil {
		t.Fatalf("write request: %v", err)
	}

	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.Contains(line, proto.Connected) {
		t.Fatalf("status line = %q, want it to contain %q", line, proto.Connected)
	}

	// The client now says nothing, so the server's wait for a yamux stream times
	// out. The connection must be closed, and whatever was written in the
	// meantime must not be an HTTP response: the bytes belong to the tunnel now.
	// yamux control frames (a GoAway as the session shuts down) are expected.
	rest, err := io.ReadAll(br)
	if err != nil && err != io.EOF {
		t.Fatalf("read after hijack: %v", err)
	}

	for _, unwanted := range []string{"HTTP/", "502", "Bad Gateway", "timeout getting session"} {
		if strings.Contains(string(rest), unwanted) {
			t.Errorf("server wrote %q into the hijacked connection; it contains %q, so an HTTP error was written after the hijack",
				rest, unwanted)
		}
	}
}
