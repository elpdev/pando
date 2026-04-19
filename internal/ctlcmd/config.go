package ctlcmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/elpdev/pando/internal/config"
)

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
