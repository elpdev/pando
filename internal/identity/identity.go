package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

type Identity struct {
	Mailbox                string             `json:"mailbox"`
	AccountSigningPublic   ed25519.PublicKey  `json:"account_signing_public"`
	AccountSigningPrivate  ed25519.PrivateKey `json:"account_signing_private"`
	DeviceSigningPublic    ed25519.PublicKey  `json:"device_signing_public"`
	DeviceSigningPrivate   ed25519.PrivateKey `json:"device_signing_private"`
	DeviceEncryptionPublic []byte             `json:"device_encryption_public"`
	DeviceEncryptionSecret []byte             `json:"device_encryption_secret"`
	DeviceSignature        []byte             `json:"device_signature"`
}

type InviteBundle struct {
	Mailbox                string            `json:"mailbox"`
	AccountSigningPublic   ed25519.PublicKey `json:"account_signing_public"`
	DeviceSigningPublic    ed25519.PublicKey `json:"device_signing_public"`
	DeviceEncryptionPublic []byte            `json:"device_encryption_public"`
	DeviceSignature        []byte            `json:"device_signature"`
}

type Contact struct {
	Mailbox                string            `json:"mailbox"`
	AccountSigningPublic   ed25519.PublicKey `json:"account_signing_public"`
	DeviceSigningPublic    ed25519.PublicKey `json:"device_signing_public"`
	DeviceEncryptionPublic []byte            `json:"device_encryption_public"`
	Verified               bool              `json:"verified"`
}

func New(mailbox string) (*Identity, error) {
	accountPub, accountPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate account signing key: %w", err)
	}
	deviceSignPub, deviceSignPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate device signing key: %w", err)
	}
	deviceEncPub, deviceEncPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate device encryption key: %w", err)
	}

	id := &Identity{
		Mailbox:                mailbox,
		AccountSigningPublic:   accountPub,
		AccountSigningPrivate:  accountPriv,
		DeviceSigningPublic:    deviceSignPub,
		DeviceSigningPrivate:   deviceSignPriv,
		DeviceEncryptionPublic: append([]byte(nil), deviceEncPub[:]...),
		DeviceEncryptionSecret: append([]byte(nil), deviceEncPriv[:]...),
	}
	id.DeviceSignature = ed25519.Sign(accountPriv, deviceStatement(mailbox, id.DeviceSigningPublic, id.DeviceEncryptionPublic))
	return id, nil
}

func (i *Identity) InviteBundle() InviteBundle {
	return InviteBundle{
		Mailbox:                i.Mailbox,
		AccountSigningPublic:   append(ed25519.PublicKey(nil), i.AccountSigningPublic...),
		DeviceSigningPublic:    append(ed25519.PublicKey(nil), i.DeviceSigningPublic...),
		DeviceEncryptionPublic: append([]byte(nil), i.DeviceEncryptionPublic...),
		DeviceSignature:        append([]byte(nil), i.DeviceSignature...),
	}
}

func (i *Identity) Fingerprint() string {
	hash := sha256.Sum256(i.AccountSigningPublic)
	return hex.EncodeToString(hash[:8])
}

func (i *Identity) EncryptionKeyPair() (*[32]byte, *[32]byte, error) {
	publicKey, err := bytesToKey(i.DeviceEncryptionPublic)
	if err != nil {
		return nil, nil, err
	}
	secretKey, err := bytesToKey(i.DeviceEncryptionSecret)
	if err != nil {
		return nil, nil, err
	}
	return publicKey, secretKey, nil
}

func ContactFromInvite(bundle InviteBundle) (*Contact, error) {
	if err := VerifyInvite(bundle); err != nil {
		return nil, err
	}
	return &Contact{
		Mailbox:                bundle.Mailbox,
		AccountSigningPublic:   append(ed25519.PublicKey(nil), bundle.AccountSigningPublic...),
		DeviceSigningPublic:    append(ed25519.PublicKey(nil), bundle.DeviceSigningPublic...),
		DeviceEncryptionPublic: append([]byte(nil), bundle.DeviceEncryptionPublic...),
		Verified:               false,
	}, nil
}

func VerifyInvite(bundle InviteBundle) error {
	if bundle.Mailbox == "" {
		return fmt.Errorf("invite mailbox is required")
	}
	if len(bundle.AccountSigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid account signing public key")
	}
	if len(bundle.DeviceSigningPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing public key")
	}
	if _, err := bytesToKey(bundle.DeviceEncryptionPublic); err != nil {
		return fmt.Errorf("invalid device encryption public key: %w", err)
	}
	if !ed25519.Verify(bundle.AccountSigningPublic, deviceStatement(bundle.Mailbox, bundle.DeviceSigningPublic, bundle.DeviceEncryptionPublic), bundle.DeviceSignature) {
		return fmt.Errorf("device bundle signature is invalid")
	}
	return nil
}

func deviceStatement(mailbox string, deviceSignPub ed25519.PublicKey, deviceEncPub []byte) []byte {
	statement, _ := json.Marshal(struct {
		Mailbox                string `json:"mailbox"`
		DeviceSigningPublic    string `json:"device_signing_public"`
		DeviceEncryptionPublic string `json:"device_encryption_public"`
	}{
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
