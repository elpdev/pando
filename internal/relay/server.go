package relay

import (
	"context"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
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
	subs map[*subscriber]struct{}
}

type subscriber struct {
	conn    *websocket.Conn
	mailbox string
	mu      sync.Mutex
}

const genericClientError = "request rejected"
const subscribeChallengeTTL = 30 * time.Second

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
	if configurable, ok := queue.(interface{ SetLimits(QueueLimits) }); ok {
		configurable.SetLimits(QueueLimits{MaxMessages: options.MaxQueuedMessages, MaxBytes: options.MaxQueuedBytes})
	}
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
	mux.HandleFunc("/", s.handleLanding)
	mux.HandleFunc("/logo.webp", s.handleLogo)
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/up", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *Server) handleLogo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(logoWebP)
}

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.landing.Execute(w, nil); err != nil {
		http.Error(w, "render landing page", http.StatusInternalServerError)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.options.AuthToken != "" && r.Header.Get(authHeader) != s.options.AuthToken {
		http.Error(w, "relay auth token is required", http.StatusUnauthorized)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("upgrade websocket", "error", err)
		return
	}

	defer conn.Close()
	challenge := newSubscribeChallenge(time.Now().UTC())
	s.writeConn(nil, conn, protocol.Message{Type: protocol.MessageTypeSubscribeChallenge, Challenge: challenge})

	var current *subscriber
	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			if current != nil {
				s.unregister(current)
			}
			s.logger.Info("client disconnected")
			return
		}

		if err := msg.Validate(); err != nil {
			s.logger.Warn("reject invalid websocket message", "error", err)
			s.writeClientError(current, conn, genericClientError)
			continue
		}

		switch msg.Type {
		case protocol.MessageTypeSubscribe:
			now := time.Now().UTC()
			if !s.limiter.Allow("subscribe:"+r.RemoteAddr, now) {
				s.writeClientError(current, conn, "relay rate limit exceeded")
				continue
			}
			if err := s.verifySubscribeRequest(*msg.Subscribe, challenge, now); err != nil {
				s.logger.Warn("reject subscribe request", "mailbox", msg.Subscribe.Mailbox, "error", err)
				s.writeClientError(current, conn, genericClientError)
				challenge = newSubscribeChallenge(now)
				s.writeConn(current, conn, protocol.Message{Type: protocol.MessageTypeSubscribeChallenge, Challenge: challenge})
				continue
			}
			challenge = nil
			if current != nil {
				s.unregister(current)
			}

			current = &subscriber{conn: conn, mailbox: msg.Subscribe.Mailbox}
			backlog, err := s.register(current)
			if err != nil {
				s.logger.Warn("register subscriber", "mailbox", msg.Subscribe.Mailbox, "error", err)
				s.writeClientError(current, conn, genericClientError)
				continue
			}
			s.writeSubscriber(current, protocol.Message{
				Type: protocol.MessageTypeAck,
				Ack:  &protocol.Ack{ID: current.mailbox},
			})
			for _, envelope := range backlog {
				s.writeSubscriber(current, protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelope})
			}
		case protocol.MessageTypePublish:
			envelope := msg.Publish.Envelope
			now := time.Now().UTC()
			if err := validateEnvelopeLimits(envelope, s.options); err != nil {
				s.logger.Warn("reject oversized envelope", "sender_mailbox", envelope.SenderMailbox, "recipient_mailbox", envelope.RecipientMailbox, "error", err)
				s.writeClientError(current, conn, genericClientError)
				continue
			}
			if !s.limiter.Allow(envelope.SenderMailbox, now) {
				s.writeClientError(current, conn, "relay rate limit exceeded")
				continue
			}
			envelope.ID = uuid.NewString()
			envelope.Timestamp = now
			envelope.ExpiresAt = now.Add(s.options.QueueTTL)
			if err := s.publish(envelope); err != nil {
				if errors.Is(err, ErrQueueFull) {
					s.writeClientError(current, conn, "mailbox queue is full")
					continue
				}
				s.logger.Warn("publish envelope", "sender_mailbox", envelope.SenderMailbox, "recipient_mailbox", envelope.RecipientMailbox, "error", err)
				s.writeClientError(current, conn, genericClientError)
				continue
			}
			s.writeConn(current, conn, protocol.Message{
				Type: protocol.MessageTypeAck,
				Ack:  &protocol.Ack{ID: envelope.ID},
			})
		}
	}
}

func (s *Server) verifySubscribeRequest(req protocol.SubscribeRequest, challenge *protocol.SubscribeChallenge, now time.Time) error {
	if challenge == nil {
		return fmt.Errorf("subscribe challenge is required")
	}
	if req.ChallengeNonce != challenge.Nonce {
		return fmt.Errorf("invalid challenge nonce")
	}
	if !req.ChallengeExpiresAt.Equal(challenge.ExpiresAt) {
		return fmt.Errorf("invalid challenge expiry")
	}
	if !challenge.ExpiresAt.After(now) {
		return fmt.Errorf("subscribe challenge expired")
	}
	signingPublic, err := base64.StdEncoding.DecodeString(req.DeviceSigningKey)
	if err != nil {
		return fmt.Errorf("decode device signing key: %w", err)
	}
	if len(signingPublic) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid device signing key length")
	}
	proof, err := base64.StdEncoding.DecodeString(req.DeviceProof)
	if err != nil {
		return fmt.Errorf("decode device proof: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(signingPublic), protocol.SubscribeProofBytes(req.Mailbox, req.ChallengeNonce, req.ChallengeExpiresAt), proof) {
		return fmt.Errorf("invalid device proof")
	}
	if err := s.queue.AuthorizeMailbox(req.Mailbox, signingPublic); err != nil {
		return err
	}
	return nil
}

func newSubscribeChallenge(now time.Time) *protocol.SubscribeChallenge {
	return &protocol.SubscribeChallenge{Nonce: uuid.NewString(), ExpiresAt: now.Add(subscribeChallengeTTL)}
}

func (s *Server) register(sub *subscriber) ([]protocol.Envelope, error) {
	s.mu.Lock()
	mb := s.getMailboxLocked(sub.mailbox)
	if mb.subs == nil {
		mb.subs = make(map[*subscriber]struct{})
	}
	mb.subs[sub] = struct{}{}
	s.mu.Unlock()

	backlog, err := s.queue.Drain(sub.mailbox)
	if err != nil {
		return nil, fmt.Errorf("drain mailbox queue: %w", err)
	}
	return backlog, nil
}

func (s *Server) unregister(sub *subscriber) {
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
	subs := make([]*subscriber, 0, len(mb.subs))
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
		s.writeSubscriber(sub, message)
	}
	return nil
}

func (s *Server) getMailboxLocked(name string) *mailbox {
	mb, ok := s.mailboxes[name]
	if !ok {
		mb = &mailbox{subs: make(map[*subscriber]struct{})}
		s.mailboxes[name] = mb
	}
	return mb
}

func (s *Server) write(conn *websocket.Conn, msg protocol.Message) {
	if conn == nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(msg); err != nil {
		s.logger.Info("write websocket message")
	}
}

func (s *Server) writeSubscriber(sub *subscriber, msg protocol.Message) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.conn == nil {
		return
	}
	s.write(sub.conn, msg)
}

func (s *Server) writeClientError(sub *subscriber, conn *websocket.Conn, message string) {
	s.writeConn(sub, conn, protocol.Message{Type: protocol.MessageTypeError, Error: &protocol.Error{Message: message}})
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if len(s.options.AllowedOrigins) != 0 {
		for _, allowed := range s.options.AllowedOrigins {
			if origin == allowed {
				return true
			}
		}
		return false
	}
	return parsedOrigin.Host == r.Host
}

func (s *Server) writeConn(sub *subscriber, conn *websocket.Conn, msg protocol.Message) {
	if sub != nil {
		s.writeSubscriber(sub, msg)
		return
	}
	s.write(conn, msg)
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
