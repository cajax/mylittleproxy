package tunnel

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/proto"
)

func TestDialTargetSchemesAndDefaultPorts(t *testing.T) {
	// A listener stands in for the local server so the dial can succeed.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	t.Run("explicit port", func(t *testing.T) {
		conn, err := dialTarget("http://" + ln.Addr().String())
		if err != nil {
			t.Fatalf("dialTarget: %v", err)
		}
		conn.Close()
	})

	t.Run("unsupported scheme", func(t *testing.T) {
		if _, err := dialTarget("ftp://127.0.0.1:21"); err == nil {
			t.Error("dialTarget accepted an unsupported scheme")
		}
	})

	t.Run("unparsable target", func(t *testing.T) {
		if _, err := dialTarget("http://[::1"); err == nil {
			t.Error("dialTarget accepted an unparsable target")
		}
	})
}

func TestHostHeaderFor(t *testing.T) {
	tests := []struct {
		target   string
		original string
		want     string
	}{
		{"http://127.0.0.1:8080", "", "127.0.0.1:8080"},
		{"https://local.host", "", "local.host"},
		// Nothing usable in the target: keep what arrived.
		{"", "app.example.com", "app.example.com"},
	}

	for _, tt := range tests {
		if got := hostHeaderFor(tt.target, tt.original); got != tt.want {
			t.Errorf("hostHeaderFor(%q, %q) = %q, want %q", tt.target, tt.original, got, tt.want)
		}
	}
}

func TestWSProxyRefusesWrongProtocol(t *testing.T) {
	client, remote := net.Pipe()
	defer client.Close()

	p := &WSProxy{TargetHost: "http://127.0.0.1:1"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Proxy(remote, &proto.ControlMessage{Action: proto.RequestClientSession, Protocol: proto.HTTP})
	}()

	if got := readAll(t, client); got != "" {
		t.Errorf("got %q, want the connection closed with no reply", got)
	}
	<-done
}

// The upgrade request must reach the local server with a usable Host header:
// the tunnel server clears it when it rewrites the request.
func TestWSProxySetsHostHeaderForLocalServer(t *testing.T) {
	gotHost := make(chan string, 1)

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost <- r.Host
	}))
	defer local.Close()

	client, remote := net.Pipe()
	defer client.Close()

	p := &WSProxy{TargetHost: local.URL}
	go p.Proxy(remote, &proto.ControlMessage{Action: proto.RequestClientSession, Protocol: proto.WS})

	upgrade := "GET /socket HTTP/1.1\r\nHost: \r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := client.Write([]byte(upgrade)); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}

	select {
	case host := <-gotHost:
		if host != strings.TrimPrefix(local.URL, "http://") {
			t.Errorf("local server saw Host %q, want %q", host, strings.TrimPrefix(local.URL, "http://"))
		}
	case <-time.After(functionalTimeout):
		t.Fatal("the upgrade request never reached the local server")
	}
}

// dialTarget verifies certificates, so a local server with a self-signed
// certificate is refused. That is the behaviour today, not necessarily the one
// we want for a tool aimed at development boxes: see #42.
func TestDialTargetVerifiesCertificates(t *testing.T) {
	local := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer local.Close()

	conn, err := dialTarget(local.URL)
	if err == nil {
		conn.Close()
		t.Fatal("dialTarget accepted a self-signed certificate")
	}
	if !strings.Contains(err.Error(), "certificate") && !strings.Contains(err.Error(), "x509") {
		t.Errorf("error = %v, want a certificate failure", err)
	}
}

func TestTargetAddress(t *testing.T) {
	tests := []struct {
		target   string
		wantAddr string
		wantTLS  bool
		wantErr  bool
	}{
		{target: "http://127.0.0.1:3000", wantAddr: "127.0.0.1:3000"},
		{target: "http://local.host", wantAddr: "local.host:80"},
		{target: "https://local.host", wantAddr: "local.host:443", wantTLS: true},
		{target: "https://local.host:8443", wantAddr: "local.host:8443", wantTLS: true},
		{target: "ws://local.host", wantAddr: "local.host:80"},
		{target: "wss://local.host", wantAddr: "local.host:443", wantTLS: true},
		{target: "//local.host:3000", wantAddr: "local.host:3000"},
		{target: "ftp://local.host", wantErr: true},
		{target: "http://[::1", wantErr: true},
		{target: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			addr, useTLS, err := targetAddress(tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("targetAddress(%q) error = %v, wantErr %v", tt.target, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if addr != tt.wantAddr {
				t.Errorf("address = %q, want %q", addr, tt.wantAddr)
			}
			if useTLS != tt.wantTLS {
				t.Errorf("useTLS = %v, want %v", useTLS, tt.wantTLS)
			}
		})
	}
}
