package messaging

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

const (
	contactRequestDecisionAccept = "accept"
	contactRequestDecisionReject = "reject"
)

func (s *Service) ContactRequestEnvelopes(entry *relayapi.SignedDirectoryEntry, note string) ([]protocol.Envelope, *store.ContactRequest, error) {
	if entry == nil {
		return nil, nil, fmt.Errorf("directory entry is required")
	}
	if err := relayapi.VerifySignedDirectoryEntry(*entry); err != nil {
		return nil, nil, err
	}
	contact, err := identity.ContactFromInvite(entry.Entry.Bundle)
	if err != nil {
		return nil, nil, err
	}
	payload, err := json.Marshal(contentPayload{
		Kind: contentKindContactRequest,
		ContactRequest: &contactRequest{
			Bundle: s.identity.InviteBundle(),
			Note:   strings.TrimSpace(note),
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("encode contact request payload: %w", err)
	}
	envelopes, err := session.Encrypt(s.identity, contact, string(payload))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	return envelopes, &store.ContactRequest{
		AccountID: entry.Entry.Bundle.AccountID,
		Direction: store.ContactRequestDirectionOutgoing,
		Status:    store.ContactRequestStatusPending,
		Note:      strings.TrimSpace(note),
		Bundle:    entry.Entry.Bundle,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Service) ContactRequestResponseEnvelopes(bundle identity.InviteBundle, decision string) ([]protocol.Envelope, error) {
	if decision != contactRequestDecisionAccept && decision != contactRequestDecisionReject {
		return nil, fmt.Errorf("invalid contact request decision %q", decision)
	}
	contact, err := identity.ContactFromInvite(bundle)
	if err != nil {
		return nil, err
	}
	payload := contentPayload{
		Kind:            contentKindContactResponse,
		ContactResponse: &contactRequestResponse{Decision: decision},
	}
	if decision == contactRequestDecisionAccept {
		payload.ContactResponse.Bundle = &identity.InviteBundle{
			AccountID:            s.identity.AccountID,
			AccountSigningPublic: append([]byte(nil), s.identity.AccountSigningPublic...),
			Devices:              s.identity.DeviceBundles(),
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode contact request response payload: %w", err)
	}
	return session.Encrypt(s.identity, contact, string(body))
}

func (s *Service) SaveContactRequest(request *store.ContactRequest) error {
	return s.store.SaveContactRequest(request)
}

func (s *Service) LoadContactRequest(accountID string) (*store.ContactRequest, error) {
	return s.store.LoadContactRequest(accountID)
}

func (s *Service) DeleteContactRequest(accountID string) error {
	return s.store.DeleteContactRequest(accountID)
}
