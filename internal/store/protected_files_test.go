package store

import (
	"errors"
	"os"
	"testing"

	"github.com/elpdev/pando/internal/identity"
)

func TestUsePassphraseMigratesProtectedFiles(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	bobID, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	contact, err := identity.ContactFromInvite(bobID.InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	if err := clientStore.SaveContact(contact); err != nil {
		t.Fatalf("save contact: %v", err)
	}
	pending, err := identity.NewPendingEnrollment("alice", "alice-laptop")
	if err != nil {
		t.Fatalf("new pending enrollment: %v", err)
	}
	if err := clientStore.SavePendingEnrollment(pending); err != nil {
		t.Fatalf("save pending enrollment: %v", err)
	}
	state, err := clientStore.ProtectionState()
	if err != nil {
		t.Fatalf("protection state: %v", err)
	}
	if state != ProtectionStatePlaintext {
		t.Fatalf("expected plaintext state, got %q", state)
	}
	if err := clientStore.UsePassphrase([]byte("secret-passphrase")); err != nil {
		t.Fatalf("use passphrase: %v", err)
	}
	state, err = clientStore.ProtectionState()
	if err != nil {
		t.Fatalf("protection state after migration: %v", err)
	}
	if state != ProtectionStateEncrypted {
		t.Fatalf("expected encrypted state, got %q", state)
	}
	for _, path := range []string{clientStore.identityPath(), clientStore.contactsPath(), clientStore.pendingEnrollmentPath()} {
		bytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read protected file %s: %v", path, err)
		}
		if !isProtectedEnvelope(bytes) {
			t.Fatalf("expected encrypted envelope for %s", path)
		}
	}
	reopened := NewClientStore(clientStore.dir)
	if _, err := reopened.LoadIdentity(); !errors.Is(err, ErrPassphraseRequired) {
		t.Fatalf("expected passphrase required before unlock, got %v", err)
	}
	if err := reopened.UsePassphrase([]byte("secret-passphrase")); err != nil {
		t.Fatalf("unlock reopened store: %v", err)
	}
	loadedIdentity, err := reopened.LoadIdentity()
	if err != nil {
		t.Fatalf("load identity after unlock: %v", err)
	}
	if loadedIdentity.AccountID != "alice" {
		t.Fatalf("unexpected identity account id: %q", loadedIdentity.AccountID)
	}
	loadedContact, err := reopened.LoadContact("bob")
	if err != nil {
		t.Fatalf("load contact after unlock: %v", err)
	}
	if loadedContact.AccountID != "bob" {
		t.Fatalf("unexpected contact account id: %q", loadedContact.AccountID)
	}
	loadedPending, err := reopened.LoadPendingEnrollment()
	if err != nil {
		t.Fatalf("load pending enrollment after unlock: %v", err)
	}
	if loadedPending.AccountID != "alice" {
		t.Fatalf("unexpected pending account id: %q", loadedPending.AccountID)
	}
}

func TestUsePassphraseRejectsWrongPassphrase(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	if err := clientStore.UsePassphrase([]byte("secret-passphrase")); err != nil {
		t.Fatalf("use passphrase: %v", err)
	}
	reopened := NewClientStore(clientStore.dir)
	err = reopened.UsePassphrase([]byte("wrong-passphrase"))
	if !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected invalid passphrase error, got %v", err)
	}
}

func TestUsePassphraseMigratesMixedState(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	if err := clientStore.UsePassphrase([]byte("secret-passphrase")); err != nil {
		t.Fatalf("use passphrase: %v", err)
	}
	bobID, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	contact, err := identity.ContactFromInvite(bobID.InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	plainStore := NewClientStore(clientStore.dir)
	if err := plainStore.writeJSON(clientStore.contactsPath(), map[string]identity.Contact{"bob": *contact}, 0o600); err != nil {
		t.Fatalf("write plaintext contacts: %v", err)
	}
	state, err := plainStore.ProtectionState()
	if err != nil {
		t.Fatalf("mixed protection state: %v", err)
	}
	if state != ProtectionStateMixed {
		t.Fatalf("expected mixed state, got %q", state)
	}
	if err := plainStore.UsePassphrase([]byte("secret-passphrase")); err != nil {
		t.Fatalf("reuse passphrase for migration: %v", err)
	}
	bytes, err := os.ReadFile(clientStore.contactsPath())
	if err != nil {
		t.Fatalf("read contacts after migration: %v", err)
	}
	if !isProtectedEnvelope(bytes) {
		t.Fatal("expected contacts to be re-encrypted after mixed-state migration")
	}
	loadedContact, err := plainStore.LoadContact("bob")
	if err != nil {
		t.Fatalf("load migrated contact: %v", err)
	}
	if loadedContact.AccountID != "bob" {
		t.Fatalf("unexpected migrated contact account id: %q", loadedContact.AccountID)
	}
}

func TestChangePassphraseReencryptsProtectedFiles(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		t.Fatalf("save identity: %v", err)
	}
	bobID, err := identity.New("bob")
	if err != nil {
		t.Fatalf("new bob identity: %v", err)
	}
	contact, err := identity.ContactFromInvite(bobID.InviteBundle())
	if err != nil {
		t.Fatalf("contact from invite: %v", err)
	}
	if err := clientStore.SaveContact(contact); err != nil {
		t.Fatalf("save contact: %v", err)
	}
	if err := clientStore.UsePassphrase([]byte("old-passphrase")); err != nil {
		t.Fatalf("use old passphrase: %v", err)
	}
	before, err := os.ReadFile(clientStore.identityPath())
	if err != nil {
		t.Fatalf("read identity before change: %v", err)
	}
	if err := clientStore.ChangePassphrase([]byte("new-passphrase")); err != nil {
		t.Fatalf("change passphrase: %v", err)
	}
	after, err := os.ReadFile(clientStore.identityPath())
	if err != nil {
		t.Fatalf("read identity after change: %v", err)
	}
	if string(before) == string(after) {
		t.Fatal("expected encrypted identity payload to change after passphrase rotation")
	}
	reopened := NewClientStore(clientStore.dir)
	if err := reopened.UsePassphrase([]byte("old-passphrase")); !errors.Is(err, ErrInvalidPassphrase) {
		t.Fatalf("expected old passphrase to fail, got %v", err)
	}
	if err := reopened.UsePassphrase([]byte("new-passphrase")); err != nil {
		t.Fatalf("unlock with new passphrase: %v", err)
	}
	loadedContact, err := reopened.LoadContact("bob")
	if err != nil {
		t.Fatalf("load contact after passphrase change: %v", err)
	}
	if loadedContact.AccountID != "bob" {
		t.Fatalf("unexpected contact after passphrase change: %q", loadedContact.AccountID)
	}
}
