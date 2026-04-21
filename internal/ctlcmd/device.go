package ctlcmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/identity"
)

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

func runListDevices(args []string) error {
	bfs := NewBaseFlagSet("device list")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, dataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	clientStore, err := prepareClientStore(mailbox, dataDir)
	if err != nil {
		return err
	}
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
	if err != nil {
		return err
	}
	return writeJSON(os.Stdout, id.DeviceBundles())
}

func runCreateEnrollment(args []string) error {
	bfs := NewBaseFlagSet("device enroll create")
	accountID := bfs.FS.String("account", "", "stable account identifier")
	outputPath := bfs.FS.String("out", "", "enrollment request output file")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if *accountID == "" {
		return fmt.Errorf("-account is required")
	}
	pending, err := identity.NewPendingEnrollment(*accountID, mailbox)
	if err != nil {
		return err
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	if err := clientStore.SavePendingEnrollment(pending); err != nil {
		return err
	}
	return writeJSONOutput(*outputPath, pending.Request())
}

func runApproveEnrollment(args []string) error {
	bfs := NewBaseFlagSet("device enroll approve")
	requestPath := bfs.FS.String("request", "", "path to enrollment request JSON")
	outputPath := bfs.FS.String("out", "", "approval output file")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if *requestPath == "" {
		return fmt.Errorf("-request is required")
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
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
	return writeJSONOutput(*outputPath, approval)
}

func runCompleteEnrollment(args []string) error {
	bfs := NewBaseFlagSet("device enroll complete")
	approvalPath := bfs.FS.String("approval", "", "path to approval JSON")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if *approvalPath == "" {
		return fmt.Errorf("-approval is required")
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	pending, err := clientStore.LoadPendingEnrollment()
	if err != nil {
		return err
	}
	if pending.Device.Mailbox != mailbox {
		return fmt.Errorf("pending enrollment is for device mailbox %q, not %q", pending.Device.Mailbox, mailbox)
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
	fmt.Printf("completed enrollment for %s on device %s\n", id.AccountID, mailbox)
	return nil
}

func runRevokeDevice(args []string) error {
	bfs := NewBaseFlagSet("device revoke")
	deviceID := bfs.FS.String("device", "", "device id or mailbox to revoke")
	if err := bfs.Parse(args); err != nil {
		return err
	}
	mailbox, resolvedDataDir, err := bfs.Resolve()
	if err != nil {
		return err
	}
	if *deviceID == "" {
		return fmt.Errorf("-device is required")
	}
	clientStore, err := prepareClientStore(mailbox, resolvedDataDir)
	if err != nil {
		return err
	}
	id, _, err := clientStore.LoadOrCreateIdentity(mailbox)
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
