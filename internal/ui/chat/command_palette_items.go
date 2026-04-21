package chat

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elpdev/pando/internal/ui/style"
)

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
	if m.mode == commandPaletteModeRelays {
		return m.relayItems(false)
	}
	if m.mode == commandPaletteModeRemoveRelay {
		return m.relayItems(true)
	}
	if m.mode == commandPaletteModeEditRelay {
		return m.relayItems(false)
	}
	if m.mode == commandPaletteModeMessageTTL {
		return m.messageTTLItems()
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
			id:      string(commandPaletteCommandSendContactRequest),
			title:   "Send contact request",
			detail:  "Ask a discoverable mailbox to connect without importing them yet.",
			meta:    "CONTACT",
			aliases: []string{"contact", "request", "send", "introduce"},
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
			id:      string(commandPaletteCommandRelays),
			title:   "Relay",
			detail:  "Switch the active relay, reconnect, and use it for discovery.",
			meta:    "RELAY",
			aliases: []string{"relay", "switch", "server"},
		},
		{
			id:      string(commandPaletteCommandAddRelay),
			title:   "Add relay",
			detail:  "Save a new relay profile with name, URL, and optional token.",
			meta:    "RELAY",
			aliases: []string{"relay", "add", "server"},
		},
		{
			id:      string(commandPaletteCommandRemoveRelay),
			title:   "Remove relay",
			detail:  "Delete a saved relay profile and keep another relay active.",
			meta:    "RELAY",
			aliases: []string{"relay", "remove", "delete", "server"},
		},
		{
			id:      string(commandPaletteCommandEditRelay),
			title:   "Edit relay",
			detail:  "Update a saved relay profile, including its name, URL, or token.",
			meta:    "RELAY",
			aliases: []string{"relay", "edit", "rename", "server"},
		},
		{
			id:      string(commandPaletteCommandThemes),
			title:   "Themes",
			detail:  "Switch the active terminal theme and save it to device config.",
			meta:    "THEME",
			aliases: []string{"theme", "themes", "appearance"},
		},
		{
			id:      string(commandPaletteCommandMessageTTL),
			title:   "Message TTL",
			detail:  fmt.Sprintf("Set how long messages live before self-destructing. Current: %s.", formatMessageTTL(m.currentMessageTTLValue())),
			meta:    "TTL",
			aliases: []string{"ttl", "expire", "self-destruct", "destruct", "message"},
		},
	}
	if hasPeer {
		items = append(items, commandPaletteItem{
			id:      string(commandPaletteCommandVerifyContact),
			title:   "Verify contact",
			detail:  "Confirm the active contact's fingerprint and mark them as manually verified.",
			meta:    "VERIFY",
			aliases: []string{"verify", "trust", "fingerprint", "contact"},
		})
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

func (m commandPaletteModel) relayItems(remove bool) []commandPaletteItem {
	if m.deps.relayProfiles == nil {
		return nil
	}
	relays := m.deps.relayProfiles()
	current := m.currentRelayName()
	items := make([]commandPaletteItem, 0, len(relays))
	for _, relay := range relays {
		detail := relay.URL
		meta := ""
		if relay.Token != "" {
			detail += "  token configured"
		}
		if relay.Name == current {
			meta = "ACTIVE"
		}
		if remove && len(relays) <= 1 {
			meta = "LOCKED"
		}
		items = append(items, commandPaletteItem{
			id:      relay.Name,
			title:   relay.Name,
			detail:  detail,
			meta:    meta,
			aliases: []string{relay.Name, relay.URL, "relay"},
		})
	}
	return items
}

func (m commandPaletteModel) currentRelayName() string {
	if m.deps.currentRelayName == nil {
		return ""
	}
	return m.deps.currentRelayName()
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

func (m commandPaletteModel) currentMessageTTLValue() time.Duration {
	if m.deps.currentMessageTTL == nil {
		return 0
	}
	return m.deps.currentMessageTTL()
}

func (m commandPaletteModel) messageTTLItems() []commandPaletteItem {
	current := m.currentMessageTTLValue()
	items := make([]commandPaletteItem, 0, len(messageTTLOptions))
	for _, option := range messageTTLOptions {
		label := formatMessageTTL(option)
		detail := "Self-destruct after " + label + "."
		meta := ""
		if option == current {
			detail = "Current setting."
			meta = "ACTIVE"
		}
		items = append(items, commandPaletteItem{
			id:      option.String(),
			title:   label,
			detail:  detail,
			meta:    meta,
			aliases: []string{label, "ttl", "expire"},
		})
	}
	return items
}

func formatMessageTTL(d time.Duration) string {
	if d <= 0 {
		return "default"
	}
	hours := int(d / time.Hour)
	if hours > 0 && d%time.Hour == 0 {
		if hours == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", hours)
	}
	return d.String()
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
