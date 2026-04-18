package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRelayURL  = "ws://localhost:8080/ws"
	DefaultRelayAddr = ":8080"
	defaultRootDir   = ".pando"
)

type Client struct {
	RelayURL         string
	RelayToken       string
	Mailbox          string
	RecipientMailbox string
	RootDir          string
	DataDir          string
}

type Relay struct {
	Addr               string
	RootDir            string
	StorePath          string
	QueueTTL           time.Duration
	MaxMessageBytes    int
	RateLimitPerMinute int
	AuthToken          string
}

func DefaultClient() Client {
	return Client{RelayURL: DefaultRelayURL, RootDir: DefaultRootDir()}
}

func DefaultRelay() Relay {
	return Relay{
		Addr:               DefaultRelayAddr,
		RootDir:            DefaultRootDir(),
		QueueTTL:           24 * time.Hour,
		MaxMessageBytes:    64 * 1024,
		RateLimitPerMinute: 120,
	}
}

func (c Client) Validate() error {
	if strings.TrimSpace(c.Mailbox) == "" {
		return fmt.Errorf("mailbox is required")
	}
	if strings.TrimSpace(c.RecipientMailbox) == "" {
		return fmt.Errorf("recipient mailbox is required")
	}
	if strings.TrimSpace(c.RelayURL) == "" {
		return fmt.Errorf("relay URL is required")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data dir is required")
	}
	return nil
}

func (r Relay) Validate() error {
	if strings.TrimSpace(r.Addr) == "" {
		return fmt.Errorf("listen address is required")
	}
	if strings.TrimSpace(r.StorePath) == "" {
		return fmt.Errorf("relay store path is required")
	}
	if r.QueueTTL <= 0 {
		return fmt.Errorf("relay queue ttl must be positive")
	}
	if r.MaxMessageBytes <= 0 {
		return fmt.Errorf("relay max message bytes must be positive")
	}
	if r.RateLimitPerMinute <= 0 {
		return fmt.Errorf("relay rate limit per minute must be positive")
	}
	return nil
}

func DefaultRootDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", defaultRootDir)
	}
	return filepath.Join(home, defaultRootDir)
}

func ClientDataDir(rootDir, mailbox string) string {
	return filepath.Join(rootDir, "clients", mailbox)
}

func RelayStorePath(rootDir string) string {
	return filepath.Join(rootDir, "relay", "relay.db")
}

func ApplyRelayEnv(cfg *Relay) error {
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_ADDR"); ok {
		cfg.Addr = value
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_STORE_PATH"); ok {
		cfg.StorePath = value
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_AUTH_TOKEN"); ok {
		cfg.AuthToken = value
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_QUEUE_TTL"); ok {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_QUEUE_TTL %q: %w", value, err)
		}
		cfg.QueueTTL = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_MAX_MESSAGE_BYTES"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_MAX_MESSAGE_BYTES %q: %w", value, err)
		}
		cfg.MaxMessageBytes = parsed
	}
	if value, ok := lookupEnvTrimmed("PANDO_RELAY_RATE_LIMIT_PER_MINUTE"); ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid PANDO_RELAY_RATE_LIMIT_PER_MINUTE %q: %w", value, err)
		}
		cfg.RateLimitPerMinute = parsed
	}
	return nil
}

func lookupEnvTrimmed(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}
