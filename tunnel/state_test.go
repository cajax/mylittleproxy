package tunnel

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestClientStateChangeString(t *testing.T) {
	change := &ClientStateChange{
		Identifier: "1234",
		Previous:   ClientConnecting,
		Current:    ClientConnected,
	}

	if got, want := change.String(), "[1234] ClientConnecting->ClientConnected"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	failed := &ClientStateChange{
		Identifier: "1234",
		Previous:   ClientConnected,
		Current:    ClientClosed,
		Error:      errors.New("connection reset"),
	}

	if got := failed.String(); !strings.Contains(got, "connection reset") {
		t.Errorf("String() = %q, want it to carry the error", got)
	}
}

func TestClientStateString(t *testing.T) {
	// The generated stringer has to keep up with the enum, including the value
	// past the end.
	if got, want := ClientStarted.String(), "ClientStarted"; got != want {
		t.Errorf("ClientStarted.String() = %q, want %q", got, want)
	}
	if got := ClientState(99).String(); !strings.Contains(got, "99") {
		t.Errorf("String() of an unknown state = %q, want it to mention the value", got)
	}
}

// The state channel is public API for anyone embedding the client, so the
// transitions it reports have to be the real ones.
func TestClientStateChangesThroughAConnection(t *testing.T) {
	states := make(chan *ClientStateChange, 16)

	f := newFixture(t, fixtureConfig{noClient: true})

	c := f.startClientWith(func(cfg *ClientConfig) {
		cfg.StateChanges = states
	}, "1234", "app.example.com", f.Local.URL, nil)
	f.waitConnected(c)

	want := []ClientState{ClientStarted, ClientConnecting, ClientConnected}
	got := collectStates(t, states, len(want))

	for i, state := range want {
		if got[i] != state {
			t.Errorf("transition %d was %s, want %s (whole sequence: %v)", i, got[i], state, got)
		}
	}

	// Closing the client has to be reported too.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	eventually(t, "the disconnect to be reported", func() bool {
		select {
		case change := <-states:
			return change.Current == ClientDisconnected || change.Current == ClientClosed
		default:
			return false
		}
	})
}

func TestServerReportsClientStateChanges(t *testing.T) {
	states := make(chan *ClientStateChange, 16)

	f := newFixture(t, fixtureConfig{noClient: true, stateChanges: states})

	c := f.startClient("1234", "app.example.com", f.Local.URL, nil)
	f.waitConnected(c)

	select {
	case change := <-states:
		if change.Current != ClientConnected {
			t.Errorf("first state = %s, want %s", change.Current, ClientConnected)
		}
		if change.Identifier != "1234" {
			t.Errorf("identifier = %q, want %q", change.Identifier, "1234")
		}
	case <-time.After(functionalTimeout):
		t.Fatal("the server reported no state change when a client connected")
	}

	c.Close()

	eventually(t, "the server to report the disconnect", func() bool {
		select {
		case change := <-states:
			return change.Current == ClientClosed
		default:
			return false
		}
	})
}

func TestServerOnConnectAndOnDisconnectCallbacks(t *testing.T) {
	connected := make(chan struct{}, 1)
	disconnected := make(chan struct{}, 1)

	f := newFixture(t, fixtureConfig{noClient: true})

	f.Server.OnConnect("1234", func() error {
		connected <- struct{}{}
		return nil
	})
	f.Server.OnDisconnect("1234", func() error {
		disconnected <- struct{}{}
		return nil
	})

	c := f.startClient("1234", "app.example.com", f.Local.URL, nil)
	f.waitConnected(c)

	select {
	case <-connected:
	case <-time.After(functionalTimeout):
		t.Fatal("OnConnect was never called")
	}

	c.Close()

	select {
	case <-disconnected:
	case <-time.After(functionalTimeout):
		t.Fatal("OnDisconnect was never called")
	}
}

func TestCallbacksAreCalledOnceAndSurviveMisuse(t *testing.T) {
	cb := newCallbacks("OnConnect")

	var calls int
	cb.add("1234", func() error {
		calls++
		return nil
	})

	if err := cb.call("1234"); err != nil {
		t.Fatalf("call: %v", err)
	}
	// Documented behaviour: a callback is removed once it has run.
	if err := cb.call("1234"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 1 {
		t.Errorf("callback ran %d times, want once", calls)
	}

	// An identifier nobody registered is a no-op, not an error.
	if err := cb.call("nobody"); err != nil {
		t.Errorf("call for an unknown identifier = %v, want nil", err)
	}

	cb.add("broken", nil)
	if err := cb.call("broken"); err == nil {
		t.Error("a nil callback was accepted silently")
	}

	cb.add("failing", func() error { return errors.New("boom") })
	if err := cb.call("failing"); err == nil {
		t.Error("an error from a callback was swallowed")
	}
}

func collectStates(t *testing.T, states <-chan *ClientStateChange, n int) []ClientState {
	t.Helper()

	got := make([]ClientState, 0, n)
	for len(got) < n {
		select {
		case change := <-states:
			got = append(got, change.Current)
		case <-time.After(functionalTimeout):
			t.Fatalf("only %d of %d state changes arrived: %v", len(got), n, got)
		}
	}

	return got
}
