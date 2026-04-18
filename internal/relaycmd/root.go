package relaycmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/elpdev/chatui/internal/config"
	"github.com/elpdev/chatui/internal/logging"
	"github.com/elpdev/chatui/internal/relay"
)

func Execute(args []string) error {
	cfg := config.DefaultRelay()
	fs := flag.NewFlagSet("chatui-relay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid relay config: %w", err)
	}

	logger := logging.New("chatui-relay", false)
	server := relay.NewServer(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.ListenAndServe(ctx, cfg.Addr)
}
