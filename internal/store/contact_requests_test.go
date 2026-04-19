package store

import (
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

func TestContactRequestRoundTrip(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	request := &ContactRequest{
		AccountID: "bob",
		Direction: ContactRequestDirectionIncoming,
		Status:    ContactRequestStatusPending,
		Note:      "hello",
		Bundle:    id.InviteBundle(),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := clientStore.SaveContactRequest(request); err != nil {
		t.Fatalf("save contact request: %v", err)
	}
	loaded, err := clientStore.LoadContactRequest("bob")
	if err != nil {
		t.Fatalf("load contact request: %v", err)
	}
	if loaded.AccountID != "bob" || loaded.Note != "hello" {
		t.Fatalf("unexpected loaded contact request: %+v", loaded)
	}
	requests, err := clientStore.ListContactRequests()
	if err != nil {
		t.Fatalf("list contact requests: %v", err)
	}
	if len(requests) != 1 || requests[0].AccountID != "bob" {
		t.Fatalf("unexpected contact requests: %+v", requests)
	}
	if err := clientStore.DeleteContactRequest("bob"); err != nil {
		t.Fatalf("delete contact request: %v", err)
	}
	if _, err := clientStore.LoadContactRequest("bob"); err != ErrNotFound {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}
