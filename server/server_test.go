package main

import (
	"net/http"
	"testing"
)

func TestNewHTTPServerBoundsSlowClients(t *testing.T) {
	srv := newHTTPServer(":8080", http.NewServeMux())

	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout is unset: a client can hold a connection open by sending headers slowly")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout is unset: idle keep-alive connections are never reaped")
	}

	// Deliberately unset: a tunnelled response has no bound on how long it may
	// legitimately take.
	if srv.ReadTimeout != 0 {
		t.Errorf("ReadTimeout = %v, want unset: it would cut off long request bodies", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want unset: it would cut off long or streaming responses", srv.WriteTimeout)
	}

	if srv.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", srv.Addr, ":8080")
	}
}
