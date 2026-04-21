package ctlcmd

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/ui/style"
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
		return fmt.Errorf("usage: pando config set <relay|relay-token|mailbox|theme> <value>")
	}
	switch args[0] {
	case "relay":
		return runConfigSetRelay(args[1:])
	case "relay-token":
		return runConfigSetRelayToken(args[1:])
	case "mailbox":
		return runConfigSetMailbox(args[1:])
	case "theme":
		return runConfigSetTheme(args[1:])
	case "help":
		return fmt.Errorf("usage: pando config set <relay|relay-token|mailbox|theme> <value>")
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
	theme := devCfg.Theme
	if theme == "" {
		theme = fmt.Sprintf("(default: %s)", style.DefaultThemeName)
	}
	fmt.Printf("theme: %s\n", theme)
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

func runConfigSetTheme(args []string) error {
	fs := flag.NewFlagSet("config set theme", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config set theme <%s>", availableThemes())
	}
	name := fs.Arg(0)
	if _, ok := style.Themes[name]; !ok {
		return fmt.Errorf("unknown theme %q; available: %s", name, availableThemes())
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	devCfg.Theme = name
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("theme set to %s\n", devCfg.Theme)
	return nil
}

// availableThemes returns the registered theme names joined with "|" for
// usage messages. Sorted so help output is stable across runs.
func availableThemes() string {
	names := make([]string, 0, len(style.Themes))
	for name := range style.Themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}
