package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/gorilla/websocket"
)

type wsSession struct {
	server     *Server
	conn       *websocket.Conn
	remoteAddr string

	mu        sync.Mutex
	mailbox   string
	challenge *protocol.SubscribeChallenge
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

	session := &wsSession{server: s, conn: conn, remoteAddr: r.RemoteAddr}
	session.run()
}

func (sess *wsSession) run() {
	defer sess.conn.Close()
	defer sess.close()

	sess.challenge = newSubscribeChallenge(time.Now().UTC())
	sess.send(protocol.Message{Type: protocol.MessageTypeSubscribeChallenge, Challenge: sess.challenge})

	for {
		var msg protocol.Message
		if err := sess.conn.ReadJSON(&msg); err != nil {
			return
		}
		if err := msg.Validate(); err != nil {
			sess.server.logger.Warn("reject invalid websocket message", "error", err)
			sess.sendError(genericClientError)
			continue
		}
		_ = sess.handleMessage(msg)
	}
}

func (sess *wsSession) close() {
	if sess.mailbox != "" {
		sess.server.unregister(sess)
	}
	sess.server.logger.Info("client disconnected")
}

func (sess *wsSession) send(msg protocol.Message) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.conn == nil {
		return
	}
	_ = sess.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := sess.conn.WriteJSON(msg); err != nil {
		sess.server.logger.Info("write websocket message")
	}
}

func (sess *wsSession) sendError(message string) {
	sess.send(protocol.Message{Type: protocol.MessageTypeError, Error: &protocol.Error{Message: message}})
}

func (sess *wsSession) handleMessage(msg protocol.Message) error {
	switch msg.Type {
	case protocol.MessageTypeSubscribe:
		return sess.handleSubscribe(*msg.Subscribe)
	case protocol.MessageTypePublish:
		return sess.handlePublish(msg.Publish.Envelope)
	default:
		return nil
	}
}

func (sess *wsSession) handleSubscribe(req protocol.SubscribeRequest) error {
	now := time.Now().UTC()
	if !sess.server.allowRateLimit("subscribe:"+sess.remoteAddr, now) {
		sess.sendError("relay rate limit exceeded")
		return fmt.Errorf("subscribe rate limited")
	}
	if err := sess.server.verifySubscribeRequest(req, sess.challenge, now); err != nil {
		sess.server.logger.Warn("reject subscribe request", "mailbox", req.Mailbox, "error", err)
		sess.sendError(subscribeErrorMessage(err))
		sess.challenge = newSubscribeChallenge(now)
		sess.send(protocol.Message{Type: protocol.MessageTypeSubscribeChallenge, Challenge: sess.challenge})
		return err
	}
	if sess.mailbox != "" {
		sess.server.unregister(sess)
	}
	sess.challenge = nil
	sess.mailbox = req.Mailbox

	backlog, err := sess.server.register(sess)
	if err != nil {
		sess.server.logger.Warn("register subscriber", "mailbox", req.Mailbox, "error", err)
		sess.mailbox = ""
		sess.sendError(genericClientError)
		return err
	}
	sess.send(protocol.Message{Type: protocol.MessageTypeAck, Ack: &protocol.Ack{ID: sess.mailbox}})
	for _, envelope := range backlog {
		envelope := envelope
		sess.send(protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelope})
	}
	return nil
}

func (sess *wsSession) handlePublish(envelope protocol.Envelope) error {
	now := time.Now().UTC()
	if err := validateEnvelopeLimits(envelope, sess.server.options); err != nil {
		sess.server.logger.Warn("reject oversized envelope", "sender_mailbox", envelope.SenderMailbox, "recipient_mailbox", envelope.RecipientMailbox, "error", err)
		sess.sendError(genericClientError)
		return err
	}
	if !sess.server.allowRateLimit(envelope.SenderMailbox, now) {
		sess.sendError("relay rate limit exceeded")
		return fmt.Errorf("publish rate limited")
	}
	envelope = sess.server.finalizePublishedEnvelope(envelope, now)
	if err := sess.server.publish(envelope); err != nil {
		if errors.Is(err, ErrQueueFull) {
			sess.sendError("mailbox queue is full")
			return err
		}
		sess.server.logger.Warn("publish envelope", "sender_mailbox", envelope.SenderMailbox, "recipient_mailbox", envelope.RecipientMailbox, "error", err)
		sess.sendError(genericClientError)
		return err
	}
	sess.send(protocol.Message{Type: protocol.MessageTypeAck, Ack: &protocol.Ack{ID: envelope.ID}})
	return nil
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
	if err := s.verifyMailboxOwnership(req.Mailbox, signingPublic); err != nil {
		return err
	}
	if err := s.queue.AuthorizeMailbox(req.Mailbox, signingPublic); err != nil {
		return err
	}
	return nil
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
