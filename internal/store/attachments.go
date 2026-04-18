package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s *ClientStore) SaveAttachment(peerMailbox, attachmentID, filename string, bytes []byte) (string, error) {
	if err := s.Ensure(); err != nil {
		return "", err
	}
	path := s.attachmentPath(peerMailbox, attachmentID, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create attachment dir: %w", err)
	}
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		return "", fmt.Errorf("write attachment: %w", err)
	}
	return path, nil
}

func (s *ClientStore) attachmentPath(peerMailbox, attachmentID, filename string) string {
	peerMailbox = strings.ReplaceAll(peerMailbox, string(os.PathSeparator), "_")
	filename = strings.ReplaceAll(filepath.Base(strings.TrimSpace(filename)), string(os.PathSeparator), "_")
	if filename == "." || filename == "" {
		filename = "attachment.bin"
	}
	return filepath.Join(s.dir, "attachments", peerMailbox, attachmentID+"-"+filename)
}
