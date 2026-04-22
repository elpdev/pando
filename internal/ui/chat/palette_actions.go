package chat

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/ui/style"
)

func (m *Model) openCommandPalette() tea.Cmd {
	m.commandPalette.SyncContext(m.peer.mailbox != "", m.pendingRequestsCount, m.recentVoiceNotes(), m.recording.active, m.voicePlayer != nil && m.voicePlayer.IsPlaying())
	m.input.Blur()
	return m.commandPalette.Open()
}

func (m *Model) handleCommandPaletteAction(action commandPaletteAction) tea.Cmd {
	switch action.command {
	case commandPaletteCommandAttachFile:
		return m.handleAttachKey()
	case commandPaletteCommandRecordVoiceNote:
		return m.startVoiceRecordingCmd()
	case commandPaletteCommandStopRecording:
		return m.stopVoiceRecordingCmd()
	case commandPaletteCommandCancelRecording:
		return m.cancelVoiceRecordingCmd()
	case commandPaletteCommandThemes:
		return m.applyPaletteTheme(action.themeName)
	case commandPaletteCommandSwitchRelay:
		return m.switchRelay(action.relayName)
	case commandPaletteCommandPlayVoiceNote:
		return m.playVoiceNoteCmd(action.voiceNoteID)
	case commandPaletteCommandStopVoiceNote:
		return m.stopVoiceNotePlayback()
	case commandPaletteCommandVoiceNotes:
		return nil
	case commandPaletteCommandRemoveRelay:
		return m.removeRelayProfile(action.relayName)
	case commandPaletteCommandMessageTTL:
		return m.applyPaletteMessageTTL(action.messageTTL)
	default:
		return nil
	}
}

func (m *Model) applyPaletteTheme(name string) tea.Cmd {
	theme, ok := style.Themes[name]
	if !ok {
		m.pushToast(fmt.Sprintf("unknown theme %q", name), ToastBad)
		return nil
	}
	style.Apply(theme)
	m.pushToast(fmt.Sprintf("theme set to %s", name), ToastInfo)
	if m.commandPalette.deps.saveTheme == nil {
		return nil
	}
	if err := m.commandPalette.deps.saveTheme(name); err != nil {
		m.pushToast(fmt.Sprintf("theme applied but not saved: %v", err), ToastWarn)
	}
	return nil
}

func (m *Model) applyPaletteMessageTTL(ttl time.Duration) tea.Cmd {
	if ttl <= 0 {
		return nil
	}
	m.messaging.SetMessageTTL(ttl)
	m.pushToast(fmt.Sprintf("message TTL set to %s", formatMessageTTL(ttl)), ToastInfo)
	if m.commandPalette.deps.saveMessageTTL == nil {
		return nil
	}
	if err := m.commandPalette.deps.saveMessageTTL(ttl); err != nil {
		m.pushToast(fmt.Sprintf("TTL applied but not saved: %v", err), ToastWarn)
	}
	return nil
}
