package clientcmd

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/chatui/internal/config"
	"github.com/elpdev/chatui/internal/messaging"
	"github.com/elpdev/chatui/internal/store"
	"github.com/elpdev/chatui/internal/transport/ws"
	"github.com/elpdev/chatui/internal/ui"
	"github.com/elpdev/chatui/internal/ui/chat"
)

func Execute(args []string) error {
	cfg := config.DefaultClient()
	fs := flag.NewFlagSet("chatui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.RelayURL, "relay", cfg.RelayURL, "relay websocket URL")
	fs.StringVar(&cfg.RelayToken, "relay-token", cfg.RelayToken, "relay auth token")
	fs.StringVar(&cfg.Mailbox, "mailbox", "", "local mailbox identifier")
	fs.StringVar(&cfg.RecipientMailbox, "to", "", "recipient mailbox identifier")
	fs.StringVar(&cfg.DataDir, "data-dir", "", "client state directory")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.DataDir == "" && cfg.Mailbox != "" {
		cfg.DataDir = config.ClientDataDir(cfg.Mailbox)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	clientStore := store.NewClientStore(cfg.DataDir)
	service, _, err := messaging.New(clientStore, cfg.Mailbox)
	if err != nil {
		return err
	}
	client := ws.NewClient(cfg.RelayURL, cfg.RelayToken, cfg.Mailbox)
	chatModel := chat.New(chat.Deps{
		Client:           client,
		Messaging:        service,
		Mailbox:          cfg.Mailbox,
		RecipientMailbox: cfg.RecipientMailbox,
		RelayURL:         cfg.RelayURL,
	})

	program := tea.NewProgram(ui.New(chatModel), tea.WithAltScreen())
	_, err = program.Run()
	return err
}
