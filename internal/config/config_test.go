package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestDeviceConfigRoundTripIncludesRelayToken(t *testing.T) {
	rootDir := t.TempDir()
	want := DeviceConfig{
		RelayURL:       "wss://relay.example/ws",
		RelayToken:     "secret-token",
		DefaultMailbox: "alice",
	}
	if err := SaveDeviceConfig(rootDir, want); err != nil {
		t.Fatalf("save device config: %v", err)
	}
	got, err := LoadDeviceConfig(rootDir)
	if err != nil {
		t.Fatalf("load device config: %v", err)
	}
	if got != want {
		t.Fatalf("expected device config %+v, got %+v", want, got)
	}
	path := DeviceConfigPath(rootDir)
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if !strings.Contains(string(bytes), "relay_token: secret-token") {
		t.Fatalf("expected relay_token in config file, got %q", string(bytes))
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
	t.Setenv("PANDO_RELAY_MAX_QUEUED_MESSAGES", "42")
	t.Setenv("PANDO_RELAY_MAX_QUEUED_BYTES", "9999")
	t.Setenv("PANDO_RELAY_RATE_LIMIT_PER_MINUTE", "77")
	t.Setenv("PANDO_RELAY_ALLOWED_ORIGINS", "https://app.example, https://admin.example")

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
	if cfg.MaxQueuedMessages != 42 {
		t.Fatalf("expected max queued messages override, got %d", cfg.MaxQueuedMessages)
	}
	if cfg.MaxQueuedBytes != 9999 {
		t.Fatalf("expected max queued bytes override, got %d", cfg.MaxQueuedBytes)
	}
	if cfg.RateLimitPerMinute != 77 {
		t.Fatalf("expected rate limit override, got %d", cfg.RateLimitPerMinute)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != "https://app.example" || cfg.AllowedOrigins[1] != "https://admin.example" {
		t.Fatalf("expected allowed origins override, got %v", cfg.AllowedOrigins)
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
		{name: "max queued messages", envKey: "PANDO_RELAY_MAX_QUEUED_MESSAGES", envVal: "xyz", prefix: "invalid PANDO_RELAY_MAX_QUEUED_MESSAGES \"xyz\":"},
		{name: "max queued bytes", envKey: "PANDO_RELAY_MAX_QUEUED_BYTES", envVal: "xyz", prefix: "invalid PANDO_RELAY_MAX_QUEUED_BYTES \"xyz\":"},
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

func TestRelayValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  Relay
		want string
	}{
		{name: "missing addr", cfg: func() Relay { cfg := DefaultRelay(); cfg.StorePath = "/tmp/relay.db"; cfg.Addr = ""; return cfg }(), want: "listen address is required"},
		{name: "missing store path", cfg: func() Relay { cfg := DefaultRelay(); cfg.StorePath = ""; return cfg }(), want: "relay store path is required"},
		{name: "non-positive ttl", cfg: func() Relay { cfg := DefaultRelay(); cfg.StorePath = "/tmp/relay.db"; cfg.QueueTTL = 0; return cfg }(), want: "relay queue ttl must be positive"},
		{name: "non-positive max message bytes", cfg: func() Relay {
			cfg := DefaultRelay()
			cfg.StorePath = "/tmp/relay.db"
			cfg.MaxMessageBytes = 0
			return cfg
		}(), want: "relay max message bytes must be positive"},
		{name: "non-positive max queued messages", cfg: func() Relay {
			cfg := DefaultRelay()
			cfg.StorePath = "/tmp/relay.db"
			cfg.MaxQueuedMessages = 0
			return cfg
		}(), want: "relay max queued messages must be positive"},
		{name: "non-positive max queued bytes", cfg: func() Relay {
			cfg := DefaultRelay()
			cfg.StorePath = "/tmp/relay.db"
			cfg.MaxQueuedBytes = 0
			return cfg
		}(), want: "relay max queued bytes must be positive"},
		{name: "non-positive rate limit", cfg: func() Relay {
			cfg := DefaultRelay()
			cfg.StorePath = "/tmp/relay.db"
			cfg.RateLimitPerMinute = 0
			return cfg
		}(), want: "relay rate limit per minute must be positive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || err.Error() != tt.want {
				t.Fatalf("expected %q, got %v", tt.want, err)
			}
		})
	}
}

func TestRelayValidateAcceptsValidConfig(t *testing.T) {
	cfg := DefaultRelay()
	cfg.StorePath = "/tmp/pando-relay.db"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate relay config: %v", err)
	}
}
