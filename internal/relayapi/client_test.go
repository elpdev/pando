package relayapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elpdev/pando/internal/identity"
)

func TestRelayHTTPBaseURLConvertsWebsocketURLs(t *testing.T) {
	baseURL, err := RelayHTTPBaseURL("wss://relay.example/ws?token=123#frag")
	if err != nil {
		t.Fatalf("relay http base url: %v", err)
	}
	if baseURL != "https://relay.example" {
		t.Fatalf("expected https relay base url, got %q", baseURL)
	}

	baseURL, err = RelayHTTPBaseURL("ws://relay.example/custom/ws")
	if err != nil {
		t.Fatalf("relay http base url: %v", err)
	}
	if baseURL != "http://relay.example/custom" {
		t.Fatalf("expected http relay base url, got %q", baseURL)
	}
}

func TestRelayHTTPBaseURLRejectsUnsupportedScheme(t *testing.T) {
	_, err := RelayHTTPBaseURL("ftp://relay.example/ws")
	if err == nil || !strings.Contains(err.Error(), "unsupported relay URL scheme") {
		t.Fatalf("expected unsupported scheme error, got %v", err)
	}
}

func TestClientDoJSONIncludesAuthHeaderAndDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("expected put method, got %s", r.Method)
		}
		if r.URL.Path != "/directory/mailboxes/alice" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get(authHeader) != "secret" {
			t.Fatalf("expected auth header, got %q", r.Header.Get(authHeader))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("expected json content type, got %q", r.Header.Get("Content-Type"))
		}
		var entry SignedDirectoryEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(entry)
	}))
	defer server.Close()

	alice := mustIdentity(t, "alice")
	signed := mustSignedDirectoryEntry(t, alice, 1)
	client := &Client{baseURL: server.URL, token: "secret", httpClient: server.Client()}

	loaded, err := client.PublishDirectoryEntry(*signed)
	if err != nil {
		t.Fatalf("publish directory entry: %v", err)
	}
	if loaded.Entry.Mailbox != "alice" {
		t.Fatalf("unexpected response: %+v", loaded)
	}
}

func TestClientDoJSONReturnsStructuredRelayErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(struct {
			Message string `json:"message"`
		}{Message: "directory entry conflicts with existing mailbox owner"})
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	err := client.PutRendezvousPayload("room", RendezvousPayload{})
	if err == nil || !strings.Contains(err.Error(), "directory entry conflicts with existing mailbox owner") {
		t.Fatalf("expected structured relay error, got %v", err)
	}
}

func TestClientDoJSONFallsBackToHTTPStatusAndHandlesDecodeErrors(t *testing.T) {
	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer statusServer.Close()

	client := &Client{baseURL: statusServer.URL, httpClient: statusServer.Client()}
	err := client.DeleteRendezvous("room")
	if err == nil || !strings.Contains(err.Error(), http.StatusText(http.StatusBadGateway)) {
		t.Fatalf("expected status fallback error, got %v", err)
	}

	decodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not-json")
	}))
	defer decodeServer.Close()

	client = &Client{baseURL: decodeServer.URL, httpClient: decodeServer.Client()}
	_, err = client.LookupDirectoryEntry("alice")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestClientDoJSONRejectsUnencodableRequestBody(t *testing.T) {
	client := &Client{baseURL: "http://relay.example", httpClient: &http.Client{Timeout: time.Second}}
	err := client.doJSON(http.MethodPut, "/directory/mailboxes/alice", map[string]any{"bad": make(chan int)}, nil)
	if err == nil || !strings.Contains(err.Error(), "encode request") {
		t.Fatalf("expected encode request error, got %v", err)
	}
}

func TestRelayAPIClientRoundTripsDirectoryAndRendezvousEndpoints(t *testing.T) {
	payload := RendezvousPayload{Ciphertext: "cipher", Nonce: "nonce", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}
	alice := mustIdentity(t, "alice")
	signed := mustSignedDirectoryEntry(t, alice, 7)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/directory/mailboxes/alice":
			switch r.Method {
			case http.MethodPut:
				var got SignedDirectoryEntry
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode publish entry: %v", err)
				}
				if got.Entry.Version != signed.Entry.Version {
					t.Fatalf("unexpected directory entry: %+v", got)
				}
				_ = json.NewEncoder(w).Encode(got)
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(signed)
			default:
				t.Fatalf("unexpected method %s", r.Method)
			}
		case "/rendezvous/test-room":
			switch r.Method {
			case http.MethodPut:
				w.WriteHeader(http.StatusNoContent)
			case http.MethodGet:
				_ = json.NewEncoder(w).Encode(GetRendezvousResponse{Payloads: []RendezvousPayload{payload}})
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Fatalf("unexpected method %s", r.Method)
			}
		case "/directory/devices/alice":
			_ = json.NewEncoder(w).Encode(signed)
		case "/directory/discoverable":
			_ = json.NewEncoder(w).Encode(ListDirectoryResponse{Entries: []SignedDirectoryEntry{*signed}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	loaded, err := client.PublishDirectoryEntry(*signed)
	if err != nil {
		t.Fatalf("publish directory entry: %v", err)
	}
	if loaded.Entry.Mailbox != "alice" {
		t.Fatalf("unexpected published entry: %+v", loaded)
	}
	loaded, err = client.LookupDirectoryEntry("alice")
	if err != nil {
		t.Fatalf("lookup directory entry: %v", err)
	}
	if loaded.Entry.Version != signed.Entry.Version {
		t.Fatalf("unexpected looked up entry: %+v", loaded)
	}
	if err := client.PutRendezvousPayload("test-room", payload); err != nil {
		t.Fatalf("put rendezvous payload: %v", err)
	}
	payloads, err := client.GetRendezvousPayloads("test-room")
	if err != nil {
		t.Fatalf("get rendezvous payloads: %v", err)
	}
	if len(payloads) != 1 || payloads[0].Ciphertext != payload.Ciphertext {
		t.Fatalf("unexpected rendezvous payloads: %+v", payloads)
	}
	if err := client.DeleteRendezvous("test-room"); err != nil {
		t.Fatalf("delete rendezvous: %v", err)
	}
	loaded, err = client.LookupDirectoryEntryByDeviceMailbox("alice")
	if err != nil {
		t.Fatalf("lookup directory entry by device mailbox: %v", err)
	}
	if loaded.Entry.Mailbox != "alice" {
		t.Fatalf("unexpected device mailbox lookup entry: %+v", loaded)
	}
	entries, err := client.ListDiscoverableEntries()
	if err != nil {
		t.Fatalf("list discoverable entries: %v", err)
	}
	if len(entries) != 1 || entries[0].Entry.Mailbox != "alice" {
		t.Fatalf("unexpected discoverable entries: %+v", entries)
	}
}

func TestVerifySignedDirectoryEntryRoundTripAndOrdering(t *testing.T) {
	alice := mustIdentity(t, "alice")
	pending, err := identity.NewPendingEnrollment("alice", "alice-phone")
	if err != nil {
		t.Fatalf("new pending enrollment: %v", err)
	}
	approval, err := alice.Approve(pending.Request())
	if err != nil {
		t.Fatalf("approve enrollment: %v", err)
	}
	_, err = pending.Complete(*approval)
	if err != nil {
		t.Fatalf("complete enrollment: %v", err)
	}
	entry := DirectoryEntry{Mailbox: "alice", Bundle: alice.InviteBundle(), PublishedAt: time.Now().UTC(), Version: 9}
	signed, err := SignDirectoryEntry(entry, alice.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	if err := VerifySignedDirectoryEntry(*signed); err != nil {
		t.Fatalf("verify signed directory entry: %v", err)
	}

	reordered := entry
	if len(reordered.Bundle.Devices) < 2 {
		t.Fatalf("expected two devices in invite bundle, got %+v", reordered.Bundle.Devices)
	}
	reordered.Bundle.Devices = []identity.DeviceBundle{entry.Bundle.Devices[1], entry.Bundle.Devices[0]}
	originalBytes, err := directoryEntrySigningBytes(entry)
	if err != nil {
		t.Fatalf("canonical bytes original: %v", err)
	}
	reorderedBytes, err := directoryEntrySigningBytes(reordered)
	if err != nil {
		t.Fatalf("canonical bytes reordered: %v", err)
	}
	if string(originalBytes) != string(reorderedBytes) {
		t.Fatal("expected canonical directory bytes to ignore device order")
	}
}

func TestVerifySignedDirectoryEntryRejectsInvalidInputs(t *testing.T) {
	alice := mustIdentity(t, "alice")
	signed := mustSignedDirectoryEntry(t, alice, 3)

	missingMailbox := *signed
	missingMailbox.Entry.Mailbox = ""
	if err := VerifySignedDirectoryEntry(missingMailbox); err == nil || !strings.Contains(err.Error(), "directory mailbox is required") {
		t.Fatalf("expected mailbox error, got %v", err)
	}

	zeroPublished := *signed
	zeroPublished.Entry.PublishedAt = time.Time{}
	if err := VerifySignedDirectoryEntry(zeroPublished); err == nil || !strings.Contains(err.Error(), "directory published_at is required") {
		t.Fatalf("expected published_at error, got %v", err)
	}

	mismatch := *signed
	mismatch.Entry.Mailbox = "mallory"
	if err := VerifySignedDirectoryEntry(mismatch); err == nil || !strings.Contains(err.Error(), "must match account") {
		t.Fatalf("expected mailbox/account mismatch error, got %v", err)
	}

	badSignature := *signed
	badSignature.Signature = "%%%"
	if err := VerifySignedDirectoryEntry(badSignature); err == nil || !strings.Contains(err.Error(), "decode directory signature") {
		t.Fatalf("expected signature decode error, got %v", err)
	}

	tampered := *signed
	tampered.Entry.Version++
	if err := VerifySignedDirectoryEntry(tampered); err == nil || !strings.Contains(err.Error(), "directory signature is invalid") {
		t.Fatalf("expected invalid signature error, got %v", err)
	}

	invalidInvite := *signed
	invalidInvite.Entry.Bundle.AccountSigningPublic = nil
	if err := VerifySignedDirectoryEntry(invalidInvite); err == nil {
		t.Fatal("expected invalid invite bundle error")
	}
}

func mustIdentity(t *testing.T, mailbox string) *identity.Identity {
	t.Helper()
	id, err := identity.New(mailbox)
	if err != nil {
		t.Fatalf("new identity %s: %v", mailbox, err)
	}
	return id
}

func mustSignedDirectoryEntry(t *testing.T, id *identity.Identity, version int64) *SignedDirectoryEntry {
	t.Helper()
	signed, err := SignDirectoryEntry(DirectoryEntry{Mailbox: id.AccountID, Bundle: id.InviteBundle(), PublishedAt: time.Now().UTC(), Version: version}, id.AccountSigningPrivate)
	if err != nil {
		t.Fatalf("sign directory entry: %v", err)
	}
	return signed
}

func TestNewClientRejectsInvalidRelayURL(t *testing.T) {
	_, err := NewClient("mailto:relay", "")
	if err == nil || !strings.Contains(err.Error(), "unsupported relay URL scheme") {
		t.Fatalf("expected invalid relay url error, got %v", err)
	}
}
