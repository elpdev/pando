package ctlcmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/elpdev/chatui/internal/config"
	"github.com/elpdev/chatui/internal/identity"
	"github.com/elpdev/chatui/internal/store"
)

func Execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: chatuictl <init|show-identity|export-invite|import-contact|list-contacts|show-contact|verify-contact> [flags]")
	}

	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "show-identity":
		return runShowIdentity(args[1:])
	case "export-invite":
		return runExportInvite(args[1:])
	case "import-contact":
		return runImportContact(args[1:])
	case "list-contacts":
		return runListContacts(args[1:])
	case "show-contact":
		return runShowContact(args[1:])
	case "verify-contact":
		return runVerifyContact(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runInit(args []string) error {
	mailbox, dataDir, err := parseClientFlags("init", args)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(dataDir)
	id, created, err := clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("initialized identity for %s\n", id.Mailbox)
	} else {
		fmt.Printf("identity already exists for %s\n", id.Mailbox)
	}
	fmt.Printf("fingerprint: %s\n", id.Fingerprint())
	return nil
}

func runShowIdentity(args []string) error {
	mailbox, dataDir, err := parseClientFlags("show-identity", args)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(dataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, struct {
		Mailbox     string `json:"mailbox"`
		Fingerprint string `json:"fingerprint"`
		DataDir     string `json:"data_dir"`
	}{Mailbox: id.Mailbox, Fingerprint: id.Fingerprint(), DataDir: dataDir})
}

func runExportInvite(args []string) error {
	fs := flag.NewFlagSet("export-invite", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	outputPath := fs.String("out", "", "invite output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(*mailbox)
	if err != nil {
		return err
	}
	bundle := id.InviteBundle()
	bytes, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	if *outputPath == "" {
		_, err = os.Stdout.Write(bytes)
		if err == nil {
			_, err = os.Stdout.Write([]byte("\n"))
		}
		return err
	}
	return os.WriteFile(*outputPath, bytes, 0o600)
}

func runImportContact(args []string) error {
	fs := flag.NewFlagSet("import-contact", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	invitePath := fs.String("invite", "", "path to invite bundle JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return err
	}
	if *invitePath == "" {
		return fmt.Errorf("-invite is required")
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	_, _, err = clientStore.LoadOrCreateIdentity(*mailbox)
	if err != nil {
		return err
	}
	bytes, err := os.ReadFile(*invitePath)
	if err != nil {
		return err
	}
	var bundle identity.InviteBundle
	if err := json.Unmarshal(bytes, &bundle); err != nil {
		return fmt.Errorf("decode invite: %w", err)
	}
	contact, err := identity.ContactFromInvite(bundle)
	if err != nil {
		return err
	}
	if err := clientStore.SaveContact(contact); err != nil {
		return err
	}
	fmt.Printf("imported contact %s\n", contact.Mailbox)
	return nil
}

func runListContacts(args []string) error {
	mailbox, dataDir, err := parseClientFlags("list-contacts", args)
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
	}, 0, len(contacts))
	for _, contact := range contacts {
		output = append(output, struct {
			Mailbox     string `json:"mailbox"`
			Fingerprint string `json:"fingerprint"`
			Verified    bool   `json:"verified"`
		}{Mailbox: contact.Mailbox, Fingerprint: contact.Fingerprint(), Verified: contact.Verified})
	}
	return writeJSON(os.Stdout, output)
}

func runShowContact(args []string) error {
	fs := flag.NewFlagSet("show-contact", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	contactMailbox := fs.String("contact", "", "contact mailbox identifier")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
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
		Mailbox     string `json:"mailbox"`
		Fingerprint string `json:"fingerprint"`
		Verified    bool   `json:"verified"`
	}{Mailbox: contact.Mailbox, Fingerprint: contact.Fingerprint(), Verified: contact.Verified})
}

func runVerifyContact(args []string) error {
	fs := flag.NewFlagSet("verify-contact", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	contactMailbox := fs.String("contact", "", "contact mailbox identifier")
	expectedFingerprint := fs.String("fingerprint", "", "expected contact fingerprint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
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
	fmt.Printf("verified contact %s (%s)\n", contact.Mailbox, contact.Fingerprint())
	return nil
}

func parseClientFlags(name string, args []string) (string, string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return "", "", err
	}
	return *mailbox, resolvedDataDir, nil
}

func resolveDataDir(mailbox, dataDir string) (string, error) {
	if mailbox == "" {
		return "", fmt.Errorf("-mailbox is required")
	}
	if dataDir == "" {
		return config.ClientDataDir(mailbox), nil
	}
	return dataDir, nil
}

func writeJSON(file *os.File, value any) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
