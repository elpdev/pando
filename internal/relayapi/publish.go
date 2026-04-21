package relayapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/gorilla/websocket"
)

// PublishEnvelopes opens a short-lived relay websocket connection and publishes
// each envelope, waiting for the relay ack before sending the next one.
func PublishEnvelopes(ctx context.Context, relayURL, relayToken string, envelopes []protocol.Envelope) error {
	headers := http.Header{}
	if relayToken != "" {
		headers.Set(authHeader, relayToken)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, headers)
	if err != nil {
		return err
	}
	defer conn.Close()

	var challenge protocol.Message
	if err := conn.ReadJSON(&challenge); err != nil {
		return fmt.Errorf("read subscribe challenge: %w", err)
	}
	for _, envelope := range envelopes {
		if err := conn.WriteJSON(protocol.Message{Type: protocol.MessageTypePublish, Publish: &protocol.PublishRequest{Envelope: envelope}}); err != nil {
			return fmt.Errorf("write publish request: %w", err)
		}
		var response protocol.Message
		if err := conn.ReadJSON(&response); err != nil {
			return fmt.Errorf("read publish ack: %w", err)
		}
		if response.Type == protocol.MessageTypeError && response.Error != nil {
			return errors.New(response.Error.Message)
		}
		if response.Type != protocol.MessageTypeAck || response.Ack == nil {
			return fmt.Errorf("expected publish ack, got %q", response.Type)
		}
	}
	return nil
}

// PublishIdentityDirectoryEntry signs and publishes the current identity's
// relay-backed directory entry.
func PublishIdentityDirectoryEntry(id *identity.Identity, relayURL, relayToken string, discoverable bool) error {
	client, err := NewClient(relayURL, relayToken)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	signed, err := SignDirectoryEntry(DirectoryEntry{
		Mailbox:      id.AccountID,
		Bundle:       id.InviteBundle(),
		Discoverable: discoverable,
		PublishedAt:  now,
		Version:      now.UnixNano(),
	}, id.AccountSigningPrivate)
	if err != nil {
		return err
	}
	if _, err := client.PublishDirectoryEntry(*signed); err != nil {
		return err
	}
	return nil
}
