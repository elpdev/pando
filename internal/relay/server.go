package relay

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/elpdev/chatui/internal/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Server struct {
	logger   *slog.Logger
	upgrader websocket.Upgrader

	mu        sync.Mutex
	mailboxes map[string]*mailbox
}

type mailbox struct {
	queue []protocol.Envelope
	subs  map[*subscriber]struct{}
}

type subscriber struct {
	conn    *websocket.Conn
	mailbox string
	mu      sync.Mutex
}

func NewServer(logger *slog.Logger) *Server {
	return &Server{
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		mailboxes: make(map[string]*mailbox),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("upgrade websocket", "error", err)
		return
	}

	defer conn.Close()

	var current *subscriber
	for {
		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			if current != nil {
				s.unregister(current)
			}
			s.logger.Info("client disconnected", "error", err)
			return
		}

		if err := msg.Validate(); err != nil {
			s.write(conn, protocol.Message{
				Type:  protocol.MessageTypeError,
				Error: &protocol.Error{Message: err.Error()},
			})
			continue
		}

		switch msg.Type {
		case protocol.MessageTypeSubscribe:
			if current != nil {
				s.unregister(current)
			}

			current = &subscriber{conn: conn, mailbox: msg.Subscribe.Mailbox}
			backlog := s.register(current)
			s.writeSubscriber(current, protocol.Message{
				Type: protocol.MessageTypeAck,
				Ack:  &protocol.Ack{ID: current.mailbox},
			})
			for _, envelope := range backlog {
				s.writeSubscriber(current, protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelope})
			}
		case protocol.MessageTypePublish:
			envelope := msg.Publish.Envelope
			envelope.ID = uuid.NewString()
			envelope.Timestamp = time.Now().UTC()
			s.publish(envelope)
			s.write(conn, protocol.Message{
				Type: protocol.MessageTypeAck,
				Ack:  &protocol.Ack{ID: envelope.ID},
			})
		}
	}
}

func (s *Server) register(sub *subscriber) []protocol.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()

	mb := s.getMailboxLocked(sub.mailbox)
	if mb.subs == nil {
		mb.subs = make(map[*subscriber]struct{})
	}
	mb.subs[sub] = struct{}{}
	backlog := append([]protocol.Envelope(nil), mb.queue...)
	mb.queue = nil
	return backlog
}

func (s *Server) unregister(sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mb, ok := s.mailboxes[sub.mailbox]
	if !ok {
		return
	}
	delete(mb.subs, sub)
	if len(mb.subs) == 0 && len(mb.queue) == 0 {
		delete(s.mailboxes, sub.mailbox)
	}
}

func (s *Server) publish(envelope protocol.Envelope) {
	s.mu.Lock()
	mb := s.getMailboxLocked(envelope.RecipientMailbox)
	subs := make([]*subscriber, 0, len(mb.subs))
	for sub := range mb.subs {
		subs = append(subs, sub)
	}
	if len(subs) == 0 {
		mb.queue = append(mb.queue, envelope)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	message := protocol.Message{Type: protocol.MessageTypeIncoming, Incoming: &envelope}
	for _, sub := range subs {
		s.writeSubscriber(sub, message)
	}
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
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(msg); err != nil {
		s.logger.Info("write websocket message", "error", err)
	}
}

func (s *Server) writeSubscriber(sub *subscriber, msg protocol.Message) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	s.write(sub.conn, msg)
}

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
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
