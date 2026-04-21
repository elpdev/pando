package chat

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/ui/style"
)

type commandPaletteMode int

const (
	commandPaletteModeRoot commandPaletteMode = iota
	commandPaletteModeThemes
	commandPaletteModeRelays
	commandPaletteModeRemoveRelay
	commandPaletteModeEditRelay
	commandPaletteModeMessageTTL
)

type commandPaletteCommand string

const (
	commandPaletteCommandAddContact         commandPaletteCommand = "add-contact"
	commandPaletteCommandSendContactRequest commandPaletteCommand = "send-contact-request"
	commandPaletteCommandAttachFile         commandPaletteCommand = "attach-file"
	commandPaletteCommandContactRequests    commandPaletteCommand = "contact-requests"
	commandPaletteCommandPeerDetail         commandPaletteCommand = "peer-detail"
	commandPaletteCommandVerifyContact      commandPaletteCommand = "verify-contact"
	commandPaletteCommandRelays             commandPaletteCommand = "relays"
	commandPaletteCommandAddRelay           commandPaletteCommand = "add-relay"
	commandPaletteCommandRemoveRelay        commandPaletteCommand = "remove-relay"
	commandPaletteCommandEditRelay          commandPaletteCommand = "edit-relay"
	commandPaletteCommandSwitchRelay        commandPaletteCommand = "switch-relay"
	commandPaletteCommandThemes             commandPaletteCommand = "themes"
	commandPaletteCommandMessageTTL         commandPaletteCommand = "message-ttl"
)

// messageTTLOptions are the choices shown in the Message TTL sub-mode. The
// maximum of 24h is enforced by offering no larger option; users editing
// config.yml directly are further clamped by config.EffectiveMessageTTL.
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

type commandPaletteItem struct {
	id      string
	title   string
	detail  string
	meta    string
	aliases []string
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
	mode                 commandPaletteMode
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
	m.mode = commandPaletteModeRoot
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
	m.mode = commandPaletteModeRoot
	m.selected = 0
	m.filter.SetValue("")
	m.filter.Blur()
}

func (m *commandPaletteModel) back() {
	if m.mode == commandPaletteModeThemes || m.mode == commandPaletteModeRelays || m.mode == commandPaletteModeRemoveRelay || m.mode == commandPaletteModeEditRelay || m.mode == commandPaletteModeMessageTTL {
		m.mode = commandPaletteModeRoot
		m.selected = 0
		m.filter.SetValue("")
		return
	}
	m.Close()
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
	switch m.mode {
	case commandPaletteModeRoot:
		if item.id == string(commandPaletteCommandThemes) {
			m.mode = commandPaletteModeThemes
			m.selected = 0
			m.filter.SetValue("")
			return nil, nil
		}
		if item.id == string(commandPaletteCommandRelays) {
			m.mode = commandPaletteModeRelays
			m.selected = 0
			m.filter.SetValue("")
			return nil, nil
		}
		if item.id == string(commandPaletteCommandRemoveRelay) {
			m.mode = commandPaletteModeRemoveRelay
			m.selected = 0
			m.filter.SetValue("")
			return nil, nil
		}
		if item.id == string(commandPaletteCommandEditRelay) {
			m.mode = commandPaletteModeEditRelay
			m.selected = 0
			m.filter.SetValue("")
			return nil, nil
		}
		if item.id == string(commandPaletteCommandMessageTTL) {
			m.mode = commandPaletteModeMessageTTL
			m.selected = 0
			m.filter.SetValue("")
			return nil, nil
		}
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommand(item.id)}, nil
	case commandPaletteModeThemes:
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommandThemes, themeName: item.id}, nil
	case commandPaletteModeRelays:
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommandSwitchRelay, relayName: item.id}, nil
	case commandPaletteModeRemoveRelay:
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommandRemoveRelay, relayName: item.id}, nil
	case commandPaletteModeEditRelay:
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommandEditRelay, relayName: item.id}, nil
	case commandPaletteModeMessageTTL:
		ttl, err := time.ParseDuration(item.id)
		if err != nil {
			return nil, nil
		}
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommandMessageTTL, messageTTL: ttl}, nil
	default:
		return nil, nil
	}
}
