package ctlcmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/identity"
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
		return fmt.Errorf("usage: pando contact <add|import|invite|list|lookup|publish-directory|show|verify> [flags]")
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

func writeJSON(file *os.File, value any) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
