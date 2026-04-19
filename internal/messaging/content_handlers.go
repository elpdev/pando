package messaging

import (
	"bytes"
	"fmt"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/store"
)

func (s *Service) dispatchIncomingPayload(contact *identity.Contact, body string) (*IncomingResult, bool, error) {
	payload, ok, err := decodeContentPayload(body)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	result, err := s.handleIncomingPayload(contact, payload)
	if err != nil {
		return nil, true, err
	}
	if result == nil {
		return nil, false, nil
	}
	return result, true, nil
}

func (s *Service) handleIncomingPayload(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	switch payload.Kind {
	case contentKindContactUpdate:
		return s.handleContactUpdate(contact, payload)
	case contentKindDeliveryAck:
		return s.handleDeliveryAck(contact, payload)
	case contentKindContactRequest:
		return s.handleContactRequest(contact, payload)
	case contentKindContactResponse:
		return s.handleContactResponse(contact, payload)
	case contentKindTyping:
		return s.handleTyping(contact, payload)
	case contentKindAttachmentChunk:
		return s.handleAttachmentChunk(contact, payload)
	default:
		return nil, nil
	}
}

func (s *Service) handleContactRequest(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	requestPayload, err := s.parseContactRequestPayload(contact, payload)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	request := &store.ContactRequest{
		AccountID: requestPayload.Bundle.AccountID,
		Direction: store.ContactRequestDirectionIncoming,
		Status:    store.ContactRequestStatusPending,
		Note:      requestPayload.Note,
		Bundle:    requestPayload.Bundle,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.SaveContactRequest(request); err != nil {
		return nil, err
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, ContactRequest: request}, nil
}

func (s *Service) handleContactResponse(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	response, err := s.parseContactRequestResponsePayload(payload)
	if err != nil {
		return nil, err
	}
	request, err := s.store.LoadContactRequest(contact.AccountID)
	if err != nil {
		return nil, err
	}
	request.UpdatedAt = time.Now().UTC()
	if response.Decision == contactRequestDecisionAccept {
		request.Status = store.ContactRequestStatusAccepted
		if response.Bundle == nil {
			return nil, fmt.Errorf("contact request acceptance bundle is required")
		}
		if _, err := s.ImportContactInviteBundle(*response.Bundle, identity.TrustSourceRelayDirectory); err != nil {
			return nil, err
		}
	} else {
		request.Status = store.ContactRequestStatusRejected
	}
	if err := s.store.SaveContactRequest(request); err != nil {
		return nil, err
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, ContactRequest: request}, nil
}

func (s *Service) handleContactUpdate(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	updated, change, err := s.parseAndApplyContactUpdate(contact, payload)
	if err != nil {
		return nil, err
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, ContactUpdated: updated, ContactChange: change}, nil
}

func (s *Service) handleDeliveryAck(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	ack, err := s.parseDeliveryAckPayload(payload)
	if err != nil {
		return nil, err
	}
	if err := s.MarkDelivered(contact.AccountID, ack.MessageID, ack.DeliveredAt); err != nil {
		return nil, err
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, MessageID: ack.MessageID}, nil
}

func (s *Service) handleTyping(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	typing, err := s.parseTypingPayload(payload)
	if err != nil {
		return nil, err
	}
	return &IncomingResult{Control: true, PeerAccountID: contact.AccountID, TypingState: typing.State, TypingExpiresAt: typing.ExpiresAt}, nil
}

func (s *Service) handleAttachmentChunk(contact *identity.Contact, payload *contentPayload) (*IncomingResult, error) {
	message, done, err := s.handleIncomingAttachmentChunk(contact.AccountID, payload.AttachmentChunk)
	if err != nil {
		return nil, err
	}
	if !done {
		return &IncomingResult{Control: true, PeerAccountID: contact.AccountID}, nil
	}
	return &IncomingResult{PeerAccountID: contact.AccountID, Body: message}, nil
}

func (s *Service) handleIncomingAttachmentChunk(peerAccountID string, chunk *attachmentChunkPayload) (string, bool, error) {
	return s.incomingAttachments.handleChunk(peerAccountID, chunk)
}

func (s *Service) parseAndApplyContactUpdate(existing *identity.Contact, payload *contentPayload) (*identity.Contact, ContactUpdateChange, error) {
	if payload == nil || payload.ContactUpdate == nil {
		return nil, ContactUpdateUnchanged, fmt.Errorf("contact update payload is required")
	}
	return s.applyContactUpdate(existing, *payload.ContactUpdate)
}

func (s *Service) applyContactUpdate(existing *identity.Contact, bundle identity.InviteBundle) (*identity.Contact, ContactUpdateChange, error) {
	updated, err := identity.ContactFromInvite(bundle)
	if err != nil {
		return nil, ContactUpdateUnchanged, err
	}
	if existing.Fingerprint() != updated.Fingerprint() || existing.AccountID != updated.AccountID {
		return nil, ContactUpdateUnchanged, fmt.Errorf("contact update does not match stored identity for account %s", existing.AccountID)
	}
	change := detectContactUpdateChange(existing, updated)
	updated.Verified = existing.Verified
	updated.TrustSource = existing.TrustSource
	updated.NormalizeTrust()
	if err := s.store.SaveContact(updated); err != nil {
		return nil, ContactUpdateUnchanged, err
	}
	return updated, change, nil
}

func detectContactUpdateChange(existing, updated *identity.Contact) ContactUpdateChange {
	if existing == nil || updated == nil {
		return ContactUpdateUnchanged
	}
	existingDevices := activeContactDevicesByKey(existing)
	updatedDevices := activeContactDevicesByKey(updated)

	added := false
	revoked := false
	rotated := false

	for key, next := range updatedDevices {
		current, ok := existingDevices[key]
		if !ok {
			added = true
			continue
		}
		if next.Mailbox != current.Mailbox || !bytes.Equal(next.SigningPublic, current.SigningPublic) || !bytes.Equal(next.EncryptionPublic, current.EncryptionPublic) {
			rotated = true
		}
	}
	for key := range existingDevices {
		if _, ok := updatedDevices[key]; !ok {
			revoked = true
		}
	}

	changes := 0
	if added {
		changes++
	}
	if revoked {
		changes++
	}
	if rotated {
		changes++
	}
	if changes == 0 {
		return ContactUpdateUnchanged
	}
	if changes > 1 {
		return ContactUpdateDeviceChanged
	}
	if added {
		return ContactUpdateDeviceAdded
	}
	if revoked {
		return ContactUpdateDeviceRevoked
	}
	return ContactUpdateDeviceRotated
}

func activeContactDevicesByKey(contact *identity.Contact) map[string]identity.ContactDevice {
	devices := make(map[string]identity.ContactDevice)
	if contact == nil {
		return devices
	}
	for _, device := range contact.ActiveDevices() {
		devices[contactDeviceKey(device)] = device
	}
	return devices
}

func contactDeviceKey(device identity.ContactDevice) string {
	if device.ID != "" {
		return device.ID
	}
	return device.Mailbox
}

func (s *Service) parseDeliveryAckPayload(payload *contentPayload) (*deliveryAck, error) {
	if payload == nil || payload.DeliveryAck == nil {
		return nil, fmt.Errorf("delivery ack payload is required")
	}
	ack := *payload.DeliveryAck
	if ack.MessageID == "" {
		return nil, fmt.Errorf("delivery ack message id is required")
	}
	if ack.DeliveredAt.IsZero() {
		ack.DeliveredAt = time.Now().UTC()
	}
	return &ack, nil
}

func (s *Service) parseTypingPayload(payload *contentPayload) (*typingIndicator, error) {
	if payload == nil || payload.Typing == nil {
		return nil, fmt.Errorf("typing payload is required")
	}
	typing := *payload.Typing
	switch typing.State {
	case TypingStateActive:
		if typing.ExpiresAt.IsZero() {
			return nil, fmt.Errorf("typing expiry is required")
		}
	case TypingStateIdle:
		typing.ExpiresAt = time.Time{}
	default:
		return nil, fmt.Errorf("invalid typing state %q", typing.State)
	}
	return &typing, nil
}

func (s *Service) parseContactRequestPayload(contact *identity.Contact, payload *contentPayload) (*contactRequest, error) {
	if payload == nil || payload.ContactRequest == nil {
		return nil, fmt.Errorf("contact request payload is required")
	}
	request := *payload.ContactRequest
	if request.Bundle.AccountID == "" {
		return nil, fmt.Errorf("contact request bundle is required")
	}
	if contact.AccountID != request.Bundle.AccountID || contact.Fingerprint() != identity.Fingerprint(request.Bundle.AccountSigningPublic) {
		return nil, fmt.Errorf("contact request does not match sender identity for account %s", contact.AccountID)
	}
	return &request, nil
}

func (s *Service) parseContactRequestResponsePayload(payload *contentPayload) (*contactRequestResponse, error) {
	if payload == nil || payload.ContactResponse == nil {
		return nil, fmt.Errorf("contact request response payload is required")
	}
	response := *payload.ContactResponse
	if response.Decision != contactRequestDecisionAccept && response.Decision != contactRequestDecisionReject {
		return nil, fmt.Errorf("invalid contact request response decision %q", response.Decision)
	}
	return &response, nil
}
