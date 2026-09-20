package tunnel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/proto"
	"go.uber.org/zap"
)

// This file builds a real tunnel for the functional tests: a local target
// server, a tunnel server, and a tunnel client connected to it over a live
// yamux session. Tests drive it through the tunnel server's public address,
// exactly as an outside caller would.

const functionalTimeout = 15 * time.Second

type fixture struct {
	t *testing.T

	// Local is the server the client proxies to.
	Local *httptest.Server
	// Tunnel is the public face of the tunnel server.
	Tunnel *httptest.Server

	Server *Server
	Client *Client

	domain string
}

type fixtureConfig struct {
	domain     string
	identifier string
	rewrites   []proto.HTTPRewriteRule
	handler    http.HandlerFunc
	// customHeaders the client sets on requests to the local server.
	customHeaders map[string]string

	allowedHosts   []string
	allowedClients []string
	// stateChanges receives the server's view of client state transitions.
	stateChanges chan<- *ClientStateChange
	// target overrides the local server's address, for the cases where there is
	// nothing listening.
	target string
	// noClient starts the server without a client.
	noClient bool
}

func (c *fixtureConfig) withDefaults() {
	if c.domain == "" {
		c.domain = "app.example.com"
	}
	if c.identifier == "" {
		c.identifier = "1234"
	}
	if len(c.rewrites) == 0 {
		c.rewrites = []proto.HTTPRewriteRule{{From: "/", To: "/"}}
	}
	if c.handler == nil {
		c.handler = func(w http.ResponseWriter, r *http.Request) {}
	}
	if len(c.allowedHosts) == 0 {
		c.allowedHosts = []string{`^.*\.example\.com$`}
	}
}

// newFixture starts the three servers and waits for the client to connect.
func newFixture(t *testing.T, cfg fixtureConfig) *fixture {
	t.Helper()

	cfg.withDefaults()

	f := &fixture{t: t, domain: cfg.domain}

	f.Local = httptest.NewServer(cfg.handler)

	f.Server = testServer(t, &ServerConfig{
		Log:            zap.NewNop(),
		AllowedHosts:   cfg.allowedHosts,
		AllowedClients: cfg.allowedClients,
		StateChanges:   cfg.stateChanges,
	})
	f.Tunnel = httptest.NewServer(f.Server)

	t.Cleanup(func() {
		if f.Client != nil {
			f.Client.Close()
		}
		f.Tunnel.Close()
		f.Local.Close()
	})

	if cfg.noClient {
		return f
	}

	target := cfg.target
	if target == "" {
		target = f.Local.URL
	}

	f.Client = f.startClientWith(func(c *ClientConfig) {
		c.CustomHeaders = cfg.customHeaders
	}, cfg.identifier, cfg.domain, target, cfg.rewrites)
	f.waitConnected(f.Client)

	return f
}

// startClient connects another client to the same tunnel server.
func (f *fixture) startClient(identifier, domain, target string, rewrites []proto.HTTPRewriteRule) *Client {
	f.t.Helper()

	return f.startClientWith(nil, identifier, domain, target, rewrites)
}

// startClientWith is startClient with a hook to adjust the config first.
func (f *fixture) startClientWith(adjust func(*ClientConfig), identifier, domain, target string, rewrites []proto.HTTPRewriteRule) *Client {
	f.t.Helper()

	if len(rewrites) == 0 {
		rewrites = []proto.HTTPRewriteRule{{From: "/", To: "/"}}
	}

	cfg := &ClientConfig{
		Identifier:    identifier,
		SignatureKey:  testSignatureKey,
		ServerAddr:    trimScheme(f.Tunnel.URL),
		ControlPath:   proto.DefaultControlPath,
		ControlMethod: proto.DefaultControlMethod,
		Log:           zap.NewNop(),
		ConnectionConfig: proto.ConnectionConfig{Http: proto.HTTPConfig{
			Domain:  domain,
			Target:  target,
			Rewrite: rewrites,
		}},
	}

	if adjust != nil {
		adjust(cfg)
	}

	c, err := NewClient(cfg)
	if err != nil {
		f.t.Fatalf("NewClient: %v", err)
	}

	go c.Start()

	return c
}

func trimScheme(url string) string {
	return strings.TrimPrefix(url, "http://")
}

func nopLogger() *zap.Logger {
	return zap.NewNop()
}

func (f *fixture) waitConnected(c *Client) {
	f.t.Helper()

	select {
	case <-c.StartNotify():
	case <-time.After(functionalTimeout):
		f.t.Fatal("client did not connect to the tunnel server")
	}
}

// Do sends a request to the tunnel server for the fixture's domain.
func (f *fixture) Do(method, path string, body string) *http.Response {
	f.t.Helper()
	return f.DoForHost(f.domain, method, path, body)
}

func (f *fixture) DoForHost(host, method, path, body string) *http.Response {
	f.t.Helper()

	req, err := http.NewRequest(method, f.Tunnel.URL+path, strings.NewReader(body))
	if err != nil {
		f.t.Fatalf("NewRequest: %v", err)
	}
	// What the tunnel server routes on.
	req.Host = host

	client := &http.Client{Timeout: functionalTimeout}
	resp, err := client.Do(req)
	if err != nil {
		f.t.Fatalf("%s %s: %v", method, path, err)
	}

	return resp
}

// eventually retries fn until it returns true or the timeout expires. Used for
// the states a client reaches on its own schedule, like reconnecting.
func eventually(t *testing.T, what string, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(functionalTimeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return string(b)
}
