package tunnel

import (
	"bufio"
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

func TestPatchRequestKeepsEverythingButSchemeAndHost(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		requestURI string
		want       string
	}{
		{
			name:       "query string is preserved",
			target:     "http://127.0.0.1:8080",
			requestURI: "/api/test?foo=bar&code=abc123",
			want:       "http://127.0.0.1:8080/api/test?foo=bar&code=abc123",
		},
		{
			name:       "no query string",
			target:     "http://127.0.0.1:8080",
			requestURI: "/api/test",
			want:       "http://127.0.0.1:8080/api/test",
		},
		{
			name:       "empty query string is kept as such",
			target:     "http://127.0.0.1:8080",
			requestURI: "/api/test?",
			want:       "http://127.0.0.1:8080/api/test?",
		},
		{
			name:       "scheme of the target wins",
			target:     "https://local.host",
			requestURI: "/callback?state=xyz",
			want:       "https://local.host/callback?state=xyz",
		},
		{
			name:       "encoded characters are not mangled",
			target:     "http://127.0.0.1:8080",
			requestURI: "/api/a%20b?q=%2Fslash%26amp",
			want:       "http://127.0.0.1:8080/api/a%20b?q=%2Fslash%26amp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The request as the client reads it off the tunnel: the server
			// wrote it with r.Write after clearing r.Host.
			req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(
				"GET " + tt.requestURI + " HTTP/1.1\r\nHost: \r\n\r\n")))
			if err != nil {
				t.Fatalf("ReadRequest: %v", err)
			}

			p := &HTTPProxy{TargetHost: tt.target, Log: zap.NewNop()}
			if err := p.patchRequest(req); err != nil {
				t.Fatalf("patchRequest: %v", err)
			}

			if got := req.URL.String(); got != tt.want {
				t.Errorf("outbound URL = %q, want %q", got, tt.want)
			}
			if req.RequestURI != "" {
				t.Errorf("RequestURI = %q, want empty (a client request must not set it)", req.RequestURI)
			}
		})
	}
}

func TestPatchRequestRejectsUnparsableTarget(t *testing.T) {
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader("GET / HTTP/1.1\r\nHost: \r\n\r\n")))
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}

	p := &HTTPProxy{TargetHost: "http://[::1", Log: zap.NewNop()}
	if err := p.patchRequest(req); err == nil {
		t.Fatal("patchRequest accepted an unparsable target")
	}
}

// TestProxyForwardsQueryToLocalServer drives HTTPProxy.Proxy the way the client
// does: a request arrives on the tunnel connection, and the response is written
// back to it.
func TestProxyForwardsQueryToLocalServer(t *testing.T) {
	var gotURI string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		io.WriteString(w, "ok")
	}))
	defer local.Close()

	p := &HTTPProxy{TargetHost: local.URL, Log: zap.NewNop()}

	client, remote := net.Pipe()
	defer client.Close()

	go p.Proxy(remote, &proto.ControlMessage{Action: proto.RequestClientSession, Protocol: proto.HTTP})

	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	if _, err := io.WriteString(client, "GET /callback?code=abc123&state=xyz HTTP/1.1\r\nHost: \r\n\r\n"); err != nil {
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
	if want := "/callback?code=abc123&state=xyz"; gotURI != want {
		t.Errorf("local server saw %q, want %q", gotURI, want)
	}
}
