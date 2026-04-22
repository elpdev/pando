package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/relayapi"
	"github.com/elpdev/pando/internal/relayclient"
	"github.com/elpdev/pando/internal/transport"
	"github.com/elpdev/pando/internal/transport/ws"
	"github.com/elpdev/pando/internal/ui/audio"
	"github.com/elpdev/pando/internal/ui/style"
)

type Model struct {
	client        transport.Client
	messaging     *messaging.Service
	voicePlayer   VoicePlayer
	voiceRecorder VoiceRecorder
	mailbox       string

	relay     relayState
	peer      peerState
	conn      connectionState
	msgs      messageState
	typing    typingState
	recording recordingState
	roomSync  roomSyncState
	ui        uiState

	input    textarea.Model
	viewport viewport.Model

	contacts      []contactItem
	selectedIndex int
	drafts        draftState
	pending       *pendingAttachment

	filePicker           filePickerModel
	commandPalette       commandPaletteModel
	addContact           addContactModal
	addRelay             addRelayModal
	contactRequestSend   contactRequestSendModal
	contactVerify        contactVerifyModal
	contactRequests      contactRequestsModal
	pendingRequestsCount int
	unread               map[string]int
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
	transportFactory := deps.RelayTransportFactory
	if transportFactory == nil {
		identity := deps.Messaging.Identity()
		transportFactory = func(url, token string) transport.Client {
			return ws.NewClient(url, token, identity, relayclient.ClientOptions{})
		}
	}
	profiles := append([]config.RelayProfile(nil), deps.RelayProfiles...)
	active := relayProfileName(profiles, deps.RelayURL, deps.RelayToken)
	m := &Model{
		client:        deps.Client,
		messaging:     deps.Messaging,
		voicePlayer:   deps.VoicePlayer,
		voiceRecorder: deps.VoiceRecorder,
		mailbox:       deps.Mailbox,
		relay: relayState{
			url:              deps.RelayURL,
			token:            deps.RelayToken,
			active:           active,
			profiles:         profiles,
			clientFactory:    factory,
			transportFactory: transportFactory,
			saveProfiles:     deps.SaveRelays,
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
		unread:        map[string]int{},
	}
	if m.voicePlayer == nil {
		m.voicePlayer = audio.NewPlayer()
	}
	if m.voiceRecorder == nil {
		m.voiceRecorder = audio.NewRecorder()
	}
	m.commandPalette = newCommandPaletteModel(commandPaletteDeps{
		applyTheme: style.Apply,
		currentTheme: func() string {
			return style.Current().Name
		},
		saveTheme: deps.SaveTheme,
		currentMessageTTL: func() time.Duration {
			if deps.Messaging == nil {
				return 0
			}
			return deps.Messaging.MessageTTL()
		},
		saveMessageTTL: deps.SaveMessageTTL,
		currentRelayName: func() string {
			return m.relay.active
		},
		relayProfiles: func() []config.RelayProfile {
			return append([]config.RelayProfile(nil), m.relay.profiles...)
		},
		resolveView: m.resolvePaletteView,
		onEnterView: m.enterPaletteView,
		onExitView:  m.exitPaletteView,
	})
	m.addContact = newAddContactModal(addContactDeps{
		messaging:         deps.Messaging,
		ensureRelayClient: m.ensureRelayClient,
		relayConfigured:   m.relayConfigured,
	})
	m.addRelay = newAddRelayModal(m)
	m.contactRequestSend = newContactRequestSendModal(contactRequestSendDeps{
		messaging:         deps.Messaging,
		ensureRelayClient: m.ensureRelayClient,
		relayConfigured:   m.relayConfigured,
		relayURL: func() string {
			return m.relay.url
		},
		relayToken: func() string {
			return m.relay.token
		},
		publishEnvelopes: publishRelayEnvelopes,
		relayCAPath: func() string {
			return deps.RelayCAPath
		},
	})
	m.contactRequests = newContactRequestsModal(contactRequestsDeps{
		decide: m.makeContactRequestDecision,
	})
	m.loadContacts(deps.RecipientMailbox)
	m.loadContactRequests()
	m.syncRecipientDetails()
	m.syncInputPlaceholder()
	m.syncComposer()
	if m.peer.mailbox == "" {
		m.ui.focus = focusSidebar
	}
	m.filePicker.SetSize(m.ui.width, m.ui.height)
	return m
}

func defaultRelayClientFactory(url, token string) (RelayClient, error) {
	return relayapi.NewClient(url, token, relayclient.ClientOptions{})
}

func relayProfileName(profiles []config.RelayProfile, url, token string) string {
	for _, relay := range profiles {
		if relay.URL == url && relay.Token == token {
			return relay.Name
		}
	}
	return ""
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
	case paletteCloseMsg:
		return m.handlePaletteCloseMsg(msg)
	case paletteNavigateMsg:
		return m, m.commandPalette.OpenAtPath(msg.path)
	case paletteBackMsg:
		m.commandPalette.back()
		if !m.commandPalette.open && m.ui.focus == focusChat {
			m.input.Focus()
		}
		return m, nil
	case addContactCompletedMsg:
		return m.handleAddContactCompletedMsg(msg)
	case addContactImportResultMsg:
		return m.handleAddContactImportResult(msg)
	case addContactLookupResultMsg:
		return m.handleAddContactLookupResult(msg)
	case addContactInviteExchangeResultMsg:
		return m.handleAddContactInviteExchangeResult(msg)
	case addContactInviteStartedMsg:
		return m.handleAddContactInviteStarted(msg)
	case addRelaySavedMsg:
		return m.handleAddRelaySavedMsg(msg)
	case contactRequestSendResultMsg:
		return m.handleContactRequestSendResult(msg)
	case contactVerifyConfirmedMsg:
		return m.handleContactVerifyConfirmedMsg(msg)
	case editRelaySavedMsg:
		return m.handleEditRelaySavedMsg(msg)
	case contactRequestDecisionResultMsg:
		return m.handleContactRequestDecisionResult(msg)
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
		return m.handleConnectResultMsg(msg.client, msg.err)
	case reconnectResultMsg:
		return m.handleConnectResultMsg(msg.client, msg.err)
	case typingTickMsg:
		return m.handleTypingTickMsg(msg)
	case sendResultMsg:
		return m.handleSendResultMsg(msg)
	case voicePlaybackResultMsg:
		return m.handleVoicePlaybackResultMsg(msg)
	case voiceRecordingStartedMsg:
		return m.handleVoiceRecordingStartedMsg(msg)
	case voiceRecordingStoppedMsg:
		return m.handleVoiceRecordingStoppedMsg(msg)
	case voiceRecordingCanceledMsg:
		return m.handleVoiceRecordingCanceledMsg(msg)
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
	if m.voiceRecorder != nil {
		if err := m.voiceRecorder.Close(); err != nil {
			if m.voicePlayer != nil {
				_ = m.voicePlayer.Close()
			}
			_ = m.client.Close()
			return err
		}
	}
	if m.voicePlayer != nil {
		if err := m.voicePlayer.Close(); err != nil {
			_ = m.client.Close()
			return err
		}
	}
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
