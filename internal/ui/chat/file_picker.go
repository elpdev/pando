package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/ui/style"
)

type filePickerEntry struct {
	Name     string
	Path     string
	IsDir    bool
	IsParent bool
	Size     int64
}

type filePickerVisibleEntry struct {
	index int
	entry filePickerEntry
}

type filePickerModel struct {
	open     bool
	dir      string
	entries  []filePickerEntry
	selected int
	width    int
	height   int
}

func newFilePickerModel() filePickerModel {
	return filePickerModel{dir: defaultFilePickerDir()}
}

func defaultFilePickerDir() string {
	if dir, err := os.Getwd(); err == nil && dir != "" {
		return dir
	}
	if dir, err := os.UserHomeDir(); err == nil && dir != "" {
		return dir
	}
	return string(filepath.Separator)
}

func (m filePickerModel) Init() tea.Cmd {
	return nil
}

func (m *filePickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *filePickerModel) Open() error {
	return m.openAt(m.dir)
}

func (m *filePickerModel) Close() {
	m.open = false
	m.entries = nil
	m.selected = 0
}

func (m filePickerModel) Update(msg tea.Msg) (filePickerModel, tea.Cmd) {
	if !m.open {
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.Close()
		return m, func() tea.Msg { return filePickerClosedMsg{} }
	case tea.KeyBackspace:
		if err := m.goToParentDirectory(); err != nil {
			return m, func() tea.Msg { return filePickerErrorMsg{err: err} }
		}
		return m, nil
	case tea.KeyUp:
		m.moveSelection(-1)
		return m, nil
	case tea.KeyDown:
		m.moveSelection(1)
		return m, nil
	case tea.KeyEnter:
		entry := m.selectedEntry()
		if entry == nil {
			return m, nil
		}
		if entry.IsDir {
			if err := m.openAt(entry.Path); err != nil {
				return m, func() tea.Msg { return filePickerErrorMsg{err: err} }
			}
			return m, nil
		}
		m.Close()
		return m, func() tea.Msg { return filePickerSelectedMsg{path: entry.Path} }
	default:
		return m, nil
	}
}

func (m filePickerModel) View() string {
	title := style.Bold.Render("Attach File")
	dirLine := style.Muted.Render(m.dir)
	hint := style.Muted.Render("enter open/select  |  backspace up  |  esc cancel")
	lines := []string{title, dirLine, hint, ""}
	modalWidth := min(max(48, m.width-6), m.width)
	rowWidth := max(1, modalWidth-6)
	visibleEntries, hiddenAbove, hiddenBelow := m.visibleEntries(max(1, m.height-12))
	if len(m.entries) == 0 {
		lines = append(lines, style.Muted.Render("(empty) - backspace to go up"))
	} else {
		if hiddenAbove {
			lines = append(lines, style.Muted.Render("..."))
		}
		for _, visible := range visibleEntries {
			lines = append(lines, m.renderRow(visible.entry, visible.index == m.selected, rowWidth))
		}
		if hiddenBelow {
			lines = append(lines, style.Muted.Render("..."))
		}
	}
	modalHeight := max(8, m.height-4)
	modal := style.Modal.Padding(1).Width(max(1, modalWidth-4)).Height(max(1, modalHeight-4)).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, max(1, m.height), lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceBackground(style.BackdropTint))
}

func (m *filePickerModel) openAt(dir string) error {
	entries, cleanedDir, err := readFilePickerEntries(dir)
	if err != nil {
		return err
	}
	m.open = true
	m.dir = cleanedDir
	m.entries = entries
	m.selected = 0
	return nil
}

func (m *filePickerModel) goToParentDirectory() error {
	parent := filepath.Dir(m.dir)
	if parent == m.dir {
		return nil
	}
	return m.openAt(parent)
}

func (m *filePickerModel) moveSelection(delta int) {
	if len(m.entries) == 0 {
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.entries) {
		m.selected = len(m.entries) - 1
	}
}

func (m *filePickerModel) selectedEntry() *filePickerEntry {
	if m.selected < 0 || m.selected >= len(m.entries) {
		return nil
	}
	return &m.entries[m.selected]
}

func (m filePickerModel) renderRow(entry filePickerEntry, selected bool, width int) string {
	label := entry.Name
	if entry.IsDir {
		label += string(filepath.Separator)
	}
	sizeStr := ""
	if !entry.IsDir {
		sizeStr = formatFileSize(entry.Size)
	}
	pad := width - lipgloss.Width(label) - lipgloss.Width(sizeStr)
	if pad < 1 {
		pad = 1
	}
	line := label + strings.Repeat(" ", pad) + style.Muted.Render(sizeStr)

	rowStyle := lipgloss.NewStyle().Width(width)
	if entry.IsDir {
		rowStyle = rowStyle.Inherit(style.StatusOk)
	}
	if selected {
		rowStyle = rowStyle.Inherit(style.Selected).Bold(true)
	}
	return rowStyle.Render(line)
}

func (m filePickerModel) visibleEntries(maxEntries int) ([]filePickerVisibleEntry, bool, bool) {
	if len(m.entries) == 0 {
		return nil, false, false
	}
	if maxEntries <= 0 || len(m.entries) <= maxEntries {
		visible := make([]filePickerVisibleEntry, 0, len(m.entries))
		for idx, entry := range m.entries {
			visible = append(visible, filePickerVisibleEntry{index: idx, entry: entry})
		}
		return visible, false, false
	}
	start := m.selected - (maxEntries / 2)
	if start < 0 {
		start = 0
	}
	end := start + maxEntries
	if end > len(m.entries) {
		end = len(m.entries)
		start = end - maxEntries
	}
	visible := make([]filePickerVisibleEntry, 0, end-start)
	for idx := start; idx < end; idx++ {
		visible = append(visible, filePickerVisibleEntry{index: idx, entry: m.entries[idx]})
	}
	return visible, start > 0, end < len(m.entries)
}

func (m *Model) openFilePicker() error {
	if err := m.filePicker.Open(); err != nil {
		return err
	}
	m.input.Blur()
	return nil
}

func (m *Model) closeFilePicker() {
	m.filePicker.Close()
	m.input.Focus()
}

func (m *Model) sendAttachment(path, attachmentType string) tea.Cmd {
	if m.peer.isRoom {
		m.pushToast("attachments are not supported in #general yet", ToastWarn)
		return nil
	}
	var (
		batch       *messaging.OutgoingBatch
		displayBody string
		err         error
	)
	switch attachmentType {
	case messaging.AttachmentTypePhoto:
		batch, displayBody, err = m.messaging.PreparePhotoOutgoing(m.peer.mailbox, path)
	case messaging.AttachmentTypeVoice:
		batch, displayBody, err = m.messaging.PrepareVoiceOutgoing(m.peer.mailbox, path)
	case messaging.AttachmentTypeFile:
		batch, displayBody, err = m.messaging.PrepareFileOutgoing(m.peer.mailbox, path)
	default:
		m.pushToast(fmt.Sprintf("unsupported attachment type %q", attachmentType), ToastBad)
		return nil
	}
	if err != nil {
		m.pushToast(err.Error(), ToastBad)
		return nil
	}
	m.appendMessageItem(messageItem{
		direction:    "outbound",
		sender:       m.mailbox,
		body:         displayBody,
		timestamp:    time.Now().UTC(),
		messageID:    batchMessageID(batch),
		status:       statusPending,
		isAttachment: true,
	})
	m.input.SetValue("")
	m.resetLocalTypingState()
	m.syncViewportToBottom()
	return m.sendCmd(m.peer.mailbox, displayBody, batch)
}

func (m *Model) filePickerVisibleEntries(maxEntries int) ([]filePickerVisibleEntry, bool, bool) {
	return m.filePicker.visibleEntries(maxEntries)
}

func (m *Model) selectedFilePickerEntry() *filePickerEntry {
	return m.filePicker.selectedEntry()
}

func readFilePickerEntries(dir string) ([]filePickerEntry, string, error) {
	cleanedDir := filepath.Clean(dir)
	entries, err := os.ReadDir(cleanedDir)
	if err != nil {
		return nil, "", err
	}
	items := make([]filePickerEntry, 0, len(entries)+1)
	for _, entry := range entries {
		name := entry.Name()
		var size int64
		if !entry.IsDir() {
			if info, err := entry.Info(); err == nil {
				size = info.Size()
			}
		}
		items = append(items, filePickerEntry{
			Name:  name,
			Path:  filepath.Join(cleanedDir, name),
			IsDir: entry.IsDir(),
			Size:  size,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	if parent := filepath.Dir(cleanedDir); parent != cleanedDir {
		items = append([]filePickerEntry{{
			Name:     "..",
			Path:     parent,
			IsDir:    true,
			IsParent: true,
		}}, items...)
	}
	return items, cleanedDir, nil
}

func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes < KB:
		return fmt.Sprintf("%d B", bytes)
	case bytes < MB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	case bytes < GB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	default:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	}
}
