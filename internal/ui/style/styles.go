// Package style holds the visual tokens for the Pando TUI.
//
// All color literals live here. Downstream rendering code must consume named
// tokens (Muted, StatusOk, VerifiedOk, ...) rather than picking raw
// lipgloss.Color values. This keeps the palette consistent across screens and
// makes a future theme swap possible without touching rendering code.
package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ----------------------------------------------------------------------------
// Palette — single source of truth for raw 256-color values.
// Keep this list short; add tokens below rather than inventing new colors
// inline.
// ----------------------------------------------------------------------------

var (
	colorMuted   = lipgloss.Color("241") // secondary text, hint lines
	colorSubtle  = lipgloss.Color("243") // tertiary text, meta
	colorDim     = lipgloss.Color("248") // modal body text
	colorBright  = lipgloss.Color("230") // modal titles
	colorFaint   = lipgloss.Color("240") // dim borders
	colorBgSel   = lipgloss.Color("238") // selection background, active-row highlight
	colorBgModal = lipgloss.Color("234") // subtle modal inset + backdrop vignette
	colorDivider = lipgloss.Color("60")  // sidebar divider accent

	colorOk   = lipgloss.Color("86")  // green / connected / verified / delivered
	colorWarn = lipgloss.Color("214") // amber / reconnecting / unverified
	colorBad  = lipgloss.Color("203") // red / failed / auth-failed
	colorInfo = lipgloss.Color("69")  // blue / input accent, unread badge

	colorPhosphor = lipgloss.Color("#9FE8B0") // CRT phosphor green / banner letters
	colorAmber    = lipgloss.Color("#FFB347") // warm amber / banner slashes
)

// ----------------------------------------------------------------------------
// Foreground / emphasis tokens.
// ----------------------------------------------------------------------------

var (
	// Muted is secondary copy: hints, timestamps, fingerprints.
	Muted = lipgloss.NewStyle().Foreground(colorMuted)
	// Subtle is tertiary copy: meta counters, footnotes.
	Subtle = lipgloss.NewStyle().Foreground(colorSubtle)
	// Dim is body copy inside a darker container (modals).
	Dim = lipgloss.NewStyle().Foreground(colorDim)
	// Bright is the highest-contrast foreground for headings inside modals.
	Bright = lipgloss.NewStyle().Foreground(colorBright)
	// Faint is barely-visible text for thin dividers and placeholder borders.
	Faint = lipgloss.NewStyle().Foreground(colorFaint)

	Bold   = lipgloss.NewStyle().Bold(true)
	Italic = lipgloss.NewStyle().Italic(true)

	// ModalTitle is the bold bright heading at the top of every modal.
	ModalTitle = Bright.Bold(true)
)

// ----------------------------------------------------------------------------
// Status tokens — for connection, toasts, badges.
// ----------------------------------------------------------------------------

var (
	StatusOk   = lipgloss.NewStyle().Foreground(colorOk)
	StatusWarn = lipgloss.NewStyle().Foreground(colorWarn)
	StatusBad  = lipgloss.NewStyle().Foreground(colorBad)
	StatusInfo = lipgloss.NewStyle().Foreground(colorInfo)
)

// ----------------------------------------------------------------------------
// Semantic tokens — meaning-bearing aliases. Prefer these at call sites so
// intent is obvious in the rendering code.
// ----------------------------------------------------------------------------

var (
	VerifiedOk     = StatusOk
	UnverifiedWarn = StatusWarn

	DeliveryPending   = Muted
	DeliverySent      = Muted
	DeliveryDelivered = StatusOk
	DeliveryFailed    = StatusBad

	UnreadBadge = StatusInfo.Bold(true)

	// CursorBlock styles the blinking block cursor in the add-contact editor.
	CursorBlock = StatusInfo
)

// ----------------------------------------------------------------------------
// Surfaces — backgrounds, selection highlight, borders.
// ----------------------------------------------------------------------------

var (
	Selected = lipgloss.NewStyle().Background(colorBgSel)
	BgModal  = lipgloss.NewStyle().Background(colorBgModal)

	// ActiveRow highlights the currently open chat row in the sidebar.
	ActiveRow = lipgloss.NewStyle().Background(colorBgSel).Bold(true)

	// BackdropTint is the raw color used to tint whitespace around modals via
	// lipgloss.WithWhitespaceBackground.
	BackdropTint = colorBgModal

	// RoomAccent is the signature color for encrypted rooms (fingerprint-less).
	RoomAccent = colorInfo

	SidebarBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderRight(true).BorderLeft(false).BorderTop(false).BorderBottom(false).
			BorderForeground(colorDivider)

	ModalBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(colorInfo)

	// Modal combines the modal border and background. Downstream code should
	// use this rather than composing them inline.
	Modal = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(colorInfo).
		Background(colorBgModal)

	InputBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorFaint)
)

// ----------------------------------------------------------------------------
// Glyphs — unicode symbols used by rendering code. Exported as constants so
// tests can assert against them without depending on lipgloss styling.
// ----------------------------------------------------------------------------

const (
	GlyphConnected    = "●"
	GlyphReconnecting = "◐"
	GlyphOffline      = "○"
	GlyphAuthFailed   = "⚠"

	GlyphVerified   = "✓"
	GlyphUnverified = "?"

	GlyphDeliveryPending   = "⋯"
	GlyphDeliverySent      = "✓"
	GlyphDeliveryDelivered = "✓✓"
	GlyphDeliveryFailed    = "!"

	GlyphCursorRow    = "▌" // sidebar: keyboard cursor marker
	GlyphActiveChat   = "●" // sidebar: currently open chat marker
	GlyphUnreadDot    = "●" // sidebar: unread-count bullet
	GlyphJumpToLatest = "↓"

	GroupSep = "·" // fingerprint group separator
)

// ----------------------------------------------------------------------------
// Peer accent palette — a stable, small set of colors assigned per-fingerprint
// so the same peer always renders in the same color.
// ----------------------------------------------------------------------------

var peerAccentPalette = []lipgloss.Color{
	lipgloss.Color("75"),  // sky blue
	lipgloss.Color("141"), // lilac
	lipgloss.Color("215"), // peach
	lipgloss.Color("120"), // mint
	lipgloss.Color("209"), // coral
	lipgloss.Color("180"), // sand
	lipgloss.Color("117"), // ice
	lipgloss.Color("177"), // orchid
}

// BannerText renders the PANDO wordmark rows in the welcome-screen banner.
// CRT phosphor green, bold.
var BannerText = lipgloss.NewStyle().Foreground(colorPhosphor).Bold(true)

// BannerSlash renders the diagonal-slash decoration bracketing the wordmark.
// Warm amber, reads as a signature-terminal accent beside the phosphor letters.
var BannerSlash = lipgloss.NewStyle().Foreground(colorAmber)

// PeerAccent returns a stable color for the given fingerprint. An empty
// fingerprint falls back to the ok (green) token.
func PeerAccent(fingerprint string) lipgloss.Color {
	if fingerprint == "" {
		return colorOk
	}
	// Polynomial rolling hash — stable across runs, no crypto dependency.
	var n uint32
	for _, r := range fingerprint {
		n = n*131 + uint32(r)
	}
	return peerAccentPalette[int(n)%len(peerAccentPalette)]
}

// PeerAccentStyle is PeerAccent wrapped in a lipgloss.Style for direct
// rendering of a mailbox name.
func PeerAccentStyle(fingerprint string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(PeerAccent(fingerprint))
}

// ----------------------------------------------------------------------------
// Fingerprint formatting — shared between TUI and ctlcmd so fingerprints read
// the same everywhere. Input is any hex string; non-hex runes are preserved.
// Empty input returns "".
// ----------------------------------------------------------------------------

// FormatFingerprint renders a fingerprint in 4-rune groups separated by "·".
//
//	FormatFingerprint("abcdef0123456789") == "abcd·ef01·2345·6789"
func FormatFingerprint(fp string) string {
	return formatGroups(fp, 4)
}

// FormatFingerprintShort returns up to the first 8 runes of the fingerprint,
// grouped. Useful for compact header/pill rendering.
//
//	FormatFingerprintShort("abcdef0123456789") == "abcd·ef01"
func FormatFingerprintShort(fp string) string {
	runes := []rune(fp)
	if len(runes) > 8 {
		runes = runes[:8]
	}
	return formatGroups(string(runes), 4)
}

func formatGroups(s string, group int) string {
	if s == "" || group <= 0 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%group == 0 {
			b.WriteString(GroupSep)
		}
		b.WriteRune(r)
	}
	return b.String()
}
