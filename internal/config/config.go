package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultRelayURL  = "ws://localhost:8080/ws"
	DefaultRelayAddr = ":8080"
)

type Client struct {
	RelayURL         string
	RelayToken       string
	Mailbox          string
	RecipientMailbox string
	DataDir          string
}

type Relay struct {
	Addr               string
	StorePath          string
	QueueTTL           time.Duration
	MaxMessageBytes    int
	RateLimitPerMinute int
	AuthToken          string
}

func DefaultClient() Client {
	return Client{RelayURL: DefaultRelayURL}
}

func DefaultRelay() Relay {
	return Relay{
		Addr:               DefaultRelayAddr,
		StorePath:          RelayStorePath(),
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

func ClientDataDir(mailbox string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".chatui", mailbox)
	}
	return filepath.Join(home, ".local", "share", "chatui", mailbox)
}

func RelayStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".chatui", "relay.db")
	}
	return filepath.Join(home, ".local", "share", "chatui", "relay.db")
}
