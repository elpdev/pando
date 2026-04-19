package relay

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

//go:embed landing.html
var landingHTML string

//go:embed logo.webp
var logoWebP []byte

type Server struct {
	logger   *slog.Logger
	upgrader websocket.Upgrader
	queue    QueueStore
	options  Options
	limiter  *rateLimiter
	landing  *template.Template

	mu        sync.Mutex
	mailboxes map[string]*mailbox
}

type mailbox struct {
	subs map[*wsSession]struct{}
}

const genericClientError = "request rejected"
const subscribeChallengeTTL = 30 * time.Second
const maxRendezvousPayloads = 2

func NewServer(logger *slog.Logger, queue QueueStore, options Options) *Server {
	if queue == nil {
		queue = NewMemoryQueueStore()
	}
	if options.QueueTTL <= 0 {
		options.QueueTTL = 24 * time.Hour
	}
	if options.MaxMessageBytes <= 0 {
		options.MaxMessageBytes = 64 * 1024
	}
	if options.MaxQueuedMessages <= 0 {
		options.MaxQueuedMessages = 512
	}
	if options.MaxQueuedBytes <= 0 {
		options.MaxQueuedBytes = 16 * 1024 * 1024
	}
	if options.RateLimitPerMinute <= 0 {
		options.RateLimitPerMinute = 120
	}
	queue.SetLimits(QueueLimits{MaxMessages: options.MaxQueuedMessages, MaxBytes: options.MaxQueuedBytes})
	server := &Server{
		logger:    logger,
		queue:     queue,
		options:   options,
		limiter:   newRateLimiter(options.RateLimitPerMinute),
		landing:   template.Must(template.New("landing").Parse(landingHTML)),
		mailboxes: make(map[string]*mailbox),
	}
	server.upgrader = websocket.Upgrader{CheckOrigin: server.checkOrigin}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if s.options.LandingPage {
		mux.HandleFunc("/", s.handleLanding)
		mux.HandleFunc("/logo.webp", s.handleLogo)
	}
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/directory/mailboxes/", s.handleDirectory)
	mux.HandleFunc("/rendezvous/", s.handleRendezvous)
	mux.HandleFunc("/up", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *Server) handleLogo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(logoWebP)
}

func (s *Server) finalizePublishedEnvelope(envelope protocol.Envelope, now time.Time) protocol.Envelope {
	envelope.ID = uuid.NewString()
	envelope.Timestamp = now
	envelope.ExpiresAt = now.Add(s.options.QueueTTL)
	return envelope
}

func (s *Server) register(sub *wsSession) ([]protocol.Envelope, error) {
	s.mu.Lock()
	mb := s.getMailboxLocked(sub.mailbox)
	if mb.subs == nil {
		mb.subs = make(map[*wsSession]struct{})
	}
	mb.subs[sub] = struct{}{}
	s.mu.Unlock()

	backlog, err := s.queue.Drain(sub.mailbox)
	if err != nil {
		return nil, fmt.Errorf("drain mailbox queue: %w", err)
	}
	return backlog, nil
}

func (s *Server) unregister(sub *wsSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mb, ok := s.mailboxes[sub.mailbox]
	if !ok {
		return
	}
	delete(mb.subs, sub)
	if len(mb.subs) == 0 {
		delete(s.mailboxes, sub.mailbox)
	}
}

func (s *Server) publish(envelope protocol.Envelope) error {
	s.mu.Lock()
	mb := s.getMailboxLocked(envelope.RecipientMailbox)
	subs := make([]*wsSession, 0, len(mb.subs))
	for sub := range mb.subs {
		subs = append(subs, sub)
	}
	if len(subs) == 0 {
		s.mu.Unlock()
		if err := s.queue.Enqueue(envelope); err != nil {
			return fmt.Errorf("queue offline envelope: %w", err)
		}
		return nil
	}
	s.mu.Unlock()

	message := protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelope}
	for _, sub := range subs {
		sub.send(message)
	}
	return nil
}

func (s *Server) getMailboxLocked(name string) *mailbox {
	mb, ok := s.mailboxes[name]
	if !ok {
		mb = &mailbox{subs: make(map[*wsSession]struct{})}
		s.mailboxes[name] = mb
	}
	return mb
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	defer s.queue.Close()
	httpServer := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	s.logger.Info("relay listening", "addr", addr)
	err := httpServer.ListenAndServe()
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}
