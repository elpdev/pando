package ctlcmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/store"
)

func Execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pandoctl <init|show-identity|export-invite|import-contact|list-contacts|show-contact|verify-contact|list-devices|create-enrollment|approve-enrollment|complete-enrollment|revoke-device> [flags]")
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
	case "list-devices":
		return runListDevices(args[1:])
	case "create-enrollment":
		return runCreateEnrollment(args[1:])
	case "approve-enrollment":
		return runApproveEnrollment(args[1:])
	case "complete-enrollment":
		return runCompleteEnrollment(args[1:])
	case "revoke-device":
		return runRevokeDevice(args[1:])
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
		fmt.Printf("initialized identity for %s on device %s\n", id.AccountID, mustCurrentMailbox(id))
	} else {
		fmt.Printf("identity already exists for %s on device %s\n", id.AccountID, mustCurrentMailbox(id))
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
		AccountID      string                  `json:"account_id"`
		Fingerprint    string                  `json:"fingerprint"`
		CurrentMailbox string                  `json:"current_mailbox"`
		Devices        []identity.DeviceBundle `json:"devices"`
		DataDir        string                  `json:"data_dir"`
	}{AccountID: id.AccountID, Fingerprint: id.Fingerprint(), CurrentMailbox: mustCurrentMailbox(id), Devices: id.DeviceBundles(), DataDir: dataDir})
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
	if existing, loadErr := clientStore.LoadContact(contact.AccountID); loadErr == nil && existing.Fingerprint() == contact.Fingerprint() {
		contact.Verified = existing.Verified
	}
	if err := clientStore.SaveContact(contact); err != nil {
		return err
	}
	fmt.Printf("imported contact %s with %d active devices\n", contact.AccountID, len(contact.ActiveDevices()))
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
		Mailbox     string                   `json:"mailbox"`
		Fingerprint string                   `json:"fingerprint"`
		Verified    bool                     `json:"verified"`
		Devices     []identity.ContactDevice `json:"devices"`
	}{Mailbox: contact.AccountID, Fingerprint: contact.Fingerprint(), Verified: contact.Verified, Devices: contact.Devices})
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
	fmt.Printf("verified contact %s (%s)\n", contact.AccountID, contact.Fingerprint())
	return nil
}

func runListDevices(args []string) error {
	mailbox, dataDir, err := parseClientFlags("list-devices", args)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(dataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, id.DeviceBundles())
}

func runCreateEnrollment(args []string) error {
	fs := flag.NewFlagSet("create-enrollment", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	accountID := fs.String("account", "", "stable account identifier")
	mailbox := fs.String("mailbox", "", "new device mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	outputPath := fs.String("out", "", "enrollment request output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return err
	}
	if *accountID == "" {
		return fmt.Errorf("-account is required")
	}
	pending, err := identity.NewPendingEnrollment(*accountID, *mailbox)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	if err := clientStore.SavePendingEnrollment(pending); err != nil {
		return err
	}
	request := pending.Request()
	bytes, err := json.MarshalIndent(request, "", "  ")
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

func runApproveEnrollment(args []string) error {
	fs := flag.NewFlagSet("approve-enrollment", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "trusted device mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	requestPath := fs.String("request", "", "path to enrollment request JSON")
	outputPath := fs.String("out", "", "approval output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return err
	}
	if *requestPath == "" {
		return fmt.Errorf("-request is required")
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(*mailbox)
	if err != nil {
		return err
	}
	bytes, err := os.ReadFile(*requestPath)
	if err != nil {
		return err
	}
	var request identity.EnrollmentRequest
	if err := json.Unmarshal(bytes, &request); err != nil {
		return fmt.Errorf("decode enrollment request: %w", err)
	}
	approval, err := id.Approve(request)
	if err != nil {
		return err
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		return err
	}
	approvalBytes, err := json.MarshalIndent(approval, "", "  ")
	if err != nil {
		return err
	}
	if *outputPath == "" {
		_, err = os.Stdout.Write(approvalBytes)
		if err == nil {
			_, err = os.Stdout.Write([]byte("\n"))
		}
		return err
	}
	return os.WriteFile(*outputPath, approvalBytes, 0o600)
}

func runCompleteEnrollment(args []string) error {
	fs := flag.NewFlagSet("complete-enrollment", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "new device mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	approvalPath := fs.String("approval", "", "path to approval JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return err
	}
	if *approvalPath == "" {
		return fmt.Errorf("-approval is required")
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	pending, err := clientStore.LoadPendingEnrollment()
	if err != nil {
		return err
	}
	if pending.Device.Mailbox != *mailbox {
		return fmt.Errorf("pending enrollment is for device mailbox %q, not %q", pending.Device.Mailbox, *mailbox)
	}
	bytes, err := os.ReadFile(*approvalPath)
	if err != nil {
		return err
	}
	var approval identity.EnrollmentApproval
	if err := json.Unmarshal(bytes, &approval); err != nil {
		return fmt.Errorf("decode enrollment approval: %w", err)
	}
	id, err := pending.Complete(approval)
	if err != nil {
		return err
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		return err
	}
	if err := clientStore.ClearPendingEnrollment(); err != nil {
		return err
	}
	fmt.Printf("completed enrollment for %s on device %s\n", id.AccountID, *mailbox)
	return nil
}

func runRevokeDevice(args []string) error {
	fs := flag.NewFlagSet("revoke-device", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "trusted device mailbox identifier")
	dataDir := fs.String("data-dir", "", "client state directory")
	deviceID := fs.String("device", "", "device id or mailbox to revoke")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *dataDir)
	if err != nil {
		return err
	}
	if *deviceID == "" {
		return fmt.Errorf("-device is required")
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(*mailbox)
	if err != nil {
		return err
	}
	if err := id.RevokeDevice(*deviceID); err != nil {
		return err
	}
	if err := clientStore.SaveIdentity(id); err != nil {
		return err
	}
	fmt.Printf("revoked device %s\n", *deviceID)
	return nil
}

func mustCurrentMailbox(id *identity.Identity) string {
	mailbox, err := id.CurrentMailbox()
	if err != nil {
		return ""
	}
	return mailbox
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
