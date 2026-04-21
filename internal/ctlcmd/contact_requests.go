package ctlcmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/ui/style"
	"github.com/gorilla/websocket"
)

const relayAuthHeader = "X-Pando-Relay-Token"

func runDiscoverContacts(args []string) error {
	bfs := NewBaseFlagSet("contact discover")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	resolvedRelayURL, resolvedRelayToken, err := resolveRelayConfig(*bfs.RootDir, *relayURL, *relayToken)
	if err != nil {
		return err
	}
	client, err := relayapi.NewClient(resolvedRelayURL, resolvedRelayToken)
	if err != nil {
		return err
	}
	entries, err := client.ListDiscoverableEntries()
	if err != nil {
		return err
	}
	output := make([]struct {
		Mailbox      string    `json:"mailbox"`
		Fingerprint  string    `json:"fingerprint"`
		Devices      int       `json:"devices"`
		PublishedAt  time.Time `json:"published_at"`
		Discoverable bool      `json:"discoverable"`
	}, 0, len(entries))
	for _, entry := range entries {
		output = append(output, struct {
			Mailbox      string    `json:"mailbox"`
			Fingerprint  string    `json:"fingerprint"`
			Devices      int       `json:"devices"`
			PublishedAt  time.Time `json:"published_at"`
			Discoverable bool      `json:"discoverable"`
		}{
			Mailbox:      entry.Entry.Mailbox,
			Fingerprint:  identity.Fingerprint(entry.Entry.Bundle.AccountSigningPublic),
			Devices:      len(entry.Entry.Bundle.Devices),
			PublishedAt:  entry.Entry.PublishedAt,
			Discoverable: entry.Entry.Discoverable,
		})
	}
	return writeJSON(os.Stdout, output)
}

func runRequestContact(args []string) error {
	bfs := NewBaseFlagSet("contact request")
	contactMailbox := bfs.FS.String("contact", "", "contact mailbox identifier")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	note := bfs.FS.String("note", "", "optional contact request note")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*contactMailbox) == "" {
		return fmt.Errorf("-contact is required")
	}
	resolvedRelayURL, resolvedRelayToken, err := resolveRelayConfig(*bfs.RootDir, *relayURL, *relayToken)
	if err != nil {
		return err
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	service, _, err := messaging.New(clientStore, mailbox)
	if err != nil {
		return err
	}
	client, err := relayapi.NewClient(resolvedRelayURL, resolvedRelayToken)
	if err != nil {
		return err
	}
	entry, err := client.LookupDirectoryEntry(*contactMailbox)
	if err != nil {
		return err
	}
	if !entry.Entry.Discoverable {
		return fmt.Errorf("contact %s is not discoverable", *contactMailbox)
	}
	envelopes, request, err := service.ContactRequestEnvelopes(entry, *note)
	if err != nil {
		return err
	}
	if err := publishRelayEnvelopes(context.Background(), resolvedRelayURL, resolvedRelayToken, envelopes); err != nil {
		return err
	}
	if err := service.SaveContactRequest(request); err != nil {
		return err
	}
	fmt.Printf("sent contact request to %s\n", request.AccountID)
	return nil
}

func runListContactRequests(args []string) error {
	bfs := NewBaseFlagSet("contact requests")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	service, _, err := messaging.New(clientStore, mailbox)
	if err != nil {
		return err
	}
	requests, err := service.ContactRequests()
	if err != nil {
		return err
	}
	output := make([]struct {
		Mailbox     string    `json:"mailbox"`
		Direction   string    `json:"direction"`
		Status      string    `json:"status"`
		Fingerprint string    `json:"fingerprint"`
		Note        string    `json:"note,omitempty"`
		UpdatedAt   time.Time `json:"updated_at"`
	}, 0, len(requests))
	for _, request := range requests {
		output = append(output, struct {
			Mailbox     string    `json:"mailbox"`
			Direction   string    `json:"direction"`
			Status      string    `json:"status"`
			Fingerprint string    `json:"fingerprint"`
			Note        string    `json:"note,omitempty"`
			UpdatedAt   time.Time `json:"updated_at"`
		}{
			Mailbox:     request.AccountID,
			Direction:   request.Direction,
			Status:      request.Status,
			Fingerprint: identity.Fingerprint(request.Bundle.AccountSigningPublic),
			Note:        request.Note,
			UpdatedAt:   request.UpdatedAt,
		})
	}
	return writeJSON(os.Stdout, output)
}

func runAcceptContactRequest(args []string) error {
	return runResolveContactRequest("contact accept", args, "accept")
}

func runRejectContactRequest(args []string) error {
	return runResolveContactRequest("contact reject", args, "reject")
}

func runResolveContactRequest(name string, args []string, decision string) error {
	bfs := NewBaseFlagSet(name)
	contactMailbox := bfs.FS.String("contact", "", "contact mailbox identifier")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*contactMailbox) == "" {
		return fmt.Errorf("-contact is required")
	}
	resolvedRelayURL, resolvedRelayToken, err := resolveRelayConfig(*bfs.RootDir, *relayURL, *relayToken)
	if err != nil {
		return err
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	service, _, err := messaging.New(clientStore, mailbox)
	if err != nil {
		return err
	}
	request, err := service.LoadContactRequest(*contactMailbox)
	if err != nil {
		return err
	}
	if request.Direction != store.ContactRequestDirectionIncoming || request.Status != store.ContactRequestStatusPending {
		return fmt.Errorf("contact request for %s is not pending incoming", request.AccountID)
	}
	envelopes, err := service.ContactRequestResponseEnvelopes(request.Bundle, decision)
	if err != nil {
		return err
	}
	if err := publishRelayEnvelopes(context.Background(), resolvedRelayURL, resolvedRelayToken, envelopes); err != nil {
		return err
	}
	request.UpdatedAt = time.Now().UTC()
	if decision == "accept" {
		request.Status = store.ContactRequestStatusAccepted
		contact, err := service.ImportContactInviteBundle(request.Bundle, identity.TrustSourceRelayDirectory)
		if err != nil {
			return err
		}
		if err := service.SaveContactRequest(request); err != nil {
			return err
		}
		fmt.Printf("accepted contact request from %s\n", request.AccountID)
		fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(contact.Fingerprint()))
		return nil
	}
	request.Status = store.ContactRequestStatusRejected
	if err := service.SaveContactRequest(request); err != nil {
		return err
	}
	fmt.Printf("rejected contact request from %s\n", request.AccountID)
	return nil
}

func publishRelayEnvelopes(ctx context.Context, relayURL, relayToken string, envelopes []protocol.Envelope) error {
	headers := http.Header{}
	if relayToken != "" {
		headers.Set(relayAuthHeader, relayToken)
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
