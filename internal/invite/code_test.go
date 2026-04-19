package invite

import (
	"testing"

	"github.com/elpdev/pando/internal/identity"
)

func TestCodeRoundTrip(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := EncodeCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := DecodeCode(code)
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

func TestExtractCodeFromVerboseOutput(t *testing.T) {
	text := "account: leo\nfingerprint: abcdef0123456789\ninvite-code: raw-invite-code\n"
	if got := ExtractCode(text); got != "raw-invite-code" {
		t.Fatalf("expected invite code extraction, got %q", got)
	}
}

func TestDecodeTextAcceptsVerboseInviteOutput(t *testing.T) {
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	code, err := EncodeCode(id.InviteBundle())
	if err != nil {
		t.Fatalf("encode invite code: %v", err)
	}
	bundle, err := DecodeText("account: alice\nfingerprint: " + id.Fingerprint() + "\ninvite-code: " + code + "\n")
	if err != nil {
		t.Fatalf("decode verbose invite text: %v", err)
	}
	if bundle.AccountID != "alice" {
		t.Fatalf("expected alice bundle, got %q", bundle.AccountID)
	}
}
