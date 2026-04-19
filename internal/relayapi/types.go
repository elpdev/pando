package relayapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

type DirectoryEntry struct {
	Mailbox      string                `json:"mailbox"`
	Bundle       identity.InviteBundle `json:"bundle"`
	Discoverable bool                  `json:"discoverable,omitempty"`
	PublishedAt  time.Time             `json:"published_at"`
	Version      int64                 `json:"version"`
}

type SignedDirectoryEntry struct {
	Entry     DirectoryEntry `json:"entry"`
	Signature string         `json:"signature"`
}

type RendezvousPayload struct {
	Ciphertext string    `json:"ciphertext"`
	Nonce      string    `json:"nonce"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type PutRendezvousRequest struct {
	Payload RendezvousPayload `json:"payload"`
}

type GetRendezvousResponse struct {
	Payloads []RendezvousPayload `json:"payloads"`
}

type ListDirectoryResponse struct {
	Entries []SignedDirectoryEntry `json:"entries"`
}

func SignDirectoryEntry(entry DirectoryEntry, privateKey ed25519.PrivateKey) (*SignedDirectoryEntry, error) {
	bytes, err := directoryEntrySigningBytes(entry)
	if err != nil {
		return nil, err
	}
	return &SignedDirectoryEntry{
		Entry:     entry,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, bytes)),
	}, nil
}

func VerifySignedDirectoryEntry(entry SignedDirectoryEntry) error {
	if strings.TrimSpace(entry.Entry.Mailbox) == "" {
		return fmt.Errorf("directory mailbox is required")
	}
	if entry.Entry.PublishedAt.IsZero() {
		return fmt.Errorf("directory published_at is required")
	}
	if err := identity.VerifyInvite(entry.Entry.Bundle); err != nil {
		return err
	}
	if entry.Entry.Bundle.AccountID != entry.Entry.Mailbox {
		return fmt.Errorf("directory mailbox %q must match account %q", entry.Entry.Mailbox, entry.Entry.Bundle.AccountID)
	}
	bytes, err := directoryEntrySigningBytes(entry.Entry)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(entry.Signature)
	if err != nil {
		return fmt.Errorf("decode directory signature: %w", err)
	}
	if !ed25519.Verify(entry.Entry.Bundle.AccountSigningPublic, bytes, signature) {
		return fmt.Errorf("directory signature is invalid")
	}
	return nil
}

func directoryEntrySigningBytes(entry DirectoryEntry) ([]byte, error) {
	type canonicalDevice struct {
		ID               string    `json:"id"`
		Mailbox          string    `json:"mailbox"`
		SigningPublic    string    `json:"signing_public"`
		EncryptionPublic string    `json:"encryption_public"`
		Signature        string    `json:"signature"`
		Revoked          bool      `json:"revoked"`
		RevokedAt        time.Time `json:"revoked_at,omitempty"`
	}
	devices := make([]canonicalDevice, 0, len(entry.Bundle.Devices))
	for _, device := range entry.Bundle.Devices {
		devices = append(devices, canonicalDevice{
			ID:               device.ID,
			Mailbox:          device.Mailbox,
			SigningPublic:    base64.StdEncoding.EncodeToString(device.SigningPublic),
			EncryptionPublic: base64.StdEncoding.EncodeToString(device.EncryptionPublic),
			Signature:        base64.StdEncoding.EncodeToString(device.Signature),
			Revoked:          device.Revoked,
			RevokedAt:        device.RevokedAt.UTC(),
		})
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Mailbox == devices[j].Mailbox {
			return devices[i].ID < devices[j].ID
		}
		return devices[i].Mailbox < devices[j].Mailbox
	})
	bytes, err := json.Marshal(struct {
		Mailbox           string            `json:"mailbox"`
		AccountID         string            `json:"account_id"`
		AccountSigningKey string            `json:"account_signing_public"`
		Devices           []canonicalDevice `json:"devices"`
		Discoverable      bool              `json:"discoverable,omitempty"`
		PublishedAt       time.Time         `json:"published_at"`
		Version           int64             `json:"version"`
	}{
		Mailbox:           entry.Mailbox,
		AccountID:         entry.Bundle.AccountID,
		AccountSigningKey: base64.StdEncoding.EncodeToString(entry.Bundle.AccountSigningPublic),
		Devices:           devices,
		Discoverable:      entry.Discoverable,
		PublishedAt:       entry.PublishedAt.UTC(),
		Version:           entry.Version,
	})
	if err != nil {
		return nil, fmt.Errorf("encode directory signing payload: %w", err)
	}
	return bytes, nil
}
