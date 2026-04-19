package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestQueuedMessageDeliveredOnSubscribe(t *testing.T) {
	server := newTestServer(t)

	publisher := dialTestConn(t, server)
	defer publisher.Close()
	bobIdentity := mustIdentity(t, "bob")

	writeMessage(t, publisher, protocol.Message{
		Type: protocol.MessageTypePublish,
		Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{
			SenderMailbox:    "alice",
			RecipientMailbox: "bob",
			Body:             "queued hello",
		}},
	})
	readMessage(t, publisher)

	subscriber := dialTestConn(t, server)
	defer subscriber.Close()

	writeMessage(t, subscriber, protocol.Message{
		Type:      protocol.MessageTypeSubscribe,
		Subscribe: subscribeRequest(t, bobIdentity),
	})

	ack := readMessage(t, subscriber)
	if ack.Type != protocol.MessageTypeAck {
		t.Fatalf("expected ack, got %q", ack.Type)
	}

	incoming := readMessage(t, subscriber)
	if incoming.Type != protocol.MessageTypeIncoming {
		t.Fatalf("expected incoming, got %q", incoming.Type)
	}
	if incoming.Incoming == nil || incoming.Incoming.Body != "queued hello" {
		t.Fatalf("unexpected incoming payload: %+v", incoming.Incoming)
	}
}

func TestLiveMessageDeliveredToSubscriber(t *testing.T) {
	server := newTestServer(t)

	subscriber := dialTestConn(t, server)
	defer subscriber.Close()
	bobIdentity := mustIdentity(t, "bob")

	writeMessage(t, subscriber, protocol.Message{
		Type:      protocol.MessageTypeSubscribe,
		Subscribe: subscribeRequest(t, bobIdentity),
	})
	readMessage(t, subscriber)

	publisher := dialTestConn(t, server)
	defer publisher.Close()

	writeMessage(t, publisher, protocol.Message{
		Type: protocol.MessageTypePublish,
		Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{
			SenderMailbox:    "alice",
			RecipientMailbox: "bob",
			Body:             "live hello",
		}},
	})
	readMessage(t, publisher)

	incoming := readMessage(t, subscriber)
	if incoming.Type != protocol.MessageTypeIncoming {
		t.Fatalf("expected incoming, got %q", incoming.Type)
	}
	if incoming.Incoming == nil || incoming.Incoming.Body != "live hello" {
		t.Fatalf("unexpected incoming payload: %+v", incoming.Incoming)
	}
}

func TestSubscriberPublishAckDoesNotBlockIncomingDelivery(t *testing.T) {
	server := newTestServer(t)

	subscriber := dialTestConn(t, server)
	defer subscriber.Close()
	_ = subscriber.SetReadDeadline(time.Now().Add(10 * time.Second))
	bobIdentity := mustIdentity(t, "bob")

	writeMessage(t, subscriber, protocol.Message{
		Type:      protocol.MessageTypeSubscribe,
		Subscribe: subscribeRequest(t, bobIdentity),
	})
	readMessage(t, subscriber)

	publisher := dialTestConn(t, server)
	defer publisher.Close()

	const total = 48
	done := make(chan error, 1)
	go func() {
		for i := 0; i < total; i++ {
			writeMessage(t, publisher, protocol.Message{
				Type: protocol.MessageTypePublish,
				Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{
					SenderMailbox:    "alice",
					RecipientMailbox: "bob",
					Body:             "chunk",
				}},
			})
			msg := readMessage(t, publisher)
			if msg.Type != protocol.MessageTypeAck {
				done <- &unexpectedMessageTypeError{got: msg.Type, want: protocol.MessageTypeAck}
				return
			}
		}
		done <- nil
	}()

	acks := 0
	incoming := 0
	for acks < total || incoming < total {
		msg := readMessage(t, subscriber)
		switch msg.Type {
		case protocol.MessageTypeIncoming:
			incoming++
			writeMessage(t, subscriber, protocol.Message{
				Type: protocol.MessageTypePublish,
				Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{
					SenderMailbox:    "bob",
					RecipientMailbox: "alice",
					Body:             "ack",
				}},
			})
		case protocol.MessageTypeAck:
			acks++
		default:
			t.Fatalf("unexpected message type %q", msg.Type)
		}
	}

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeRejectsMailboxClaimConflict(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")
	malloryIdentity := mustIdentity(t, "mallory")

	first := dialTestConn(t, server)
	defer first.Close()
	writeMessage(t, first, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity)})
	msg := readMessage(t, first)
	if msg.Type != protocol.MessageTypeAck {
		t.Fatalf("expected first subscribe ack, got %q", msg.Type)
	}

	second := dialTestConn(t, server)
	defer second.Close()
	writeMessage(t, second, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: &protocol.SubscribeRequest{
		Mailbox:          "bob",
		DeviceSigningKey: base64.StdEncoding.EncodeToString(malloryIdentity.Devices[0].SigningPublic),
		DeviceProof:      base64.StdEncoding.EncodeToString(ed25519.Sign(malloryIdentity.Devices[0].SigningPrivate, protocol.SubscribeProofBytes("bob"))),
	}})
	msg = readMessage(t, second)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected subscribe rejection, got %+v", msg)
	}
	if msg.Error.Message != genericClientError {
		t.Fatalf("expected generic error, got %q", msg.Error.Message)
	}
}

type unexpectedMessageTypeError struct {
	got  string
	want string
}

func (e *unexpectedMessageTypeError) Error() string {
	return "unexpected message type: got " + e.got + " want " + e.want
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{}).Handler())
	t.Cleanup(server.Close)
	return server
}

func dialTestConn(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	return conn
}

func writeMessage(t *testing.T, conn *websocket.Conn, msg protocol.Message) {
	t.Helper()
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write websocket message: %v", err)
	}
}

func readMessage(t *testing.T, conn *websocket.Conn) protocol.Message {
	t.Helper()
	var msg protocol.Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read websocket message: %v", err)
	}
	return msg
}

func mustIdentity(t *testing.T, mailbox string) *identity.Identity {
	t.Helper()
	id, err := identity.New(mailbox)
	if err != nil {
		t.Fatalf("new identity %s: %v", mailbox, err)
	}
	return id
}

func subscribeRequest(t *testing.T, id *identity.Identity) *protocol.SubscribeRequest {
	t.Helper()
	device, err := id.CurrentDevice()
	if err != nil {
		t.Fatalf("current device: %v", err)
	}
	return &protocol.SubscribeRequest{
		Mailbox:          device.Mailbox,
		DeviceSigningKey: base64.StdEncoding.EncodeToString(device.SigningPublic),
		DeviceProof:      base64.StdEncoding.EncodeToString(ed25519.Sign(device.SigningPrivate, protocol.SubscribeProofBytes(device.Mailbox))),
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (n int, err error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}
