package relay

import (
	"context"
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
	if options.RateLimitPerMinute <= 0 {
		options.RateLimitPerMinute = 120
	}
	return &Server{
		logger:  logger,
		queue:   queue,
		options: options,
		limiter: newRateLimiter(options.RateLimitPerMinute),
		landing: template.Must(template.New("landing").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Pando Relay</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #070a0f;
      --panel: #0d1218;
      --hairline: rgba(124, 245, 214, 0.24);
      --phosphor: #d7fff2;
      --phosphor-dim: #88b8ad;
      --amber: #ffb54d;
      --signal: #ff6b57;
      --cyan: #7cf5d6;
      --shadow: rgba(0, 0, 0, 0.45);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      font-family: "IBM Plex Mono", "SFMono-Regular", ui-monospace, monospace;
      color: var(--phosphor);
      background:
        linear-gradient(180deg, rgba(10, 255, 194, 0.08), transparent 18%),
        radial-gradient(circle at top right, rgba(255, 181, 77, 0.14), transparent 30%),
        linear-gradient(180deg, #06080c 0%, #0b1016 100%);
      position: relative;
      overflow-x: hidden;
    }
    body::before {
      content: "";
      position: fixed;
      inset: 0;
      pointer-events: none;
      background: repeating-linear-gradient(
        to bottom,
        rgba(255,255,255,0.03) 0,
        rgba(255,255,255,0.03) 1px,
        transparent 1px,
        transparent 4px
      );
      opacity: 0.22;
    }
    .shell {
      width: min(1180px, calc(100vw - 28px));
      margin: 0 auto;
      padding: 28px 0 40px;
    }
    .masthead {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 18px;
      color: var(--phosphor-dim);
      font-size: 0.72rem;
      text-transform: uppercase;
      letter-spacing: 0.24em;
    }
    .frame {
      position: relative;
      border: 1px solid var(--hairline);
      background: linear-gradient(180deg, rgba(14, 19, 26, 0.94), rgba(9, 13, 18, 0.98));
      box-shadow: 0 24px 70px var(--shadow);
    }
    .frame::after {
      content: "";
      position: absolute;
      top: 14px;
      right: 14px;
      width: 18px;
      height: 18px;
      border-top: 2px solid var(--amber);
      border-right: 2px solid var(--amber);
      opacity: 0.75;
    }
    .hero {
      display: grid;
      grid-template-columns: minmax(0, 1.4fr) minmax(280px, 0.8fr);
      gap: 0;
    }
    .hero-copy {
      padding: 38px 34px 34px;
      border-right: 1px solid var(--hairline);
    }
    .kicker {
      display: inline-block;
      margin-bottom: 18px;
      padding: 8px 12px;
      border: 1px solid rgba(255, 181, 77, 0.45);
      background: rgba(255, 181, 77, 0.08);
      color: var(--amber);
      font-size: 0.72rem;
      letter-spacing: 0.22em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0;
      max-width: 10ch;
      font-family: Impact, Haettenschweiler, "Arial Narrow Bold", sans-serif;
      font-size: clamp(3.5rem, 10vw, 7.2rem);
      line-height: 0.92;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--phosphor);
      text-shadow: 0 0 24px rgba(124, 245, 214, 0.1);
    }
    .lede {
      max-width: 42rem;
      margin: 22px 0 0;
      font-size: 1rem;
      line-height: 1.8;
      color: var(--phosphor-dim);
    }
    .cta-row {
      display: flex;
      flex-wrap: wrap;
      gap: 12px;
      margin-top: 28px;
    }
    .chip {
      display: inline-flex;
      align-items: center;
      gap: 10px;
      padding: 11px 14px;
      border: 1px solid var(--hairline);
      color: var(--phosphor);
      text-decoration: none;
      text-transform: uppercase;
      letter-spacing: 0.16em;
      font-size: 0.72rem;
      background: rgba(124, 245, 214, 0.05);
    }
    .chip strong {
      color: var(--amber);
      font-weight: 700;
    }
    .chip:hover {
      border-color: rgba(124, 245, 214, 0.45);
      background: rgba(124, 245, 214, 0.1);
    }
    .hero-panel {
      display: flex;
      flex-direction: column;
      justify-content: space-between;
      min-height: 100%;
      padding: 22px;
      background:
        linear-gradient(180deg, rgba(124, 245, 214, 0.08), transparent 34%),
        rgba(6, 9, 13, 0.85);
    }
    .panel-label,
    .section-label {
      font-size: 0.68rem;
      text-transform: uppercase;
      letter-spacing: 0.24em;
      color: var(--phosphor-dim);
    }
    .status-box {
      margin-top: 16px;
      padding: 18px;
      border: 1px solid rgba(255, 107, 87, 0.3);
      background: linear-gradient(180deg, rgba(255, 107, 87, 0.1), rgba(255, 107, 87, 0.03));
    }
    .status-box strong {
      display: block;
      margin-bottom: 10px;
      color: #ffd8d2;
      font-size: 0.95rem;
      text-transform: uppercase;
      letter-spacing: 0.12em;
    }
    .status-box p {
      margin: 0;
      color: #f0c4bc;
      line-height: 1.7;
      font-size: 0.95rem;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 16px;
      margin-top: 16px;
    }
    .card {
      padding: 20px;
      border: 1px solid var(--hairline);
      background: rgba(10, 14, 19, 0.86);
    }
    .card h2 {
      margin: 0 0 10px;
      font-size: 1rem;
      text-transform: uppercase;
      letter-spacing: 0.14em;
      color: var(--phosphor);
    }
    .card p {
      margin: 0;
      color: var(--phosphor-dim);
      line-height: 1.7;
      font-size: 0.95rem;
    }
    .code-row {
      margin-top: 14px;
      padding: 12px 14px;
      border-left: 3px solid var(--cyan);
      background: rgba(124, 245, 214, 0.06);
      color: var(--phosphor);
      font-size: 0.82rem;
      overflow-x: auto;
    }
    code {
      font-family: inherit;
      color: var(--amber);
    }
    .footer {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      margin-top: 16px;
      padding: 18px 20px;
      border: 1px solid var(--hairline);
      background: rgba(8, 11, 15, 0.9);
      color: var(--phosphor-dim);
      font-size: 0.74rem;
      text-transform: uppercase;
      letter-spacing: 0.18em;
    }
    .footer a {
      color: var(--cyan);
      text-decoration: none;
    }
    .footer a:hover {
      color: var(--phosphor);
    }
    @media (max-width: 860px) {
      .hero {
        grid-template-columns: 1fr;
      }
      .hero-copy {
        border-right: 0;
        border-bottom: 1px solid var(--hairline);
      }
      .grid {
        grid-template-columns: 1fr;
      }
      .footer,
      .masthead {
        flex-direction: column;
      }
    }
  </style>
</head>
<body>
  <div class="shell">
    <div class="masthead">
      <span>Pando Relay Node</span>
      <span>Encrypted transport / live mailbox broker / terminal native</span>
    </div>

    <section class="frame hero">
      <div class="hero-copy">
        <div class="kicker">Retro-futurist relay surface</div>
        <h1>Ship Signal Through The Static.</h1>
        <p class="lede">Pando Relay is the network edge for encrypted terminal-native messaging. This host accepts WebSocket traffic, queues offline mail, and keeps peer delivery moving through a hardened low-friction transport layer.</p>
        <div class="cta-row">
          <div class="chip"><strong>WS</strong> <code>/ws</code></div>
          <div class="chip"><strong>UP</strong> <code>/up</code></div>
          <div class="chip"><strong>MODE</strong> public relay</div>
          <a class="chip" href="https://github.com/elpdev/pando" target="_blank" rel="noreferrer"><strong>GH</strong> source</a>
        </div>
      </div>

      <aside class="hero-panel">
        <div>
          <div class="panel-label">Relay status</div>
          <div class="status-box">
            <strong>Online / accepting traffic</strong>
            <p>Use the health endpoint for probes and connect clients over the websocket transport. Root is now reserved for this landing surface instead of a blank proxy-style 404.</p>
          </div>
        </div>

        <div class="code-row"><code>wss://pandorelay.network/ws</code></div>
      </aside>
    </section>

    <section class="grid">
      <article class="card">
        <div class="section-label">Transport</div>
        <h2>WebSocket ingress</h2>
        <p>Encrypted envelope traffic enters through a persistent websocket endpoint designed for durable relay handoff and reconnect-friendly clients.</p>
        <div class="code-row"><code>GET /ws</code></div>
      </article>

      <article class="card">
        <div class="section-label">Health</div>
        <h2>Probe friendly</h2>
        <p>Operational checks should target the minimal health endpoint so deploy orchestration and tunnels can verify that the relay is actually live.</p>
        <div class="code-row"><code>GET /up</code></div>
      </article>

      <article class="card">
        <div class="section-label">Queue</div>
        <h2>Mailbox buffering</h2>
        <p>Offline delivery stays durable through the on-disk relay store, allowing clients to reconnect and drain pending messages when they come back online.</p>
        <div class="code-row"><code>/storage/relay.db</code></div>
      </article>
    </section>

    <div class="footer">
      <span>Built for terminal-native encrypted messaging</span>
      <span><a href="https://github.com/elpdev/pando" target="_blank" rel="noreferrer">github.com/elpdev/pando</a></span>
    </div>
  </div>
</body>
</html>`)),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		mailboxes: make(map[string]*mailbox),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleLanding)
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/up", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
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
			now := time.Now().UTC()
			if err := validateEnvelopeLimits(envelope, s.options); err != nil {
				s.write(conn, protocol.Message{Type: protocol.MessageTypeError, Error: &protocol.Error{Message: err.Error()}})
				continue
			}
			if !s.limiter.Allow(envelope.SenderMailbox, now) {
				s.write(conn, protocol.Message{Type: protocol.MessageTypeError, Error: &protocol.Error{Message: "relay rate limit exceeded for sender mailbox"}})
				continue
			}
			envelope.ID = uuid.NewString()
			envelope.Timestamp = now
			envelope.ExpiresAt = now.Add(s.options.QueueTTL)
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
