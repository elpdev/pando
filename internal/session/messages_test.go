package session

import (
	"testing"

	"github.com/elpdev/chatui/internal/identity"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	alice, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new alice identity: %v", err)
	}
	bob, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bob.InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	aliceContact, err := identity.ContactFromInvite(alice.InviteBundle())
	if err != nil {
		t.Fatalf("alice invite to contact: %v", err)
	}

	envelopes, err := Encrypt(alice, bobContact, "hello bob")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(envelopes) != 1 {
		t.Fatalf("expected one envelope, got %d", len(envelopes))
	}

	plaintext, err := Decrypt(bob, aliceContact, envelopes[0])
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "hello bob" {
		t.Fatalf("unexpected plaintext: %q", plaintext)
	}
}

func TestDecryptRejectsMismatchedSenderKeys(t *testing.T) {
	alice, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new alice identity: %v", err)
	}
	bob, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	eve, err := identity.New("eve")
	if err != nil {
		t.Fatalf("new eve identity: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bob.InviteBundle())
	if err != nil {
		t.Fatalf("bob invite to contact: %v", err)
	}
	eveContact, err := identity.ContactFromInvite(eve.InviteBundle())
	if err != nil {
		t.Fatalf("eve invite to contact: %v", err)
	}

	envelopes, err := Encrypt(alice, bobContact, "hello bob")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := Decrypt(bob, eveContact, envelopes[0]); err == nil {
		t.Fatalf("expected decrypt to fail for mismatched sender keys")
	}
}

func TestEncryptFansOutToAllActiveRecipientDevices(t *testing.T) {
	alice, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new alice identity: %v", err)
	}
	bob, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	pending, err := identity.NewPendingEnrollment("bob", "bob-phone")
	if err != nil {
		t.Fatalf("new pending enrollment: %v", err)
	}
	approval, err := bob.Approve(pending.Request())
	if err != nil {
		t.Fatalf("approve bob device: %v", err)
	}
	bobPhone, err := pending.Complete(*approval)
	if err != nil {
		t.Fatalf("complete bob phone enrollment: %v", err)
	}
	bobContact, err := identity.ContactFromInvite(bobPhone.InviteBundle())
	if err != nil {
		t.Fatalf("contact from multi-device invite: %v", err)
	}

	envelopes, err := Encrypt(alice, bobContact, "hello both devices")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(envelopes) != 2 {
		t.Fatalf("expected two envelopes for two active devices, got %d", len(envelopes))
	}
}
