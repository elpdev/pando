package session

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/elpdev/chatui/internal/identity"
	"github.com/elpdev/chatui/internal/protocol"
	"golang.org/x/crypto/nacl/box"
)

const BodyEncodingBox = "box-sealed-v1"

func Encrypt(sender *identity.Identity, recipient *identity.Contact, plaintext string) ([]protocol.Envelope, error) {
	currentDevice, err := sender.CurrentDevice()
	if err != nil {
		return nil, err
	}
	senderPub, senderPriv, err := sender.EncryptionKeyPair()
	if err != nil {
		return nil, err
	}
	devices := recipient.ActiveDevices()
	if len(devices) == 0 {
		return nil, fmt.Errorf("contact %s has no active devices", recipient.AccountID)
	}
	envelopes := make([]protocol.Envelope, 0, len(devices))
	for _, device := range devices {
		recipientPub, err := bytesToKey(device.EncryptionPublic)
		if err != nil {
			return nil, fmt.Errorf("recipient encryption key: %w", err)
		}
		var nonce [24]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, fmt.Errorf("generate nonce: %w", err)
		}
		ciphertext := box.Seal(nil, []byte(plaintext), &nonce, recipientPub, senderPriv)
		envelope := protocol.Envelope{
			SenderMailbox:                currentDevice.Mailbox,
			RecipientMailbox:             device.Mailbox,
			BodyEncoding:                 BodyEncodingBox,
			Ciphertext:                   base64.StdEncoding.EncodeToString(ciphertext),
			Nonce:                        base64.StdEncoding.EncodeToString(nonce[:]),
			SenderDeviceSigningPublic:    base64.StdEncoding.EncodeToString(currentDevice.SigningPublic),
			SenderDeviceEncryptionPublic: base64.StdEncoding.EncodeToString(senderPub[:]),
		}
		envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(currentDevice.SigningPrivate, signingBytes(envelope)))
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func Decrypt(recipient *identity.Identity, sender *identity.Contact, envelope protocol.Envelope) (string, error) {
	if envelope.BodyEncoding == "" {
		return envelope.Body, nil
	}
	if envelope.BodyEncoding != BodyEncodingBox {
		return "", fmt.Errorf("unsupported body encoding %q", envelope.BodyEncoding)
	}
	senderDevice, err := sender.DeviceByMailbox(envelope.SenderMailbox)
	if err != nil {
		return "", err
	}
	if senderDevice.Revoked {
		return "", fmt.Errorf("sender device %s is revoked", senderDevice.Mailbox)
	}
	if envelope.SenderDeviceSigningPublic != base64.StdEncoding.EncodeToString(senderDevice.SigningPublic) {
		return "", fmt.Errorf("sender signing key does not match stored contact device")
	}
	if envelope.SenderDeviceEncryptionPublic != base64.StdEncoding.EncodeToString(senderDevice.EncryptionPublic) {
		return "", fmt.Errorf("sender encryption key does not match stored contact device")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(senderDevice.SigningPublic, signingBytes(envelope), signature) {
		return "", fmt.Errorf("invalid message signature")
	}
	nonceBytes, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}
	if len(nonceBytes) != 24 {
		return "", fmt.Errorf("invalid nonce length")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	_, recipientPriv, err := recipient.EncryptionKeyPair()
	if err != nil {
		return "", err
	}
	senderPub, err := bytesToKey(senderDevice.EncryptionPublic)
	if err != nil {
		return "", fmt.Errorf("sender encryption key: %w", err)
	}
	var nonce [24]byte
	copy(nonce[:], nonceBytes)
	plaintext, ok := box.Open(nil, ciphertext, &nonce, senderPub, recipientPriv)
	if !ok {
		return "", fmt.Errorf("decrypt message: failed to open ciphertext")
	}
	return string(plaintext), nil
}

func signingBytes(envelope protocol.Envelope) []byte {
	return []byte(envelope.SenderMailbox + "\n" + envelope.RecipientMailbox + "\n" + envelope.BodyEncoding + "\n" + envelope.Nonce + "\n" + envelope.Ciphertext + "\n" + envelope.SenderDeviceSigningPublic + "\n" + envelope.SenderDeviceEncryptionPublic)
}

func bytesToKey(value []byte) (*[32]byte, error) {
	if len(value) != 32 {
		return nil, fmt.Errorf("expected 32 bytes, got %d", len(value))
	}
	var key [32]byte
	copy(key[:], value)
	return &key, nil
}
