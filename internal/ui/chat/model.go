package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/transport"
	"github.com/elpdev/pando/internal/ui/style"
)

type Model struct {
	client    transport.Client
	messaging *messaging.Service
	mailbox   string

	relay    relayState
	peer     peerState
	conn     connectionState
	msgs     messageState
	typing   typingState
	roomSync roomSyncState
	ui       uiState

	input    textarea.Model
	viewport viewport.Model

	contacts      []contactItem
	selectedIndex int
	drafts        draftState
	pending       *pendingAttachment

	filePicker     filePickerModel
	commandPalette commandPaletteModel
	addContact     addContactModal
	helpOpen       bool
	peerDetailOpen bool
	unread         map[string]int
}

func New(deps Deps) *Model {
	input := textarea.New()
	input.Focus()
	input.CharLimit = 4096
	input.ShowLineNumbers = false
	input.Prompt = style.GlyphPrompt + " "
	input.KeyMap.InsertNewline.SetKeys("shift+enter")
	input.KeyMap.InsertNewline.SetHelp("shift+enter", "newline")
	input.KeyMap.LinePrevious.SetKeys("ctrl+p")
	input.KeyMap.LineNext.SetKeys("ctrl+n")
	input.FocusedStyle.Base = style.InputFrame
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.EndOfBuffer = lipgloss.NewStyle()
	input.FocusedStyle.Placeholder = style.Muted
	input.FocusedStyle.Prompt = style.StatusInfo
	input.FocusedStyle.Text = lipgloss.NewStyle()
	input.BlurredStyle = input.FocusedStyle
	input.SetHeight(1)
	input.SetWidth(20)

	vp := viewport.New(0, 0)
	vp.SetContent("")

	factory := deps.RelayClientFactory
	if factory == nil {
		factory = defaultRelayClientFactory
	}
	m := &Model{
		client:    deps.Client,
		messaging: deps.Messaging,
		mailbox:   deps.Mailbox,
		relay: relayState{
			url:           deps.RelayURL,
			token:         deps.RelayToken,
			clientFactory: factory,
		},
		peer: peerState{mailbox: deps.RecipientMailbox},
		conn: connectionState{
			status:     fmt.Sprintf("connecting as %s", deps.Mailbox),
			connecting: true,
		},
		msgs:          messageState{followLatest: true},
		typing:        typingState{spinner: newTypingSpinner()},
		input:         input,
		viewport:      vp,
		selectedIndex: -1,
		filePicker:    newFilePickerModel(),
		commandPalette: newCommandPaletteModel(commandPaletteDeps{
			applyTheme: style.Apply,
			currentTheme: func() string {
				return style.Current().Name
			},
			saveTheme: deps.SaveTheme,
		}),
		unread: map[string]int{},
	}
	m.addContact = newAddContactModal(addContactDeps{
		messaging:         deps.Messaging,
		ensureRelayClient: m.ensureRelayClient,
		relayConfigured:   m.relayConfigured,
	})
	m.loadContacts(deps.RecipientMailbox)
	m.syncRecipientDetails()
	m.syncInputPlaceholder()
	m.syncComposer()
	if m.peer.mailbox == "" {
		m.ui.focus = focusSidebar
	}
	m.filePicker.SetSize(m.conversationWidth(), m.ui.height)
	return m
}

func defaultRelayClientFactory(url, token string) (RelayClient, error) {
	return relayapi.NewClient(url, token)
}

// ensureRelayClient builds the relay client on demand. Returns an error if no
// relay URL is configured — callers should gate relay-dependent flows before
// reaching this point.
func (m *Model) ensureRelayClient() (RelayClient, error) {
	if m.relay.client != nil {
		return m.relay.client, nil
	}
	if strings.TrimSpace(m.relay.url) == "" {
		return nil, fmt.Errorf("no relay configured")
	}
	client, err := m.relay.clientFactory(m.relay.url, m.relay.token)
	if err != nil {
		return nil, err
	}
	m.relay.client = client
	return client, nil
}

func (m *Model) Init() tea.Cmd {
	m.loadHistory()
	return tea.Batch(m.connectCmd(), m.waitForEvent(), m.typingTickCmd())
}

func (m *Model) SetSize(width, height int) {
	m.ui.width = width
	m.ui.height = height
	m.updateLayout()
	m.syncComposer()
	m.updateLayout()
	m.syncViewport()
}

func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if handled, cmd := m.handleOverlays(msg); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if next, cmd := m.handleKeyMsg(msg); next != nil {
			return next, cmd
		}
	case addContactCompletedMsg:
		return m.handleAddContactCompletedMsg(msg)
	case addContactClosedMsg:
		return m.handleAddContactClosedMsg(msg)
	case filePickerClosedMsg:
		m.closeFilePicker()
		return m, nil
	case filePickerErrorMsg:
		m.pushToast(fmt.Sprintf("file picker failed: %v", msg.err), ToastBad)
		return m, nil
	case filePickerSelectedMsg:
		if err := m.setPendingAttachment(msg.path, messaging.AttachmentTypeFile); err != nil {
			m.pushToast(fmt.Sprintf("attach failed: %v", err), ToastBad)
		}
		return m, nil
	case clientEventMsg:
		return m.handleClientEventMsg(msg)
	case connectResultMsg:
		return m.handleConnectResultMsg(msg.err)
	case reconnectResultMsg:
		return m.handleConnectResultMsg(msg.err)
	case typingTickMsg:
		return m.handleTypingTickMsg(msg)
	case sendResultMsg:
		return m.handleSendResultMsg(msg)
	case typingSendResultMsg:
		return m.handleTypingSendResultMsg(msg)
	case roomHistorySyncResultMsg:
		return m.handleRoomHistorySyncResultMsg(msg)
	}

	if m.ui.focus != focusChat {
		return m, nil
	}
	previousValue := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncComposer()
	m.updateLayout()
	m.syncViewport()
	typingCmd := m.handleInputActivity(previousValue, m.input.Value())
	return m, tea.Batch(cmd, typingCmd)
}

// pushToast posts an ephemeral message to the toast slot. The message
// persists for toastLifetime; after that the next typing tick clears it.
func (m *Model) pushToast(text string, level ToastLevel) {
	if text == "" {
		m.ui.toast = nil
		return
	}
	m.ui.toast = &toastState{
		text:      text,
		level:     level,
		expiresAt: time.Now().Add(toastLifetime),
	}
}

func (m *Model) Close() error {
	return m.client.Close()
}

func (m *Model) sendCmd(recipient, body string, batch *messaging.OutgoingBatch) tea.Cmd {
	return func() tea.Msg {
		if batch == nil {
			return sendResultMsg{recipient: recipient, body: body}
		}
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return sendResultMsg{recipient: recipient, messageID: batch.MessageID, body: body, attachment: batch.Attachment, err: err}
			}
		}
		return sendResultMsg{recipient: recipient, messageID: batch.MessageID, body: body, attachment: batch.Attachment}
	}
}

func (m *Model) sendRoomCmd(roomID, body string, batch *messaging.OutgoingBatch) tea.Cmd {
	return func() tea.Msg {
		if batch == nil {
			return sendResultMsg{roomID: roomID, body: body}
		}
		for _, envelope := range batch.Envelopes {
			if err := m.client.Send(envelope); err != nil {
				return sendResultMsg{roomID: roomID, messageID: batch.MessageID, body: body, err: err}
			}
		}
		return sendResultMsg{roomID: roomID, messageID: batch.MessageID, body: body}
	}
}
