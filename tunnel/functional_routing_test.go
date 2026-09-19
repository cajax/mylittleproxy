package tunnel

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/proto"
)

// connectTimeout is how long a client that is expected to be refused is given
// to prove it, well short of functionalTimeout.
const connectTimeout = 2 * time.Second

func refuseConnection(t *testing.T, c *Client, why string) {
	t.Helper()

	select {
	case <-c.StartNotify():
		t.Fatalf("client connected although %s", why)
	case <-time.After(connectTimeout):
	}
}

func TestFunctionalRequestForUnknownHost(t *testing.T) {
	f := newFixture(t, fixtureConfig{})

	resp := f.DoForHost("nobody.example.com", http.MethodGet, "/", "")
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestFunctionalTwoClientsDoNotCrossTalk(t *testing.T) {
	f := newFixture(t, fixtureConfig{
		domain:     "alice.example.com",
		identifier: "alice",
		handler: func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "alice")
		},
	})

	// A second local server and a second client on the same tunnel server.
	bob := newFixture(t, fixtureConfig{noClient: true, handler: func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "bob")
	}})
	bobClient := f.startClient("bob", "bob.example.com", bob.Local.URL,
		[]proto.HTTPRewriteRule{{From: "/", To: "/"}})
	f.waitConnected(bobClient)
	t.Cleanup(func() { bobClient.Close() })

	if got := bodyOf(t, f.DoForHost("alice.example.com", http.MethodGet, "/", "")); got != "alice" {
		t.Errorf("alice.example.com served %q, want %q", got, "alice")
	}
	if got := bodyOf(t, f.DoForHost("bob.example.com", http.MethodGet, "/", "")); got != "bob" {
		t.Errorf("bob.example.com served %q, want %q", got, "bob")
	}
}

// Regression test for #22: a second client must not be able to take over a
// domain that is already serving.
func TestFunctionalHostTakeoverIsRefused(t *testing.T) {
	f := newFixture(t, fixtureConfig{
		domain:     "app.example.com",
		identifier: "alice",
		handler: func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "alice")
		},
	})

	mallory := newFixture(t, fixtureConfig{noClient: true, handler: func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "mallory")
	}})
	malloryClient := f.startClient("mallory", "app.example.com", mallory.Local.URL,
		[]proto.HTTPRewriteRule{{From: "/", To: "/"}})
	t.Cleanup(func() { malloryClient.Close() })

	refuseConnection(t, malloryClient, "the host belongs to another client")

	if got := bodyOf(t, f.Do(http.MethodGet, "/", "")); got != "alice" {
		t.Errorf("the domain served %q, want %q: it was taken over", got, "alice")
	}
}

func TestFunctionalClientNotInAllowList(t *testing.T) {
	f := newFixture(t, fixtureConfig{
		noClient:       true,
		allowedClients: []string{"alice"},
	})

	c := f.startClient("mallory", "app.example.com", f.Local.URL,
		[]proto.HTTPRewriteRule{{From: "/", To: "/"}})
	t.Cleanup(func() { c.Close() })

	refuseConnection(t, c, "its identifier is not in allowedClients")
}

func TestFunctionalHostNotInAllowList(t *testing.T) {
	f := newFixture(t, fixtureConfig{
		noClient:     true,
		allowedHosts: []string{`^.*\.example\.com$`},
	})

	c := f.startClient("1234", "app.evil.com", f.Local.URL,
		[]proto.HTTPRewriteRule{{From: "/", To: "/"}})
	t.Cleanup(func() { c.Close() })

	refuseConnection(t, c, "its domain does not match allowedHosts")
}

func TestFunctionalWrongSignatureKey(t *testing.T) {
	f := newFixture(t, fixtureConfig{noClient: true})

	c, err := NewClient(&ClientConfig{
		Identifier:    "1234",
		SignatureKey:  "not-the-server-key",
		ServerAddr:    trimScheme(f.Tunnel.URL),
		ControlPath:   proto.DefaultControlPath,
		ControlMethod: proto.DefaultControlMethod,
		Log:           nopLogger(),
		ConnectionConfig: proto.ConnectionConfig{Http: proto.HTTPConfig{
			Domain:  "app.example.com",
			Target:  f.Local.URL,
			Rewrite: []proto.HTTPRewriteRule{{From: "/", To: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	go c.Start()
	t.Cleanup(func() { c.Close() })

	refuseConnection(t, c, "its identifier is signed with the wrong key")
}
