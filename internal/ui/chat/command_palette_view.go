package chat

import (
	"fmt"
	"strings"

	"github.com/elpdev/pando/internal/ui/style"
)

// View renders the palette as a floating overlay on top of the provided base
// view. The surrounding chat view keeps rendering so the user never loses
// context while the palette is open.
func (m commandPaletteModel) View(base string, width, height int, peerLabel string) string {
	if !m.open {
		return base
	}
	bodyWidth := max(1, paletteWidth(width)-6)
	bodyHeight := max(1, paletteHeight(height)-6)
	// When the path points to a view node, hand the body region over to the
	// view's Body method and let it pick its own subtitle/footer.
	if view := m.activeView(); view != nil {
		subtitle := view.Subtitle()
		if subtitle == "" {
			subtitle = m.subtitle(peerLabel)
		}
		footer := view.Footer()
		if footer == "" {
			footer = m.footer()
		}
		return renderFloatingPaletteOverlay(base, width, height, m.title(), subtitle, []string{view.Body(bodyWidth, bodyHeight)}, footer)
	}
	m.filter.Width = max(1, bodyWidth-2)
	filterBox := style.PaletteInput.Width(bodyWidth).Padding(0, 1).Render(m.filter.View())
	items := m.visibleItems(m.hasPeer)
	lines := []string{filterBox}
	if len(items) == 0 {
		lines = append(lines, style.Muted.Render("No commands match this search."))
	} else {
		for idx, item := range items {
			title := item.item.title
			if item.item.breadcrumb != "" {
				title = style.Muted.Render(item.item.breadcrumb+" › ") + title
			}
			lines = append(lines, renderPaletteListItemMatched(bodyWidth, idx == m.selected, title, item.item.detail, item.item.meta, item.matched))
		}
	}
	return renderFloatingPaletteOverlay(base, width, height, m.title(), m.subtitle(peerLabel), []string{strings.Join(lines, "\n")}, m.footer())
}

func (m commandPaletteModel) title() string {
	parts := []string{"Pando"}
	ctx := m.ctx()
	nodes := rootNodes(ctx)
	for _, id := range m.path {
		node, ok := findNode(nodes, id, ctx)
		if !ok {
			parts = append(parts, id)
			break
		}
		parts = append(parts, node.title)
		if node.children == nil {
			break
		}
		nodes = node.children(ctx)
	}
	return strings.Join(parts, " › ")
}

func (m commandPaletteModel) subtitle(peerLabel string) string {
	if m.atRoot() {
		if m.hasPeer {
			return fmt.Sprintf("Jump to actions for %s or the current session.", peerLabel)
		}
		return "Search for a command or browse the available actions."
	}
	node, ok := m.nodeAtPath(m.path)
	if !ok {
		return ""
	}
	switch node.id {
	case paletteNodeIDTheme:
		current := currentThemeLabel(m.ctx())
		if current == "" {
			return "Choose a theme and apply it immediately."
		}
		return fmt.Sprintf("Choose a theme. Current: %s", current)
	case paletteNodeIDMessageTTL:
		return fmt.Sprintf("Messages self-destruct after this duration on both sides. Current: %s", formatMessageTTL(currentTTL(m.ctx())))
	case paletteNodeIDSwitchRelay:
		current := ""
		if m.deps.currentRelayName != nil {
			current = m.deps.currentRelayName()
		}
		if current == "" {
			return "Choose the active relay for this device."
		}
		return fmt.Sprintf("Choose the active relay. Current: %s", current)
	case paletteNodeIDVoiceNotes:
		return "Choose a recent voice note from this chat. Selecting one replaces the current playback."
	case paletteNodeIDEditRelay:
		return "Choose a saved relay profile to update its name, URL, or token."
	case paletteNodeIDRemoveRelay:
		return "Remove a saved relay profile. The active relay cannot leave you with none saved."
	}
	if node.detail != "" {
		return node.detail
	}
	return ""
}

func (m commandPaletteModel) footer() string {
	if m.atRoot() {
		return "type filter · up/down browse · enter select · esc close"
	}
	node, ok := m.nodeAtPath(m.path)
	if !ok {
		return "type filter · up/down browse · enter select · esc back"
	}
	switch node.id {
	case paletteNodeIDTheme, paletteNodeIDMessageTTL, paletteNodeIDVoiceNotes:
		return "type filter · up/down browse · enter apply · esc back"
	case paletteNodeIDSwitchRelay:
		return "type filter · up/down browse · enter switch · esc back"
	case paletteNodeIDEditRelay:
		return "type filter · up/down browse · enter edit · esc back"
	case paletteNodeIDRemoveRelay:
		return "type filter · up/down browse · enter remove · esc back"
	}
	return "type filter · up/down browse · enter select · esc back"
}
