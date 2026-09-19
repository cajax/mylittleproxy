package tunnel

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
)

func requestFromWire(t *testing.T, wire string) *http.Request {
	t.Helper()

	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(wire)))
	if err != nil {
		t.Fatalf("ReadRequest: %v", err)
	}

	return req
}

func TestApplyCustomHeaders(t *testing.T) {
	t.Run("adds a header the caller did not send", func(t *testing.T) {
		req := requestFromWire(t, "GET / HTTP/1.1\r\nHost: \r\n\r\n")

		applyCustomHeaders(req, map[string]string{"X-Api-Key": "abc123"})

		if got := req.Header.Get("X-Api-Key"); got != "abc123" {
			t.Errorf("X-Api-Key = %q, want %q", got, "abc123")
		}
	})

	t.Run("overwrites what the caller sent", func(t *testing.T) {
		req := requestFromWire(t, "GET / HTTP/1.1\r\nHost: \r\nX-Env: production\r\n\r\n")

		applyCustomHeaders(req, map[string]string{"X-Env": "preprod"})

		if got := req.Header.Values("X-Env"); len(got) != 1 || got[0] != "preprod" {
			t.Errorf("X-Env = %v, want exactly [preprod]", got)
		}
	})

	t.Run("matches regardless of the case written in the config", func(t *testing.T) {
		req := requestFromWire(t, "GET / HTTP/1.1\r\nHost: \r\nX-Env: production\r\n\r\n")

		applyCustomHeaders(req, map[string]string{"x-env": "preprod"})

		if got := req.Header.Values("X-Env"); len(got) != 1 || got[0] != "preprod" {
			t.Errorf("X-Env = %v, want exactly [preprod]", got)
		}
	})

	t.Run("an empty value sets an empty header", func(t *testing.T) {
		req := requestFromWire(t, "GET / HTTP/1.1\r\nHost: \r\nX-Env: production\r\n\r\n")

		applyCustomHeaders(req, map[string]string{"X-Env": ""})

		values, ok := req.Header["X-Env"]
		if !ok {
			t.Fatal("the header was removed, want it present and empty")
		}
		if len(values) != 1 || values[0] != "" {
			t.Errorf("X-Env = %v, want one empty value", values)
		}
	})

	// Host does not live in the header map, so setting it through the header
	// map would silently do nothing.
	t.Run("Host is applied to the request, not the header map", func(t *testing.T) {
		req := requestFromWire(t, "GET / HTTP/1.1\r\nHost: \r\n\r\n")

		applyCustomHeaders(req, map[string]string{"Host": "internal.example.com"})

		if req.Host != "internal.example.com" {
			t.Errorf("req.Host = %q, want %q", req.Host, "internal.example.com")
		}
	})

	t.Run("no headers configured changes nothing", func(t *testing.T) {
		req := requestFromWire(t, "GET / HTTP/1.1\r\nHost: \r\nX-Env: production\r\n\r\n")

		applyCustomHeaders(req, nil)

		if got := req.Header.Get("X-Env"); got != "production" {
			t.Errorf("X-Env = %q, want it untouched", got)
		}
	})
}

func TestValidateCustomHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		wantErr bool
	}{
		{name: "none", headers: nil},
		{name: "ordinary", headers: map[string]string{"X-Api-Key": "abc", "Accept": "application/json"}},
		{name: "empty name", headers: map[string]string{"": "abc"}, wantErr: true},
		{name: "space in the name", headers: map[string]string{"X Api Key": "abc"}, wantErr: true},
		{name: "colon in the name", headers: map[string]string{"X-Api:Key": "abc"}, wantErr: true},
		{name: "newline in the value", headers: map[string]string{"X-Api-Key": "abc\r\nX-Evil: yes"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCustomHeaders(tt.headers)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCustomHeaders(%v) error = %v, wantErr %v", tt.headers, err, tt.wantErr)
			}
		})
	}
}

func TestNewClientRejectsInvalidCustomHeaders(t *testing.T) {
	_, err := NewClient(&ClientConfig{
		Identifier:    "1234",
		ServerAddr:    "127.0.0.1:8080",
		CustomHeaders: map[string]string{"X Api Key": "abc"},
	})
	if err == nil {
		t.Fatal("NewClient accepted an invalid header name")
	}
}
