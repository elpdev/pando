package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

func sanitizeStoreMailboxComponent(value string) (string, error) {
	return sanitizeStoreIdentifier(value, "mailbox")
}

func sanitizeStoreAttachmentID(value string) (string, error) {
	return sanitizeStoreIdentifier(value, "attachment id")
}

func sanitizeStoreRoomID(value string) (string, error) {
	return sanitizeStoreIdentifier(value, "room id")
}

func joinStorePath(base, name string) string {
	return filepath.Join(base, name)
}

func sanitizeStoreIdentifier(value, label string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "." || trimmed == ".." {
		return "", fmt.Errorf("invalid %s", label)
	}
	if strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, "/\\\x00") {
		return "", fmt.Errorf("invalid %s", label)
	}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '.', '-', '_', '@':
			continue
		default:
			return "", fmt.Errorf("invalid %s", label)
		}
	}
	return trimmed, nil
}

func sanitizeStoreFilename(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.NewReplacer("/", "_", `\\`, "_").Replace(trimmed)
	clean := filepath.Base(trimmed)
	if clean == "" || clean == "." {
		return "attachment.bin"
	}
	b := strings.Builder{}
	b.Grow(len(clean))
	for _, r := range clean {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		switch r {
		case '.', '-', '_', '@':
			b.WriteRune(r)
		case ' ':
			b.WriteByte('_')
		default:
			b.WriteByte('_')
		}
	}
	result := strings.Trim(b.String(), " .")
	if result == "" || result == "." || result == ".." {
		return "attachment.bin"
	}
	return result
}
