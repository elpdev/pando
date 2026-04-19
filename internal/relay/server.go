package relay

import (
	"context"
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
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
const rendezvousTTL = 10 * time.Minute
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

func (s *Server) handleDirectory(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRequest(w, r) {
		return
	}
	mailbox := strings.TrimPrefix(r.URL.Path, "/directory/mailboxes/")
	if mailbox == "" || strings.Contains(mailbox, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		entry, err := s.queue.GetDirectoryEntry(mailbox)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				s.writeJSONError(w, http.StatusNotFound, "directory entry not found")
				return
			}
			s.writeJSONError(w, http.StatusInternalServerError, "load directory entry")
			return
		}
		s.writeJSON(w, http.StatusOK, entry)
	case http.MethodPut:
		var entry relayapi.SignedDirectoryEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "decode directory entry")
			return
		}
		if entry.Entry.Mailbox != mailbox {
			s.writeJSONError(w, http.StatusBadRequest, "directory mailbox mismatch")
			return
		}
		if err := relayapi.VerifySignedDirectoryEntry(entry); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.queue.PutDirectoryEntry(entry); err != nil {
			if errors.Is(err, ErrDirectoryConflict) {
				s.writeJSONError(w, http.StatusConflict, err.Error())
				return
			}
			s.writeJSONError(w, http.StatusInternalServerError, "save directory entry")
			return
		}
		s.writeJSON(w, http.StatusOK, entry)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRendezvous(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRequest(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/rendezvous/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		payloads, err := s.queue.GetRendezvousPayloads(id, time.Now().UTC())
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "load rendezvous payloads")
			return
		}
		s.writeJSON(w, http.StatusOK, relayapi.GetRendezvousResponse{Payloads: payloads})
	case http.MethodPut:
		if !s.limiter.Allow("rendezvous:"+r.RemoteAddr, time.Now().UTC()) {
			s.writeJSONError(w, http.StatusTooManyRequests, "relay rate limit exceeded")
			return
		}
		var request relayapi.PutRendezvousRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, "decode rendezvous payload")
			return
		}
		if err := validateRendezvousPayload(request.Payload, time.Now().UTC(), s.options.MaxMessageBytes); err != nil {
			s.writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		existing, err := s.queue.GetRendezvousPayloads(id, time.Now().UTC())
		if err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "load rendezvous payloads")
			return
		}
		if len(existing) >= maxRendezvousPayloads {
			s.writeJSONError(w, http.StatusConflict, "rendezvous slot is full")
			return
		}
		if err := s.queue.PutRendezvousPayload(id, request.Payload); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "save rendezvous payload")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := s.queue.DeleteRendezvous(id); err != nil {
			s.writeJSONError(w, http.StatusInternalServerError, "delete rendezvous payloads")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

func (s *Server) authorizeRequest(w http.ResponseWriter, r *http.Request) bool {
	if s.options.AuthToken == "" || r.Header.Get(authHeader) == s.options.AuthToken {
		return true
	}
	http.Error(w, "relay auth token is required", http.StatusUnauthorized)
	return false
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeRequest(w, r) {
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
		if err := s.handleWebSocketMessage(conn, r.RemoteAddr, &current, &challenge, msg); err != nil {
			continue
		}
	}
}

func (s *Server) handleWebSocketMessage(conn *websocket.Conn, remoteAddr string, current **subscriber, challenge **protocol.SubscribeChallenge, msg protocol.Message) error {
	switch msg.Type {
	case protocol.MessageTypeSubscribe:
		return s.handleSubscribeMessage(conn, remoteAddr, current, challenge, *msg.Subscribe)
	case protocol.MessageTypePublish:
		return s.handlePublishMessage(conn, current, msg.Publish.Envelope)
	default:
		return nil
	}
}

func (s *Server) handleSubscribeMessage(conn *websocket.Conn, remoteAddr string, current **subscriber, challenge **protocol.SubscribeChallenge, req protocol.SubscribeRequest) error {
	now := time.Now().UTC()
	if !s.limiter.Allow("subscribe:"+remoteAddr, now) {
		s.writeClientError(*current, conn, "relay rate limit exceeded")
		return fmt.Errorf("subscribe rate limited")
	}
	if err := s.verifySubscribeRequest(req, *challenge, now); err != nil {
		s.logger.Warn("reject subscribe request", "mailbox", req.Mailbox, "error", err)
		s.writeClientError(*current, conn, genericClientError)
		*challenge = newSubscribeChallenge(now)
		s.writeConn(*current, conn, protocol.Message{Type: protocol.MessageTypeSubscribeChallenge, Challenge: *challenge})
		return err
	}
	*challenge = nil
	if *current != nil {
		s.unregister(*current)
	}

	next := &subscriber{conn: conn, mailbox: req.Mailbox}
	backlog, err := s.register(next)
	if err != nil {
		s.logger.Warn("register subscriber", "mailbox", req.Mailbox, "error", err)
		s.writeClientError(*current, conn, genericClientError)
		return err
	}
	*current = next
	s.ackSubscriber(next)
	s.writeBacklog(next, backlog)
	return nil
}

func (s *Server) handlePublishMessage(conn *websocket.Conn, current **subscriber, envelope protocol.Envelope) error {
	now := time.Now().UTC()
	if err := validateEnvelopeLimits(envelope, s.options); err != nil {
		s.logger.Warn("reject oversized envelope", "sender_mailbox", envelope.SenderMailbox, "recipient_mailbox", envelope.RecipientMailbox, "error", err)
		s.writeClientError(*current, conn, genericClientError)
		return err
	}
	if !s.limiter.Allow(envelope.SenderMailbox, now) {
		s.writeClientError(*current, conn, "relay rate limit exceeded")
		return fmt.Errorf("publish rate limited")
	}
	envelope.ID = uuid.NewString()
	envelope.Timestamp = now
	envelope.ExpiresAt = now.Add(s.options.QueueTTL)
	if err := s.publish(envelope); err != nil {
		if errors.Is(err, ErrQueueFull) {
			s.writeClientError(*current, conn, "mailbox queue is full")
			return err
		}
		s.logger.Warn("publish envelope", "sender_mailbox", envelope.SenderMailbox, "recipient_mailbox", envelope.RecipientMailbox, "error", err)
		s.writeClientError(*current, conn, genericClientError)
		return err
	}
	s.writeConn(*current, conn, protocol.Message{Type: protocol.MessageTypeAck, Ack: &protocol.Ack{ID: envelope.ID}})
	return nil
}

func (s *Server) ackSubscriber(sub *subscriber) {
	s.writeSubscriber(sub, protocol.Message{Type: protocol.MessageTypeAck, Ack: &protocol.Ack{ID: sub.mailbox}})
}

func (s *Server) writeBacklog(sub *subscriber, backlog []protocol.Envelope) {
	for _, envelope := range backlog {
		s.writeSubscriber(sub, protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelope})
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

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeJSONError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, struct {
		Message string `json:"message"`
	}{Message: message})
}

func validateRendezvousPayload(payload relayapi.RendezvousPayload, now time.Time, maxBytes int) error {
	if strings.TrimSpace(payload.Ciphertext) == "" {
		return fmt.Errorf("rendezvous ciphertext is required")
	}
	if strings.TrimSpace(payload.Nonce) == "" {
		return fmt.Errorf("rendezvous nonce is required")
	}
	if payload.CreatedAt.IsZero() {
		return fmt.Errorf("rendezvous created_at is required")
	}
	if payload.ExpiresAt.IsZero() {
		return fmt.Errorf("rendezvous expires_at is required")
	}
	if payload.ExpiresAt.After(now.Add(rendezvousTTL)) {
		return fmt.Errorf("rendezvous expiry exceeds maximum TTL")
	}
	if !payload.ExpiresAt.After(now) {
		return fmt.Errorf("rendezvous payload is already expired")
	}
	if len(payload.Ciphertext)+len(payload.Nonce) > maxBytes {
		return fmt.Errorf("rendezvous payload exceeds relay size limit")
	}
	return nil
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
