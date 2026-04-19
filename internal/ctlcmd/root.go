package ctlcmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/invite"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/store"
	"github.com/makiuchi-d/gozxing"
	gozxingqr "github.com/makiuchi-d/gozxing/qrcode"
	qrterminal "github.com/mdp/qrterminal/v3"
	_ "image/jpeg"
	_ "image/png"
)

func Execute(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando <identity|contact|device|config|eject|help> [flags]")
	}

	switch args[0] {
	case "identity":
		return runIdentity(args[1:])
	case "contact":
		return runContact(args[1:])
	case "device":
		return runDevice(args[1:])
	case "eject":
		return runEject(args[1:])
	case "config":
		return runConfig(args[1:])
	case "help":
		return runHelp(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func IsSubcommand(arg string) bool {
	switch arg {
	case "identity", "contact", "device", "config", "eject", "help":
		return true
	default:
		return false
	}
}

func runHelp(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando <identity|contact|device|config|eject> [flags]")
	}
	switch args[0] {
	case "identity":
		return fmt.Errorf("usage: pando identity <init|show|invite-code|export-invite> [flags]")
	case "contact":
		return fmt.Errorf("usage: pando contact <add|import|list|show|verify> [flags]")
	case "device":
		return fmt.Errorf("usage: pando device <list|revoke|enroll> [flags]")
	case "config":
		return fmt.Errorf("usage: pando config <show|set> [flags]")
	case "eject":
		return fmt.Errorf("usage: pando eject --mailbox <mailbox> [flags]")
	default:
		return fmt.Errorf("unknown help topic %q", args[0])
	}
}

func runIdentity(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando identity <init|show|invite-code|export-invite> [flags]")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "show":
		return runShowIdentity(args[1:])
	case "invite-code":
		return runInviteCode(args[1:])
	case "export-invite":
		return runExportInvite(args[1:])
	case "help":
		return runHelp([]string{"identity"})
	default:
		return fmt.Errorf("unknown identity subcommand %q", args[0])
	}
}

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

func runDevice(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando device <list|revoke|enroll> [flags]")
	}
	switch args[0] {
	case "list":
		return runListDevices(args[1:])
	case "revoke":
		return runRevokeDevice(args[1:])
	case "enroll":
		return runDeviceEnroll(args[1:])
	case "help":
		return runHelp([]string{"device"})
	default:
		return fmt.Errorf("unknown device subcommand %q", args[0])
	}
}

func runDeviceEnroll(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando device enroll <create|approve|complete> [flags]")
	}
	switch args[0] {
	case "create":
		return runCreateEnrollment(args[1:])
	case "approve":
		return runApproveEnrollment(args[1:])
	case "complete":
		return runCompleteEnrollment(args[1:])
	case "help":
		return fmt.Errorf("usage: pando device enroll <create|approve|complete> [flags]")
	default:
		return fmt.Errorf("unknown device enroll subcommand %q", args[0])
	}
}

func runInit(args []string) error {
	mailbox, dataDir, err := parseClientFlags("identity init", args)
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
	mailbox, dataDir, err := parseClientFlags("identity show", args)
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
	fs := flag.NewFlagSet("identity export-invite", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	outputPath := fs.String("out", "", "invite output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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

func runInviteCode(args []string) error {
	fs := flag.NewFlagSet("identity invite-code", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	copyToClipboard := fs.Bool("copy", false, "copy the invite code to the clipboard")
	rawOutput := fs.Bool("raw", false, "print only the invite code")
	qrOutput := fs.Bool("qr", false, "render the invite code as a terminal QR code")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rawOutput && *qrOutput {
		return fmt.Errorf("use only one of -raw or -qr")
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(*mailbox)
	if err != nil {
		return err
	}
	code, err := invite.EncodeCode(id.InviteBundle())
	if err != nil {
		return err
	}
	if *copyToClipboard {
		if err := clipboard.WriteAll(code); err != nil {
			return fmt.Errorf("copy invite code: %w", err)
		}
		fmt.Printf("copied invite code for %s to clipboard\n", id.AccountID)
		fmt.Println("tell the other person to run: pando contact add --mailbox <their-mailbox> --from-clipboard")
	}
	if *rawOutput {
		fmt.Println(code)
		return nil
	}
	if *qrOutput {
		fmt.Printf("account: %s\n", id.AccountID)
		fmt.Printf("fingerprint: %s\n", id.Fingerprint())
		qrterminal.GenerateHalfBlock(code, qrterminal.L, os.Stdout)
		fmt.Println("share this QR or import a saved QR image with: pando contact add --mailbox <their-mailbox> --qr-image <path>")
		return nil
	}
	fmt.Printf("account: %s\n", id.AccountID)
	fmt.Printf("fingerprint: %s\n", id.Fingerprint())
	fmt.Printf("invite-code: %s\n", code)
	fmt.Println("share the invite-code value above, or use --raw, --copy, or --qr for easier sharing")
	fmt.Println("the other person can import it with: pando contact add --mailbox <their-mailbox> --paste")
	return nil
}

func runAddContact(args []string) error {
	return runImportContactWithName("contact add", args)
}

func runImportContact(args []string) error {
	return runImportContactWithName("contact import", args)
}

func runImportContactWithName(name string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	invitePath := fs.String("invite", "", "path to invite bundle JSON")
	inviteCode := fs.String("code", "", "shareable invite code")
	readStdin := fs.Bool("stdin", false, "read invite code or invite JSON from stdin")
	readPaste := fs.Bool("paste", false, "read a pasted invite from stdin until EOF")
	fromClipboard := fs.Bool("from-clipboard", false, "read the invite code from the clipboard")
	qrImagePath := fs.String("qr-image", "", "path to a QR image containing an invite code")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
	if err != nil {
		return err
	}
	if err := validateInviteInputFlags(*invitePath, *inviteCode, *readStdin, *readPaste, *fromClipboard, *qrImagePath); err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	service, _, err := messaging.New(clientStore, *mailbox)
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
	contact, err := service.ImportContactInviteBundle(*bundle, name == "contact add")
	if err != nil {
		return err
	}
	fmt.Printf("imported contact %s with %d active devices\n", contact.AccountID, len(contact.ActiveDevices()))
	fmt.Printf("fingerprint: %s\n", contact.Fingerprint())
	if contact.Verified {
		fmt.Printf("verified contact %s (%s)\n", contact.AccountID, contact.Fingerprint())
	} else {
		fmt.Printf("next: pando contact verify --mailbox %s --contact %s --fingerprint %s\n", *mailbox, contact.AccountID, contact.Fingerprint())
	}
	return nil
}

type inviteInputOptions struct {
	InvitePath    string
	InviteCode    string
	ReadStdin     bool
	ReadPaste     bool
	ReadClipboard bool
	QRImagePath   string
}

func validateInviteInputFlags(invitePath, inviteCode string, readStdin, readPaste, fromClipboard bool, qrImagePath string) error {
	inputs := 0
	if strings.TrimSpace(invitePath) != "" {
		inputs++
	}
	if strings.TrimSpace(inviteCode) != "" {
		inputs++
	}
	if readStdin {
		inputs++
	}
	if readPaste {
		inputs++
	}
	if fromClipboard {
		inputs++
	}
	if strings.TrimSpace(qrImagePath) != "" {
		inputs++
	}
	if inputs == 0 {
		return fmt.Errorf("provide one of -invite, -code, -stdin, -paste, -from-clipboard, or -qr-image")
	}
	if inputs > 1 {
		return fmt.Errorf("use only one of -invite, -code, -stdin, -paste, -from-clipboard, or -qr-image")
	}
	return nil
}

func runListContacts(args []string) error {
	mailbox, dataDir, err := parseClientFlags("contact list", args)
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
	fs := flag.NewFlagSet("contact show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	contactMailbox := fs.String("contact", "", "contact mailbox identifier")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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
	fs := flag.NewFlagSet("contact verify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	contactMailbox := fs.String("contact", "", "contact mailbox identifier")
	expectedFingerprint := fs.String("fingerprint", "", "expected contact fingerprint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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
	mailbox, dataDir, err := parseClientFlags("device list", args)
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
	fs := flag.NewFlagSet("device enroll create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	accountID := fs.String("account", "", "stable account identifier")
	mailbox := fs.String("mailbox", "", "new device mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	outputPath := fs.String("out", "", "enrollment request output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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
	fs := flag.NewFlagSet("device enroll approve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "trusted device mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	requestPath := fs.String("request", "", "path to enrollment request JSON")
	outputPath := fs.String("out", "", "approval output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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
	fs := flag.NewFlagSet("device enroll complete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "new device mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	approvalPath := fs.String("approval", "", "path to approval JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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
	fs := flag.NewFlagSet("device revoke", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "trusted device mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	deviceID := fs.String("device", "", "device id or mailbox to revoke")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
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

func runEject(args []string) error {
	fs := flag.NewFlagSet("eject", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mailbox := fs.String("mailbox", "", "local mailbox identifier")
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	force := fs.Bool("force", false, "skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedDataDir, err := resolveDataDir(*mailbox, *rootDir, *dataDir)
	if err != nil {
		return err
	}
	if !*force {
		fmt.Fprintf(os.Stderr, "This will permanently delete all local Pando data for mailbox %q at %s.\n", *mailbox, resolvedDataDir)
		fmt.Fprintf(os.Stderr, "Type the mailbox name to confirm: ")
		reader := bufio.NewReader(os.Stdin)
		input, readErr := reader.ReadString('\n')
		if readErr != nil {
			return fmt.Errorf("read confirmation: %w", readErr)
		}
		if strings.TrimSpace(input) != *mailbox {
			return fmt.Errorf("aborted")
		}
	}
	if err := os.RemoveAll(resolvedDataDir); err != nil {
		return fmt.Errorf("eject %s: %w", resolvedDataDir, err)
	}
	fmt.Printf("ejected local Pando data for %s\n", *mailbox)
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
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	dataDir := fs.String("data-dir", "", "client state directory")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return "", "", err
	}
	resolvedMailbox := *mailbox
	if resolvedMailbox == "" {
		resolvedMailbox = devCfg.DefaultMailbox
	}
	resolvedDataDir, err := resolveDataDir(resolvedMailbox, *rootDir, *dataDir)
	if err != nil {
		return "", "", err
	}
	return resolvedMailbox, resolvedDataDir, nil
}

func resolveDataDir(mailbox, rootDir, dataDir string) (string, error) {
	if mailbox == "" {
		return "", fmt.Errorf("-mailbox is required")
	}
	if dataDir == "" {
		return config.ClientDataDir(rootDir, mailbox), nil
	}
	return dataDir, nil
}

func runConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando config <show|set> [flags]")
	}
	switch args[0] {
	case "show":
		return runConfigShow(args[1:])
	case "set":
		return runConfigSet(args[1:])
	case "help":
		return runHelp([]string{"config"})
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runConfigSet(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando config set <relay|relay-token|mailbox> <value>")
	}
	switch args[0] {
	case "relay":
		return runConfigSetRelay(args[1:])
	case "relay-token":
		return runConfigSetRelayToken(args[1:])
	case "mailbox":
		return runConfigSetMailbox(args[1:])
	case "help":
		return fmt.Errorf("usage: pando config set <relay|relay-token|mailbox> <value>")
	default:
		return fmt.Errorf("unknown config set subcommand %q", args[0])
	}
}

func runConfigShow(args []string) error {
	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	fmt.Printf("config file: %s\n", config.DeviceConfigPath(*rootDir))
	fmt.Printf("relay_url: %s\n", devCfg.RelayURL)
	fmt.Printf("relay_token: %s\n", devCfg.RelayToken)
	fmt.Printf("default_mailbox: %s\n", devCfg.DefaultMailbox)
	return nil
}

func runConfigSetRelay(args []string) error {
	fs := flag.NewFlagSet("config set relay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config set relay <url>")
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	devCfg.RelayURL = fs.Arg(0)
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("relay_url set to %s\n", devCfg.RelayURL)
	return nil
}

func runConfigSetRelayToken(args []string) error {
	fs := flag.NewFlagSet("config set relay-token", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config set relay-token <token>")
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	devCfg.RelayToken = fs.Arg(0)
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("relay_token set to %s\n", devCfg.RelayToken)
	return nil
}

func runConfigSetMailbox(args []string) error {
	fs := flag.NewFlagSet("config set mailbox", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config set mailbox <mailbox>")
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	devCfg.DefaultMailbox = fs.Arg(0)
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("default_mailbox set to %s\n", devCfg.DefaultMailbox)
	return nil
}

func writeJSON(file *os.File, value any) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func encodeInviteCode(bundle identity.InviteBundle) (string, error) {
	return invite.EncodeCode(bundle)
}

func decodeInviteCode(code string) (*identity.InviteBundle, error) {
	return invite.DecodeCode(code)
}

func decodeInviteText(text string) (*identity.InviteBundle, error) {
	return invite.DecodeText(text)
}

func extractInviteCode(text string) string {
	return invite.ExtractCode(text)
}

func readInviteBundle(input inviteInputOptions) (*identity.InviteBundle, error) {
	switch {
	case strings.TrimSpace(input.InviteCode) != "":
		return invite.DecodeText(input.InviteCode)
	case input.ReadClipboard:
		text, err := clipboard.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("read invite from clipboard: %w", err)
		}
		return invite.DecodeText(text)
	case strings.TrimSpace(input.QRImagePath) != "":
		return readInviteBundleFromQRImage(input.QRImagePath)
	case input.ReadStdin || input.ReadPaste || input.InvitePath == "-":
		if input.ReadPaste {
			fmt.Fprintln(os.Stderr, "paste the invite, then press Ctrl-D when finished:")
		}
		bytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read invite from stdin: %w", err)
		}
		return invite.DecodeText(string(bytes))
	case strings.TrimSpace(input.InvitePath) != "":
		bytes, err := os.ReadFile(input.InvitePath)
		if err != nil {
			return nil, err
		}
		return invite.DecodeText(string(bytes))
	default:
		return nil, fmt.Errorf("provide one of -invite, -code, -stdin, -paste, -from-clipboard, or -qr-image")
	}
}

func readInviteBundleFromQRImage(path string) (*identity.InviteBundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open QR image: %w", err)
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode QR image: %w", err)
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("read QR image: %w", err)
	}
	result, err := gozxingqr.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		return nil, fmt.Errorf("read QR image: %w", err)
	}
	return invite.DecodeText(result.GetText())
}
