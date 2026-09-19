package main

import (
	"os"
	"testing"

	"github.com/cajax/mylittleproxy/appConfig"
	"github.com/cajax/mylittleproxy/proto"
	"go.uber.org/zap"
)

func TestGetIdentifier(t *testing.T) {
	t.Run("from the config", func(t *testing.T) {
		got := getIdentifier(appConfig.Client{Identifier: "1234"}, zap.NewNop())
		if got != "1234" {
			t.Errorf("identifier = %q, want %q", got, "1234")
		}
	})

	t.Run("falls back to the host name", func(t *testing.T) {
		want, err := os.Hostname()
		if err != nil {
			t.Skipf("no host name available: %v", err)
		}

		got := getIdentifier(appConfig.Client{}, zap.NewNop())
		if got != want {
			t.Errorf("identifier = %q, want the host name %q", got, want)
		}
	})
}

func TestGetSignatureKey(t *testing.T) {
	t.Run("from the config", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "from-the-environment")

		got := getSignatureKey(appConfig.Client{SignatureKey: "from-the-config"}, zap.NewNop())
		if got != "from-the-config" {
			t.Errorf("key = %q, want the config to win over the environment", got)
		}
	})

	t.Run("falls back to the environment", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "from-the-environment")

		got := getSignatureKey(appConfig.Client{}, zap.NewNop())
		if got != "from-the-environment" {
			t.Errorf("key = %q, want the environment value", got)
		}
	})
}

// The control path and method have to match the server, so the defaults applied
// here are the ones in proto.
func TestGetTunnelConfig(t *testing.T) {
	config := appConfig.Client{
		Identifier:    "1234",
		ServerAddress: "proxy.example.com:8080",
		Proxy: appConfig.Proxy{Http: appConfig.HTTPConfig{
			Domain: "app.example.com",
			Target: "http://127.0.0.1:3000",
		}},
	}
	rewrites := []proto.HTTPRewriteRule{{From: "/test", To: "/api/test"}}

	t.Run("defaults when the config is silent", func(t *testing.T) {
		got := getTunnelConfig("1234", config, rewrites, "secret", zap.NewNop())

		if got.ControlPath != proto.DefaultControlPath {
			t.Errorf("ControlPath = %q, want the default %q", got.ControlPath, proto.DefaultControlPath)
		}
		if got.ControlMethod != proto.DefaultControlMethod {
			t.Errorf("ControlMethod = %q, want the default %q", got.ControlMethod, proto.DefaultControlMethod)
		}
		if got.SignatureKey != "secret" {
			t.Errorf("SignatureKey = %q, want %q", got.SignatureKey, "secret")
		}
		if got.ServerAddr != "proxy.example.com:8080" {
			t.Errorf("ServerAddr = %q, want %q", got.ServerAddr, "proxy.example.com:8080")
		}
		if got.ConnectionConfig.Http.Domain != "app.example.com" {
			t.Errorf("Domain = %q, want %q", got.ConnectionConfig.Http.Domain, "app.example.com")
		}
		if len(got.ConnectionConfig.Http.Rewrite) != 1 || got.ConnectionConfig.Http.Rewrite[0].To != "/api/test" {
			t.Errorf("Rewrite = %v, want the rules passed in", got.ConnectionConfig.Http.Rewrite)
		}
	})

	t.Run("config overrides the defaults", func(t *testing.T) {
		custom := config
		custom.ControlPath = "/custom"
		custom.ControlMethod = "PUT"

		got := getTunnelConfig("1234", custom, rewrites, "secret", zap.NewNop())

		if got.ControlPath != "/custom" {
			t.Errorf("ControlPath = %q, want %q", got.ControlPath, "/custom")
		}
		if got.ControlMethod != "PUT" {
			t.Errorf("ControlMethod = %q, want %q", got.ControlMethod, "PUT")
		}
	})
}
