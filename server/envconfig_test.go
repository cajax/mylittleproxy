package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cajax/mylittleproxy/appConfig"
)

func TestApplyEnvOverridesTheFile(t *testing.T) {
	t.Setenv("MYLITTLEPROXY_LISTEN", ":9090")
	t.Setenv("MYLITTLEPROXY_LISTEN_CONTROL", "127.0.0.1:9091")
	t.Setenv("MYLITTLEPROXY_CONTROL_PATH", "/env-control")
	t.Setenv("MYLITTLEPROXY_CONTROL_METHOD", "PUT")
	t.Setenv("MYLITTLEPROXY_DEBUG", "true")

	cfg := appConfig.Server{
		Listen:        ":8080",
		ListenControl: "127.0.0.1:8081",
		ControlPath:   "/file-control",
		ControlMethod: "POST",
		Debug:         false,
	}

	if err := applyEnv(&cfg); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want the environment's", cfg.Listen)
	}
	if cfg.ListenControl != "127.0.0.1:9091" {
		t.Errorf("ListenControl = %q, want the environment's", cfg.ListenControl)
	}
	if cfg.ControlPath != "/env-control" {
		t.Errorf("ControlPath = %q, want the environment's", cfg.ControlPath)
	}
	if cfg.ControlMethod != "PUT" {
		t.Errorf("ControlMethod = %q, want the environment's", cfg.ControlMethod)
	}
	if !cfg.Debug {
		t.Error("Debug = false, want the environment's true")
	}
}

func TestApplyEnvLeavesUnsetValuesAlone(t *testing.T) {
	cfg := appConfig.Server{
		Listen:       ":8080",
		SignatureKey: "from-the-file",
		AllowedHosts: []string{`^.*\.example\.com$`},
	}
	want := cfg

	if err := applyEnv(&cfg); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}

	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("config = %+v, want it untouched: %+v", cfg, want)
	}
}

func TestApplyEnvLists(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "single value", value: `^.*\.example\.com$`, want: []string{`^.*\.example\.com$`}},
		{name: "comma separated", value: "^a$,^b$", want: []string{"^a$", "^b$"}},
		{name: "spaces are trimmed", value: "^a$ , ^b$", want: []string{"^a$", "^b$"}},
		{name: "empty entries are dropped", value: "^a$,,^b$", want: []string{"^a$", "^b$"}},
		// A regex may contain a comma, so the JSON form has to be available.
		{name: "JSON array", value: `["^a{1,3}$","^b$"]`, want: []string{"^a{1,3}$", "^b$"}},
		{name: "JSON array with one entry", value: `["^a{1,3}$"]`, want: []string{"^a{1,3}$"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MYLITTLEPROXY_ALLOWED_HOSTS", tt.value)

			var cfg appConfig.Server
			if err := applyEnv(&cfg); err != nil {
				t.Fatalf("applyEnv: %v", err)
			}

			if !reflect.DeepEqual(cfg.AllowedHosts, tt.want) {
				t.Errorf("AllowedHosts = %q, want %q", cfg.AllowedHosts, tt.want)
			}
		})
	}
}

func TestApplyEnvAllowedClients(t *testing.T) {
	t.Setenv("MYLITTLEPROXY_ALLOWED_CLIENTS", "alice,bob")

	cfg := appConfig.Server{AllowedClients: []string{"from-the-file"}}
	if err := applyEnv(&cfg); err != nil {
		t.Fatalf("applyEnv: %v", err)
	}

	if !reflect.DeepEqual(cfg.AllowedClients, []string{"alice", "bob"}) {
		t.Errorf("AllowedClients = %q, want the environment's", cfg.AllowedClients)
	}
}

func TestApplyEnvRejectsBadValues(t *testing.T) {
	t.Run("debug is not a boolean", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_DEBUG", "maybe")

		var cfg appConfig.Server
		err := applyEnv(&cfg)
		if err == nil {
			t.Fatal("a non-boolean MYLITTLEPROXY_DEBUG was accepted")
		}
		if !strings.Contains(err.Error(), "MYLITTLEPROXY_DEBUG") {
			t.Errorf("error = %v, want it to name the variable", err)
		}
	})

	t.Run("list is broken JSON", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_ALLOWED_HOSTS", `["^a$"`)

		var cfg appConfig.Server
		err := applyEnv(&cfg)
		if err == nil {
			t.Fatal("a malformed JSON array was accepted")
		}
		if !strings.Contains(err.Error(), "MYLITTLEPROXY_ALLOWED_HOSTS") {
			t.Errorf("error = %v, want it to name the variable", err)
		}
	})
}

func TestCheckRequiredSettings(t *testing.T) {
	complete := appConfig.Server{
		Listen:       ":8080",
		SignatureKey: "secret",
		AllowedHosts: []string{"^a$"},
	}

	if err := checkRequiredSettings(complete); err != nil {
		t.Errorf("a complete config was rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*appConfig.Server)
		wantVar string
	}{
		{name: "no listen", mutate: func(c *appConfig.Server) { c.Listen = "" }, wantVar: "MYLITTLEPROXY_LISTEN"},
		{name: "no signature key", mutate: func(c *appConfig.Server) { c.SignatureKey = "" }, wantVar: "MYLITTLEPROXY_SIGNATURE_KEY"},
		{name: "no allowed hosts", mutate: func(c *appConfig.Server) { c.AllowedHosts = nil }, wantVar: "MYLITTLEPROXY_ALLOWED_HOSTS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := complete
			tt.mutate(&cfg)

			err := checkRequiredSettings(cfg)
			if err == nil {
				t.Fatal("an incomplete config was accepted")
			}
			// The message has to say how to supply it without a file.
			if !strings.Contains(err.Error(), tt.wantVar) {
				t.Errorf("error = %v, want it to name %s", err, tt.wantVar)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("file and environment together", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "from-the-environment")

		path := writeConfig(t, `{"listen": ":8080", "signatureKey": "from-the-file", "allowedHosts": ["^a$"]}`)

		got, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}

		if got.Listen != ":8080" {
			t.Errorf("Listen = %q, want the file's", got.Listen)
		}
		if got.SignatureKey != "from-the-environment" {
			t.Errorf("SignatureKey = %q, want the environment to win", got.SignatureKey)
		}
	})

	// The point of the environment support: an image with no mounted file.
	t.Run("no file, complete environment", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_LISTEN", ":8080")
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "secret")
		t.Setenv("MYLITTLEPROXY_ALLOWED_HOSTS", `^.*\.example\.com$`)

		got, err := loadConfig(t.TempDir() + "/absent.json")
		if err != nil {
			t.Fatalf("loadConfig with no file: %v", err)
		}

		if got.Listen != ":8080" {
			t.Errorf("Listen = %q, want the environment's", got.Listen)
		}
		if len(got.AllowedHosts) != 1 {
			t.Errorf("AllowedHosts = %q, want one entry from the environment", got.AllowedHosts)
		}
	})

	t.Run("no file and nothing in the environment", func(t *testing.T) {
		_, err := loadConfig(t.TempDir() + "/absent.json")
		if err == nil {
			t.Fatal("a server with no config at all was accepted")
		}
		if !strings.Contains(err.Error(), "MYLITTLEPROXY_LISTEN") {
			t.Errorf("error = %v, want it to name the variables that would fix it", err)
		}
	})

	// A file that exists but cannot be read is a mistake, not an invitation to
	// fall back to the environment.
	t.Run("malformed file", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_LISTEN", ":8080")
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "secret")
		t.Setenv("MYLITTLEPROXY_ALLOWED_HOSTS", "^a$")

		path := writeConfig(t, `{"listen": ":8080"`)

		if _, err := loadConfig(path); err == nil {
			t.Fatal("a malformed config file was ignored in favour of the environment")
		}
	})
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return path
}
