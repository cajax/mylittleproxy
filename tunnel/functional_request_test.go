package tunnel

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/cajax/mylittleproxy/proto"
)

func TestFunctionalGETRoundTrip(t *testing.T) {
	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-From-Local", "yes")
			fmt.Fprintf(w, "hello from %s", r.URL.Path)
		},
	})

	resp := f.Do(http.MethodGet, "/greeting", "")

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-From-Local"); got != "yes" {
		t.Errorf("X-From-Local = %q, want %q: response headers did not survive the tunnel", got, "yes")
	}
	if got, want := bodyOf(t, resp), "hello from /greeting"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestFunctionalRequestHeadersAndBodyReachLocalServer(t *testing.T) {
	var (
		gotHeader string
		gotBody   string
		gotMethod string
	)

	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotHeader = r.Header.Get("X-Request-Id")
			b, _ := io.ReadAll(r.Body)
			gotBody = string(b)
			w.WriteHeader(http.StatusCreated)
		},
	})

	req, err := http.NewRequest(http.MethodPost, f.Tunnel.URL+"/submit", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = f.domain
	req.Header.Set("X-Request-Id", "abc-123")

	resp, err := (&http.Client{Timeout: functionalTimeout}).Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotHeader != "abc-123" {
		t.Errorf("X-Request-Id = %q, want %q: request headers did not survive the tunnel", gotHeader, "abc-123")
	}
	if gotBody != `{"a":1}` {
		t.Errorf("body = %q, want %q", gotBody, `{"a":1}`)
	}
}

// Regression test for #24.
func TestFunctionalQueryStringSurvives(t *testing.T) {
	var gotURI string
	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			gotURI = r.URL.RequestURI()
		},
	})

	resp := f.Do(http.MethodGet, "/callback?code=abc123&state=xyz", "")
	resp.Body.Close()

	if want := "/callback?code=abc123&state=xyz"; gotURI != want {
		t.Errorf("local server saw %q, want %q", gotURI, want)
	}
}

func TestFunctionalRewriteWithCaptureGroups(t *testing.T) {
	var gotPath string
	f := newFixture(t, fixtureConfig{
		rewrites: []proto.HTTPRewriteRule{{From: `^/test/(.*)$`, To: "/api/$1"}},
		handler: func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
		},
	})

	resp := f.Do(http.MethodGet, "/test/widgets/7", "")
	resp.Body.Close()

	if want := "/api/widgets/7"; gotPath != want {
		t.Errorf("local server saw %q, want %q", gotPath, want)
	}
}

func TestFunctionalPathMatchingNoRuleIsRefused(t *testing.T) {
	var called bool
	f := newFixture(t, fixtureConfig{
		rewrites: []proto.HTTPRewriteRule{{From: `^/allowed`, To: "/allowed"}},
		handler: func(w http.ResponseWriter, r *http.Request) {
			called = true
		},
	})

	resp := f.Do(http.MethodGet, "/secret", "")
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if called {
		t.Error("the local server was reached by a path matching no rewrite rule")
	}
}

// A body larger than one yamux frame has to be streamed in several pieces.
func TestFunctionalLargeBodiesBothWays(t *testing.T) {
	const size = 1 << 20 // 1MiB

	payload := bytes.Repeat([]byte("x"), size)

	var gotLen int
	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("local server read body: %v", err)
			}
			gotLen = len(b)
			w.Write(payload)
		},
	})

	resp := f.Do(http.MethodPost, "/upload", string(payload))

	if gotLen != size {
		t.Errorf("local server received %d bytes, want %d", gotLen, size)
	}
	if got := len(bodyOf(t, resp)); got != size {
		t.Errorf("caller received %d bytes, want %d", got, size)
	}
}

func TestFunctionalConcurrentRequests(t *testing.T) {
	f := newFixture(t, fixtureConfig{
		handler: func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, r.URL.Query().Get("echo"))
		},
	})

	const n = 50

	var wg sync.WaitGroup
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			want := fmt.Sprintf("message-%d", i)
			resp := f.Do(http.MethodGet, "/echo?echo="+want, "")
			if got := bodyOf(t, resp); got != want {
				errs <- fmt.Errorf("got %q, want %q: responses were crossed between concurrent requests", got, want)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

func TestFunctionalCustomHeadersReachTheLocalServer(t *testing.T) {
	seen := make(chan http.Header, 1)

	f := newFixture(t, fixtureConfig{
		customHeaders: map[string]string{
			"X-Api-Key": "abc123",
			"X-Env":     "preprod",
		},
		handler: func(w http.ResponseWriter, r *http.Request) {
			seen <- r.Header.Clone()
		},
	})

	resp := f.Do(http.MethodGet, "/", "")
	resp.Body.Close()

	headers := <-seen
	if got := headers.Get("X-Api-Key"); got != "abc123" {
		t.Errorf("X-Api-Key = %q, want %q", got, "abc123")
	}
	if got := headers.Get("X-Env"); got != "preprod" {
		t.Errorf("X-Env = %q, want %q", got, "preprod")
	}
}

func TestFunctionalCustomHeadersOverwriteWhatTheCallerSent(t *testing.T) {
	seen := make(chan http.Header, 1)

	f := newFixture(t, fixtureConfig{
		customHeaders: map[string]string{"X-Env": "preprod"},
		handler: func(w http.ResponseWriter, r *http.Request) {
			seen <- r.Header.Clone()
		},
	})

	req, err := http.NewRequest(http.MethodGet, f.Tunnel.URL+"/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = f.domain
	req.Header.Set("X-Env", "spoofed-by-the-caller")

	resp, err := (&http.Client{Timeout: functionalTimeout}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	headers := <-seen
	if got := headers.Values("X-Env"); len(got) != 1 || got[0] != "preprod" {
		t.Errorf("X-Env = %v, want exactly [preprod]: the caller's value survived", got)
	}
}
