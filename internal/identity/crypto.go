package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func clonePubKey(k ed25519.PublicKey) ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), k...)
}

func clonePrivKey(k ed25519.PrivateKey) ed25519.PrivateKey {
	return append(ed25519.PrivateKey(nil), k...)
}

func cloneBytes(b []byte) []byte {
	return append([]byte(nil), b...)
}

func VerifyInvite(bundle InviteBundle) error {
	if bundle.AccountID == "" {
		return fmt.Errorf("invite account id is required")
	}
	if len(bundle.AccountSigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid account signing public key")
	}
	if len(bundle.Devices) == 0 {
		return fmt.Errorf("invite must contain at least one device")
	}
	for _, device := range bundle.Devices {
		if device.Revoked {
			continue
		}
		if err := verifyDeviceBundle(bundle.AccountID, bundle.AccountSigningPublic, device); err != nil {
			return err
		}
	}
	return nil
}

func verifyDeviceBundle(accountID string, accountPublic ed25519.PublicKey, bundle DeviceBundle) error {
	if bundle.ID == "" {
		return fmt.Errorf("device id is required")
	}
	if bundle.Mailbox == "" {
		return fmt.Errorf("device mailbox is required")
	}
	if len(bundle.SigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing public key")
	}
	if _, err := bytesToKey(bundle.EncryptionPublic); err != nil {
		return fmt.Errorf("invalid device encryption public key: %w", err)
	}
	if !ed25519.Verify(accountPublic, deviceStatement(accountID, bundle.ID, bundle.Mailbox, bundle.SigningPublic, bundle.EncryptionPublic), bundle.Signature) {
		return fmt.Errorf("device bundle signature is invalid")
	}
	return nil
}

func deviceStatement(accountID, deviceID, mailbox string, deviceSignPub ed25519.PublicKey, deviceEncPub []byte) []byte {
	statement, _ := json.Marshal(struct {
		AccountID              string `json:"account_id"`
		DeviceID               string `json:"device_id"`
		Mailbox                string `json:"mailbox"`
		DeviceSigningPublic    string `json:"device_signing_public"`
		DeviceEncryptionPublic string `json:"device_encryption_public"`
	}{
		AccountID:              accountID,
		DeviceID:               deviceID,
		Mailbox:                mailbox,
		DeviceSigningPublic:    base64.StdEncoding.EncodeToString(deviceSignPub),
		DeviceEncryptionPublic: base64.StdEncoding.EncodeToString(deviceEncPub),
	})
	return statement
}

func bytesToKey(value []byte) (*[32]byte, error) {
	if len(value) != 32 {
		return nil, fmt.Errorf("expected 32 bytes, got %d", len(value))
	}
	var key [32]byte
	copy(key[:], value)
	return &key, nil
}

func Fingerprint(publicKey ed25519.PublicKey) string {
	hash := sha256.Sum256(publicKey)
	return hex.EncodeToString(hash[:8])
}
