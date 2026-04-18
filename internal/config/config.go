package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultRelayURL  = "ws://localhost:8080/ws"
	DefaultRelayAddr = ":8080"
)

type Client struct {
	RelayURL         string
	Mailbox          string
	RecipientMailbox string
	DataDir          string
}

type Relay struct {
	Addr      string
	StorePath string
}

func DefaultClient() Client {
	return Client{RelayURL: DefaultRelayURL}
}

func DefaultRelay() Relay {
	return Relay{Addr: DefaultRelayAddr, StorePath: RelayStorePath()}
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
