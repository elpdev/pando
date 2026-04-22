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

// visibleItems returns the items the palette should render in its list. The
// hasPeer parameter is redundant with the model's field but kept for backward
// compatibility with tests that call this directly.
func (m commandPaletteModel) visibleItems(hasPeer bool) []commandPaletteVisibleItem {
	ctx := m.ctx()
	ctx.hasPeer = hasPeer
	query := strings.TrimSpace(strings.ToLower(m.filter.Value()))

	var items []commandPaletteItem
	if m.atRoot() && query != "" {
		items = m.flattenForSearch(ctx)
	} else {
		items = m.itemsAtCurrentLevel(ctx)
	}

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
		if query != "" && matched == nil && item.breadcrumb != "" {
			if subsequenceMatch(item.breadcrumb+" "+item.title, query) != nil {
				matched = map[int]struct{}{}
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

// itemsAtCurrentLevel returns the children of the node at m.path, or the root
// children if m.path is empty.
func (m commandPaletteModel) itemsAtCurrentLevel(ctx paletteCtx) []commandPaletteItem {
	nodes := m.nodesAtCurrentLevel(ctx)
	parent := append([]string(nil), m.path...)
	items := make([]commandPaletteItem, 0, len(nodes))
	for _, node := range nodes {
		if node.visible != nil && !node.visible(ctx) {
			continue
		}
		items = append(items, commandPaletteItem{
			id:       node.id,
			title:    node.title,
			detail:   node.detail,
			meta:     node.meta,
			aliases:  node.aliases,
			node:     node,
			nodePath: append(append([]string(nil), parent...), node.id),
		})
	}
	return items
}

func (m commandPaletteModel) nodesAtCurrentLevel(ctx paletteCtx) []paletteNode {
	if m.atRoot() {
		return rootNodes(ctx)
	}
	nodes := rootNodes(ctx)
	for _, id := range m.path {
		next, ok := findNode(nodes, id, ctx)
		if !ok {
			return nil
		}
		if next.children == nil {
			return nil
		}
		nodes = next.children(ctx)
	}
	return nodes
}

func findNode(nodes []paletteNode, id string, ctx paletteCtx) (paletteNode, bool) {
	for _, node := range nodes {
		if node.visible != nil && !node.visible(ctx) {
			continue
		}
		if node.id == id {
			return node, true
		}
	}
	return paletteNode{}, false
}

// nodeAtPath walks the tree along the given id path and returns the node (or
// false if any link is missing). Used by the view layer to compute breadcrumb
// titles and subtitles.
func (m commandPaletteModel) nodeAtPath(path []string) (paletteNode, bool) {
	ctx := m.ctx()
	nodes := rootNodes(ctx)
	var current paletteNode
	for i, id := range path {
		node, ok := findNode(nodes, id, ctx)
		if !ok {
			return paletteNode{}, false
		}
		current = node
		if i == len(path)-1 {
			return current, true
		}
		if node.children == nil {
			return paletteNode{}, false
		}
		nodes = node.children(ctx)
	}
	return current, len(path) > 0
}

// flattenForSearch walks the structural tree (skipping dynamic children like
// themes and relay profiles) and returns a flat list of items with breadcrumb
// prefixes. Each item points back to its node so activate() can drill into it
// or fire its action.
func (m commandPaletteModel) flattenForSearch(ctx paletteCtx) []commandPaletteItem {
	var out []commandPaletteItem
	var walk func(nodes []paletteNode, parentLabels []string, parentIDs []string)
	walk = func(nodes []paletteNode, parentLabels []string, parentIDs []string) {
		for _, node := range nodes {
			if node.visible != nil && !node.visible(ctx) {
				continue
			}
			breadcrumb := strings.Join(parentLabels, " › ")
			path := append(append([]string(nil), parentIDs...), node.id)
			out = append(out, commandPaletteItem{
				id:         node.id,
				title:      node.title,
				detail:     node.detail,
				meta:       node.meta,
				aliases:    node.aliases,
				breadcrumb: breadcrumb,
				node:       node,
				nodePath:   path,
			})
			if node.children != nil && !node.dynamic {
				walk(node.children(ctx), append(append([]string(nil), parentLabels...), node.title), path)
			}
		}
	}
	walk(rootNodes(ctx), nil, nil)
	return out
}

// rootNodes builds the top-level command tree. Structure:
//
//	Contacts  ├─ Add contact / Send contact request / Contact requests (N)
//	          └─ Verify contact / Peer detail  (only with an active chat)
//	Relays    ├─ Switch relay (dynamic) / Add relay
//	          └─ Edit relay (dynamic) / Remove relay (dynamic)
//	Settings  ├─ Theme (dynamic)
//	          └─ Message TTL (dynamic)
//	Attach file  (only with an active chat)
func rootNodes(ctx paletteCtx) []paletteNode {
	return []paletteNode{
		contactsNode(ctx),
		relaysNode(),
		settingsNode(),
		{
			id:      string(commandPaletteCommandAttachFile),
			title:   "Attach file",
			detail:  "Browse the local filesystem and queue one attachment for the active chat.",
			meta:    "ATTACH",
			aliases: []string{"attach", "file", "upload"},
			visible: func(c paletteCtx) bool { return c.hasPeer },
			action:  &commandPaletteAction{command: commandPaletteCommandAttachFile},
		},
		{
			id:      string(commandPaletteCommandStopVoiceNote),
			title:   "Stop voice note",
			detail:  "Stop the voice note that is currently playing.",
			meta:    "VOICE",
			aliases: []string{"voice", "audio", "stop", "playback"},
			visible: func(c paletteCtx) bool { return c.voicePlaybackActive },
			action:  &commandPaletteAction{command: commandPaletteCommandStopVoiceNote},
		},
		voiceNotesNode(),
	}
}

func voiceNotesNode() paletteNode {
	return paletteNode{
		id:      paletteNodeIDVoiceNotes,
		title:   "Voice notes",
		detail:  "Browse recent voice notes in this chat and play one in the terminal.",
		meta:    "VOICE",
		aliases: []string{"voice", "audio", "notes", "play"},
		visible: func(c paletteCtx) bool { return c.hasPeer && len(c.voiceNotes) > 0 },
		dynamic: true,
		children: func(c paletteCtx) []paletteNode {
			nodes := make([]paletteNode, 0, len(c.voiceNotes))
			for _, note := range c.voiceNotes {
				note := note
				nodes = append(nodes, paletteNode{
					id:      note.id,
					title:   note.filename,
					detail:  formatVoiceNoteDetail(note),
					meta:    "VOICE",
					aliases: []string{note.filename, note.direction, "voice", "audio"},
					action:  &commandPaletteAction{command: commandPaletteCommandPlayVoiceNote, voiceNoteID: note.id},
				})
			}
			return nodes
		},
	}
}

func contactsNode(ctx paletteCtx) paletteNode {
	title := "Contacts"
	meta := "CONTACTS"
	if ctx.pendingRequestsCount > 0 {
		title = fmt.Sprintf("Contacts (%d pending)", ctx.pendingRequestsCount)
		meta = "PENDING"
	}
	return paletteNode{
		id:      paletteNodeIDContacts,
		title:   title,
		detail:  "Add, verify, or review contacts and connection requests.",
		meta:    meta,
		aliases: []string{"contact", "contacts", "peer", "peers", "requests", "inbox"},
		children: func(c paletteCtx) []paletteNode {
			kids := []paletteNode{
				{
					id:      string(commandPaletteCommandAddContact),
					title:   "Add contact",
					detail:  "Import a peer invite, look up a mailbox, or verify with an exchange code.",
					meta:    "ADD",
					aliases: []string{"add", "invite", "import", "contact"},
					action:  &commandPaletteAction{command: commandPaletteCommandAddContact},
				},
				{
					id:      string(commandPaletteCommandSendContactRequest),
					title:   "Send contact request",
					detail:  "Ask a discoverable mailbox to connect without importing them yet.",
					meta:    "SEND",
					aliases: []string{"send", "request", "introduce", "invite"},
					view:    paletteViewContactRequestSend,
				},
				{
					id:      string(commandPaletteCommandContactRequests),
					title:   contactRequestsPaletteTitle(c.pendingRequestsCount),
					detail:  "Review pending requests to connect, then accept or reject them.",
					meta:    contactRequestsPaletteMeta(c.pendingRequestsCount),
					aliases: []string{"requests", "inbox", "pending", "accept", "reject"},
					view:    paletteViewContactRequests,
				},
			}
			if c.hasPeer {
				kids = append(kids, paletteNode{
					id:      string(commandPaletteCommandVerifyContact),
					title:   "Verify contact",
					detail:  "Confirm the active contact's fingerprint and mark them as manually verified.",
					meta:    "VERIFY",
					aliases: []string{"verify", "trust", "fingerprint"},
					view:    paletteViewContactVerify,
				})
				kids = append(kids, paletteNode{
					id:      string(commandPaletteCommandPeerDetail),
					title:   "Peer detail",
					detail:  "Inspect the current peer fingerprint, trust state, devices, and relay.",
					meta:    "DETAIL",
					aliases: []string{"detail", "peer", "info"},
					view:    paletteViewPeerDetail,
				})
			}
			return kids
		},
	}
}

func relaysNode() paletteNode {
	return paletteNode{
		id:      paletteNodeIDRelays,
		title:   "Relays",
		detail:  "Switch the active relay or manage saved relay profiles.",
		meta:    "RELAYS",
		aliases: []string{"relay", "relays", "server", "servers"},
		children: func(c paletteCtx) []paletteNode {
			return []paletteNode{
				{
					id:       paletteNodeIDSwitchRelay,
					title:    "Switch relay",
					detail:   "Pick a saved relay profile to become the active relay for this device.",
					meta:     "SWITCH",
					aliases:  []string{"switch", "change", "select", "active"},
					dynamic:  true,
					children: relayListChildren(commandPaletteCommandSwitchRelay, false),
				},
				{
					id:      string(commandPaletteCommandAddRelay),
					title:   "Add relay",
					detail:  "Save a new relay profile with name, URL, and optional token.",
					meta:    "ADD",
					aliases: []string{"add", "new", "create"},
					view:    paletteViewAddRelay,
				},
				{
					id:       paletteNodeIDEditRelay,
					title:    "Edit relay",
					detail:   "Update a saved relay profile, including its name, URL, or token.",
					meta:     "EDIT",
					aliases:  []string{"edit", "rename", "update"},
					dynamic:  true,
					children: editRelayChildren,
				},
				{
					id:       paletteNodeIDRemoveRelay,
					title:    "Remove relay",
					detail:   "Delete a saved relay profile and keep another relay active.",
					meta:     "REMOVE",
					aliases:  []string{"remove", "delete"},
					dynamic:  true,
					children: relayListChildren(commandPaletteCommandRemoveRelay, true),
				},
			}
		},
	}
}

func settingsNode() paletteNode {
	return paletteNode{
		id:      paletteNodeIDSettings,
		title:   "Settings",
		detail:  "Appearance and message retention preferences for this device.",
		meta:    "SETTINGS",
		aliases: []string{"settings", "config", "preferences", "options"},
		children: func(c paletteCtx) []paletteNode {
			return []paletteNode{
				{
					id:       paletteNodeIDTheme,
					title:    "Theme",
					detail:   fmt.Sprintf("Switch the active terminal theme. Current: %s.", currentThemeLabel(c)),
					meta:     "THEME",
					aliases:  []string{"theme", "themes", "appearance", "colors", "color"},
					dynamic:  true,
					children: themeChildren,
				},
				{
					id:       paletteNodeIDMessageTTL,
					title:    "Message TTL",
					detail:   fmt.Sprintf("Set how long messages live before self-destructing. Current: %s.", formatMessageTTL(currentTTL(c))),
					meta:     "TTL",
					aliases:  []string{"ttl", "expire", "self-destruct", "destruct", "retention", "message"},
					dynamic:  true,
					children: messageTTLChildren,
				},
				{
					id:      paletteNodeIDHelp,
					title:   "Help",
					detail:  "Keyboard shortcuts for navigation and messaging.",
					meta:    "HELP",
					aliases: []string{"help", "shortcuts", "keys", "keyboard", "?"},
					view:    paletteViewHelp,
				},
			}
		},
	}
}

func editRelayChildren(ctx paletteCtx) []paletteNode {
	if ctx.deps.relayProfiles == nil {
		return nil
	}
	relays := ctx.deps.relayProfiles()
	current := ""
	if ctx.deps.currentRelayName != nil {
		current = ctx.deps.currentRelayName()
	}
	nodes := make([]paletteNode, 0, len(relays))
	for _, relay := range relays {
		detail := relay.URL
		meta := ""
		if relay.Token != "" {
			detail += "  token configured"
		}
		if relay.Name == current {
			meta = "ACTIVE"
		}
		nodes = append(nodes, paletteNode{
			id:      relay.Name,
			title:   relay.Name,
			detail:  detail,
			meta:    meta,
			aliases: []string{relay.Name, relay.URL},
			view:    paletteViewAddRelay,
		})
	}
	return nodes
}

func relayListChildren(command commandPaletteCommand, markLocked bool) func(paletteCtx) []paletteNode {
	return func(ctx paletteCtx) []paletteNode {
		if ctx.deps.relayProfiles == nil {
			return nil
		}
		relays := ctx.deps.relayProfiles()
		current := ""
		if ctx.deps.currentRelayName != nil {
			current = ctx.deps.currentRelayName()
		}
		nodes := make([]paletteNode, 0, len(relays))
		for _, relay := range relays {
			detail := relay.URL
			meta := ""
			if relay.Token != "" {
				detail += "  token configured"
			}
			if relay.Name == current {
				meta = "ACTIVE"
			}
			if markLocked && len(relays) <= 1 {
				meta = "LOCKED"
			}
			nodes = append(nodes, paletteNode{
				id:      relay.Name,
				title:   relay.Name,
				detail:  detail,
				meta:    meta,
				aliases: []string{relay.Name, relay.URL},
				action:  &commandPaletteAction{command: command, relayName: relay.Name},
			})
		}
		return nodes
	}
}

func themeChildren(ctx paletteCtx) []paletteNode {
	names := make([]string, 0, len(style.Themes))
	for name := range style.Themes {
		names = append(names, name)
	}
	sort.Strings(names)
	current := currentThemeLabel(ctx)
	nodes := make([]paletteNode, 0, len(names))
	for _, name := range names {
		detail := "Built-in theme"
		meta := ""
		if name == current {
			detail = "Current theme"
			meta = "ACTIVE"
		}
		nodes = append(nodes, paletteNode{
			id:      name,
			title:   name,
			detail:  detail,
			meta:    meta,
			aliases: []string{name},
			action:  &commandPaletteAction{command: commandPaletteCommandThemes, themeName: name},
		})
	}
	return nodes
}

func messageTTLChildren(ctx paletteCtx) []paletteNode {
	current := currentTTL(ctx)
	nodes := make([]paletteNode, 0, len(messageTTLOptions))
	for _, option := range messageTTLOptions {
		label := formatMessageTTL(option)
		detail := "Self-destruct after " + label + "."
		meta := ""
		if option == current {
			detail = "Current setting."
			meta = "ACTIVE"
		}
		nodes = append(nodes, paletteNode{
			id:      option.String(),
			title:   label,
			detail:  detail,
			meta:    meta,
			aliases: []string{label},
			action:  &commandPaletteAction{command: commandPaletteCommandMessageTTL, messageTTL: option},
		})
	}
	return nodes
}

func currentThemeLabel(ctx paletteCtx) string {
	if ctx.deps.currentTheme == nil {
		return ""
	}
	return ctx.deps.currentTheme()
}

func currentTTL(ctx paletteCtx) time.Duration {
	if ctx.deps.currentMessageTTL == nil {
		return 0
	}
	return ctx.deps.currentMessageTTL()
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
