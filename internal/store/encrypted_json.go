package store

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/identity"
)

func writeEncryptedJSON(id *identity.Identity, path string, value any, encodeErr, encryptErr, writeErr string, pretty bool) error {
	var (
		plaintext []byte
		err       error
	)
	if pretty {
		plaintext, err = json.MarshalIndent(value, "", "  ")
	} else {
		plaintext, err = json.Marshal(value)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", encodeErr, err)
	}
	sealed, err := encryptStorePayload(id, plaintext)
	if err != nil {
		return fmt.Errorf("%s: %w", encryptErr, err)
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return fmt.Errorf("%s: %w", writeErr, err)
	}
	return nil
}
