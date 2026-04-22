package chat

import tea "github.com/charmbracelet/bubbletea"

// paletteViewID names a hosted interactive sub-screen that lives inside the
// command palette's frame. Adding a new view is a two-step process: declare an
// id here, and register a resolver in commandPaletteDeps.resolveView so the
// palette can dispatch Update/Body calls to the correct backing struct.
type paletteViewID int

const (
	paletteViewNone paletteViewID = iota
	paletteViewHelp
	paletteViewPeerDetail
	paletteViewContactVerify
	paletteViewContactRequestSend
	paletteViewAddRelay
	paletteViewContactRequests
	paletteViewAddContact
)

// paletteView is implemented by every detail modal that renders inside the
// palette frame. The palette owns the outer chrome (breadcrumb title, subtitle,
// footer) and delegates the body region plus input handling to the active
// view. Close must be idempotent — the palette may invoke it during back-nav
// even if Open was never called (e.g., after a transient open that never
// rendered).
type paletteView interface {
	Open(ctx viewOpenCtx) tea.Cmd
	Close()
	Update(msg tea.Msg) (handled bool, cmd tea.Cmd)
	Body(width, height int) string
	Subtitle() string
	Footer() string
}

// viewOpenCtx carries the Model-owned values a view may need at open time.
// Kept intentionally narrow so the palette stays decoupled from the concrete
// Model type. Fields are populated by the Model's onEnterView callback.
//
// path is the palette's current id path at the moment the view is entered.
// Views that share a single struct across multiple tree locations (e.g.
// addRelay serving both Add and Edit) use it to decide the initial mode.
type viewOpenCtx struct {
	peerMailbox     string
	peerFingerprint string
	path            []string
}

// paletteCloseMsg asks the Model to close the palette entirely — used both
// when a view finishes successfully (set toast to show a confirmation) and
// when a view wants to dismiss itself without error (empty toast).
type paletteCloseMsg struct {
	toast     string
	toastKind ToastLevel
}

// dismissPaletteCmd fires paletteCloseMsg with no toast — the view wants to
// close the palette silently.
func dismissPaletteCmd() tea.Cmd {
	return func() tea.Msg { return paletteCloseMsg{} }
}

// completePaletteCmd fires paletteCloseMsg with a success toast.
func completePaletteCmd(toast string, kind ToastLevel) tea.Cmd {
	return func() tea.Msg { return paletteCloseMsg{toast: toast, toastKind: kind} }
}

// paletteNavigateMsg asks the Model to jump the palette's current path to the
// target id list. Used by views that need to chain into another view — e.g.,
// pressing `v` in peer detail opens the Verify contact view.
type paletteNavigateMsg struct {
	path []string
}

func paletteNavigateCmd(path ...string) tea.Cmd {
	segs := append([]string(nil), path...)
	return func() tea.Msg { return paletteNavigateMsg{path: segs} }
}

// paletteBackMsg asks the palette to pop the current path segment, used by
// views that want keys other than Esc to trigger back-navigation (e.g., q/n
// in Verify contact).
type paletteBackMsg struct{}

func paletteBackCmd() tea.Cmd {
	return func() tea.Msg { return paletteBackMsg{} }
}
