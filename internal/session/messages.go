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

func Encrypt(sender *identity.Identity, recipient *identity.Contact, plaintext string) (protocol.Envelope, error) {
	senderPub, senderPriv, err := sender.EncryptionKeyPair()
	if err != nil {
		return protocol.Envelope{}, err
	}
	recipientPub, err := bytesToKey(recipient.DeviceEncryptionPublic)
	if err != nil {
		return protocol.Envelope{}, fmt.Errorf("recipient encryption key: %w", err)
	}

	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return protocol.Envelope{}, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := box.Seal(nil, []byte(plaintext), &nonce, recipientPub, senderPriv)
	envelope := protocol.Envelope{
		SenderMailbox:                sender.Mailbox,
		RecipientMailbox:             recipient.Mailbox,
		BodyEncoding:                 BodyEncodingBox,
		Ciphertext:                   base64.StdEncoding.EncodeToString(ciphertext),
		Nonce:                        base64.StdEncoding.EncodeToString(nonce[:]),
		SenderDeviceSigningPublic:    base64.StdEncoding.EncodeToString(sender.DeviceSigningPublic),
		SenderDeviceEncryptionPublic: base64.StdEncoding.EncodeToString(senderPub[:]),
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(sender.DeviceSigningPrivate, signingBytes(envelope)))
	return envelope, nil
}

func Decrypt(recipient *identity.Identity, sender *identity.Contact, envelope protocol.Envelope) (string, error) {
	if envelope.BodyEncoding == "" {
		return envelope.Body, nil
	}
	if envelope.BodyEncoding != BodyEncodingBox {
		return "", fmt.Errorf("unsupported body encoding %q", envelope.BodyEncoding)
	}
	if envelope.SenderMailbox != sender.Mailbox {
		return "", fmt.Errorf("sender mailbox mismatch")
	}
	if envelope.SenderDeviceSigningPublic != base64.StdEncoding.EncodeToString(sender.DeviceSigningPublic) {
		return "", fmt.Errorf("sender signing key does not match stored contact")
	}
	if envelope.SenderDeviceEncryptionPublic != base64.StdEncoding.EncodeToString(sender.DeviceEncryptionPublic) {
		return "", fmt.Errorf("sender encryption key does not match stored contact")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return "", fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(sender.DeviceSigningPublic, signingBytes(envelope), signature) {
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
	senderPub, err := bytesToKey(sender.DeviceEncryptionPublic)
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
