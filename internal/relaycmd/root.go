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
	fs.StringVar(&cfg.StorePath, "store", cfg.StorePath, "path to relay queue store")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid relay config: %w", err)
	}

	logger := logging.New("chatui-relay", false)
	queueStore, err := relay.NewBoltQueueStore(cfg.StorePath)
	if err != nil {
		return err
	}
	server := relay.NewServer(logger, queueStore)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.ListenAndServe(ctx, cfg.Addr)
}
