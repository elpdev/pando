package relaycmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/logging"
	"github.com/elpdev/pando/internal/relay"
)

func Execute(args []string) error {
	cfg := config.DefaultRelay()
	if err := config.ApplyRelayEnv(&cfg); err != nil {
		return fmt.Errorf("invalid relay environment: %w", err)
	}
	fs := flag.NewFlagSet("pando-relay", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "HTTP listen address")
	fs.StringVar(&cfg.RootDir, "root-dir", cfg.RootDir, "root directory for Pando storage")
	fs.StringVar(&cfg.StorePath, "store", cfg.StorePath, "path to relay queue store")
	fs.DurationVar(&cfg.QueueTTL, "ttl", cfg.QueueTTL, "offline message retention TTL")
	fs.IntVar(&cfg.MaxMessageBytes, "max-message-bytes", cfg.MaxMessageBytes, "maximum accepted message payload size")
	fs.IntVar(&cfg.RateLimitPerMinute, "rate-limit-per-minute", cfg.RateLimitPerMinute, "maximum publishes per sender mailbox per minute")
	fs.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "optional shared token required for relay websocket access")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.StorePath == "" {
		cfg.StorePath = config.RelayStorePath(cfg.RootDir)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid relay config: %w", err)
	}

	logger := logging.New("pando-relay", false)
	queueStore, err := relay.NewBoltQueueStore(cfg.StorePath)
	if err != nil {
		return err
	}
	server := relay.NewServer(logger, queueStore, relay.Options{
		QueueTTL:           cfg.QueueTTL,
		MaxMessageBytes:    cfg.MaxMessageBytes,
		RateLimitPerMinute: cfg.RateLimitPerMinute,
		AuthToken:          cfg.AuthToken,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.ListenAndServe(ctx, cfg.Addr)
}
