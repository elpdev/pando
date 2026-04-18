package clientcmd

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/chatui/internal/config"
	"github.com/elpdev/chatui/internal/transport/ws"
	"github.com/elpdev/chatui/internal/ui"
	"github.com/elpdev/chatui/internal/ui/chat"
)

func Execute(args []string) error {
	cfg := config.DefaultClient()
	fs := flag.NewFlagSet("chatui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.RelayURL, "relay", cfg.RelayURL, "relay websocket URL")
	fs.StringVar(&cfg.Mailbox, "mailbox", "", "local mailbox identifier")
	fs.StringVar(&cfg.RecipientMailbox, "to", "", "recipient mailbox identifier")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	client := ws.NewClient(cfg.RelayURL, cfg.Mailbox)
	chatModel := chat.New(chat.Deps{
		Client:           client,
		Mailbox:          cfg.Mailbox,
		RecipientMailbox: cfg.RecipientMailbox,
		RelayURL:         cfg.RelayURL,
	})

	program := tea.NewProgram(ui.New(chatModel), tea.WithAltScreen())
	_, err := program.Run()
	return err
}
