package chat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/ui/style"
)

type commandPaletteMode int

const (
	commandPaletteModeRoot commandPaletteMode = iota
	commandPaletteModeThemes
)

type commandPaletteCommand string

const (
	commandPaletteCommandAddContact      commandPaletteCommand = "add-contact"
	commandPaletteCommandAttachFile      commandPaletteCommand = "attach-file"
	commandPaletteCommandContactRequests commandPaletteCommand = "contact-requests"
	commandPaletteCommandPeerDetail      commandPaletteCommand = "peer-detail"
	commandPaletteCommandThemes          commandPaletteCommand = "themes"
)

type commandPaletteDeps struct {
	applyTheme   func(style.Theme)
	currentTheme func() string
	saveTheme    func(name string) error
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
	command   commandPaletteCommand
	themeName string
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
	if m.mode == commandPaletteModeThemes {
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
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommand(item.id)}, nil
	case commandPaletteModeThemes:
		m.Close()
		return &commandPaletteAction{command: commandPaletteCommandThemes, themeName: item.id}, nil
	default:
		return nil, nil
	}
}

func (m commandPaletteModel) View(width, height int, peerLabel string) string {
	if !m.open {
		return ""
	}
	bodyWidth := max(1, paletteWidth(width)-6)
	m.filter.Width = max(1, bodyWidth-2)
	filterBox := style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(m.filter.View())
	items := m.visibleItems(m.hasPeer)
	lines := []string{filterBox}
	if len(items) == 0 {
		lines = append(lines, style.Muted.Render("No commands match this search."))
	} else {
		for idx, item := range items {
			lines = append(lines, renderPaletteListItemMatched(bodyWidth, idx == m.selected, item.item.title, item.item.detail, item.item.meta, item.matched))
		}
	}
	return renderPaletteOverlay(width, height, m.title(), m.subtitle(peerLabel), []string{strings.Join(lines, "\n")}, m.footer())
}

func (m commandPaletteModel) title() string {
	if m.mode == commandPaletteModeThemes {
		return "Themes"
	}
	return "Command Palette"
}

func (m commandPaletteModel) subtitle(peerLabel string) string {
	if m.mode == commandPaletteModeThemes {
		current := m.currentThemeName()
		if current == "" {
			return "Choose a theme and apply it immediately."
		}
		return fmt.Sprintf("Choose a theme. Current: %s", current)
	}
	if m.hasPeer {
		return fmt.Sprintf("Jump to actions for %s or the current session.", peerLabel)
	}
	return "Search for a command or browse the available actions."
}

func (m commandPaletteModel) footer() string {
	if m.mode == commandPaletteModeThemes {
		return "type filter · up/down browse · enter apply · esc back"
	}
	return "type filter · up/down browse · enter select · esc close"
}

func (m *commandPaletteModel) moveSelection(delta int) {
	items := m.visibleItems(m.hasPeer)
	if len(items) == 0 {
		m.selected = 0
		return
	}
	m.selected = (m.selected + delta) % len(items)
	if m.selected < 0 {
		m.selected += len(items)
	}
}

func (m commandPaletteModel) selectedItem() *commandPaletteItem {
	items := m.visibleItems(m.hasPeer)
	if m.selected < 0 || m.selected >= len(items) {
		return nil
	}
	item := items[m.selected].item
	return &item
}

func (m commandPaletteModel) visibleItems(hasPeer bool) []commandPaletteVisibleItem {
	items := m.items(hasPeer)
	query := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	visible := make([]commandPaletteVisibleItem, 0, len(items))
	for _, item := range items {
		matched := subsequenceMatch(item.title, query)
		if query != "" && matched == nil {
			for _, alias := range item.aliases {
				if subsequenceMatch(alias, query) != nil {
					matched = map[int]struct{}{}
					break
				}
			}
		}
		if query != "" && matched == nil {
			continue
		}
		visible = append(visible, commandPaletteVisibleItem{item: item, matched: matched})
	}
	if m.selected >= len(visible) {
		m.selected = max(0, len(visible)-1)
	}
	return visible
}

func (m commandPaletteModel) items(hasPeer bool) []commandPaletteItem {
	if m.mode == commandPaletteModeThemes {
		return m.themeItems()
	}
	items := []commandPaletteItem{
		{
			id:      string(commandPaletteCommandAddContact),
			title:   "Add contact",
			detail:  "Import a peer invite, look up a mailbox, or verify with an exchange code.",
			meta:    "CONTACT",
			aliases: []string{"contact", "add", "invite"},
		},
		{
			id:      string(commandPaletteCommandContactRequests),
			title:   contactRequestsPaletteTitle(m.pendingRequestsCount),
			detail:  "Review pending requests to connect, then accept or reject them.",
			meta:    contactRequestsPaletteMeta(m.pendingRequestsCount),
			aliases: []string{"contact", "requests", "inbox", "pending"},
		},
		{
			id:      string(commandPaletteCommandAttachFile),
			title:   "Attach file",
			detail:  "Browse the local filesystem and queue one attachment for the active chat.",
			meta:    "ATTACH",
			aliases: []string{"attach", "file", "upload"},
		},
		{
			id:      string(commandPaletteCommandThemes),
			title:   "Themes",
			detail:  "Switch the active terminal theme and save it to device config.",
			meta:    "THEME",
			aliases: []string{"theme", "themes", "appearance"},
		},
	}
	if hasPeer {
		items = append(items, commandPaletteItem{
			id:      string(commandPaletteCommandPeerDetail),
			title:   "Peer detail",
			detail:  "Inspect the current peer fingerprint, trust state, devices, and relay.",
			meta:    "DETAIL",
			aliases: []string{"detail", "peer", "info"},
		})
	}
	return items
}

func (m commandPaletteModel) themeItems() []commandPaletteItem {
	names := make([]string, 0, len(style.Themes))
	for name := range style.Themes {
		names = append(names, name)
	}
	sort.Strings(names)
	current := m.currentThemeName()
	items := make([]commandPaletteItem, 0, len(names))
	for _, name := range names {
		detail := "Built-in theme"
		meta := ""
		if name == current {
			detail = "Current theme"
			meta = "ACTIVE"
		}
		items = append(items, commandPaletteItem{
			id:      name,
			title:   name,
			detail:  detail,
			meta:    meta,
			aliases: []string{name, "theme"},
		})
	}
	return items
}

func (m commandPaletteModel) currentThemeName() string {
	if m.deps.currentTheme == nil {
		return ""
	}
	return m.deps.currentTheme()
}

func contactRequestsPaletteTitle(count int) string {
	if count <= 0 {
		return "Contact requests"
	}
	return fmt.Sprintf("Contact requests (%d)", count)
}

func contactRequestsPaletteMeta(count int) string {
	if count <= 0 {
		return "INBOX"
	}
	return "PENDING"
}
