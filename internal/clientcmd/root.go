package clientcmd

import (
	"flag"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/passphrase"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/transport/ws"
	"github.com/elpdev/pando/internal/ui"
	"github.com/elpdev/pando/internal/ui/chat"
	"github.com/elpdev/pando/internal/ui/style"
)

func Execute(args []string) error {
	// Resolve root dir early so we can load the device config before setting flag defaults.
	rootDir := scanRootDir(args)
	devCfg, err := config.LoadDeviceConfig(rootDir)
	if err != nil {
		return err
	}

	cfg := config.DefaultClient()
	cfg.RootDir = rootDir
	if devCfg.RelayURL != "" {
		cfg.RelayURL = devCfg.RelayURL
	}
	if devCfg.RelayToken != "" {
		cfg.RelayToken = devCfg.RelayToken
	}
	if devCfg.DefaultMailbox != "" {
		cfg.Mailbox = devCfg.DefaultMailbox
	}

	fs := flag.NewFlagSet("pando", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.RelayURL, "relay", cfg.RelayURL, "relay websocket URL")
	fs.StringVar(&cfg.RelayToken, "relay-token", cfg.RelayToken, "relay auth token")
	fs.StringVar(&cfg.Mailbox, "mailbox", cfg.Mailbox, "local mailbox identifier")
	fs.StringVar(&cfg.RecipientMailbox, "to", "", "recipient mailbox identifier")
	fs.StringVar(&cfg.RootDir, "root-dir", cfg.RootDir, "root directory for Pando storage")
	fs.StringVar(&cfg.DataDir, "data-dir", "", "client state directory")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.DataDir == "" && cfg.Mailbox != "" {
		cfg.DataDir = config.ClientDataDir(cfg.RootDir, cfg.Mailbox)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	style.Apply(style.ResolveTheme(devCfg.Theme))

	clientStore := store.NewClientStore(cfg.DataDir)
	if err := passphrase.PrepareClientStore(clientStore, cfg.Mailbox); err != nil {
		return err
	}
	service, _, err := messaging.New(clientStore, cfg.Mailbox)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.RelayURL) != "" {
		directoryClient, err := relayapi.NewClient(cfg.RelayURL, cfg.RelayToken)
		if err != nil {
			return err
		}
		service.SetDirectoryClient(directoryClient)
	}
	client := ws.NewClient(cfg.RelayURL, cfg.RelayToken, service.Identity())
	chatModel := chat.New(chat.Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          cfg.Mailbox,
		RecipientMailbox: cfg.RecipientMailbox,
		RelayURL:         cfg.RelayURL,
		RelayToken:       cfg.RelayToken,
		SaveTheme: func(name string) error {
			devCfg, err := config.LoadDeviceConfig(rootDir)
			if err != nil {
				return err
			}
			devCfg.Theme = name
			return config.SaveDeviceConfig(rootDir, devCfg)
		},
	})

	program := tea.NewProgram(ui.New(chatModel), tea.WithAltScreen())
	_, err = program.Run()
	return err
}

// scanRootDir scans args for an explicit -root-dir or --root-dir value.
// Returns the default root dir if not found.
func scanRootDir(args []string) string {
	for i, arg := range args {
		for _, prefix := range []string{"-root-dir=", "--root-dir="} {
			if strings.HasPrefix(arg, prefix) {
				return strings.TrimPrefix(arg, prefix)
			}
		}
		if (arg == "-root-dir" || arg == "--root-dir") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return config.DefaultRootDir()
}
