package ctlcmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/rendezvous"
	"github.com/elpdev/pando/internal/ui/style"
)

func runContactInvite(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando contact invite <start|accept> [flags]")
	}
	switch args[0] {
	case "start":
		return runContactInviteStart(args[1:])
	case "accept":
		return runContactInviteAccept(args[1:])
	default:
		return fmt.Errorf("unknown contact invite subcommand %q", args[0])
	}
}

func runContactInviteStart(args []string) error {
	bfs := NewBaseFlagSet("contact invite start")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	code := bfs.FS.String("code", "", "optional invite code override")
	timeout := bfs.FS.Duration("timeout", 2*time.Minute, "how long to wait for the other person")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	generatedCode := *code
	if generatedCode == "" {
		var err error
		generatedCode, err = rendezvous.GenerateCode()
		if err != nil {
			return err
		}
	}
	fmt.Printf("invite code: %s\n", generatedCode)
	fmt.Println("tell the other person to run: pando contact invite accept --mailbox <their-mailbox> --code <invite-code>")
	return runInviteExchange(*bfs.RootDir, *bfs.DataDir, *bfs.Mailbox, *relayURL, *relayToken, generatedCode, *timeout)
}

func runContactInviteAccept(args []string) error {
	bfs := NewBaseFlagSet("contact invite accept")
	relayURL := bfs.FS.String("relay", "", "relay websocket URL")
	relayToken := bfs.FS.String("relay-token", "", "relay auth token")
	code := bfs.FS.String("code", "", "invite code")
	timeout := bfs.FS.Duration("timeout", 2*time.Minute, "how long to wait for the other person")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*code) == "" {
		return fmt.Errorf("-code is required")
	}
	return runInviteExchange(*bfs.RootDir, *bfs.DataDir, *bfs.Mailbox, *relayURL, *relayToken, *code, *timeout)
}

func runInviteExchange(rootDir, dataDir, mailbox, relayURL, relayToken, code string, timeout time.Duration) error {
	resolvedMailbox, resolvedDataDir, err := resolveDataDirWithRoot(rootDir, dataDir, mailbox)
	if err != nil {
		return err
	}
	resolvedRelayURL, resolvedRelayToken, err := resolveRelayConfig(rootDir, relayURL, relayToken)
	if err != nil {
		return err
	}
	clientStore, err := prepareClientStore(resolvedMailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	service, _, err := messaging.New(clientStore, resolvedMailbox)
	if err != nil {
		return err
	}
	client, err := relayapi.NewClient(resolvedRelayURL, resolvedRelayToken)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	bundle, err := rendezvous.Exchange(ctx, rendezvous.PollConfig{
		Client:        client,
		Code:          code,
		Self:          service.Identity().InviteBundle(),
		SelfAccountID: service.Identity().AccountID,
	})
	if err != nil {
		return err
	}
	contact, err := service.ImportInviteCodeContact(*bundle)
	if err != nil {
		return err
	}
	fmt.Printf("added invite contact %s with %d active devices\n", contact.AccountID, len(contact.ActiveDevices()))
	fmt.Printf("fingerprint: %s\n", style.FormatFingerprint(contact.Fingerprint()))
	return nil
}

func resolveDataDirWithRoot(rootDir, dataDir, mailbox string) (string, string, error) {
	resolvedMailbox := mailbox
	if strings.TrimSpace(resolvedMailbox) == "" {
		devCfg, err := config.LoadDeviceConfig(rootDir)
		if err != nil {
			return "", "", err
		}
		resolvedMailbox = devCfg.DefaultMailbox
	}
	if strings.TrimSpace(resolvedMailbox) == "" {
		return "", "", fmt.Errorf("-mailbox is required")
	}
	resolvedDataDir, err := resolveDataDir(resolvedMailbox, rootDir, dataDir)
	if err != nil {
		return "", "", err
	}
	return resolvedMailbox, resolvedDataDir, nil
}
