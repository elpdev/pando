package chat

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
)

type relayState struct {
	url           string
	token         string
	client        RelayClient
	clientFactory func(url, token string) (RelayClient, error)
}

type peerState struct {
	mailbox     string
	fingerprint string
	verified    bool
	trustSource string
}

type connectionState struct {
	status           string
	connecting       bool
	connected        bool
	disconnected     bool
	authFailed       bool
	reconnectAttempt int
	reconnectDelay   time.Duration
}

type messageState struct {
	items           []messageItem
	rendered        []string
	pendingIncoming int
}

type typingState struct {
	peerVisible   bool
	peerExpiresAt time.Time
	spinner       spinner.Model
	localSent     bool
	localPeer     string
	localAt       time.Time
}

type uiState struct {
	width        int
	height       int
	sidebarWidth int
	focus        focusState
	toast        *toastState
}
