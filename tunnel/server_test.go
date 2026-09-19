package tunnel

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cajax/mylittleproxy/proto"
	"go.uber.org/zap"
)

const testSignatureKey = "secretkey"

func testServer(t *testing.T, cfg *ServerConfig) *Server {
	t.Helper()

	if cfg.Log == nil {
		cfg.Log = zap.NewNop()
	}
	if cfg.SignatureKey == "" {
		cfg.SignatureKey = testSignatureKey
	}
	if cfg.ControlPath == "" {
		cfg.ControlPath = proto.DefaultControlPath
	}
	if cfg.ControlMethod == "" {
		cfg.ControlMethod = proto.DefaultControlMethod
	}

	s, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

// controlRequest builds the request a client sends to open a control connection.
func controlRequest(t *testing.T, s *Server, identifier string, cc proto.ConnectionConfig) *http.Request {
	t.Helper()

	body, err := json.Marshal(cc)
	if err != nil {
		t.Fatalf("marshal connection config: %v", err)
	}

	req := httptest.NewRequest(s.controlMethod, "http://tunnel.example.com"+s.controlPath, bytes.NewReader(body))
	req.Header.Set(proto.ClientIdentifierHeader, identifier)
	req.Header.Set(proto.ClientIdentifierSignature, signIdentifier(identifier, s.signatureKey))
	return req
}

func httpConfig(domain string, rewrites ...proto.HTTPRewriteRule) proto.ConnectionConfig {
	if len(rewrites) == 0 {
		rewrites = []proto.HTTPRewriteRule{{From: "/", To: "/"}}
	}
	return proto.ConnectionConfig{Http: proto.HTTPConfig{
		Domain:  domain,
		Target:  "http://127.0.0.1:8080",
		Rewrite: rewrites,
	}}
}

func TestControlRejectsHostOwnedByAnotherClient(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	if err := s.AddHost("app.example.com", "alice", nil); err != nil {
		t.Fatalf("AddHost: %v", err)
	}

	w := httptest.NewRecorder()
	s.ServeHTTP(w, controlRequest(t, s, "mallory", httpConfig("app.example.com")))

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
	if id, _ := s.getIdentifier("app.example.com"); id != "alice" {
		t.Errorf("host owner = %q, want alice: mallory hijacked the virtual host", id)
	}
}

func TestControlRejectsInvalidRewritePattern(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}})

	// An unbalanced bracket is not a valid regexp; it must not reach
	// regexp.MustCompile.
	cc := httpConfig("app.example.com", proto.HTTPRewriteRule{From: "/api/[", To: "/"})

	w := httptest.NewRecorder()
	s.ServeHTTP(w, controlRequest(t, s, "alice", cc))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if _, ok := s.getIdentifier("app.example.com"); ok {
		t.Error("virtual host was registered despite the rejected config")
	}
}

func TestControlRejections(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ServerConfig
		mutate  func(*http.Request)
		config  proto.ConnectionConfig
		wantKey int
	}{
		{
			name:    "wrong method",
			cfg:     &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}},
			mutate:  func(r *http.Request) { r.Method = http.MethodGet },
			config:  httpConfig("app.example.com"),
			wantKey: http.StatusMethodNotAllowed,
		},
		{
			name: "identifier not in allow list",
			cfg: &ServerConfig{
				AllowedHosts:   []string{`^.*\.example\.com$`},
				AllowedClients: []string{"alice"},
			},
			config:  httpConfig("app.example.com"),
			wantKey: http.StatusForbidden,
		},
		{
			name:    "bad signature",
			cfg:     &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}},
			mutate:  func(r *http.Request) { r.Header.Set(proto.ClientIdentifierSignature, "nope") },
			config:  httpConfig("app.example.com"),
			wantKey: http.StatusForbidden,
		},
		{
			name:    "host not in allow list",
			cfg:     &ServerConfig{AllowedHosts: []string{`^.*\.example\.com$`}},
			config:  httpConfig("app.evil.com"),
			wantKey: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := testServer(t, tt.cfg)
			req := controlRequest(t, s, "mallory", tt.config)
			if tt.mutate != nil {
				tt.mutate(req)
			}

			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)

			if w.Code != tt.wantKey {
				t.Errorf("status = %d, want %d", w.Code, tt.wantKey)
			}
			if _, ok := s.getIdentifier(tt.config.Http.Domain); ok {
				t.Error("virtual host was registered for a rejected request")
			}
		})
	}
}

func TestNewServerRejectsInvalidAllowedHostPattern(t *testing.T) {
	_, err := NewServer(&ServerConfig{
		Log:          zap.NewNop(),
		SignatureKey: testSignatureKey,
		AllowedHosts: []string{"^*.broken("},
	})
	if err == nil {
		t.Fatal("NewServer accepted an invalid allowedHosts pattern")
	}
}
