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
	_ = readChallenge(t, publisher)
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
	challenge := readChallenge(t, subscriber)
	writeMessage(t, subscriber, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, challenge)})

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
	challenge := readChallenge(t, subscriber)
	writeMessage(t, subscriber, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, challenge)})
	readMessage(t, subscriber)

	publisher := dialTestConn(t, server)
	defer publisher.Close()
	_ = readChallenge(t, publisher)

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
	challenge := readChallenge(t, subscriber)
	writeMessage(t, subscriber, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, challenge)})
	readMessage(t, subscriber)

	publisher := dialTestConn(t, server)
	defer publisher.Close()
	_ = readChallenge(t, publisher)

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
	firstChallenge := readChallenge(t, first)
	writeMessage(t, first, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, firstChallenge)})
	msg := readMessage(t, first)
	if msg.Type != protocol.MessageTypeAck {
		t.Fatalf("expected first subscribe ack, got %q", msg.Type)
	}

	second := dialTestConn(t, server)
	defer second.Close()
	secondChallenge := readChallenge(t, second)
	writeMessage(t, second, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequestForMailbox(t, malloryIdentity, "bob", secondChallenge)})
	msg = readMessage(t, second)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected subscribe rejection, got %+v", msg)
	}
	if msg.Error.Message != genericClientError {
		t.Fatalf("expected generic error, got %q", msg.Error.Message)
	}
	challengeMsg := readMessage(t, second)
	if challengeMsg.Type != protocol.MessageTypeSubscribeChallenge || challengeMsg.Challenge == nil {
		t.Fatalf("expected replacement challenge, got %+v", challengeMsg)
	}
}

func TestSubscribeRejectsReplayAcrossConnections(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")

	first := dialTestConn(t, server)
	defer first.Close()
	challenge := readChallenge(t, first)
	req := subscribeRequest(t, bobIdentity, challenge)
	writeMessage(t, first, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: req})
	msg := readMessage(t, first)
	if msg.Type != protocol.MessageTypeAck {
		t.Fatalf("expected first subscribe ack, got %q", msg.Type)
	}

	second := dialTestConn(t, server)
	defer second.Close()
	_ = readChallenge(t, second)
	writeMessage(t, second, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: req})
	msg = readMessage(t, second)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected replay rejection, got %+v", msg)
	}
}

func TestSubscribeRejectsExpiredChallenge(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")
	conn := dialTestConn(t, server)
	defer conn.Close()
	challenge := readChallenge(t, conn)
	challenge.ExpiresAt = time.Now().UTC().Add(-time.Second)
	writeMessage(t, conn, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, challenge)})
	msg := readMessage(t, conn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected expired challenge rejection, got %+v", msg)
	}
}

func TestSubscribeRejectsInvalidAttemptWithoutClaimingMailbox(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")
	conn := dialTestConn(t, server)
	defer conn.Close()
	challenge := readChallenge(t, conn)
	req := subscribeRequest(t, bobIdentity, challenge)
	req.DeviceProof = base64.StdEncoding.EncodeToString([]byte("bad-proof"))
	writeMessage(t, conn, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: req})
	msg := readMessage(t, conn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected invalid proof rejection, got %+v", msg)
	}
	retryChallenge := readChallenge(t, conn)
	writeMessage(t, conn, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, retryChallenge)})
	msg = readMessage(t, conn)
	if msg.Type != protocol.MessageTypeAck {
		t.Fatalf("expected mailbox to remain claimable after failed attempt, got %+v", msg)
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

func readChallenge(t *testing.T, conn *websocket.Conn) *protocol.SubscribeChallenge {
	t.Helper()
	msg := readMessage(t, conn)
	if msg.Type != protocol.MessageTypeSubscribeChallenge || msg.Challenge == nil {
		t.Fatalf("expected subscribe challenge, got %+v", msg)
	}
	return msg.Challenge
}

func subscribeRequest(t *testing.T, id *identity.Identity, challenge *protocol.SubscribeChallenge) *protocol.SubscribeRequest {
	t.Helper()
	device, err := id.CurrentDevice()
	if err != nil {
		t.Fatalf("current device: %v", err)
	}
	return subscribeRequestForMailbox(t, id, device.Mailbox, challenge)
}

func subscribeRequestForMailbox(t *testing.T, id *identity.Identity, mailbox string, challenge *protocol.SubscribeChallenge) *protocol.SubscribeRequest {
	t.Helper()
	device, err := id.CurrentDevice()
	if err != nil {
		t.Fatalf("current device: %v", err)
	}
	return &protocol.SubscribeRequest{
		Mailbox:            mailbox,
		DeviceSigningKey:   base64.StdEncoding.EncodeToString(device.SigningPublic),
		DeviceProof:        base64.StdEncoding.EncodeToString(ed25519.Sign(device.SigningPrivate, protocol.SubscribeProofBytes(mailbox, challenge.Nonce, challenge.ExpiresAt))),
		ChallengeNonce:     challenge.Nonce,
		ChallengeExpiresAt: challenge.ExpiresAt,
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (n int, err error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}
