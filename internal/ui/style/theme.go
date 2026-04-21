package style

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Theme is the single source of truth for every color the TUI renders. Each
// named palette entry maps to one or more exported Style tokens; downstream
// rendering code continues to consume those tokens (Modal, StatusOk, ...)
// and is completely unaware of the active theme.
//
// Adding a new theme is done by defining a constructor that returns a fully
// populated Theme and inserting it into Themes. All fields are required —
// there is no implicit fallback, so a compile-time zero value would render
// as no-color rather than a surprising substitute.
type Theme struct {
	Name string

	// Text ladder, faint → bright.
	Faint, Muted, Subtle, Dim, Bright lipgloss.Color

	// Surface backgrounds and chrome.
	BgSel, BgModal, BgPalette, Divider lipgloss.Color

	// Status semantics.
	Ok, Warn, Bad, Info lipgloss.Color

	// Banner + room accents.
	BannerText, BannerSlash, RoomAccent lipgloss.Color

	// Peer-accent palette — rotating stable hues assigned per fingerprint.
	PeerAccents []lipgloss.Color
}

// Themes is the built-in registry. The active theme is always one of these;
// downstream code selects by name via Apply. "phosphor" is the default;
// "classic" preserves the original 256-color palette for anyone who prefers
// it.
var Themes = map[string]Theme{
	"phosphor": phosphorTheme(),
	"classic":  classicTheme(),
}

// DefaultThemeName is the theme applied when nothing else selects one. Kept
// as an exported constant so downstream callers (config loaders, CLI) can
// reference it without duplicating the string.
const DefaultThemeName = "phosphor"

// envThemeOverride is the env var callers can set to pick a theme at launch
// before config-file selection is wired up. Unknown values fall back to the
// default theme, silently — this is a convenience knob, not a strict API.
const envThemeOverride = "PANDO_THEME"

// active is the currently applied theme. Read-only for callers — swap via
// Apply. Tests and the PeerAccent function consult it directly.
var active Theme

// Current returns the active theme. Useful for introspection ("which theme
// am I on?") and for tests that assert against theme fields.
func Current() Theme { return active }

// Apply rebuilds every exported Style token from the given theme. Must be
// called from the main goroutine before any rendering starts (typically
// once at program init, optionally again if the user switches themes).
//
// Apply mutates package-level globals — safe because the TUI renders from
// a single goroutine and theme switches are explicit user actions.
func Apply(t Theme) {
	active = t

	// Text ladder.
	Faint = lipgloss.NewStyle().Foreground(t.Faint)
	Muted = lipgloss.NewStyle().Foreground(t.Muted)
	Subtle = lipgloss.NewStyle().Foreground(t.Subtle)
	Dim = lipgloss.NewStyle().Foreground(t.Dim)
	Bright = lipgloss.NewStyle().Foreground(t.Bright)
	ModalTitle = Bright.Bold(true)

	// Status.
	StatusOk = lipgloss.NewStyle().Foreground(t.Ok)
	StatusWarn = lipgloss.NewStyle().Foreground(t.Warn)
	StatusBad = lipgloss.NewStyle().Foreground(t.Bad)
	StatusInfo = lipgloss.NewStyle().Foreground(t.Info)

	// Semantic aliases — derived from status tokens, re-bound so they track
	// theme swaps.
	VerifiedOk = StatusOk
	UnverifiedWarn = StatusWarn
	DeliveryPending = Muted
	DeliverySent = Muted
	DeliveryDelivered = StatusOk
	DeliveryFailed = StatusBad
	UnreadBadge = StatusInfo.Bold(true)
	CursorBlock = StatusInfo

	// Surfaces + borders.
	Selected = lipgloss.NewStyle().Background(t.BgSel)
	BgModal = lipgloss.NewStyle().Background(t.BgModal)
	ActiveRow = lipgloss.NewStyle().Background(t.BgSel).Bold(true)
	BackdropTint = t.BgModal
	RoomAccent = t.RoomAccent

	SidebarBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).BorderLeft(false).BorderTop(false).BorderBottom(false).
		BorderForeground(t.Divider)

	SidebarBorderFocused = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).BorderLeft(false).BorderTop(false).BorderBottom(false).
		BorderForeground(t.Info)

	ModalBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(t.Info)

	Modal = lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(t.Info).
		Background(t.BgModal)

	PaletteModal = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(t.Faint).
		Background(t.BgModal)

	PaletteInput = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Faint)

	PaletteItem = lipgloss.NewStyle().Padding(0, 1)

	PaletteSelectedItem = lipgloss.NewStyle().
		Background(t.BgPalette).
		Foreground(t.Bright).
		Padding(0, 1)

	InputBorder = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(t.Faint)

	PaletteTitle = Bright.Bold(true)
	PaletteMeta = Muted
	PaletteFooter = Subtle
	PaletteShortcut = Subtle
	PaletteAccent = StatusInfo
	PaletteMatch = Bright.Underline(true)

	// Banner.
	BannerText = lipgloss.NewStyle().Foreground(t.BannerText).Bold(true)
	BannerSlash = lipgloss.NewStyle().Foreground(t.BannerSlash)
}

// classicTheme is the stock 256-color palette that pando shipped with
// before the theming system landed. Matches the hard-coded values that
// used to live in styles.go. Kept available under the "classic" key for
// anyone who prefers the original look.
func classicTheme() Theme {
	return Theme{
		Name: "classic",

		Faint:  lipgloss.Color("240"),
		Muted:  lipgloss.Color("241"),
		Subtle: lipgloss.Color("243"),
		Dim:    lipgloss.Color("248"),
		Bright: lipgloss.Color("230"),

		BgSel:     lipgloss.Color("238"),
		BgModal:   lipgloss.Color("234"),
		BgPalette: lipgloss.Color("236"),
		Divider:   lipgloss.Color("60"),

		Ok:   lipgloss.Color("86"),
		Warn: lipgloss.Color("214"),
		Bad:  lipgloss.Color("203"),
		Info: lipgloss.Color("69"),

		BannerText:  lipgloss.Color("#9FE8B0"), // CRT phosphor
		BannerSlash: lipgloss.Color("#FFB347"), // warm amber
		RoomAccent:  lipgloss.Color("69"),      // matches Info

		PeerAccents: []lipgloss.Color{
			lipgloss.Color("75"),  // sky blue
			lipgloss.Color("141"), // lilac
			lipgloss.Color("215"), // peach
			lipgloss.Color("120"), // mint
			lipgloss.Color("209"), // coral
			lipgloss.Color("180"), // sand
			lipgloss.Color("117"), // ice
			lipgloss.Color("177"), // orchid
		},
	}
}

// phosphorTheme is a CRT-terminal palette inspired by classic amber-on-green
// phosphor monitors. All values live on a cool navy base, with phosphor green
// for primary text and warm amber as the accent. This is pando's default
// theme; override to the pre-theming 256-color look with PANDO_THEME=classic.
func phosphorTheme() Theme {
	return Theme{
		Name: "phosphor",

		Faint:  lipgloss.Color("#3A5A44"), // phosphor-faint
		Muted:  lipgloss.Color("#5A8A68"), // phosphor-dim
		Subtle: lipgloss.Color("#788E80"), // mid phosphor-gray, synthesized
		Dim:    lipgloss.Color("#8FBDA0"), // lighter mid-phosphor, synthesized
		Bright: lipgloss.Color("#9FE8B0"), // phosphor

		BgSel:     lipgloss.Color("#18233D"), // bg-3
		BgModal:   lipgloss.Color("#111A2C"), // bg-2
		BgPalette: lipgloss.Color("#18233D"), // bg-3
		Divider:   lipgloss.Color("#2A3752"), // hairline

		Ok:   lipgloss.Color("#4FAE7A"), // moss
		Warn: lipgloss.Color("#FFB347"), // amber
		Bad:  lipgloss.Color("#FF5E5B"), // signal
		Info: lipgloss.Color("#6FD0E3"), // cyan

		BannerText:  lipgloss.Color("#9FE8B0"), // phosphor
		BannerSlash: lipgloss.Color("#FFB347"), // amber
		RoomAccent:  lipgloss.Color("#6FD0E3"), // cyan

		PeerAccents: []lipgloss.Color{
			lipgloss.Color("#9FE8B0"), // phosphor
			lipgloss.Color("#6FD0E3"), // cyan
			lipgloss.Color("#FFB347"), // amber
			lipgloss.Color("#4FAE7A"), // moss
			lipgloss.Color("#5A8A68"), // phosphor-dim
			lipgloss.Color("#A87324"), // amber-dim
			lipgloss.Color("#BEE8C8"), // pale phosphor, synthesized
			lipgloss.Color("#5ACFE3"), // teal cyan, synthesized
		},
	}
}

func init() {
	name := os.Getenv(envThemeOverride)
	theme, ok := Themes[name]
	if !ok {
		theme = Themes[DefaultThemeName]
	}
	Apply(theme)
}
