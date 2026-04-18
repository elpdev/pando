package identity

import "testing"

func TestVerifyInviteRejectsTampering(t *testing.T) {
	id, err := New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	bundle := id.InviteBundle()
	bundle.Mailbox = "mallory"

	if err := VerifyInvite(bundle); err == nil {
		t.Fatalf("expected tampered invite verification to fail")
	}
}
