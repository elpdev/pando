package relay

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/relayapi"
)

func TestSubscribeErrorMessageUsesSentinelErrors(t *testing.T) {
	if got := subscribeErrorMessage(nil); got != genericClientError {
		t.Fatalf("expected generic error for nil, got %q", got)
	}
	if got := subscribeErrorMessage(errors.Join(ErrMailboxNotPublished, errors.New("wrapped"))); got != ErrMailboxNotPublished.Error() {
		t.Fatalf("expected published sentinel error, got %q", got)
	}
	if got := subscribeErrorMessage(errors.Join(ErrMailboxNotAuthorized, errors.New("wrapped"))); got != ErrMailboxNotAuthorized.Error() {
		t.Fatalf("expected authorized sentinel error, got %q", got)
	}
	if got := subscribeErrorMessage(errors.New("boom")); got != genericClientError {
		t.Fatalf("expected generic fallback error, got %q", got)
	}
}

func TestValidateRendezvousPayloadRejectsInvalidInputs(t *testing.T) {
	now := time.Now().UTC()
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Nonce: "nonce", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}, now, 1024); err == nil || err.Error() != "rendezvous ciphertext is required" {
		t.Fatalf("expected ciphertext error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: "cipher", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}, now, 1024); err == nil || err.Error() != "rendezvous nonce is required" {
		t.Fatalf("expected nonce error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}, now, 1024); err == nil || err.Error() != "rendezvous created_at is required" {
		t.Fatalf("expected created_at error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", CreatedAt: now}, now, 1024); err == nil || err.Error() != "rendezvous expires_at is required" {
		t.Fatalf("expected expires_at error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", CreatedAt: now, ExpiresAt: now.Add(rendezvousTTL + time.Second)}, now, 1024); err == nil || err.Error() != "rendezvous expiry exceeds maximum TTL" {
		t.Fatalf("expected ttl error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", CreatedAt: now, ExpiresAt: now.Add(-time.Second)}, now, 1024); err == nil || err.Error() != "rendezvous payload is already expired" {
		t.Fatalf("expected expired payload error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: strings.Repeat("c", 100), Nonce: strings.Repeat("n", 100), CreatedAt: now, ExpiresAt: now.Add(time.Minute)}, now, 50); err == nil || err.Error() != "rendezvous payload exceeds relay size limit" {
		t.Fatalf("expected size error, got %v", err)
	}
	if err := validateRendezvousPayload(relayapi.RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", CreatedAt: now, ExpiresAt: now.Add(time.Minute)}, now, 1024); err != nil {
		t.Fatalf("expected valid payload, got %v", err)
	}
}

func TestServerCheckOriginAllowsConfiguredOriginAndRejectsMalformedOrigin(t *testing.T) {
	server := NewServer(testLogger(t), NewMemoryQueueStore(), Options{AllowedOrigins: []string{"https://app.example"}})
	allowed := httptest.NewRequest(http.MethodGet, "http://relay.example/ws", nil)
	allowed.Host = "relay.example"
	allowed.Header.Set("Origin", "https://app.example")
	if !server.checkOrigin(allowed) {
		t.Fatal("expected configured origin to be allowed")
	}

	malformed := httptest.NewRequest(http.MethodGet, "http://relay.example/ws", nil)
	malformed.Host = "relay.example"
	malformed.Header.Set("Origin", "://bad-origin")
	if server.checkOrigin(malformed) {
		t.Fatal("expected malformed origin to be rejected")
	}
}

func TestRelayHandlersCoverLandingHealthAndDirectoryNotFound(t *testing.T) {
	server := httptest.NewServer(NewServer(testLogger(t), NewMemoryQueueStore(), Options{LandingPage: true, AuthToken: "secret"}).Handler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get landing page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected landing page ok, got %s", resp.Status)
	}

	resp, err = http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected health ok, got %s", resp.Status)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/directory/mailboxes/alice", nil)
	if err != nil {
		t.Fatalf("build directory request: %v", err)
	}
	req.Header.Set(authHeader, "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get missing directory entry: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected directory not found, got %s", resp.Status)
	}
}

func TestRelayRendezvousDeleteAndSlotFullOverHTTP(t *testing.T) {
	logger := testLogger(t)
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()
	client, err := relayapi.NewClient("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", "secret")
	if err != nil {
		t.Fatalf("new relay api client: %v", err)
	}
	payload := relayapi.RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}
	if err := client.PutRendezvousPayload("room", payload); err != nil {
		t.Fatalf("put rendezvous payload 1: %v", err)
	}
	if err := client.PutRendezvousPayload("room", payload); err != nil {
		t.Fatalf("put rendezvous payload 2: %v", err)
	}
	err = client.PutRendezvousPayload("room", payload)
	if err == nil || !strings.Contains(err.Error(), "rendezvous slot is full") {
		t.Fatalf("expected slot full error, got %v", err)
	}
	if err := client.DeleteRendezvous("room"); err != nil {
		t.Fatalf("delete rendezvous payloads: %v", err)
	}
	payloads, err := client.GetRendezvousPayloads("room")
	if err != nil {
		t.Fatalf("get rendezvous payloads after delete: %v", err)
	}
	if len(payloads) != 0 {
		t.Fatalf("expected no rendezvous payloads, got %+v", payloads)
	}
}

func TestWebSocketPublishRejectsOversizeRateLimitAndQueueFull(t *testing.T) {
	oversizedServer := httptest.NewServer(NewServer(testLogger(t), NewMemoryQueueStore(), Options{MaxMessageBytes: 8, MaxQueuedMessages: 10, MaxQueuedBytes: 1024, QueueTTL: time.Minute, RateLimitPerMinute: 10}).Handler())
	defer oversizedServer.Close()
	conn := dialTestConn(t, oversizedServer)
	defer conn.Close()
	_ = readChallenge(t, conn)
	writeMessage(t, conn, protocol.Message{Type: protocol.MessageTypePublish, Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: strings.Repeat("x", 32)}}})
	msg := readMessage(t, conn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil || msg.Error.Message != genericClientError {
		t.Fatalf("expected oversized publish error, got %+v", msg)
	}

	rateServer := httptest.NewServer(NewServer(testLogger(t), NewMemoryQueueStore(), Options{MaxMessageBytes: 1024, MaxQueuedMessages: 10, MaxQueuedBytes: 1024, QueueTTL: time.Minute, RateLimitPerMinute: 1}).Handler())
	defer rateServer.Close()
	rateConn := dialTestConn(t, rateServer)
	defer rateConn.Close()
	_ = readChallenge(t, rateConn)
	writeMessage(t, rateConn, protocol.Message{Type: protocol.MessageTypePublish, Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: "first"}}})
	_ = readMessage(t, rateConn)
	writeMessage(t, rateConn, protocol.Message{Type: protocol.MessageTypePublish, Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: "second"}}})
	msg = readMessage(t, rateConn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil || msg.Error.Message != "relay rate limit exceeded" {
		t.Fatalf("expected rate limit error, got %+v", msg)
	}

	queueServer := httptest.NewServer(NewServer(testLogger(t), NewMemoryQueueStore(), Options{MaxMessageBytes: 1024, MaxQueuedMessages: 1, MaxQueuedBytes: 1024, QueueTTL: time.Minute, RateLimitPerMinute: 10}).Handler())
	defer queueServer.Close()
	queueConn := dialTestConn(t, queueServer)
	defer queueConn.Close()
	_ = readChallenge(t, queueConn)
	writeMessage(t, queueConn, protocol.Message{Type: protocol.MessageTypePublish, Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{SenderMailbox: "alice", RecipientMailbox: "bob", Body: "first"}}})
	_ = readMessage(t, queueConn)
	writeMessage(t, queueConn, protocol.Message{Type: protocol.MessageTypePublish, Publish: &protocol.PublishRequest{Envelope: protocol.Envelope{SenderMailbox: "mallory", RecipientMailbox: "bob", Body: "second"}}})
	msg = readMessage(t, queueConn)
	if msg.Type != protocol.MessageTypeError || msg.Error == nil || msg.Error.Message != "mailbox queue is full" {
		t.Fatalf("expected queue full error, got %+v", msg)
	}
}

func TestVerifySubscribeRequestRejectsInvalidChallengeAndKeyInputs(t *testing.T) {
	server := NewServer(testLogger(t), NewMemoryQueueStore(), Options{})
	now := time.Now().UTC()
	challenge := newSubscribeChallenge(now)
	alice := mustIdentity(t, "alice")
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()
	publishDirectoryEntry(t, httpServer, alice)

	device, err := alice.CurrentDevice()
	if err != nil {
		t.Fatalf("current device: %v", err)
	}
	valid := &protocol.SubscribeRequest{
		Mailbox:            device.Mailbox,
		DeviceSigningKey:   "AA==",
		DeviceProof:        "AA==",
		ChallengeNonce:     challenge.Nonce,
		ChallengeExpiresAt: challenge.ExpiresAt,
	}

	if err := server.verifySubscribeRequest(*valid, nil, now); err == nil || err.Error() != "subscribe challenge is required" {
		t.Fatalf("expected missing challenge error, got %v", err)
	}
	wrongNonce := *valid
	wrongNonce.ChallengeNonce = "wrong"
	if err := server.verifySubscribeRequest(wrongNonce, challenge, now); err == nil || err.Error() != "invalid challenge nonce" {
		t.Fatalf("expected nonce error, got %v", err)
	}
	wrongExpiry := *valid
	wrongExpiry.ChallengeExpiresAt = challenge.ExpiresAt.Add(time.Second)
	if err := server.verifySubscribeRequest(wrongExpiry, challenge, now); err == nil || err.Error() != "invalid challenge expiry" {
		t.Fatalf("expected expiry error, got %v", err)
	}
	expired := *challenge
	expired.ExpiresAt = now.Add(-time.Second)
	expiredReq := *valid
	expiredReq.ChallengeExpiresAt = expired.ExpiresAt
	if err := server.verifySubscribeRequest(expiredReq, &expired, now); err == nil || err.Error() != "subscribe challenge expired" {
		t.Fatalf("expected expired challenge error, got %v", err)
	}
	badKey := *valid
	badKey.DeviceSigningKey = "%"
	if err := server.verifySubscribeRequest(badKey, challenge, now); err == nil || !strings.Contains(err.Error(), "decode device signing key") {
		t.Fatalf("expected signing key decode error, got %v", err)
	}
	shortKey := *valid
	shortKey.DeviceSigningKey = "AA=="
	if err := server.verifySubscribeRequest(shortKey, challenge, now); err == nil || err.Error() != "invalid device signing key length" {
		t.Fatalf("expected signing key length error, got %v", err)
	}
	badProof := *valid
	badProof.DeviceSigningKey = websocketTestBase64(device.SigningPublic)
	badProof.DeviceProof = "%"
	if err := server.verifySubscribeRequest(badProof, challenge, now); err == nil || !strings.Contains(err.Error(), "decode device proof") {
		t.Fatalf("expected proof decode error, got %v", err)
	}
	invalidProof := *valid
	invalidProof.DeviceSigningKey = websocketTestBase64(device.SigningPublic)
	invalidProof.DeviceProof = websocketTestBase64([]byte("bad-proof"))
	if err := server.verifySubscribeRequest(invalidProof, challenge, now); err == nil || err.Error() != "invalid device proof" {
		t.Fatalf("expected invalid proof error, got %v", err)
	}
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
}

func websocketTestBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
