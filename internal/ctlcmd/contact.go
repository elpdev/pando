package ctlcmd

import (
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/ui/style"
)

func runContact(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando contact <add|import|list|show|verify> [flags]")
	}
	switch args[0] {
	case "add":
		return runAddContact(args[1:])
	case "import":
		return runImportContact(args[1:])
	case "list":
		return runListContacts(args[1:])
	case "show":
		return runShowContact(args[1:])
	case "verify":
		return runVerifyContact(args[1:])
	case "help":
		return runHelp([]string{"contact"})
	default:
		return fmt.Errorf("unknown contact subcommand %q", args[0])
	}
}

func runAddContact(args []string) error {
	return runImportContactWithName("contact add", args)
}

func runImportContact(args []string) error {
	return runImportContactWithName("contact import", args)
}

func runImportContactWithName(name string, args []string) error {
	bfs := NewBaseFlagSet(name)
	invitePath := bfs.FS.String("invite", "", "path to invite bundle JSON")
	inviteCode := bfs.FS.String("code", "", "shareable invite code")
	readStdin := bfs.FS.Bool("stdin", false, "read invite code or invite JSON from stdin")
	readPaste := bfs.FS.Bool("paste", false, "read a pasted invite from stdin until EOF")
	fromClipboard := bfs.FS.Bool("from-clipboard", false, "read the invite code from the clipboard")
	qrImagePath := bfs.FS.String("qr-image", "", "path to a QR image containing an invite code")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if err := validateInviteInputFlags(*invitePath, *inviteCode, *readStdin, *readPaste, *fromClipboard, *qrImagePath); err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	_, _, err = clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	bundle, err := readInviteBundle(inviteInputOptions{
		InvitePath:    *invitePath,
		InviteCode:    *inviteCode,
		ReadStdin:     *readStdin,
		ReadPaste:     *readPaste,
		ReadClipboard: *fromClipboard,
		QRImagePath:   *qrImagePath,
	})
	if err != nil {
		return err
	}
	contact, err := identity.ContactFromInvite(*bundle)
	if err != nil {
		return err
	}
	if existing, loadErr := clientStore.LoadContact(contact.AccountID); loadErr == nil && existing.Fingerprint() == contact.Fingerprint() {
		contact.Verified = existing.Verified
	}
	if name == "contact add" {
		contact.Verified = true
	}
	if err := clientStore.SaveContact(contact); err != nil {
		return err
	}
	fmt.Printf("imported contact %s with %d active devices\n", contact.AccountID, len(contact.ActiveDevices()))
	fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(contact.Fingerprint()))
	if contact.Verified {
		fmt.Printf("verified contact %s (%s)\n", contact.AccountID, style.FormatFingerprint(contact.Fingerprint()))
	} else {
		fmt.Printf("next: pando contact verify --mailbox %s --contact %s --fingerprint %s\n", mailbox, contact.AccountID, contact.Fingerprint())
	}
	return nil
}

func runListContacts(args []string) error {
	bfs := NewBaseFlagSet("contact list")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, dataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(dataDir)
	_, _, err = clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	contacts, err := clientStore.ListContacts()
	if err != nil {
		return err
	}
	output := make([]struct {
		Mailbox     string `json:"mailbox"`
		Fingerprint string `json:"fingerprint"`
		Verified    bool   `json:"verified"`
		Devices     int    `json:"devices"`
	}, 0, len(contacts))
	for _, contact := range contacts {
		output = append(output, struct {
			Mailbox     string `json:"mailbox"`
			Fingerprint string `json:"fingerprint"`
			Verified    bool   `json:"verified"`
			Devices     int    `json:"devices"`
		}{Mailbox: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified, Devices: len(contact.ActiveDevices())})
	}
	return writeJSON(os.Stdout, output)
}

func runShowContact(args []string) error {
	bfs := NewBaseFlagSet("contact show")
	contactMailbox := bfs.FS.String("contact", "", "contact mailbox identifier")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	_, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if *contactMailbox == "" {
		return fmt.Errorf("-contact is required")
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	contact, err := clientStore.LoadContact(*contactMailbox)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, struct {
		Mailbox     string                   `json:"mailbox"`
		Fingerprint string                   `json:"fingerprint"`
		Verified    bool                     `json:"verified"`
		Devices     []identity.ContactDevice `json:"devices"`
	}{Mailbox: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified, Devices: contact.Devices})
}

func runVerifyContact(args []string) error {
	bfs := NewBaseFlagSet("contact verify")
	contactMailbox := bfs.FS.String("contact", "", "contact mailbox identifier")
	expectedFingerprint := bfs.FS.String("fingerprint", "", "expected contact fingerprint")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	_, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if *contactMailbox == "" {
		return fmt.Errorf("-contact is required")
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	contact, err := clientStore.LoadContact(*contactMailbox)
	if err != nil {
		return err
	}
	if *expectedFingerprint != "" && contact.Fingerprint() != *expectedFingerprint {
		return fmt.Errorf("contact fingerprint mismatch: expected %s, got %s", *expectedFingerprint, contact.Fingerprint())
	}
	contact, err = clientStore.MarkContactVerified(*contactMailbox, true)
	if err != nil {
		return err
	}
	fmt.Printf("verified contact %s (%s)\n", contact.AccountID, style.FormatFingerprint(contact.Fingerprint()))
	return nil
}
