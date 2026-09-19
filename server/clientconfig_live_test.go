package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cajax/mylittleproxy/appConfig"
	"github.com/cajax/mylittleproxy/proto"
	"github.com/cajax/mylittleproxy/tunnel"
	"go.uber.org/zap"
)

// TestGeneratedConfigConnectsToTheServer is the point of the whole feature: a
// config generated from a server's own settings has to let a client connect to
// that server and serve traffic, with no hand editing.
func TestGeneratedConfigConnectsToTheServer(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "served through the tunnel")
	}))
	defer local.Close()

	serverCfg := appConfig.Server{
		Listen:         "127.0.0.1:0",
		SignatureKey:   "secretkey",
		AllowedHosts:   []string{`^.*\.example\.com$`},
		AllowedClients: []string{"1234"},
		ControlPath:    "/custom-control",
		ControlMethod:  "PUT",
	}

	ts, err := tunnel.NewServer(&tunnel.ServerConfig{
		Log:            zap.NewNop(),
		SignatureKey:   serverCfg.SignatureKey,
		AllowedHosts:   serverCfg.AllowedHosts,
		AllowedClients: serverCfg.AllowedClients,
		ControlPath:    serverCfg.ControlPath,
		ControlMethod:  serverCfg.ControlMethod,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	public := httptest.NewServer(ts)
	defer public.Close()

	// What the operator would run, with the address clients dial.
	generated, err := buildClientConfig(serverCfg, clientConfigOptions{
		identifier: "1234",
		domain:     "app.example.com",
		target:     local.URL,
		address:    strings.TrimPrefix(public.URL, "http://"),
	})
	if err != nil {
		t.Fatalf("buildClientConfig: %v", err)
	}

	client, err := tunnel.NewClient(clientFromConfig(generated))
	if err != nil {
		t.Fatalf("NewClient from the generated config: %v", err)
	}
	go client.Start()
	defer client.Close()

	select {
	case <-client.StartNotify():
	case <-time.After(15 * time.Second):
		t.Fatal("a client built from the generated config could not connect")
	}

	req, err := http.NewRequest(http.MethodGet, public.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = generated.Proxy.Http.Domain

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("request through the tunnel: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(body), "served through the tunnel"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// clientFromConfig mirrors what the client binary does with a config file.
func clientFromConfig(c appConfig.Client) *tunnel.ClientConfig {
	rewrites := make([]proto.HTTPRewriteRule, 0, len(c.Proxy.Http.Rewrite))
	for _, r := range c.Proxy.Http.Rewrite {
		rewrites = append(rewrites, proto.HTTPRewriteRule{From: r.From, To: r.To})
	}

	return &tunnel.ClientConfig{
		Identifier:    c.Identifier,
		SignatureKey:  c.SignatureKey,
		ServerAddr:    c.ServerAddress,
		ControlPath:   c.ControlPath,
		ControlMethod: c.ControlMethod,
		Log:           zap.NewNop(),
		ConnectionConfig: proto.ConnectionConfig{Http: proto.HTTPConfig{
			Domain:  c.Proxy.Http.Domain,
			Target:  c.Proxy.Http.Target,
			Rewrite: rewrites,
		}},
	}
}
