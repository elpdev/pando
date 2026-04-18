package relay

import (
	"context"
	"fmt"
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
	queue    QueueStore

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

func NewServer(logger *slog.Logger, queue QueueStore) *Server {
	if queue == nil {
		queue = NewMemoryQueueStore()
	}
	return &Server{
		logger: logger,
		queue:  queue,
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
			backlog, err := s.register(current)
			if err != nil {
				s.write(conn, protocol.Message{Type: protocol.MessageTypeError, Error: &protocol.Error{Message: err.Error()}})
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
			envelope.ID = uuid.NewString()
			envelope.Timestamp = time.Now().UTC()
			if err := s.publish(envelope); err != nil {
				s.write(conn, protocol.Message{Type: protocol.MessageTypeError, Error: &protocol.Error{Message: err.Error()}})
				continue
			}
			s.write(conn, protocol.Message{
				Type: protocol.MessageTypeAck,
				Ack:  &protocol.Ack{ID: envelope.ID},
			})
		}
	}
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
