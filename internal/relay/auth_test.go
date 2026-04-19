package relay

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRelayRejectsMissingAuthToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		t.Fatalf("expected websocket dial without token to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized response, got %+v", resp)
	}
}

func TestRelayAcceptsMatchingAuthToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{AuthToken: "secret"}).Handler())
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	headers := http.Header{}
	headers.Set(authHeader, "secret")
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatalf("expected websocket dial with token to succeed: %v", err)
	}
	_ = conn.Close()
}

func TestRelayRejectsCrossOriginWebsocketHandshakeByDefault(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))
	server := httptest.NewServer(NewServer(logger, NewMemoryQueueStore(), Options{}).Handler())
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	headers := http.Header{}
	headers.Set("Origin", "https://evil.example")
	_, resp, err := websocket.DefaultDialer.Dial(url, headers)
	if err == nil {
		t.Fatalf("expected cross-origin websocket dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected forbidden response, got %+v", resp)
	}
}
