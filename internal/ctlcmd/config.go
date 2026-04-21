package ctlcmd

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/ui/style"
)

func runConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando config <show|set|relay> [flags]")
	}
	switch args[0] {
	case "show":
		return runConfigShow(args[1:])
	case "set":
		return runConfigSet(args[1:])
	case "relay":
		return runConfigRelay(args[1:])
	case "help":
		return runHelp([]string{"config"})
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runConfigSet(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando config set <relay|relay-token|mailbox|theme|message-ttl> <value>")
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
	case "message-ttl":
		return runConfigSetMessageTTL(args[1:])
	case "help":
		return fmt.Errorf("usage: pando config set <relay|relay-token|mailbox|theme|message-ttl> <value>")
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
	activeRelay := devCfg.ActiveRelayProfile()
	fmt.Printf("config file: %s\n", config.DeviceConfigPath(*rootDir))
	fmt.Printf("relay_url: %s\n", activeRelay.URL)
	fmt.Printf("relay_token: %s\n", activeRelay.Token)
	fmt.Printf("active_relay: %s\n", activeRelay.Name)
	profiles := devCfg.RelayProfiles()
	if len(profiles) == 0 {
		fmt.Println("relays: (none)")
	} else {
		fmt.Println("relays:")
		for _, relay := range profiles {
			status := ""
			if relay.Name == activeRelay.Name {
				status = " (active)"
			}
			tokenState := "no-token"
			if relay.Token != "" {
				tokenState = "token"
			}
			fmt.Printf("- %s%s %s [%s]\n", relay.Name, status, relay.URL, tokenState)
		}
	}
	fmt.Printf("default_mailbox: %s\n", devCfg.DefaultMailbox)
	theme := devCfg.Theme
	if theme == "" {
		theme = fmt.Sprintf("(default: %s)", style.DefaultThemeName)
	}
	fmt.Printf("theme: %s\n", theme)
	fmt.Printf("message_ttl: %s (effective: %s)\n", formatTTLRaw(devCfg.MessageTTL), devCfg.EffectiveMessageTTL())
	return nil
}

func formatTTLRaw(d time.Duration) string {
	if d <= 0 {
		return fmt.Sprintf("(default: %s)", config.DefaultMessageTTL)
	}
	return d.String()
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
	relays, active := ensureRelayProfiles(devCfg)
	for i := range relays {
		if relays[i].Name == active {
			relays[i].URL = fs.Arg(0)
			devCfg.SetRelayProfiles(relays, active)
			if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
				return err
			}
			fmt.Printf("relay_url set to %s\n", devCfg.RelayURL)
			return nil
		}
	}
	relays = append(relays, config.RelayProfile{Name: active, URL: fs.Arg(0)})
	devCfg.SetRelayProfiles(relays, active)
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
	relays, active := ensureRelayProfiles(devCfg)
	for i := range relays {
		if relays[i].Name == active {
			relays[i].Token = fs.Arg(0)
			devCfg.SetRelayProfiles(relays, active)
			if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
				return err
			}
			fmt.Printf("relay_token set to %s\n", devCfg.RelayToken)
			return nil
		}
	}
	relays = append(relays, config.RelayProfile{Name: active, URL: config.DefaultRelayURL, Token: fs.Arg(0)})
	devCfg.SetRelayProfiles(relays, active)
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

func runConfigSetMessageTTL(args []string) error {
	fs := flag.NewFlagSet("config set message-ttl", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config set message-ttl <duration> (e.g. 1h, 6h, 24h; max %s)", config.MaxMessageTTL)
	}
	ttl, err := time.ParseDuration(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", fs.Arg(0), err)
	}
	if ttl <= 0 {
		return fmt.Errorf("message-ttl must be positive")
	}
	if ttl > config.MaxMessageTTL {
		return fmt.Errorf("message-ttl %s exceeds maximum %s", ttl, config.MaxMessageTTL)
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	devCfg.MessageTTL = ttl
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("message_ttl set to %s\n", devCfg.MessageTTL)
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

func runConfigRelay(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pando config relay <list|add|remove|use> [flags]")
	}
	switch args[0] {
	case "list":
		return runConfigRelayList(args[1:])
	case "add":
		return runConfigRelayAdd(args[1:])
	case "remove":
		return runConfigRelayRemove(args[1:])
	case "use":
		return runConfigRelayUse(args[1:])
	case "help":
		return fmt.Errorf("usage: pando config relay <list|add|remove|use> [flags]")
	default:
		return fmt.Errorf("unknown config relay subcommand %q", args[0])
	}
}

func runConfigRelayList(args []string) error {
	fs := flag.NewFlagSet("config relay list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	active := devCfg.ActiveRelayProfile()
	for _, relay := range devCfg.RelayProfiles() {
		marker := " "
		if relay.Name == active.Name {
			marker = "*"
		}
		tokenState := "no-token"
		if relay.Token != "" {
			tokenState = "token"
		}
		fmt.Printf("%s %s %s [%s]\n", marker, relay.Name, relay.URL, tokenState)
	}
	return nil
}

func runConfigRelayAdd(args []string) error {
	fs := flag.NewFlagSet("config relay add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	token := fs.String("token", "", "relay auth token")
	use := fs.Bool("use", false, "set the new relay as active")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: pando config relay add [-root-dir dir] [-token token] [-use] <name> <url>")
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	relays := devCfg.RelayProfiles()
	name := strings.TrimSpace(fs.Arg(0))
	url := strings.TrimSpace(fs.Arg(1))
	if name == "" || url == "" {
		return fmt.Errorf("relay name and url are required")
	}
	for _, relay := range relays {
		if relay.Name == name {
			return fmt.Errorf("relay %q already exists", name)
		}
	}
	relays = append(relays, config.RelayProfile{Name: name, URL: url, Token: *token})
	active := devCfg.ActiveRelay
	if *use || active == "" {
		active = name
	}
	devCfg.SetRelayProfiles(relays, active)
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	status := "saved"
	if devCfg.ActiveRelay == name {
		status = "saved and active"
	}
	fmt.Printf("relay %s %s\n", name, status)
	return nil
}

func runConfigRelayRemove(args []string) error {
	fs := flag.NewFlagSet("config relay remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config relay remove [-root-dir dir] <name>")
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(fs.Arg(0))
	relays := devCfg.RelayProfiles()
	if len(relays) <= 1 {
		return fmt.Errorf("cannot remove the last saved relay")
	}
	filtered := make([]config.RelayProfile, 0, len(relays)-1)
	removed := false
	for _, relay := range relays {
		if relay.Name == name {
			removed = true
			continue
		}
		filtered = append(filtered, relay)
	}
	if !removed {
		return fmt.Errorf("relay %q not found", name)
	}
	active := devCfg.ActiveRelay
	if active == name {
		active = filtered[0].Name
	}
	devCfg.SetRelayProfiles(filtered, active)
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("removed relay %s\n", name)
	return nil
}

func runConfigRelayUse(args []string) error {
	fs := flag.NewFlagSet("config relay use", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootDir := fs.String("root-dir", config.DefaultRootDir(), "root directory for Pando storage")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: pando config relay use [-root-dir dir] <name>")
	}
	devCfg, err := config.LoadDeviceConfig(*rootDir)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(fs.Arg(0))
	found := false
	for _, relay := range devCfg.RelayProfiles() {
		if relay.Name == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("relay %q not found", name)
	}
	devCfg.SetRelayProfiles(devCfg.RelayProfiles(), name)
	if err := config.SaveDeviceConfig(*rootDir, devCfg); err != nil {
		return err
	}
	fmt.Printf("active relay set to %s\n", name)
	return nil
}

func ensureRelayProfiles(devCfg config.DeviceConfig) ([]config.RelayProfile, string) {
	relays := devCfg.RelayProfiles()
	active := devCfg.ActiveRelayProfile().Name
	if len(relays) == 0 {
		relays = []config.RelayProfile{{Name: active, URL: config.DefaultRelayURL}}
	}
	if active == "" {
		active = relays[0].Name
	}
	return relays, active
}
