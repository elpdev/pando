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

	envelope, err := Encrypt(alice, bobContact, "hello bob")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	plaintext, err := Decrypt(bob, aliceContact, envelope)
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

	envelope, err := Encrypt(alice, bobContact, "hello bob")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := Decrypt(bob, eveContact, envelope); err == nil {
		t.Fatalf("expected decrypt to fail for mismatched sender keys")
	}
}
