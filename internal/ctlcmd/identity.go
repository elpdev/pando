package ctlcmd

import (
	"fmt"
	"os"

	"github.com/atotto/clipboard"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/invite"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/ui/style"
	qrterminal "github.com/mdp/qrterminal/v3"
)

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

func runInit(args []string) error {
	bfs := NewBaseFlagSet("identity init")
	publishDirectory := bfs.FS.Bool("publish-directory", false, "publish the signed relay directory entry after initialization")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, dataDir, err := bfs.Resolve()
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
	fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(id.Fingerprint()))
	if *publishDirectory {
		resolvedRelayURL, resolvedRelayToken, err := resolveRelayConfig(*bfs.RootDir, *relayURL, *relayToken)
		if err != nil {
			return err
		}
		if err := publishIdentityDirectoryEntry(id, resolvedRelayURL, resolvedRelayToken, false); err != nil {
			return fmt.Errorf("publish relay directory entry: %w", err)
		}
		fmt.Printf("published trusted relay directory entry for %s\n", id.AccountID)
	}
	return nil
}

func runShowIdentity(args []string) error {
	bfs := NewBaseFlagSet("identity show")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, dataDir, err := bfs.Resolve()
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
	bfs := NewBaseFlagSet("identity export-invite")
	outputPath := bfs.FS.String("out", "", "invite output file")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	bundle := id.InviteBundle()
	return writeJSONOutput(*outputPath, bundle)
}

func runInviteCode(args []string) error {
	bfs := NewBaseFlagSet("identity invite-code")
	copyToClipboard := bfs.FS.Bool("copy", false, "copy the invite code to the clipboard")
	rawOutput := bfs.FS.Bool("raw", false, "print only the invite code")
	qrOutput := bfs.FS.Bool("qr", false, "render the invite code as a terminal QR code")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	if *rawOutput && *qrOutput {
		return fmt.Errorf("use only one of -raw or -qr")
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	clientStore := store.NewClientStore(resolvedDataDir)
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
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
		fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(id.Fingerprint()))
		qrterminal.GenerateHalfBlock(code, qrterminal.L, os.Stdout)
		fmt.Println("share this QR or import a saved QR image with: pando contact add --mailbox <their-mailbox> --qr-image <path>")
		return nil
	}
	fmt.Printf("account: %s\n", id.AccountID)
	fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(id.Fingerprint()))
	fmt.Printf("invite-code: %s\n", code)
	fmt.Println("share the invite-code value above, or use --raw, --copy, or --qr for easier sharing")
	fmt.Println("the other person can import it with: pando contact add --mailbox <their-mailbox> --paste")
	return nil
}
