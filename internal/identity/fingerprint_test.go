package identity

import "testing"

func TestContactFingerprintMatchesInviteSource(t *testing.T) {
	id, err := New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	contact, err := ContactFromInvite(id.InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	if contact.Fingerprint() != id.Fingerprint() {
		t.Fatalf("expected contact fingerprint %s to match identity fingerprint %s", contact.Fingerprint(), id.Fingerprint())
	}
}
