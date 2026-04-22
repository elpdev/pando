package chat

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/audio"
)

func (m *Model) startVoiceRecordingCmd() tea.Cmd {
	if err := m.guardCanSend(); err != nil {
		level := ToastWarn
		if m.conn.authFailed {
			level = ToastBad
		}
		m.pushToast(err.Error(), level)
		return nil
	}
	if m.peer.isRoom {
		m.pushToast("voice notes are not supported in #general yet", ToastWarn)
		return nil
	}
	if m.voiceRecorder == nil {
		m.pushToast("voice recorder is unavailable", ToastBad)
		return nil
	}
	if m.recording.active {
		m.pushToast("voice recording already in progress", ToastInfo)
		return nil
	}
	return func() tea.Msg {
		return voiceRecordingStartedMsg{err: m.voiceRecorder.Start()}
	}
}

func (m *Model) stopVoiceRecordingCmd() tea.Cmd {
	if !m.recording.active || m.voiceRecorder == nil {
		return nil
	}
	return func() tea.Msg {
		path, err := m.voiceRecorder.Stop()
		return voiceRecordingStoppedMsg{path: path, err: err}
	}
}

func (m *Model) cancelVoiceRecordingCmd() tea.Cmd {
	if !m.recording.active || m.voiceRecorder == nil {
		return nil
	}
	return func() tea.Msg {
		return voiceRecordingCanceledMsg{err: m.voiceRecorder.Cancel()}
	}
}

func (m *Model) handleVoiceRecordingStartedMsg(msg voiceRecordingStartedMsg) (*Model, tea.Cmd) {
	if msg.err != nil {
		m.pushToast(fmt.Sprintf("recording failed: %v", msg.err), ToastBad)
		return m, nil
	}
	m.recording.active = true
	m.recording.startedAt = time.Now().UTC()
	m.pushToast("recording voice note", ToastInfo)
	return m, nil
}

func (m *Model) handleVoiceRecordingStoppedMsg(msg voiceRecordingStoppedMsg) (*Model, tea.Cmd) {
	m.recording.active = false
	m.recording.startedAt = time.Time{}
	if msg.err != nil {
		m.pushToast(fmt.Sprintf("stop recording failed: %v", msg.err), ToastBad)
		return m, nil
	}
	if err := m.setPendingAttachmentWithCleanup(msg.path, messaging.AttachmentTypeVoice, removeFile(msg.path)); err != nil {
		m.pushToast(fmt.Sprintf("queue recording failed: %v", err), ToastBad)
		return m, nil
	}
	if m.pending != nil && m.pending.name == "" {
		m.pending.name = audio.RecordedFilename(msg.path)
	}
	return m, nil
}

func (m *Model) handleVoiceRecordingCanceledMsg(msg voiceRecordingCanceledMsg) (*Model, tea.Cmd) {
	m.recording.active = false
	m.recording.startedAt = time.Time{}
	if msg.err != nil {
		m.pushToast(fmt.Sprintf("cancel recording failed: %v", msg.err), ToastBad)
		return m, nil
	}
	m.pushToast("voice recording canceled", ToastInfo)
	return m, nil
}

func (m *Model) RecordingActive() bool {
	return m.recording.active
}

func (m *Model) RecordingDuration(now time.Time) time.Duration {
	if !m.recording.active || m.recording.startedAt.IsZero() {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Before(m.recording.startedAt) {
		return 0
	}
	return now.Sub(m.recording.startedAt)
}

func removeFile(path string) func() {
	return func() {
		if path != "" {
			_ = os.Remove(path)
		}
	}
}
