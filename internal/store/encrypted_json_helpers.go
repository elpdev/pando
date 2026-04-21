package store

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/identity"
)

func readEncryptedJSON(id *identity.Identity, path string, target any, readLabel, decryptLabel, decodeLabel string) error {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return fmt.Errorf("%s: %w", readLabel, err)
	}
	if len(bytes) < 12 {
		return fmt.Errorf("%s: file is too short", readLabel)
	}
	plaintext, err := decryptStorePayload(id, bytes)
	if err != nil {
		return fmt.Errorf("%s: %w", decryptLabel, err)
	}
	if err := json.Unmarshal(plaintext, target); err != nil {
		return fmt.Errorf("%s: %w", decodeLabel, err)
	}
	return nil
}
