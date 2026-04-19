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
	chat   *chat.Model
	ready  bool
	width  int
	height int
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
		a.chat.SetSize(msg.Width-2, msg.Height-2)
		return a, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			_ = a.chat.Close()
			return a, tea.Quit
		}
	}

	_, cmd := a.chat.Update(msg)
	return a, cmd
}

func (a *App) View() string {
	if !a.ready {
		return "loading..."
	}
	return strings.Join([]string{a.renderHeader(), a.chat.View()}, "\n")
}

// renderHeader produces a single styled row that owns every piece of
// persistent state: app name, active identity, peer (in its accent color),
// a connection pill, and the peer's short fingerprint + verification glyph.
// Ephemeral feedback lives in the chat toast slot, not here.
func (a *App) renderHeader() string {
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
	// Clip to terminal width so the pill doesn't wrap awkwardly on narrow
	// terminals.
	return lipgloss.NewStyle().MaxWidth(a.width).Render(row)
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
