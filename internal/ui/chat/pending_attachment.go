package chat

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/messaging"
)

func (m *Model) PendingAttachmentLabel() string {
	if m.pending == nil {
		return ""
	}
	label := m.pending.name
	if label == "" {
		label = m.pending.path
	}
	if m.pending.size > 0 {
		label += " " + formatFileSize(m.pending.size)
	}
	return label
}

func (m *Model) queuePendingAttachment(path, prefix string) error {
	kind := messaging.AttachmentTypeFile
	switch prefix {
	case "/send-photo":
		kind = messaging.AttachmentTypePhoto
	case "/send-voice":
		kind = messaging.AttachmentTypeVoice
	}
	return m.setPendingAttachment(path, kind)
}

func (m *Model) setPendingAttachment(path, kind string) error {
	return m.setPendingAttachmentWithCleanup(path, kind, nil)
}

func (m *Model) setPendingAttachmentWithCleanup(path, kind string, cleanup func()) error {
	info, err := fileInfo(path)
	if err != nil {
		return err
	}
	if m.pending != nil && m.pending.cleanup != nil {
		m.pending.cleanup()
	}
	m.pending = &pendingAttachment{
		path:    path,
		kind:    kind,
		name:    info.name,
		size:    info.size,
		cleanup: cleanup,
	}
	m.pushToast(fmt.Sprintf("queued %s", info.name), ToastInfo)
	return nil
}

func (m *Model) clearPendingAttachment() {
	if m.pending != nil && m.pending.cleanup != nil {
		m.pending.cleanup()
	}
	m.pending = nil
}

func (m *Model) consumePendingAttachment() tea.Cmd {
	if m.pending == nil {
		return nil
	}
	pending := *m.pending
	m.pending = nil
	return m.sendAttachment(pending.path, pending.kind)
}

type pathInfo struct {
	name string
	size int64
}

func fileInfo(path string) (pathInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return pathInfo{}, err
	}
	return pathInfo{name: info.Name(), size: info.Size()}, nil
}
