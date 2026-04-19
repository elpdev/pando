package store

import (
	"fmt"
	"os"
	"path/filepath"
)

func (s *ClientStore) SaveAttachment(peerMailbox, attachmentID, filename string, bytes []byte) (string, error) {
	if err := s.Ensure(); err != nil {
		return "", err
	}
	path, err := s.attachmentPath(peerMailbox, attachmentID, filename)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create attachment dir: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return "", fmt.Errorf("write attachment: %w", err)
	}
	return path, nil
}

func (s *ClientStore) attachmentPath(peerMailbox, attachmentID, filename string) (string, error) {
	safeMailbox, err := sanitizeStoreMailboxComponent(peerMailbox)
	if err != nil {
		return "", err
	}
	safeAttachmentID, err := sanitizeStoreAttachmentID(attachmentID)
	if err != nil {
		return "", err
	}
	safeFilename := sanitizeStoreFilename(filename)
	return filepath.Join(s.dir, "attachments", safeMailbox, safeAttachmentID+"-"+safeFilename), nil
}
