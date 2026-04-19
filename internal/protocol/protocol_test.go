package protocol

import (
	"strings"
	"testing"
	"time"
)

func TestMessageValidateAcceptsSupportedMessages(t *testing.T) {
	now := time.Date(2026, 4, 19, 12, 30, 0, 0, time.UTC)
	cipherEnvelope := Envelope{
		SenderMailbox:                "alice",
		RecipientMailbox:             "bob",
		Ciphertext:                   "ciphertext",
		BodyEncoding:                 "sealed-box-v1",
		Nonce:                        "nonce",
		SenderDeviceSigningPublic:    "signing-key",
		SenderDeviceEncryptionPublic: "encryption-key",
		Signature:                    "signature",
	}

	messages := []Message{
		{Type: MessageTypeSubscribe, Subscribe: &SubscribeRequest{Mailbox: "alice", DeviceSigningKey: "key", DeviceProof: "proof", ChallengeNonce: "nonce", ChallengeExpiresAt: now}},
		{Type: MessageTypeSubscribeChallenge, Challenge: &SubscribeChallenge{Nonce: "nonce", ExpiresAt: now}},
		{Type: MessageTypePublish, Publish: &PublishRequest{Envelope: cipherEnvelope}},
		{Type: MessageTypeIncoming, Incoming: &cipherEnvelope},
		{Type: MessageTypeAck, Ack: &Ack{ID: "ack-id"}},
		{Type: MessageTypeError, Error: &Error{Message: "rejected"}},
	}

	for _, msg := range messages {
		if err := msg.Validate(); err != nil {
			t.Fatalf("validate %q: %v", msg.Type, err)
		}
	}
}

func TestMessageValidateRejectsMissingSubscribeFields(t *testing.T) {
	now := time.Now().UTC()

	if err := (Message{Type: MessageTypeSubscribe}).Validate(); err == nil || err.Error() != "subscribe payload is required" {
		t.Fatalf("expected subscribe payload error, got %v", err)
	}
	if err := (Message{Type: MessageTypeSubscribe, Subscribe: &SubscribeRequest{DeviceSigningKey: "key", DeviceProof: "proof", ChallengeNonce: "nonce", ChallengeExpiresAt: now}}).Validate(); err == nil || err.Error() != "mailbox is required" {
		t.Fatalf("expected mailbox error, got %v", err)
	}
	if err := (Message{Type: MessageTypeSubscribe, Subscribe: &SubscribeRequest{Mailbox: "alice", DeviceProof: "proof", ChallengeNonce: "nonce", ChallengeExpiresAt: now}}).Validate(); err == nil || err.Error() != "device signing key is required" {
		t.Fatalf("expected signing key error, got %v", err)
	}
	if err := (Message{Type: MessageTypeSubscribe, Subscribe: &SubscribeRequest{Mailbox: "alice", DeviceSigningKey: "key", ChallengeNonce: "nonce", ChallengeExpiresAt: now}}).Validate(); err == nil || err.Error() != "device proof is required" {
		t.Fatalf("expected device proof error, got %v", err)
	}
	if err := (Message{Type: MessageTypeSubscribe, Subscribe: &SubscribeRequest{Mailbox: "alice", DeviceSigningKey: "key", DeviceProof: "proof", ChallengeExpiresAt: now}}).Validate(); err == nil || err.Error() != "challenge nonce is required" {
		t.Fatalf("expected challenge nonce error, got %v", err)
	}
	if err := (Message{Type: MessageTypeSubscribe, Subscribe: &SubscribeRequest{Mailbox: "alice", DeviceSigningKey: "key", DeviceProof: "proof", ChallengeNonce: "nonce"}}).Validate(); err == nil || err.Error() != "challenge expiry is required" {
		t.Fatalf("expected challenge expiry error, got %v", err)
	}
}

func TestMessageValidateRejectsOtherInvalidPayloads(t *testing.T) {
	if err := (Message{Type: MessageTypeSubscribeChallenge}).Validate(); err == nil || err.Error() != "challenge payload is required" {
		t.Fatalf("expected challenge payload error, got %v", err)
	}
	if err := (Message{Type: MessageTypePublish}).Validate(); err == nil || err.Error() != "publish payload is required" {
		t.Fatalf("expected publish payload error, got %v", err)
	}
	if err := (Message{Type: MessageTypeIncoming}).Validate(); err == nil || err.Error() != "incoming payload is required" {
		t.Fatalf("expected incoming payload error, got %v", err)
	}
	if err := (Message{Type: MessageTypeAck, Ack: &Ack{}}).Validate(); err == nil || err.Error() != "ack id is required" {
		t.Fatalf("expected ack id error, got %v", err)
	}
	if err := (Message{Type: MessageTypeError, Error: &Error{}}).Validate(); err == nil || err.Error() != "error message is required" {
		t.Fatalf("expected error message error, got %v", err)
	}
	if err := (Message{Type: "bogus"}).Validate(); err == nil || err.Error() != "unknown message type" {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}

func TestValidateEnvelopeAcceptsPlaintextAndCiphertext(t *testing.T) {
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: "hello"}); err != nil {
		t.Fatalf("validate plaintext envelope: %v", err)
	}
	if err := ValidateEnvelope(Envelope{
		SenderMailbox:                "alice",
		RecipientMailbox:             "bob",
		Ciphertext:                   "ciphertext",
		BodyEncoding:                 "sealed-box-v1",
		Nonce:                        "nonce",
		SenderDeviceSigningPublic:    "signing",
		SenderDeviceEncryptionPublic: "encryption",
		Signature:                    "signature",
	}); err != nil {
		t.Fatalf("validate ciphertext envelope: %v", err)
	}
}

func TestValidateEnvelopeRejectsMissingFields(t *testing.T) {
	if err := ValidateEnvelope(Envelope{RecipientMailbox: "bob", Body: "hello"}); err == nil || err.Error() != "sender mailbox is required" {
		t.Fatalf("expected sender mailbox error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", Body: "hello"}); err == nil || err.Error() != "recipient mailbox is required" {
		t.Fatalf("expected recipient mailbox error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob"}); err == nil || err.Error() != "message body or ciphertext is required" {
		t.Fatalf("expected body/ciphertext error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Ciphertext: "cipher"}); err == nil || err.Error() != "body encoding is required for ciphertext messages" {
		t.Fatalf("expected body encoding error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Ciphertext: "cipher", BodyEncoding: "sealed-box-v1"}); err == nil || err.Error() != "nonce is required for ciphertext messages" {
		t.Fatalf("expected nonce error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Ciphertext: "cipher", BodyEncoding: "sealed-box-v1", Nonce: "nonce"}); err == nil || err.Error() != "sender device signing public key is required for ciphertext messages" {
		t.Fatalf("expected signing public key error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Ciphertext: "cipher", BodyEncoding: "sealed-box-v1", Nonce: "nonce", SenderDeviceSigningPublic: "signing"}); err == nil || err.Error() != "sender device encryption public key is required for ciphertext messages" {
		t.Fatalf("expected encryption public key error, got %v", err)
	}
	if err := ValidateEnvelope(Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Ciphertext: "cipher", BodyEncoding: "sealed-box-v1", Nonce: "nonce", SenderDeviceSigningPublic: "signing", SenderDeviceEncryptionPublic: "encryption"}); err == nil || err.Error() != "signature is required for ciphertext messages" {
		t.Fatalf("expected signature error, got %v", err)
	}
}

func TestSubscribeProofBytesUsesUTCFormatting(t *testing.T) {
	expiresAt := time.Date(2026, 4, 19, 8, 15, 30, 123456789, time.FixedZone("local", -4*60*60))
	proof := string(SubscribeProofBytes("alice", "nonce-123", expiresAt))

	if !strings.HasPrefix(proof, "subscribe\nalice\nnonce-123\n") {
		t.Fatalf("unexpected proof prefix: %q", proof)
	}
	if !strings.HasSuffix(proof, expiresAt.UTC().Format(time.RFC3339Nano)) {
		t.Fatalf("expected utc timestamp suffix, got %q", proof)
	}
}
