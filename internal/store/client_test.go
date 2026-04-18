package store

import (
	"testing"

	"github.com/elpdev/pando/internal/identity"
)

func TestMarkContactVerified(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	contactID, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	contact, err := identity.ContactFromInvite(contactID.InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	if err := clientStore.SaveContact(contact); err != nil {
		t.Fatalf("save contact: %v", err)
	}

	verified, err := clientStore.MarkContactVerified("bob", true)
	if err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	if !verified.Verified {
		t.Fatalf("expected contact to be verified")
	}
}
