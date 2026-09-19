package tunnel

import (
	"os"
	"path/filepath"
	"testing"
)

// Both binaries load everything they know through GetConfig, so a bad file has
// to fail loudly rather than leave a zero-valued config behind.
func TestGetConfig(t *testing.T) {
	type sample struct {
		Listen string   `json:"listen"`
		Hosts  []string `json:"allowedHosts"`
	}

	t.Run("valid file", func(t *testing.T) {
		path := writeFile(t, `{"listen": ":8080", "allowedHosts": ["^a$", "^b$"]}`)

		var got sample
		if err := GetConfig(&path, &got); err != nil {
			t.Fatalf("GetConfig: %v", err)
		}

		if got.Listen != ":8080" {
			t.Errorf("Listen = %q, want %q", got.Listen, ":8080")
		}
		if len(got.Hosts) != 2 {
			t.Errorf("allowedHosts = %v, want two entries", got.Hosts)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "absent.json")

		var got sample
		if err := GetConfig(&path, &got); err == nil {
			t.Fatal("no error for a config file that does not exist")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		path := writeFile(t, `{"listen": ":8080"`)

		var got sample
		if err := GetConfig(&path, &got); err == nil {
			t.Fatal("no error for a config file that is not valid JSON")
		}
	})

	t.Run("unknown fields are ignored", func(t *testing.T) {
		path := writeFile(t, `{"listen": ":8080", "somethingElse": 42}`)

		var got sample
		if err := GetConfig(&path, &got); err != nil {
			t.Fatalf("GetConfig: %v", err)
		}
		if got.Listen != ":8080" {
			t.Errorf("Listen = %q, want %q", got.Listen, ":8080")
		}
	})
}

func writeFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}

func TestClientConfigVerify(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ClientConfig
		wantErr bool
	}{
		{
			name: "identifier and server address",
			cfg:  ClientConfig{Identifier: "1234", ServerAddr: "127.0.0.1:8080"},
		},
		{
			name: "fetchers instead of values",
			cfg: ClientConfig{
				FetchIdentifier: func() (string, error) { return "1234", nil },
				FetchServerAddr: func() (string, error) { return "127.0.0.1:8080", nil },
			},
		},
		{
			name:    "no server address",
			cfg:     ClientConfig{Identifier: "1234"},
			wantErr: true,
		},
		{
			name:    "no identifier",
			cfg:     ClientConfig{ServerAddr: "127.0.0.1:8080"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.verify()
			if (err != nil) != tt.wantErr {
				t.Errorf("verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		addr     string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{addr: "app.example.com:8080", wantHost: "app.example.com", wantPort: 8080},
		{addr: "127.0.0.1:80", wantHost: "127.0.0.1", wantPort: 80},
		{addr: "app.example.com", wantErr: true},
		{addr: "app.example.com:http", wantErr: true},
		{addr: "app.example.com:99999", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			host, port, err := parseHostPort(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseHostPort(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if host != tt.wantHost || port != tt.wantPort {
				t.Errorf("parseHostPort(%q) = %q, %d, want %q, %d", tt.addr, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestNonilReturnsTheFirstFailure(t *testing.T) {
	if err := nonil(nil, nil); err != nil {
		t.Errorf("nonil(nil, nil) = %v, want nil", err)
	}

	first := os.ErrClosed
	second := os.ErrDeadlineExceeded
	if got := nonil(nil, first, second); got != first {
		t.Errorf("nonil = %v, want the first non-nil error %v", got, first)
	}
}
