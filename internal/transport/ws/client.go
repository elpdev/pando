package ws

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayclient"
	"github.com/elpdev/pando/internal/transport"
	"github.com/gorilla/websocket"
)

const authHeader = "X-Pando-Relay-Token"
const unpublishedMailboxError = "publish your signed relay directory entry before connecting"

type Client struct {
	url     string
	token   string
	mailbox string
	device  *identity.Device
	options relayclient.ClientOptions

	mu     sync.Mutex
	conn   *websocket.Conn
	events chan transport.Event
	closed bool
}

func NewClient(url, token string, id *identity.Identity, options relayclient.ClientOptions) *Client {
	var device *identity.Device
	if id != nil {
		device, _ = id.CurrentDevice()
	}
	mailbox := ""
	if device != nil {
		mailbox = device.Mailbox
	}
	return &Client{
		url:     url,
		token:   token,
		mailbox: mailbox,
		device:  device,
		options: options,
		events:  make(chan transport.Event, 32),
	}
}

func (c *Client) Connect(ctx context.Context) error {
	tlsConfig, err := relayclient.TLSConfigForURL(c.url, c.options)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, TLSClientConfig: tlsConfig}
	headers := http.Header{}
	if c.token != "" {
		headers.Set(authHeader, c.token)
	}
	conn, resp, err := dialer.DialContext(ctx, c.url, headers)
	if err != nil {
		if errors.Is(err, websocket.ErrBadHandshake) && resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: check relay token", transport.ErrUnauthorized)
		}
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
	challenge, err := c.readChallenge(conn)
	if err != nil {
		_ = conn.Close()
		return err
	}

	if err := c.write(protocol.Message{
		Type: protocol.MessageTypeSubscribe,
		Subscribe: &protocol.SubscribeRequest{
			Mailbox:            c.mailbox,
			DeviceSigningKey:   c.deviceSigningKey(),
			DeviceProof:        c.deviceProof(challenge),
			ChallengeNonce:     challenge.Nonce,
			ChallengeExpiresAt: challenge.ExpiresAt,
		},
	}); err != nil {
		_ = conn.Close()
		return err
	}
	if err := c.readSubscribeAck(conn); err != nil {
		_ = conn.Close()
		if err.Error() == unpublishedMailboxError {
			return fmt.Errorf("%s: run `pando contact publish-directory --mailbox %s`", err.Error(), c.mailbox)
		}
		return err
	}

	go c.readLoop()
	return nil
}

func (c *Client) deviceSigningKey() string {
	if c.device == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(c.device.SigningPublic)
}

func (c *Client) deviceProof(challenge *protocol.SubscribeChallenge) string {
	if c.device == nil || challenge == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(c.device.SigningPrivate, protocol.SubscribeProofBytes(c.mailbox, challenge.Nonce, challenge.ExpiresAt)))
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

func (c *Client) Disconnect() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
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
			activeConn := c.conn
			if activeConn == conn {
				c.conn = nil
			}
			closed := c.closed
			c.mu.Unlock()
			if activeConn != conn || closed {
				return
			}
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

func (c *Client) readChallenge(conn *websocket.Conn) (*protocol.SubscribeChallenge, error) {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		return nil, err
	}
	if msg.Type != protocol.MessageTypeSubscribeChallenge || msg.Challenge == nil {
		return nil, fmt.Errorf("expected subscribe challenge, got %q", msg.Type)
	}
	if err := msg.Validate(); err != nil {
		return nil, err
	}
	_ = conn.SetReadDeadline(time.Time{})
	return msg.Challenge, nil
}

func (c *Client) readSubscribeAck(conn *websocket.Conn) error {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer func() {
		_ = conn.SetReadDeadline(time.Time{})
	}()
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		return err
	}
	if msg.Type == protocol.MessageTypeError && msg.Error != nil {
		return errors.New(msg.Error.Message)
	}
	if msg.Type != protocol.MessageTypeAck || msg.Ack == nil {
		return fmt.Errorf("expected subscribe ack, got %q", msg.Type)
	}
	return nil
}
