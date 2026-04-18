package ws

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/elpdev/chatui/internal/protocol"
	"github.com/elpdev/chatui/internal/transport"
	"github.com/gorilla/websocket"
)

type Client struct {
	url     string
	mailbox string

	mu     sync.Mutex
	conn   *websocket.Conn
	events chan transport.Event
	closed bool
}

func NewClient(url, mailbox string) *Client {
	return &Client{
		url:     url,
		mailbox: mailbox,
		events:  make(chan transport.Event, 32),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment}
	conn, _, err := dialer.DialContext(ctx, c.url, nil)
	if err != nil {
		return err
	}

	c.mu.Lock()
	previousConn := c.conn
	c.conn = conn
	c.closed = false
	c.mu.Unlock()
	if previousConn != nil {
		_ = previousConn.Close()
	}

	if err := c.write(protocol.Message{
		Type: protocol.MessageTypeSubscribe,
		Subscribe: &protocol.SubscribeRequest{
			Mailbox: c.mailbox,
		},
	}); err != nil {
		_ = conn.Close()
		return err
	}

	go c.readLoop()
	return nil
}

func (c *Client) Events() <-chan transport.Event {
	return c.events
}

func (c *Client) Send(envelope protocol.Envelope) error {
	return c.write(protocol.Message{
		Type: protocol.MessageTypePublish,
		Publish: &protocol.PublishRequest{
			Envelope: envelope,
		},
	})
}

func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	close(c.events)
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (c *Client) readLoop() {
	for {
		c.mu.Lock()
		conn := c.conn
		closed := c.closed
		c.mu.Unlock()

		if closed || conn == nil {
			return
		}

		var msg protocol.Message
		if err := conn.ReadJSON(&msg); err != nil {
			c.mu.Lock()
			if c.conn == conn {
				c.conn = nil
			}
			c.mu.Unlock()
			c.sendEvent(transport.Event{Err: err})
			return
		}
		c.sendEvent(transport.Event{Message: &msg})
	}
}

func (c *Client) write(msg protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.conn == nil {
		return fmt.Errorf("websocket is not connected")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteJSON(msg)
}

func (c *Client) sendEvent(event transport.Event) {
	defer func() {
		_ = recover()
	}()
	c.events <- event
}
