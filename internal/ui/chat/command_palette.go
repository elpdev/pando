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
	commandPaletteCommandVoiceNotes         commandPaletteCommand = "voice-notes"
	commandPaletteCommandPlayVoiceNote      commandPaletteCommand = "play-voice-note"
	commandPaletteCommandStopVoiceNote      commandPaletteCommand = "stop-voice-note"
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
	paletteNodeIDHelp        = "help"
	paletteNodeIDMessageTTL  = "message-ttl"
	paletteNodeIDVoiceNotes  = "voice-notes"
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
	// resolveView returns the backing view for id, or nil if the id has no
	// registered view. Populated by the Model when it constructs the palette.
	resolveView func(id paletteViewID) paletteView
	// onEnterView is called when the palette activates a view node. The Model
	// uses this to invoke the view's Open with its full context (peer state,
	// etc.) which the palette itself does not carry.
	onEnterView func(id paletteViewID) tea.Cmd
	// onExitView is called when the palette leaves a view path (via back or
	// Close). The Model uses this to invoke the view's Close hook.
	onExitView func(id paletteViewID)
}

type paletteCtx struct {
	hasPeer              bool
	pendingRequestsCount int
	voiceNotes           []voiceNoteOption
	voicePlaybackActive  bool
	deps                 commandPaletteDeps
}

// paletteNode describes one entry in the command tree. Exactly one of
// children, action, or view is non-zero:
//   - Group: children set — descending pushes the node id onto m.path.
//   - Leaf:  action set — activating returns the action; palette closes.
//   - View:  view != paletteViewNone — pushing the id enters a hosted
//     interactive sub-screen rendered inside the palette frame. Esc pops
//     the path back to the parent group.
//
// Nodes whose children depend on runtime state (themes, saved relays, TTL
// options) set dynamic=true so cross-level search stops at the group instead
// of flooding results with every option.
type paletteNode struct {
	id       string
	title    string
	detail   string
	meta     string
	aliases  []string
	visible  func(paletteCtx) bool
	children func(paletteCtx) []paletteNode
	action   *commandPaletteAction
	view     paletteViewID
	dynamic  bool
}

type commandPaletteItem struct {
	id         string
	title      string
	detail     string
	meta       string
	aliases    []string
	breadcrumb string
	node       paletteNode
	nodePath   []string
}

type commandPaletteVisibleItem struct {
	item    commandPaletteItem
	matched map[int]struct{}
}

type commandPaletteAction struct {
	command     commandPaletteCommand
	themeName   string
	relayName   string
	messageTTL  time.Duration
	voiceNoteID string
}

type commandPaletteModel struct {
	deps                 commandPaletteDeps
	hasPeer              bool
	pendingRequestsCount int
	voiceNotes           []voiceNoteOption
	voicePlaybackActive  bool
	open                 bool
	path                 []string
	selected             int
	filter               textinput.Model
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

// OpenAtPath opens the palette and jumps straight to the node at the given id
// path. If the path points to a view node, the view's Open hook is triggered
// via deps.onEnterView. Used by hotkeys that open a specific palette view
// (e.g. `?` opens Settings › Help).
func (m *commandPaletteModel) OpenAtPath(path []string) tea.Cmd {
	m.open = true
	m.path = append(m.path[:0], path...)
	m.selected = 0
	m.filter.SetValue("")
	cmd := m.filter.Focus()
	if id := m.activeViewID(); id != paletteViewNone && m.deps.onEnterView != nil {
		if enterCmd := m.deps.onEnterView(id); enterCmd != nil {
			return tea.Batch(cmd, enterCmd)
		}
	}
	return cmd
}

func (m *commandPaletteModel) SyncContext(hasPeer bool, pendingRequestsCount int, voiceNotes []voiceNoteOption, voicePlaybackActive bool) {
	m.hasPeer = hasPeer
	m.pendingRequestsCount = pendingRequestsCount
	m.voiceNotes = append(m.voiceNotes[:0], voiceNotes...)
	m.voicePlaybackActive = voicePlaybackActive
}

func (m *commandPaletteModel) Close() {
	if id := m.activeViewID(); id != paletteViewNone && m.deps.onExitView != nil {
		m.deps.onExitView(id)
	}
	m.open = false
	m.path = nil
	m.selected = 0
	m.filter.SetValue("")
	m.filter.Blur()
}

func (m *commandPaletteModel) back() {
	if len(m.path) > 0 {
		if id := m.activeViewID(); id != paletteViewNone && m.deps.onExitView != nil {
			m.deps.onExitView(id)
		}
		m.path = m.path[:len(m.path)-1]
		m.selected = 0
		m.filter.SetValue("")
		return
	}
	m.Close()
}

// activeViewID returns the view id for the node at the current path, or
// paletteViewNone if the path is empty or points to a group/leaf.
func (m *commandPaletteModel) activeViewID() paletteViewID {
	if len(m.path) == 0 {
		return paletteViewNone
	}
	node, ok := m.nodeAtPath(m.path)
	if !ok {
		return paletteViewNone
	}
	return node.view
}

// activeView returns the backing view struct for the active view node, or nil.
func (m *commandPaletteModel) activeView() paletteView {
	id := m.activeViewID()
	if id == paletteViewNone || m.deps.resolveView == nil {
		return nil
	}
	return m.deps.resolveView(id)
}

func (m *commandPaletteModel) ctx() paletteCtx {
	return paletteCtx{
		hasPeer:              m.hasPeer,
		pendingRequestsCount: m.pendingRequestsCount,
		voiceNotes:           append([]voiceNoteOption(nil), m.voiceNotes...),
		voicePlaybackActive:  m.voicePlaybackActive,
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
	// When a view is active, Esc always belongs to the palette (back-nav);
	// every other message is routed to the view first.
	if view := m.activeView(); view != nil {
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyEsc {
			m.back()
			return nil, nil
		}
		_, cmd := view.Update(msg)
		return nil, cmd
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
	if node.view != paletteViewNone {
		m.path = append([]string(nil), item.nodePath...)
		m.selected = 0
		m.filter.SetValue("")
		if m.deps.onEnterView != nil {
			return nil, m.deps.onEnterView(node.view)
		}
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
