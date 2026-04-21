package chat

import (
	"time"

	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/transport"
)

type Deps struct {
	Client             transport.Client
	Messaging          *messaging.Service
	Mailbox            string
	RecipientMailbox   string
	RelayURL           string
	RelayToken         string
	RelayClientFactory func(url, token string) (RelayClient, error)
}

// Dependencies.

// RelayClient is the minimum relay surface the add-contact modal needs for
// directory lookups and invite-code rendezvous. Kept as an interface so tests
// can swap in an in-memory fake.
type RelayClient interface {
	LookupDirectoryEntry(mailbox string) (*relayapi.SignedDirectoryEntry, error)
	LookupDirectoryEntryByDeviceMailbox(mailbox string) (*relayapi.SignedDirectoryEntry, error)
	ListDiscoverableEntries() ([]relayapi.SignedDirectoryEntry, error)
	PutRendezvousPayload(id string, p relayapi.RendezvousPayload) error
	GetRendezvousPayloads(id string) ([]relayapi.RendezvousPayload, error)
}

// Contact and message types.

type contactItem struct {
	Mailbox     string
	Label       string
	Fingerprint string
	Verified    bool
	TrustSource string
	IsRoom      bool
	Joined      bool
	MemberCount int
}

// messageItem is one rendered chat message. We keep these as structured records
// so the grouped renderer can reason about sender/time/delivery state without
// having to parse strings.
type messageItem struct {
	direction    string // "outbound" | "inbound"
	sender       string // mailbox that authored the message
	body         string
	timestamp    time.Time
	messageID    string
	status       deliveryStatus
	isAttachment bool
}

// deliveryStatus is a four-state outbound lifecycle. Inbound messages ignore
// it.
type deliveryStatus int

const (
	statusPending   deliveryStatus = iota // optimistic local append, awaiting relay round-trip
	statusSent                            // send succeeded; waiting for recipient ack
	statusDelivered                       // peer acked
	statusFailed                          // send returned an error
)

// Connection, focus, and toast enums.

// focusState tracks which pane owns keyboard input. In wide mode both panes
// are visible and focus only decorates borders + directs ↑/↓; in narrow mode
// only the focused pane renders.
type focusState int

const (
	focusChat    focusState = iota // input + viewport + conversation
	focusSidebar                   // contact list
)

// narrowThreshold is the terminal width below which the sidebar and
// conversation can't coexist comfortably. Below this, only the focused pane
// renders.
const narrowThreshold = 60

// ConnState is the coarse connection state used by the app header to pick a
// glyph and color. Call ConnectionState() to read it.
type ConnState int

const (
	ConnConnecting ConnState = iota
	ConnConnected
	ConnReconnecting
	ConnDisconnected
	ConnAuthFailed
)

// ToastLevel controls the color of an ephemeral message shown below the
// viewport.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastWarn
	ToastBad
)

type toastState struct {
	text      string
	level     ToastLevel
	expiresAt time.Time
}

const toastLifetime = 3 * time.Second

// Internal tea.Msg types.

type clientEventMsg transport.Event
type connectResultMsg struct{ err error }
type reconnectResultMsg struct{ err error }
type typingTickMsg time.Time
type typingSendResultMsg struct{ err error }
type roomHistorySyncResultMsg struct {
	requestID string
	err       error
	skipped   string
}
type filePickerClosedMsg struct{}
type filePickerErrorMsg struct{ err error }
type filePickerSelectedMsg struct{ path string }

type sendResultMsg struct {
	recipient string
	roomID    string
	messageID string
	body      string
	err       error
}

const (
	typingAnimationInterval = 350 * time.Millisecond
	typingIdleTimeout       = 2 * time.Second
	addContactLimit         = 16384
)
