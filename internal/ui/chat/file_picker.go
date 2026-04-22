package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
	filter   textinput.Model
	width    int
	height   int
}

func newFilePickerModel() filePickerModel {
	return filePickerModel{dir: defaultFilePickerDir(), filter: newFilePickerInput()}
}

func newFilePickerInput() textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Type to filter files"
	input.CharLimit = 256
	return input
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
	m.filter = newFilePickerInput()
}

func (m filePickerModel) Update(msg tea.Msg) (filePickerModel, tea.Cmd) {
	if !m.open {
		return m, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, cmd
	}
	switch keyMsg.Type {
	case tea.KeyEsc:
		m.Close()
		return m, func() tea.Msg { return filePickerClosedMsg{} }
	case tea.KeyBackspace:
		if strings.TrimSpace(m.filter.Value()) != "" {
			before := m.filter.Value()
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			if m.filter.Value() != before {
				m.selected = 0
			}
			return m, cmd
		}
		if err := m.goToParentDirectory(); err != nil {
			return m, func() tea.Msg { return filePickerErrorMsg{err: err} }
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveSelection(-1)
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
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
		before := m.filter.Value()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.selected = 0
		}
		return m, cmd
	}
}

func (m filePickerModel) View(base string) string {
	bodyWidth := max(1, paletteWidth(m.width)-6)
	filterBox := style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(m.filter.View())
	lines := []string{style.PaletteMeta.Width(bodyWidth).Render(m.dir), filterBox}
	visibleEntries, hiddenAbove, hiddenBelow := m.visibleEntries(max(1, paletteHeight(m.height)-11))
	if len(visibleEntries) == 0 {
		emptyText := "(empty)"
		if strings.TrimSpace(m.filter.Value()) != "" {
			emptyText = "No files match this filter."
		} else if len(m.entries) > 0 {
			emptyText = "(empty) - backspace to go up"
		}
		lines = append(lines, style.Muted.Render(emptyText))
	} else {
		if hiddenAbove {
			lines = append(lines, style.PaletteMeta.Render("..."))
		}
		for _, visible := range visibleEntries {
			lines = append(lines, m.renderRow(visible.entry, visible.index == m.selected, bodyWidth))
		}
		if hiddenBelow {
			lines = append(lines, style.PaletteMeta.Render("..."))
		}
	}
	footer := "type to filter · up/down browse · enter open or select · esc cancel"
	if strings.TrimSpace(m.filter.Value()) == "" {
		footer = "type to filter · up/down browse · enter open or select · backspace up · esc cancel"
	}
	return renderFloatingPaletteOverlay(base, m.width, max(1, m.height), "Attach File", "Browse locally and queue one attachment.", []string{strings.Join(lines, "\n")}, footer)
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
	m.filter = newFilePickerInput()
	_ = m.filter.Focus()
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
	filtered := m.filteredEntries()
	if len(filtered) == 0 {
		return
	}
	m.selected = (m.selected + delta) % len(filtered)
	if m.selected < 0 {
		m.selected += len(filtered)
	}
}

func (m *filePickerModel) selectedEntry() *filePickerEntry {
	filtered := m.filteredEntries()
	if m.selected < 0 || m.selected >= len(filtered) {
		return nil
	}
	return &filtered[m.selected]
}

func (m filePickerModel) renderRow(entry filePickerEntry, selected bool, width int) string {
	label := entry.Name
	if entry.IsDir {
		label += string(filepath.Separator)
	}
	detail := ""
	meta := ""
	if entry.IsParent {
		detail = "Go to the parent directory."
		meta = "UP"
	}
	if !entry.IsDir {
		detail = "Ready to attach this file."
		meta = formatFileSize(entry.Size)
	} else if !entry.IsParent {
		detail = "Open this directory."
		meta = "DIR"
	}
	return renderPaletteListItemMatched(width, selected, label, detail, meta, subsequenceMatch(label, m.filter.Value()))
}

func (m filePickerModel) visibleEntries(maxEntries int) ([]filePickerVisibleEntry, bool, bool) {
	entries := m.filteredEntries()
	if len(entries) == 0 {
		return nil, false, false
	}
	if maxEntries <= 0 || len(entries) <= maxEntries {
		visible := make([]filePickerVisibleEntry, 0, len(entries))
		for idx, entry := range entries {
			visible = append(visible, filePickerVisibleEntry{index: idx, entry: entry})
		}
		return visible, false, false
	}
	start := m.selected - (maxEntries / 2)
	if start < 0 {
		start = 0
	}
	end := start + maxEntries
	if end > len(entries) {
		end = len(entries)
		start = end - maxEntries
	}
	visible := make([]filePickerVisibleEntry, 0, end-start)
	for idx := start; idx < end; idx++ {
		visible = append(visible, filePickerVisibleEntry{index: idx, entry: entries[idx]})
	}
	return visible, start > 0, end < len(entries)
}

func (m filePickerModel) filteredEntries() []filePickerEntry {
	query := strings.TrimSpace(m.filter.Value())
	if query == "" {
		return m.entries
	}
	filtered := make([]filePickerEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		if subsequenceMatch(entry.Name, query) != nil {
			filtered = append(filtered, entry)
		}
	}
	return filtered
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
	now := time.Now().UTC()
	m.appendMessageItem(messageItem{
		kind:       transcriptMessage,
		direction:  "outbound",
		sender:     m.mailbox,
		body:       displayBody,
		timestamp:  now,
		messageID:  batchMessageID(batch),
		status:     statusPending,
		attachment: batch.Attachment,
		expiresAt:  m.outgoingItemExpiresAt(now),
	})
	m.input.SetValue("")
	m.syncComposer()
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
