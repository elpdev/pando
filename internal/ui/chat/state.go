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
	label       string
	fingerprint string
	verified    bool
	trustSource string
	isRoom      bool
	joined      bool
	memberCount int
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
	followLatest    bool
}

type typingState struct {
	peerVisible   bool
	peerExpiresAt time.Time
	spinner       spinner.Model
	localSent     bool
	localPeer     string
	localAt       time.Time
}

type roomSyncState struct {
	active          bool
	requestID       string
	startedAt       time.Time
	lastRequestedAt time.Time
	syncedCount     int
}

type uiState struct {
	width        int
	height       int
	sidebarWidth int
	composerRows int
	focus        focusState
	toast        *toastState
}
