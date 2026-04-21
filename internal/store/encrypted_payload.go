package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"

	"github.com/elpdev/pando/internal/identity"
)

func encryptStorePayload(id *identity.Identity, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(historyKey(id))
	if err != nil {
		return nil, fmt.Errorf("create store cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create store AEAD: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate store nonce: %w", err)
	}
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func decryptStorePayload(id *identity.Identity, bytes []byte) ([]byte, error) {
	block, err := aes.NewCipher(historyKey(id))
	if err != nil {
		return nil, fmt.Errorf("create store cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create store AEAD: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(bytes) < nonceSize {
		return nil, fmt.Errorf("store payload is missing nonce")
	}
	plaintext, err := gcm.Open(nil, bytes[:nonceSize], bytes[nonceSize:], nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
