package store

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestSaveAttachmentRejectsTraversalComponents(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	if _, err := clientStore.SaveAttachment(id, "../bob", "file-1", "photo.png", []byte("hello")); err == nil {
		t.Fatal("expected traversal mailbox to be rejected")
	}
	if _, err := clientStore.SaveAttachment(id, "bob", "../file-1", "photo.png", []byte("hello")); err == nil {
		t.Fatal("expected traversal attachment id to be rejected")
	}
}

func TestSaveAttachmentReplacesSpacesInFilename(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	path, err := clientStore.SaveAttachment(id, "bob", "file-1", "my photo clip.m4a", []byte("hello"))
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}
	if got, want := filepath.Base(path), "file-1-my_photo_clip.m4a"; got != want {
		t.Fatalf("unexpected saved filename: got %q want %q", got, want)
	}
}

func TestSaveAttachmentEncryptsBytesAndReadAttachmentDecrypts(t *testing.T) {
	clientStore := NewClientStore(t.TempDir())
	id, err := identity.New("alice")
	if err != nil {
		t.Fatalf("new identity: %v", err)
	}
	original := []byte("hello attachment")
	path, err := clientStore.SaveAttachment(id, "bob", "file-1", "photo.png", original)
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read attachment: %v", err)
	}
	if bytes.Equal(onDisk, original) {
		t.Fatal("expected attachment bytes on disk to be encrypted")
	}
	plaintext, err := clientStore.ReadAttachment(id, path)
	if err != nil {
		t.Fatalf("read attachment plaintext: %v", err)
	}
	if !bytes.Equal(plaintext, original) {
		t.Fatal("expected decrypted attachment bytes to match original")
	}
}
