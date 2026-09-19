package tunnel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/proto"
)

// When the two listeners are separated, the control endpoint must exist on one
// of them and not the other.
func TestControlHandlerServesOnlyTheControlPath(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	t.Run("the control path is served", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.ControlHandler().ServeHTTP(w, controlRequest(t, s, "1234", httpConfig("app.example.com")))

		// Reaching the handler at all is the point: it fails later, on the
		// hijack that a recorder cannot do.
		if w.Code == http.StatusNotFound {
			t.Fatalf("status = 404, want the control handler to serve %q", s.controlPath)
		}
	})

	t.Run("anything else is not", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://app.example.com/somewhere", nil)

		w := httptest.NewRecorder()
		s.ControlHandler().ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404: the control listener must not proxy traffic", w.Code)
		}
	})
}

// On the public listener the control path is ordinary traffic, so a client can
// serve a domain that happens to use that path.
func TestPublicHandlerDoesNotServeTheControlProtocol(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	req := controlRequest(t, s, "1234", httpConfig("app.example.com"))

	w := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502: the control protocol must not be reachable here", w.Code)
	}
	if _, ok := s.getIdentifier("app.example.com"); ok {
		t.Error("a virtual host was registered through the public listener")
	}
}

// ServeHTTP keeps serving both, which is what a single-listener deployment uses.
func TestServeHTTPServesBoth(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	w := httptest.NewRecorder()
	req := controlRequest(t, s, "1234", httpConfig("app.example.com"))
	req.Method = http.MethodGet // rejected by the control handler, not by routing

	s.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405 from the control handler", w.Code)
	}
}

func TestProtoDefaultsAreUsedForRouting(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	if s.controlPath != proto.DefaultControlPath {
		t.Fatalf("controlPath = %q, want the default", s.controlPath)
	}
}

// TestSeparatedListenersEndToEnd runs the two handlers on two listeners, as a
// deployment that keeps the control protocol off the public network does.
func TestSeparatedListenersEndToEnd(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "served")
	}))
	defer local.Close()

	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	control := httptest.NewServer(s.ControlHandler())
	defer control.Close()
	public := httptest.NewServer(s.PublicHandler())
	defer public.Close()

	client, err := NewClient(&ClientConfig{
		Identifier:    "1234",
		SignatureKey:  testSignatureKey,
		ServerAddr:    trimScheme(control.URL),
		ControlPath:   proto.DefaultControlPath,
		ControlMethod: proto.DefaultControlMethod,
		Log:           nopLogger(),
		ConnectionConfig: proto.ConnectionConfig{Http: proto.HTTPConfig{
			Domain:  "app.example.com",
			Target:  local.URL,
			Rewrite: []proto.HTTPRewriteRule{{From: "/", To: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	go client.Start()
	defer client.Close()

	select {
	case <-client.StartNotify():
	case <-time.After(functionalTimeout):
		t.Fatal("the client could not connect through the control listener")
	}

	// Traffic goes to the public listener.
	req, err := http.NewRequest(http.MethodGet, public.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "app.example.com"

	resp, err := (&http.Client{Timeout: functionalTimeout}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "served" {
		t.Errorf("body = %q, want %q", body, "served")
	}
}

// A second client cannot open a control connection through the public listener.
func TestControlProtocolIsUnreachableOnThePublicListener(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	public := httptest.NewServer(s.PublicHandler())
	defer public.Close()

	client, err := NewClient(&ClientConfig{
		Identifier:    "mallory",
		SignatureKey:  testSignatureKey,
		ServerAddr:    trimScheme(public.URL),
		ControlPath:   proto.DefaultControlPath,
		ControlMethod: proto.DefaultControlMethod,
		Log:           nopLogger(),
		ConnectionConfig: proto.ConnectionConfig{Http: proto.HTTPConfig{
			Domain:  "app.example.com",
			Target:  "http://127.0.0.1:1",
			Rewrite: []proto.HTTPRewriteRule{{From: "/", To: "/"}},
		}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	go client.Start()
	defer client.Close()

	select {
	case <-client.StartNotify():
		t.Fatal("a client connected through the public listener")
	case <-time.After(connectTimeout):
	}
}
