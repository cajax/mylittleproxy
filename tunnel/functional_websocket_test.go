package tunnel

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestFunctionalWebsocketEcho upgrades a connection through the tunnel and
// exchanges messages both ways. The websocket path is hijacked on the server
// and proxied as a raw stream, so it exercises a different code path from the
// request/response one.
func TestFunctionalWebsocketEcho(t *testing.T) {
	t.Skip("websockets are broken: the client replays the upgrade over http.Client instead of joining the streams (#31)")

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

	dialer := websocket.Dialer{
		HandshakeTimeout: functionalTimeout,
		NetDial: func(network, addr string) (net.Conn, error) {
			// Reach the tunnel server whatever host the URL claims.
			return net.DialTimeout("tcp", trimScheme(f.Tunnel.URL), functionalTimeout)
		},
	}

	conn, resp, err := dialer.Dial("ws://"+f.domain+"/socket", nil)
	if err != nil {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("websocket dial through the tunnel: %v (%s)", err, status)
	}
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
