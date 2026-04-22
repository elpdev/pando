package chat

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/ui/style"
)

type commandPaletteCommand string

const (
	commandPaletteCommandAddContact         commandPaletteCommand = "add-contact"
	commandPaletteCommandSendContactRequest commandPaletteCommand = "send-contact-request"
	commandPaletteCommandAttachFile         commandPaletteCommand = "attach-file"
	commandPaletteCommandContactRequests    commandPaletteCommand = "contact-requests"
	commandPaletteCommandPeerDetail         commandPaletteCommand = "peer-detail"
	commandPaletteCommandVerifyContact      commandPaletteCommand = "verify-contact"
	commandPaletteCommandSwitchRelay        commandPaletteCommand = "switch-relay"
	commandPaletteCommandAddRelay           commandPaletteCommand = "add-relay"
	commandPaletteCommandRemoveRelay        commandPaletteCommand = "remove-relay"
	commandPaletteCommandEditRelay          commandPaletteCommand = "edit-relay"
	commandPaletteCommandThemes             commandPaletteCommand = "themes"
	commandPaletteCommandMessageTTL         commandPaletteCommand = "message-ttl"
)

// Node ids for structural tree groups. Leaf nodes reuse the command constants
// above so palette_actions.go can continue to switch on the returned action.
const (
	paletteNodeIDContacts    = "contacts"
	paletteNodeIDRelays      = "relays"
	paletteNodeIDSettings    = "settings"
	paletteNodeIDSwitchRelay = "switch-relay"
	paletteNodeIDEditRelay   = "edit-relay"
	paletteNodeIDRemoveRelay = "remove-relay"
	paletteNodeIDTheme       = "theme"
	paletteNodeIDMessageTTL  = "message-ttl"
)

var messageTTLOptions = []time.Duration{
	1 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
}

type commandPaletteDeps struct {
	applyTheme        func(style.Theme)
	currentTheme      func() string
	saveTheme         func(name string) error
	currentRelayName  func() string
	relayProfiles     func() []config.RelayProfile
	currentMessageTTL func() time.Duration
	saveMessageTTL    func(time.Duration) error
}

type paletteCtx struct {
	hasPeer              bool
	pendingRequestsCount int
	deps                 commandPaletteDeps
}

// paletteNode describes one entry in the command tree. Groups carry children
// and no action; leaves carry an action and no children. Nodes whose children
// depend on runtime state (themes, saved relays, TTL options) set dynamic=true
// so cross-level search stops at the group instead of flooding results with
// every option.
type paletteNode struct {
	id       string
	title    string
	detail   string
	meta     string
	aliases  []string
	visible  func(paletteCtx) bool
	children func(paletteCtx) []paletteNode
	action   *commandPaletteAction
	dynamic  bool
}

type commandPaletteItem struct {
	id         string
	title      string
	detail     string
	meta       string
	aliases    []string
	breadcrumb string // parent path label shown before title in search results
	node       paletteNode
	nodePath   []string // full path (ids) from root to this node
}

type commandPaletteVisibleItem struct {
	item    commandPaletteItem
	matched map[int]struct{}
}

type commandPaletteAction struct {
	command    commandPaletteCommand
	themeName  string
	relayName  string
	messageTTL time.Duration
}

type commandPaletteModel struct {
	deps                 commandPaletteDeps
	hasPeer              bool
	pendingRequestsCount int
	open                 bool
	// path is the stack of node ids from root to the current location. Empty
	// path means the user is at the root level.
	path     []string
	selected int
	filter   textinput.Model
}

func newCommandPaletteModel(deps commandPaletteDeps) commandPaletteModel {
	filter := textinput.New()
	filter.Prompt = ""
	filter.Placeholder = "Type a command"
	filter.CharLimit = 128
	return commandPaletteModel{deps: deps, filter: filter}
}

func (m *commandPaletteModel) Open() tea.Cmd {
	m.open = true
	m.path = nil
	m.selected = 0
	m.filter.SetValue("")
	return m.filter.Focus()
}

func (m *commandPaletteModel) SyncContext(hasPeer bool, pendingRequestsCount int) {
	m.hasPeer = hasPeer
	m.pendingRequestsCount = pendingRequestsCount
}

func (m *commandPaletteModel) Close() {
	m.open = false
	m.path = nil
	m.selected = 0
	m.filter.SetValue("")
	m.filter.Blur()
}

func (m *commandPaletteModel) back() {
	if len(m.path) > 0 {
		m.path = m.path[:len(m.path)-1]
		m.selected = 0
		m.filter.SetValue("")
		return
	}
	m.Close()
}

func (m *commandPaletteModel) ctx() paletteCtx {
	return paletteCtx{
		hasPeer:              m.hasPeer,
		pendingRequestsCount: m.pendingRequestsCount,
		deps:                 m.deps,
	}
}

func (m *commandPaletteModel) atRoot() bool {
	return len(m.path) == 0
}

func (m *commandPaletteModel) Update(msg tea.Msg) (*commandPaletteAction, tea.Cmd) {
	if !m.open {
		return nil, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if cmd != nil {
			m.selected = 0
		}
		return nil, cmd
	}

	switch keyMsg.Type {
	case tea.KeyEsc:
		m.back()
		return nil, nil
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveSelection(-1)
		return nil, nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveSelection(1)
		return nil, nil
	case tea.KeyEnter:
		selected := m.selectedItem()
		if selected == nil {
			return nil, nil
		}
		return m.activate(*selected)
	default:
		before := m.filter.Value()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.selected = 0
		}
		return nil, cmd
	}
}

func (m *commandPaletteModel) activate(item commandPaletteItem) (*commandPaletteAction, tea.Cmd) {
	node := item.node
	if node.children != nil {
		m.path = append([]string(nil), item.nodePath...)
		m.selected = 0
		m.filter.SetValue("")
		return nil, nil
	}
	if node.action != nil {
		action := *node.action
		m.Close()
		return &action, nil
	}
	m.Close()
	return nil, nil
}
