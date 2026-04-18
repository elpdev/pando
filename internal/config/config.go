package config

import (
	"fmt"
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
}

type Relay struct {
	Addr string
}

func DefaultClient() Client {
	return Client{RelayURL: DefaultRelayURL}
}

func DefaultRelay() Relay {
	return Relay{Addr: DefaultRelayAddr}
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
	return nil
}

func (r Relay) Validate() error {
	if strings.TrimSpace(r.Addr) == "" {
		return fmt.Errorf("listen address is required")
	}
	return nil
}
