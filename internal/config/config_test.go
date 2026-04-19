package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultRootDirUsesCentralizedPandoRoot(t *testing.T) {
	t.Setenv("HOME", "/home/tester")

	got := DefaultRootDir()
	want := filepath.Join("/home/tester", ".pando")
	if got != want {
		t.Fatalf("expected root dir %q, got %q", want, got)
	}
}

func TestClientDataDirUsesCentralizedPandoRoot(t *testing.T) {
	got := ClientDataDir(filepath.Join("/media", "flash-drive", "pando-data"), "alice")
	want := filepath.Join("/media", "flash-drive", "pando-data", "clients", "alice")
	if got != want {
		t.Fatalf("expected client data dir %q, got %q", want, got)
	}
}

func TestClientValidateAllowsEmptyRecipientMailbox(t *testing.T) {
	cfg := Client{
		RelayURL: "ws://localhost:8080/ws",
		Mailbox:  "alice",
		DataDir:  "/tmp/pando/alice",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate client config: %v", err)
	}
}

func TestRelayStorePathUsesCentralizedPandoRoot(t *testing.T) {
	got := RelayStorePath(filepath.Join("/media", "flash-drive", "pando-data"))
	want := filepath.Join("/media", "flash-drive", "pando-data", "relay", "relay.db")
	if got != want {
		t.Fatalf("expected relay store path %q, got %q", want, got)
	}
}

func TestDefaultRelayAppliesEnvironmentOverrides(t *testing.T) {
	cfg := DefaultRelay()
	t.Setenv("PANDO_RELAY_ADDR", ":9090")
	t.Setenv("PANDO_RELAY_STORE_PATH", "/tmp/pando-relay.db")
	t.Setenv("PANDO_RELAY_AUTH_TOKEN", "secret-token")
	t.Setenv("PANDO_RELAY_QUEUE_TTL", "48h")
	t.Setenv("PANDO_RELAY_MAX_MESSAGE_BYTES", "12345")
	t.Setenv("PANDO_RELAY_RATE_LIMIT_PER_MINUTE", "77")

	if err := ApplyRelayEnv(&cfg); err != nil {
		t.Fatalf("apply relay env: %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Fatalf("expected addr override, got %q", cfg.Addr)
	}
	if cfg.StorePath != "/tmp/pando-relay.db" {
		t.Fatalf("expected store path override, got %q", cfg.StorePath)
	}
	if cfg.AuthToken != "secret-token" {
		t.Fatalf("expected auth token override, got %q", cfg.AuthToken)
	}
	if cfg.QueueTTL != 48*time.Hour {
		t.Fatalf("expected queue ttl override, got %s", cfg.QueueTTL)
	}
	if cfg.MaxMessageBytes != 12345 {
		t.Fatalf("expected max message bytes override, got %d", cfg.MaxMessageBytes)
	}
	if cfg.RateLimitPerMinute != 77 {
		t.Fatalf("expected rate limit override, got %d", cfg.RateLimitPerMinute)
	}
}

func TestApplyRelayEnvRejectsInvalidEnvironmentOverrides(t *testing.T) {
	cfg := DefaultRelay()
	t.Setenv("PANDO_RELAY_QUEUE_TTL", "not-a-duration")

	err := ApplyRelayEnv(&cfg)
	if err == nil {
		t.Fatal("expected invalid queue ttl error")
	}
	if err.Error() != "invalid PANDO_RELAY_QUEUE_TTL \"not-a-duration\": time: invalid duration \"not-a-duration\"" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyRelayEnvRejectsInvalidIntegerEnvironmentOverrides(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		prefix string
	}{
		{name: "max message bytes", envKey: "PANDO_RELAY_MAX_MESSAGE_BYTES", envVal: "abc", prefix: "invalid PANDO_RELAY_MAX_MESSAGE_BYTES \"abc\":"},
		{name: "rate limit", envKey: "PANDO_RELAY_RATE_LIMIT_PER_MINUTE", envVal: "xyz", prefix: "invalid PANDO_RELAY_RATE_LIMIT_PER_MINUTE \"xyz\":"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultRelay()
			t.Setenv(tt.envKey, tt.envVal)

			err := ApplyRelayEnv(&cfg)
			if err == nil {
				t.Fatal("expected invalid integer env error")
			}
			if got := err.Error(); len(got) < len(tt.prefix) || got[:len(tt.prefix)] != tt.prefix {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
