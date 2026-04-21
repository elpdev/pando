package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/chat"
	"github.com/elpdev/pando/internal/ui/style"
)

type App struct {
	chat       *chat.Model
	ready      bool
	width      int
	height     int
	lastInChat bool
}

func New(chatModel *chat.Model) *App {
	return &App{chat: chatModel}
}

func (a *App) Init() tea.Cmd {
	return a.chat.Init()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true
		a.lastInChat = a.chat.RecipientMailbox() != ""
		a.chat.SetSize(a.width-2, a.height-a.headerRows()-a.footerRows())
		return a, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			_ = a.chat.Close()
			return a, tea.Quit
		}
	}

	_, cmd := a.chat.Update(msg)
	// If opening/closing a conversation changed the banner mode, reflow the
	// chat viewport so the freed (or consumed) banner rows get accounted for.
	if a.ready {
		inChat := a.chat.RecipientMailbox() != ""
		if inChat != a.lastInChat {
			a.lastInChat = inChat
			a.chat.SetSize(a.width-2, a.height-a.headerRows()-a.footerRows())
		}
	}
	return a, cmd
}

func (a *App) View() string {
	if !a.ready {
		return "loading..."
	}
	return strings.Join([]string{a.renderHeader(), a.chat.View(), a.renderFooter()}, "\n")
}

// Block-letter wordmark rendered in the branded banner. Each row is 20
// columns wide; the banner decorator pads left/right with diagonal slashes
// to fill the terminal width.
var bannerLogo = [3]string{
	"█▀▄ ▄▀█ █▄ █ █▀▄ █▀█",
	"█▀  █▀█ █ ▀█ █ █ █ █",
	"▀   ▀ ▀ ▀  ▀ ▀▀  ▀▀▀",
}

const (
	bannerLogoWidth = 20
	bannerLeadSlash = 4
	bannerMinWidth  = 48 // below this, collapse to the single-line meta row
	bannerMinHeight = 20 // below this, give message history the real estate
)

// headerRows reports how many terminal rows the header occupies in the
// current state. Four rows on the welcome screen (three for the wordmark
// plus one meta line); one row once a conversation is open so message
// history gets the real estate back.
func (a *App) headerRows() int {
	if a.showBanner() {
		return 4
	}
	return 1
}

func (a *App) footerRows() int {
	return 1
}

// showBanner is true when the big PANDO wordmark should be drawn — only on
// terminals tall/wide enough for it and only while no conversation is open.
func (a *App) showBanner() bool {
	if a.width < bannerMinWidth || a.height < bannerMinHeight {
		return false
	}
	return a.chat.RecipientMailbox() == ""
}

// renderHeader is the branded top strip. On the welcome screen it renders
// a three-row PANDO wordmark with diagonal-slash decoration, then a meta
// line beneath it (identity, connection pill). Once the user opens a
// conversation it collapses to a single meta row so scrollback gets the
// freed rows.
//
// All ephemeral feedback continues to live in the chat toast slot, not here.
func (a *App) renderHeader() string {
	if !a.showBanner() {
		return a.renderMetaLine()
	}
	lead := style.BannerSlash.Render(strings.Repeat("╱", bannerLeadSlash))
	trailWidth := max(0, a.width-bannerLeadSlash-1-bannerLogoWidth-1)
	trail := style.BannerSlash.Render(strings.Repeat("╱", trailWidth))
	rows := make([]string, 0, 4)
	for _, line := range bannerLogo {
		rows = append(rows, lead+" "+style.BannerText.Render(line)+" "+trail)
	}
	meta := a.renderMetaLine()
	if meta != "" {
		rows = append(rows, strings.Repeat(" ", bannerLeadSlash+1)+meta)
	}
	return strings.Join(rows, "\n")
}

// renderMetaLine is the single-row status strip: identity, peer arrow +
// name (accent-colored), connection pill, and short fingerprint + verify
// mark. Clipped to terminal width so the pill never wraps.
func (a *App) renderMetaLine() string {
	brand := style.Bold.Render("pando")
	identity := style.Muted.Render(a.chat.Mailbox())

	peerSeg := ""
	if peer := a.chat.RecipientMailbox(); peer != "" {
		arrow := style.Muted.Render("›")
		peerStyle := style.PeerAccentStyle(a.chat.PeerFingerprint()).Bold(true)
		peerSeg = "  " + arrow + "  " + peerStyle.Render(peer)
	}

	pill := renderConnectionPill(a.chat.ConnectionState(), a.chat.ReconnectDelay(), a.chat.Status())

	fpSeg := ""
	if fp := a.chat.PeerFingerprint(); fp != "" && a.chat.RecipientMailbox() != "" {
		mark, markStyle := style.GlyphUnverified, style.UnverifiedWarn
		if a.chat.PeerVerified() {
			mark, markStyle = style.GlyphVerified, style.VerifiedOk
		}
		fpSeg = style.Muted.Render(style.FormatFingerprintShort(fp)) + " " + markStyle.Render(mark)
	}

	segs := []string{brand + "  " + identity + peerSeg, pill}
	if fpSeg != "" {
		segs = append(segs, fpSeg)
	}
	row := strings.Join(segs, "    ")
	return lipgloss.NewStyle().MaxWidth(a.width).Render(row)
}

func (a *App) renderFooter() string {
	segments := a.chat.FooterSegments()
	row := strings.Join(segments, "    ")
	return style.Faint.Render(lipgloss.NewStyle().MaxWidth(a.width).Render(row))
}

func renderConnectionPill(state chat.ConnState, delay time.Duration, detail string) string {
	switch state {
	case chat.ConnConnected:
		return style.StatusOk.Render(style.GlyphConnected) + " " + style.Muted.Render("connected")
	case chat.ConnConnecting:
		return style.StatusWarn.Render(style.GlyphReconnecting) + " " + style.Muted.Render("connecting")
	case chat.ConnReconnecting:
		txt := "reconnecting"
		if delay > 0 {
			txt = fmt.Sprintf("reconnecting in %s", delay)
		}
		return style.StatusWarn.Render(style.GlyphReconnecting) + " " + style.Muted.Render(txt)
	case chat.ConnDisconnected:
		txt := "offline"
		if detail != "" {
			txt = detail
		}
		return style.StatusBad.Render(style.GlyphOffline) + " " + style.Muted.Render(txt)
	case chat.ConnAuthFailed:
		txt := "auth failed"
		if detail != "" {
			txt = detail
		}
		return style.StatusBad.Render(style.GlyphAuthFailed) + " " + style.Muted.Render(txt)
	default:
		return ""
	}
}
