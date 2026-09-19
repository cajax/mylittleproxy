package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajax/mylittleproxy/appConfig"
)

func serverConfig() appConfig.Server {
	return appConfig.Server{
		Listen:         "proxy.example.com:8080",
		SignatureKey:   "secretkey",
		AllowedHosts:   []string{`^.*\.example\.com$`},
		AllowedClients: []string{"1234"},
		ControlPath:    "/custom-control",
		ControlMethod:  "PUT",
	}
}

func TestBuildClientConfigCopiesWhatMustMatchTheServer(t *testing.T) {
	got, err := buildClientConfig(serverConfig(), clientConfigOptions{
		identifier: "1234",
		domain:     "app.example.com",
		target:     "http://127.0.0.1:3000",
	})
	if err != nil {
		t.Fatalf("buildClientConfig: %v", err)
	}

	if got.SignatureKey != "secretkey" {
		t.Errorf("SignatureKey = %q, want the server's", got.SignatureKey)
	}
	if got.ControlPath != "/custom-control" {
		t.Errorf("ControlPath = %q, want %q", got.ControlPath, "/custom-control")
	}
	if got.ControlMethod != "PUT" {
		t.Errorf("ControlMethod = %q, want %q", got.ControlMethod, "PUT")
	}
	if got.ServerAddress != "proxy.example.com:8080" {
		t.Errorf("ServerAddress = %q, want %q", got.ServerAddress, "proxy.example.com:8080")
	}
	if got.Identifier != "1234" {
		t.Errorf("Identifier = %q, want %q", got.Identifier, "1234")
	}
	if got.Proxy.Http.Domain != "app.example.com" {
		t.Errorf("Domain = %q, want %q", got.Proxy.Http.Domain, "app.example.com")
	}
	if got.Proxy.Http.Target != "http://127.0.0.1:3000" {
		t.Errorf("Target = %q, want %q", got.Proxy.Http.Target, "http://127.0.0.1:3000")
	}

	// The client refuses to start without at least one rewrite rule.
	if len(got.Proxy.Http.Rewrite) == 0 {
		t.Error("no rewrite rule was generated; the client would abort on startup")
	}
}

func TestBuildClientConfigFillsInTheDefaultControlSettings(t *testing.T) {
	cfg := serverConfig()
	cfg.ControlPath = ""
	cfg.ControlMethod = ""

	got, err := buildClientConfig(cfg, clientConfigOptions{identifier: "1234", domain: "app.example.com"})
	if err != nil {
		t.Fatalf("buildClientConfig: %v", err)
	}

	if got.ControlPath != "/_controlPath" {
		t.Errorf("ControlPath = %q, want the default", got.ControlPath)
	}
	if got.ControlMethod != "POST" {
		t.Errorf("ControlMethod = %q, want the default", got.ControlMethod)
	}
}

// A listener bound to every interface says nothing about how a client reaches
// the server, so the operator has to be told rather than handed a config that
// cannot work.
func TestBuildClientConfigNeedsAReachableAddress(t *testing.T) {
	cfg := serverConfig()
	cfg.Listen = ":8080"

	_, err := buildClientConfig(cfg, clientConfigOptions{identifier: "1234", domain: "app.example.com"})
	if err == nil {
		t.Fatal("a config was generated although the server address is not reachable from a client")
	}
	if !strings.Contains(err.Error(), "address") {
		t.Errorf("error = %v, want it to mention the address", err)
	}

	// Supplying one resolves it.
	got, err := buildClientConfig(cfg, clientConfigOptions{
		identifier: "1234",
		domain:     "app.example.com",
		address:    "proxy.example.com:8080",
	})
	if err != nil {
		t.Fatalf("buildClientConfig with an explicit address: %v", err)
	}
	if got.ServerAddress != "proxy.example.com:8080" {
		t.Errorf("ServerAddress = %q, want the one supplied", got.ServerAddress)
	}
}

func TestBuildClientConfigRejectsWhatTheServerWouldRefuse(t *testing.T) {
	tests := []struct {
		name string
		opts clientConfigOptions
		want string
	}{
		{
			name: "domain not matching allowedHosts",
			opts: clientConfigOptions{identifier: "1234", domain: "app.evil.com"},
			want: "allowedHosts",
		},
		{
			name: "identifier not in allowedClients",
			opts: clientConfigOptions{identifier: "mallory", domain: "app.example.com"},
			want: "allowedClients",
		},
		{
			name: "no domain",
			opts: clientConfigOptions{identifier: "1234"},
			want: "domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := buildClientConfig(serverConfig(), tt.opts)
			if err == nil {
				t.Fatal("a config was generated that the server would reject")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestBuildClientConfigCanLeaveTheSignatureKeyOut(t *testing.T) {
	got, err := buildClientConfig(serverConfig(), clientConfigOptions{
		identifier:       "1234",
		domain:           "app.example.com",
		omitSignatureKey: true,
	})
	if err != nil {
		t.Fatalf("buildClientConfig: %v", err)
	}

	if got.SignatureKey != "" {
		t.Errorf("SignatureKey = %q, want it left out so the env var is used", got.SignatureKey)
	}
}

// The output has to be a config the client can actually read back.
func TestGeneratedConfigRoundTrips(t *testing.T) {
	cfg, err := buildClientConfig(serverConfig(), clientConfigOptions{
		identifier: "1234",
		domain:     "app.example.com",
		target:     "http://127.0.0.1:3000",
	})
	if err != nil {
		t.Fatalf("buildClientConfig: %v", err)
	}

	out, err := marshalClientConfig(cfg)
	if err != nil {
		t.Fatalf("marshalClientConfig: %v", err)
	}

	var back appConfig.Client
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("the generated config is not valid JSON for a client: %v", err)
	}
	if back.Proxy.Http.Domain != "app.example.com" {
		t.Errorf("after a round trip Domain = %q, want %q", back.Proxy.Http.Domain, "app.example.com")
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Error("output does not end with a newline")
	}
}
