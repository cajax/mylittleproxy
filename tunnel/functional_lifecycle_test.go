package tunnel

import (
	"net/http"
	"testing"
)

func TestFunctionalClientDisconnectRemovesTheRoute(t *testing.T) {
	f := newFixture(t, fixtureConfig{})

	resp := f.Do(http.MethodGet, "/", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status before disconnect = %d, want 200", resp.StatusCode)
	}

	if err := f.Client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	eventually(t, "the route to be removed", func() bool {
		resp := f.Do(http.MethodGet, "/", "")
		resp.Body.Close()
		return resp.StatusCode == http.StatusBadGateway
	})
}

func TestFunctionalClientReconnectsAfterControlConnectionDrops(t *testing.T) {
	f := newFixture(t, fixtureConfig{identifier: "1234"})

	resp := f.Do(http.MethodGet, "/", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status before the drop = %d, want 200", resp.StatusCode)
	}

	// Drop the control connection from the server side, as a network failure
	// would.
	ct, ok := f.Server.getControl("1234")
	if !ok {
		t.Fatal("no control connection registered for the client")
	}
	if err := ct.Close(); err != nil {
		t.Fatalf("closing the control connection: %v", err)
	}

	eventually(t, "the client to reconnect and serve again", func() bool {
		resp := f.Do(http.MethodGet, "/", "")
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

func TestFunctionalLocalServerDown(t *testing.T) {
	f := newFixture(t, fixtureConfig{noClient: true})

	// A target with nothing listening: the local server's address after it has
	// been closed.
	dead := f.Local.URL
	f.Local.Close()

	c := f.startClient("1234", "app.example.com", dead, nil)
	f.waitConnected(c)
	t.Cleanup(func() { c.Close() })

	resp := f.Do(http.MethodGet, "/", "")

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if got, want := bodyOf(t, resp), "no local server"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
