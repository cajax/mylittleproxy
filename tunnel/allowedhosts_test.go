package tunnel

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewServerWarnsAboutUnanchoredAllowedHosts(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		wantWarn bool
	}{
		{"anchored at both ends", `^.*\.example\.com$`, false},
		{"anchored at the end only", `\.example\.com$`, false},
		{"anchored at the start only", `^app\.example\.`, false},
		{"not anchored", `example\.com`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)

			if _, err := NewServer(&ServerConfig{
				Log:          zap.New(core),
				SignatureKey: testSignatureKey,
				AllowedHosts: []string{tt.pattern},
			}); err != nil {
				t.Fatalf("NewServer: %v", err)
			}

			var warned bool
			for _, e := range logs.All() {
				if strings.Contains(strings.ToLower(e.Message), "anchor") {
					warned = true
				}
			}

			if warned != tt.wantWarn {
				t.Errorf("warned = %v, want %v for pattern %q", warned, tt.wantWarn, tt.pattern)
			}
		})
	}
}

// A substring match is what the warning is about: the pattern below admits a
// host an operator would not expect.
func TestUnanchoredAllowedHostMatchesUnexpectedHost(t *testing.T) {
	s := testServer(t, &ServerConfig{AllowedHosts: []string{`example\.com`}})

	if !s.checkHost("example.com.attacker.net") {
		t.Skip("checkHost no longer matches substrings; the startup warning may be obsolete")
	}
}
