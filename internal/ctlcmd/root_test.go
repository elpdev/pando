package ctlcmd

import (
	"testing"

	"github.com/elpdev/pando/internal/identity"
)

func TestInviteCodeRoundTrip(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := encodeInviteCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := decodeInviteCode(code)
	if err != nil {
		t.Fatalf("decode invite code: %v", err)
	}
	if bundle.AccountID != id.AccountID {
		t.Fatalf("expected account id %s, got %s", id.AccountID, bundle.AccountID)
	}
	if len(bundle.Devices) != len(id.InviteBundle().Devices) {
		t.Fatalf("expected %d devices, got %d", len(id.InviteBundle().Devices), len(bundle.Devices))
	}
}
