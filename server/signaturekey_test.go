package main

import (
	"testing"

	"github.com/cajax/mylittleproxy/appConfig"
	"go.uber.org/zap"
)

func TestServerGetSignatureKey(t *testing.T) {
	t.Run("from the config", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "from-the-environment")

		got := getSignatureKey(appConfig.Server{SignatureKey: "from-the-config"}, zap.NewNop())
		if got != "from-the-config" {
			t.Errorf("key = %q, want the config to win over the environment", got)
		}
	})

	t.Run("falls back to the environment", func(t *testing.T) {
		t.Setenv("MYLITTLEPROXY_SIGNATURE_KEY", "from-the-environment")

		got := getSignatureKey(appConfig.Server{}, zap.NewNop())
		if got != "from-the-environment" {
			t.Errorf("key = %q, want the environment value", got)
		}
	})
}
