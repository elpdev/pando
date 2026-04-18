package identity

import "testing"

func TestVerifyInviteRejectsTampering(t *testing.T) {
	id, err := New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	bundle := id.InviteBundle()
	bundle.AccountID = "mallory"

	if err := VerifyInvite(bundle); err == nil {
		t.Fatalf("expected tampered invite verification to fail")
	}
}

func TestApproveAndCompleteEnrollment(t *testing.T) {
	trusted, err := New("alice")
	if err != nil {
		t.Fatalf("new trusted identity: %v", err)
	}
	pending, err := NewPendingEnrollment("alice", "alice-phone")
	if err != nil {
		t.Fatalf("new pending enrollment: %v", err)
	}
	approval, err := trusted.Approve(pending.Request())
	if err != nil {
		t.Fatalf("approve enrollment: %v", err)
	}
	completed, err := pending.Complete(*approval)
	if err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}
	if completed.AccountID != "alice" {
		t.Fatalf("unexpected account id: %s", completed.AccountID)
	}
	currentMailbox, err := completed.CurrentMailbox()
	if err != nil {
		t.Fatalf("current mailbox: %v", err)
	}
	if currentMailbox != "alice-phone" {
		t.Fatalf("expected enrolled device mailbox alice-phone, got %s", currentMailbox)
	}
	if len(completed.Devices) != 2 {
		t.Fatalf("expected two devices after enrollment, got %d", len(completed.Devices))
	}
}
