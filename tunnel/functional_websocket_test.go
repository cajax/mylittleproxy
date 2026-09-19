package tunnel

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// websocketDialer reaches the tunnel server whatever host the URL claims, so
// the request carries the tunnelled domain in its Host header.
func (f *fixture) websocketDialer() *websocket.Dialer {
	return &websocket.Dialer{
		HandshakeTimeout: functionalTimeout,
		NetDial: func(network, addr string) (net.Conn, error) {
			return net.DialTimeout("tcp", trimScheme(f.Tunnel.URL), functionalTimeout)
		},
	}
}

func (f *fixture) dialWebsocket(path string) *websocket.Conn {
	f.t.Helper()

	conn, resp, err := f.websocketDialer().Dial("ws://"+f.domain+path, nil)
	if err != nil {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		f.t.Fatalf("websocket dial through the tunnel: %v (%s)", err, status)
	}

	return conn
}

// TestFunctionalWebsocketEcho upgrades a connection through the tunnel and
// exchanges messages both ways. The websocket path is hijacked on the server
// and proxied as a raw stream, so it exercises a different code path from the
// request/response one.
func TestFunctionalWebsocketEcho(t *testing.T) {
	upgrader := websocket.Upgrader{}

	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade on the local server: %v", err)
				return
			}
			defer conn.Close()

			for {
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if err := conn.WriteMessage(mt, append([]byte("echo: "), msg...)); err != nil {
					return
				}
			}
		},
	})

	conn := f.dialWebsocket("/socket")
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(functionalTimeout)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(functionalTimeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if got, want := string(msg), "echo: ping"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFunctionalWebsocketManyMessages proves the connection stays open and
// bidirectional rather than serving a single exchange.
func TestFunctionalWebsocketManyMessages(t *testing.T) {
	upgrader := websocket.Upgrader{}

	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade on the local server: %v", err)
				return
			}
			defer conn.Close()

			for {
				mt, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if err := conn.WriteMessage(mt, append([]byte("echo: "), msg...)); err != nil {
					return
				}
			}
		},
	})

	conn := f.dialWebsocket("/socket")
	defer conn.Close()

	for i := 0; i < 20; i++ {
		want := fmt.Sprintf("message-%d", i)

		if err := conn.SetWriteDeadline(time.Now().Add(functionalTimeout)); err != nil {
			t.Fatalf("SetWriteDeadline: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}

		if err := conn.SetReadDeadline(time.Now().Add(functionalTimeout)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}

		if got := string(msg); got != "echo: "+want {
			t.Fatalf("message %d: got %q, want %q", i, got, "echo: "+want)
		}
	}
}

func TestFunctionalWebsocketBinaryFrames(t *testing.T) {
	upgrader := websocket.Upgrader{}

	payload := bytes.Repeat([]byte{0x00, 0xff, 0x7f}, 5000)

	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade on the local server: %v", err)
				return
			}
			defer conn.Close()

			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, msg)
		},
	})

	conn := f.dialWebsocket("/socket")
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(functionalTimeout)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(functionalTimeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	mt, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if mt != websocket.BinaryMessage {
		t.Errorf("message type = %d, want %d", mt, websocket.BinaryMessage)
	}
	if !bytes.Equal(msg, payload) {
		t.Errorf("payload came back altered: %d bytes, want %d", len(msg), len(payload))
	}
}

func TestFunctionalWebsocketCloseFromLocalServerReachesCaller(t *testing.T) {
	upgrader := websocket.Upgrader{}

	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade on the local server: %v", err)
				return
			}
			// Say goodbye immediately.
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"))
			conn.Close()
		},
	})

	conn := f.dialWebsocket("/socket")
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(functionalTimeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("read succeeded, want the close to have been propagated")
	}
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure) && !websocket.IsUnexpectedCloseError(err) {
		t.Logf("close surfaced as %v", err)
	}
}

func TestFunctionalWebsocketLocalServerDown(t *testing.T) {
	f := newFixture(t, fixtureConfig{noClient: true})

	dead := f.Local.URL
	f.Local.Close()

	c := f.startClient("1234", "app.example.com", dead, nil)
	f.waitConnected(c)
	t.Cleanup(func() { c.Close() })

	_, resp, err := f.websocketDialer().Dial("ws://"+f.domain+"/socket", nil)
	if err == nil {
		t.Fatal("dial succeeded, want it refused: there is no local server")
	}
	if resp == nil {
		t.Fatalf("no response came back, the caller was left hanging: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}
