package relay

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/gorilla/websocket"
)

func TestQueuedMessageDeliveredOnSubscribe(t *testing.T) {
	server := newTestServer(t)

	publisher := dialTestConn(t, server)
	defer publisher.Close()
	_ = readChallenge(t, publisher)
	bobIdentity := mustIdentity(t, "bob")
	publishDirectoryEntry(t, server, bobIdentity)

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
	publishDirectoryEntry(t, server, bobIdentity)
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
	publishDirectoryEntry(t, server, bobIdentity)
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
	publishDirectoryEntry(t, server, bobIdentity)
	publishDirectoryEntry(t, server, malloryIdentity)

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
	if msg.Error.Message != ErrMailboxNotAuthorized.Error() {
		t.Fatalf("expected mailbox authorization error, got %q", msg.Error.Message)
	}
	challengeMsg := readMessage(t, second)
	if challengeMsg.Type != protocol.MessageTypeSubscribeChallenge || challengeMsg.Challenge == nil {
		t.Fatalf("expected replacement challenge, got %+v", challengeMsg)
	}
}

func TestSubscribeRejectsReplayAcrossConnections(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")
	publishDirectoryEntry(t, server, bobIdentity)

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
	publishDirectoryEntry(t, server, bobIdentity)
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
	publishDirectoryEntry(t, server, bobIdentity)
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

func TestSubscribeRejectsMailboxWithoutPublishedDirectoryEntry(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")
	conn := dialTestConn(t, server)
	defer conn.Close()
	challenge := readChallenge(t, conn)
	writeMessage(t, conn, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobIdentity, challenge)})
	msg := readMessage(t, conn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected unpublished mailbox rejection, got %+v", msg)
	}
	if msg.Error.Message != ErrMailboxNotPublished.Error() {
		t.Fatalf("expected unpublished mailbox error, got %q", msg.Error.Message)
	}
}

func TestSubscribeRejectsRevokedDeviceMailbox(t *testing.T) {
	server := newTestServer(t)
	bobIdentity := mustIdentity(t, "bob")
	pending, err := identity.NewPendingEnrollment("bob", "bob-phone")
	if err != nil {
		t.Fatalf("new pending enrollment: %v", err)
	}
	approval, err := bobIdentity.Approve(pending.Request())
	if err != nil {
		t.Fatalf("approve enrollment: %v", err)
	}
	bobPhone, err := pending.Complete(*approval)
	if err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}
	if err := bobIdentity.RevokeDevice("bob-phone"); err != nil {
		t.Fatalf("revoke device: %v", err)
	}
	publishDirectoryEntry(t, server, bobIdentity)
	conn := dialTestConn(t, server)
	defer conn.Close()
	challenge := readChallenge(t, conn)
	writeMessage(t, conn, protocol.Message{Type: protocol.MessageTypeSubscribe, Subscribe: subscribeRequest(t, bobPhone, challenge)})
	msg := readMessage(t, conn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil {
		t.Fatalf("expected revoked device rejection, got %+v", msg)
	}
	if msg.Error.Message != ErrMailboxNotPublished.Error() {
		t.Fatalf("expected revoked mailbox to look unpublished, got %q", msg.Error.Message)
	}
}

func TestDirectoryEntryRoundTripOverHTTP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()
	alice := mustIdentity(t, "alice")
	client, err := relayapi.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "secret")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: "alice", Bundle: alice.InviteBundle(), Discoverable: true, PublishedAt: time.Now().UTC(), Version: 1}, alice.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	if _, err := client.PublishDirectoryEntry(*signed); err != nil {
		t.Fatalf("publish directory entry: %v", err)
	}
	loaded, err := client.LookupDirectoryEntry("alice")
	if err != nil {
		t.Fatalf("lookup directory entry: %v", err)
	}
	if err := relayapi.VerifySignedDirectoryEntry(*loaded); err != nil {
		t.Fatalf("verify signed directory entry: %v", err)
	}
	if loaded.Entry.Bundle.AccountID != "alice" {
		t.Fatalf("expected alice directory entry, got %+v", loaded)
	}
	if !loaded.Entry.Discoverable {
		t.Fatalf("expected discoverable directory entry, got %+v", loaded)
	}
}

func TestDiscoverableDirectoryListingAndDeviceLookupOverHTTP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()
	alice := mustIdentity(t, "alice")
	bob := mustIdentity(t, "bob")
	client, err := relayapi.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "secret")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	aliceEntry, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: "alice", Bundle: alice.InviteBundle(), Discoverable: true, PublishedAt: time.Now().UTC(), Version: 1}, alice.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign alice entry: %v", err)
	}
	bobEntry, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: "bob", Bundle: bob.InviteBundle(), PublishedAt: time.Now().UTC(), Version: 1}, bob.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign bob entry: %v", err)
	}
	if _, err := client.PublishDirectoryEntry(*aliceEntry); err != nil {
		t.Fatalf("publish alice directory entry: %v", err)
	}
	if _, err := client.PublishDirectoryEntry(*bobEntry); err != nil {
		t.Fatalf("publish bob directory entry: %v", err)
	}
	entries, err := client.ListDiscoverableEntries()
	if err != nil {
		t.Fatalf("list discoverable entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Entry.Mailbox != "alice" {
		t.Fatalf("expected only discoverable alice entry, got %+v", entries)
	}
	device, err := alice.CurrentDevice()
	if err != nil {
		t.Fatalf("alice current device: %v", err)
	}
	loaded, err := client.LookupDirectoryEntryByDeviceMailbox(device.Mailbox)
	if err != nil {
		t.Fatalf("lookup directory entry by device mailbox: %v", err)
	}
	if loaded.Entry.Mailbox != "alice" {
		t.Fatalf("expected alice directory entry from device lookup, got %+v", loaded)
	}
}

func TestRendezvousPayloadRoundTripOverHTTP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()
	baseURL := strings.TrimRight(server.URL, "/")
	payload := relayapi.PutRendezvousRequest{Payload: relayapi.RendezvousPayload{Ciphertext: base64.StdEncoding.EncodeToString([]byte("ciphertext")), Nonce: base64.StdEncoding.EncodeToString([]byte("0123456789ab")), CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+"/rendezvous/test-room", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(authHeader, "secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put rendezvous payload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected no content, got %s", resp.Status)
	}
	getReq, err := http.NewRequest(http.MethodGet, baseURL+"/rendezvous/test-room", nil)
	if err != nil {
		t.Fatalf("build get request: %v", err)
	}
	getReq.Header.Set(authHeader, "secret")
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("get rendezvous payload: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected ok, got %s", getResp.Status)
	}
	var response relayapi.GetRendezvousResponse
	if err := json.NewDecoder(getResp.Body).Decode(&response); err != nil {
		t.Fatalf("decode rendezvous response: %v", err)
	}
	if len(response.Payloads) != 1 {
		t.Fatalf("expected one payload, got %+v", response.Payloads)
	}
}

func TestDirectoryEntryRejectsDetailedValidationErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()
	entry := relayapi.SignedDirectoryEntry{Entry: relayapi.DirectoryEntry{Mailbox: "alice"}}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal directory entry: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/directory/mailboxes/alice", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(authHeader, "secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put directory entry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", resp.Status)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Message != "invalid directory entry" {
		t.Fatalf("expected generic directory error, got %q", payload.Message)
	}
}

func TestRendezvousRejectsDetailedValidationErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()
	body, err := json.Marshal(relayapi.PutRendezvousRequest{Payload: relayapi.RendezvousPayload{Ciphertext: "", Nonce: "", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}})
	if err != nil {
		t.Fatalf("marshal rendezvous payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, server.URL+"/rendezvous/test-room", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(authHeader, "secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put rendezvous payload: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %s", resp.Status)
	}
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Message != "invalid rendezvous payload" {
		t.Fatalf("expected generic rendezvous error, got %q", payload.Message)
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

func publishDirectoryEntry(t *testing.T, server *httptest.Server, id *identity.Identity) {
	t.Helper()
	client, err := relayapi.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	signed, err := relayapi.SignDirectoryEntry(relayapi.DirectoryEntry{Mailbox: id.AccountID, Bundle: id.InviteBundle(), Discoverable: true, PublishedAt: time.Now().UTC(), Version: time.Now().UTC().UnixNano()}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	if _, err := client.PublishDirectoryEntry(*signed); err != nil {
		t.Fatalf("publish directory entry: %v", err)
	}
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
