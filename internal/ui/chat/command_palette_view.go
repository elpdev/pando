package chat

import (
	"fmt"
	"strings"

	"github.com/elpdev/pando/internal/ui/style"
)

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
	switch m.mode {
	case commandPaletteModeThemes:
		return "Themes"
	case commandPaletteModeMessageTTL:
		return "Message TTL"
	}
	if m.mode == commandPaletteModeRelays {
		return "Relays"
	}
	if m.mode == commandPaletteModeRemoveRelay {
		return "Remove Relay"
	}
	if m.mode == commandPaletteModeEditRelay {
		return "Edit Relay"
	}
	return "Command Palette"
}

func (m commandPaletteModel) subtitle(peerLabel string) string {
	switch m.mode {
	case commandPaletteModeThemes:
		current := m.currentThemeName()
		if current == "" {
			return "Choose a theme and apply it immediately."
		}
		return fmt.Sprintf("Choose a theme. Current: %s", current)
	case commandPaletteModeMessageTTL:
		return fmt.Sprintf("Messages self-destruct after this duration on both sides. Current: %s", formatMessageTTL(m.currentMessageTTLValue()))
	}
	if m.mode == commandPaletteModeRelays {
		current := m.currentRelayName()
		if current == "" {
			return "Choose the active relay for this device."
		}
		return fmt.Sprintf("Choose the active relay. Current: %s", current)
	}
	if m.mode == commandPaletteModeRemoveRelay {
		return "Remove a saved relay profile. The active relay cannot leave you with none saved."
	}
	if m.mode == commandPaletteModeEditRelay {
		return "Choose a saved relay profile to update its name, URL, or token."
	}
	if m.hasPeer {
		return fmt.Sprintf("Jump to actions for %s or the current session.", peerLabel)
	}
	return "Search for a command or browse the available actions."
}

func (m commandPaletteModel) footer() string {
	if m.mode == commandPaletteModeThemes || m.mode == commandPaletteModeMessageTTL {
		return "type filter · up/down browse · enter apply · esc back"
	}
	if m.mode == commandPaletteModeRelays {
		return "type filter · up/down browse · enter switch · esc back"
	}
	if m.mode == commandPaletteModeRemoveRelay {
		return "type filter · up/down browse · enter remove · esc back"
	}
	if m.mode == commandPaletteModeEditRelay {
		return "type filter · up/down browse · enter edit · esc back"
	}
	return "type filter · up/down browse · enter select · esc close"
}
