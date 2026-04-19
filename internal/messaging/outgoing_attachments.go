package messaging

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/session"
	"github.com/elpdev/pando/internal/store"
)

func (s *Service) PreparePhotoOutgoing(recipientAccountID, path string) (*OutgoingBatch, string, error) {
	return s.prepareAttachmentOutgoing(recipientAccountID, path, attachmentTypePhoto)
}

func (s *Service) PrepareVoiceOutgoing(recipientAccountID, path string) (*OutgoingBatch, string, error) {
	return s.prepareAttachmentOutgoing(recipientAccountID, path, attachmentTypeVoice)
}

func (s *Service) PrepareFileOutgoing(recipientAccountID, path string) (*OutgoingBatch, string, error) {
	return s.prepareAttachmentOutgoing(recipientAccountID, path, attachmentTypeFile)
}

func (s *Service) prepareAttachmentOutgoing(recipientAccountID, path, attachmentType string) (*OutgoingBatch, string, error) {
	contact, err := s.loadOutgoingContact(recipientAccountID)
	if err != nil {
		return nil, "", err
	}
	bytes, filename, mimeType, err := loadAttachmentPayload(path, attachmentType)
	if err != nil {
		return nil, "", err
	}
	payloads, _, err := buildAttachmentChunkPayloads(attachmentType, filename, mimeType, bytes)
	if err != nil {
		return nil, "", err
	}
	envelopes, err := s.encryptAttachmentPayloads(contact, payloads)
	if err != nil {
		return nil, "", err
	}
	return &OutgoingBatch{Envelopes: envelopes}, fmt.Sprintf("%s sent: %s", AttachmentLabel(attachmentType), sanitizeAttachmentName(filename)), nil
}

func (s *Service) loadOutgoingContact(recipientAccountID string) (*identity.Contact, error) {
	contact, err := s.store.LoadContact(recipientAccountID)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, missingContactError(recipientAccountID)
		}
		return nil, err
	}
	return contact, nil
}

func loadAttachmentPayload(path, attachmentType string) ([]byte, string, string, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("read %s: %w", AttachmentLabel(attachmentType), err)
	}
	filename := filepath.Base(path)
	mimeType := detectAttachmentMIMEType(filename, bytes, attachmentType)
	if err := validateAttachmentMIMEType(path, mimeType, attachmentType); err != nil {
		return nil, "", "", err
	}
	return bytes, filename, mimeType, nil
}

func (s *Service) encryptAttachmentPayloads(contact *identity.Contact, payloads []string) ([]protocol.Envelope, error) {
	updateEnvelopes, err := s.contactUpdateEnvelopes(contact)
	if err != nil {
		return nil, err
	}
	envelopes := make([]protocol.Envelope, 0, len(updateEnvelopes)+(len(payloads)*len(contact.ActiveDevices())))
	envelopes = append(envelopes, updateEnvelopes...)
	for _, payload := range payloads {
		chunkEnvelopes, err := session.Encrypt(s.identity, contact, payload)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, chunkEnvelopes...)
	}
	return envelopes, nil
}
