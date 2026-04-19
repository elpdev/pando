package messaging

import (
	"fmt"
	"strings"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/invite"
	"github.com/elpdev/pando/internal/relayapi"
)

func (s *Service) ImportContactInviteText(text string, verified bool) (*identity.Contact, error) {
	bundle, err := invite.DecodeText(text)
	if err != nil {
		return nil, err
	}
	return s.ImportContactInviteBundle(*bundle, trustSourceForInviteVerification(verified))
}

// PreviewContactInviteText parses an invite text into a Contact without
// saving it. Used by the add-contact modal to show the user what they're
// about to import before committing.
func (s *Service) PreviewContactInviteText(text string) (*identity.Contact, error) {
	bundle, err := invite.DecodeText(text)
	if err != nil {
		return nil, err
	}
	return identity.ContactFromInvite(*bundle)
}

func (s *Service) ImportContactInviteBundle(bundle identity.InviteBundle, trustSource string) (*identity.Contact, error) {
	contact, err := identity.ContactFromInvite(bundle)
	if err != nil {
		return nil, err
	}
	contact, err = s.mergeStoredContactTrust(contact, trustSource)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveContact(contact); err != nil {
		return nil, err
	}
	return contact, nil
}

// ImportDirectoryContact looks up a mailbox in the trusted relay directory,
// verifies the signature, and saves the result as a verified contact with
// trust source "relay-directory". Used by both the CLI `contact lookup`
// command and the TUI add-contact modal so the trust-rank logic lives in one
// place.
func (s *Service) ImportDirectoryContact(client DirectoryClient, mailbox string) (*identity.Contact, error) {
	if client == nil {
		return nil, fmt.Errorf("relay client is required")
	}
	if strings.TrimSpace(mailbox) == "" {
		return nil, fmt.Errorf("mailbox is required")
	}
	entry, err := client.LookupDirectoryEntry(mailbox)
	if err != nil {
		return nil, err
	}
	if err := relayapi.VerifySignedDirectoryEntry(*entry); err != nil {
		return nil, err
	}
	return s.ImportContactInviteBundle(entry.Entry.Bundle, identity.TrustSourceRelayDirectory)
}

// ImportInviteCodeContact saves a contact obtained via the short-code
// rendezvous flow with trust source "invite-code".
func (s *Service) ImportInviteCodeContact(bundle identity.InviteBundle) (*identity.Contact, error) {
	return s.ImportContactInviteBundle(bundle, identity.TrustSourceInviteCode)
}

func trustSourceForInviteVerification(verified bool) string {
	if verified {
		return identity.TrustSourceManualVerified
	}
	return identity.TrustSourceUnverified
}

func (s *Service) mergeStoredContactTrust(contact *identity.Contact, trustSource string) (*identity.Contact, error) {
	if existing, err := s.store.LoadContact(contact.AccountID); err == nil && existing.Fingerprint() == contact.Fingerprint() {
		contact.Verified = existing.Verified
		contact.TrustSource = existing.TrustSource
	}
	if identity.TrustRank(trustSource) > identity.TrustRank(contact.TrustSource) {
		contact.TrustSource = trustSource
	}
	if identity.TrustRank(contact.TrustSource) >= identity.TrustRank(identity.TrustSourceInviteCode) {
		contact.Verified = true
	}
	contact.NormalizeTrust()
	return contact, nil
}
