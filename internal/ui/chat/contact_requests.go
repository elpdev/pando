package chat

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/identity"
	"github.com/elpdev/pando/internal/messaging"
	"github.com/elpdev/pando/internal/protocol"
	"github.com/elpdev/pando/internal/store"
	"github.com/elpdev/pando/internal/ui/style"
)

type contactRequestsDeps struct {
	decide func(req store.ContactRequest, accept bool) tea.Cmd
}

// contactRequestsModal renders the Contact requests inbox inside the command
// palette. Items live on the struct between renders so background updates
// (loadContactRequests, handleContactRequestUpdate) can mutate the list even
// while the view is not active.
type contactRequestsModal struct {
	deps     contactRequestsDeps
	items    []store.ContactRequest
	selected int
	busy     bool
	error    string
}

type contactRequestDecisionResultMsg struct {
	request  *store.ContactRequest
	contact  *identity.Contact
	accepted bool
	err      error
}

func newContactRequestsModal(deps contactRequestsDeps) contactRequestsModal {
	return contactRequestsModal{deps: deps}
}

func (m *contactRequestsModal) Open(viewOpenCtx) tea.Cmd {
	m.items = sortContactRequests(m.items)
	m.selected = m.firstPendingIncoming()
	m.busy = false
	m.error = ""
	return nil
}

func (m *contactRequestsModal) Close() {
	m.busy = false
	m.error = ""
}

func (m *contactRequestsModal) replaceItems(items []store.ContactRequest) {
	current := ""
	if m.selected >= 0 && m.selected < len(m.items) {
		current = m.items[m.selected].AccountID
	}
	m.items = sortContactRequests(items)
	if current != "" {
		for idx := range m.items {
			if m.items[idx].AccountID == current {
				m.selected = idx
				return
			}
		}
	}
	m.selected = m.firstPendingIncoming()
}

func (m *contactRequestsModal) firstPendingIncoming() int {
	for idx, item := range m.items {
		if isPendingIncoming(item) {
			return idx
		}
	}
	return 0
}

func isPendingIncoming(req store.ContactRequest) bool {
	return req.Direction == store.ContactRequestDirectionIncoming && req.Status == store.ContactRequestStatusPending
}

func sortContactRequests(items []store.ContactRequest) []store.ContactRequest {
	out := make([]store.ContactRequest, 0, len(items))
	for _, item := range items {
		if isPendingIncoming(item) {
			out = append(out, item)
		}
	}
	for _, item := range items {
		if !isPendingIncoming(item) {
			out = append(out, item)
		}
	}
	return out
}

func countPendingIncoming(items []store.ContactRequest) int {
	count := 0
	for _, item := range items {
		if isPendingIncoming(item) {
			count++
		}
	}
	return count
}

func (m *contactRequestsModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if m.busy {
		return true, nil
	}
	switch keyMsg.Type {
	case tea.KeyUp:
		m.moveSelection(-1)
		return true, nil
	case tea.KeyDown:
		m.moveSelection(1)
		return true, nil
	case tea.KeyEnter:
		return true, m.activate(true)
	}
	switch strings.ToLower(keyMsg.String()) {
	case "a", "y":
		return true, m.activate(true)
	case "r", "x", "n":
		return true, m.activate(false)
	case "q":
		return true, paletteBackCmd()
	}
	return true, nil
}

// applyDecisionResult updates modal state after an accept/reject command
// completes. Called by Model.handleContactRequestDecisionResult so the modal
// reflects success/failure without owning the result-message routing.
func (m *contactRequestsModal) applyDecisionResult(msg contactRequestDecisionResultMsg) {
	m.busy = false
	if msg.err != nil {
		m.error = msg.err.Error()
		return
	}
	m.error = ""
	if msg.request == nil {
		return
	}
	for idx := range m.items {
		if m.items[idx].AccountID == msg.request.AccountID {
			m.items[idx] = *msg.request
			break
		}
	}
	m.items = sortContactRequests(m.items)
	m.selected = m.firstPendingIncoming()
}

func (m *contactRequestsModal) moveSelection(delta int) {
	if len(m.items) == 0 {
		m.selected = 0
		return
	}
	m.selected = (m.selected + delta) % len(m.items)
	if m.selected < 0 {
		m.selected += len(m.items)
	}
}

func (m *contactRequestsModal) selectedRequest() *store.ContactRequest {
	if m.selected < 0 || m.selected >= len(m.items) {
		return nil
	}
	return &m.items[m.selected]
}

func (m *contactRequestsModal) activate(accept bool) tea.Cmd {
	req := m.selectedRequest()
	if req == nil {
		return nil
	}
	if !isPendingIncoming(*req) {
		m.error = "only pending incoming requests can be answered"
		return nil
	}
	if m.deps.decide == nil {
		m.error = "contact request handler not wired"
		return nil
	}
	m.busy = true
	m.error = ""
	return m.deps.decide(*req, accept)
}

// makeContactRequestDecision builds the tea.Cmd that sends accept/reject
// envelopes, persists the updated request, and on accept imports the
// sender's bundle so they show up as a contact.
func (m *Model) makeContactRequestDecision(req store.ContactRequest, accept bool) tea.Cmd {
	svc := m.messaging
	sendFn := func(env protocol.Envelope) error { return m.client.Send(env) }
	return contactRequestDecisionCmd(svc, sendFn, req, accept)
}

// contactRequestDecisionCmd is package-level (not a Model method) so tests
// can exercise it with fakes without constructing a full Model.
func contactRequestDecisionCmd(svc *messaging.Service, send func(protocol.Envelope) error, req store.ContactRequest, accept bool) tea.Cmd {
	return func() tea.Msg {
		decision := contactRequestDecisionReject
		if accept {
			decision = contactRequestDecisionAccept
		}
		envelopes, err := svc.ContactRequestResponseEnvelopes(req.Bundle, decision)
		if err != nil {
			return contactRequestDecisionResultMsg{err: err, accepted: accept, request: &req}
		}
		for _, env := range envelopes {
			if err := send(env); err != nil {
				return contactRequestDecisionResultMsg{err: err, accepted: accept, request: &req}
			}
		}
		updated := req
		if accept {
			updated.Status = store.ContactRequestStatusAccepted
		} else {
			updated.Status = store.ContactRequestStatusRejected
		}
		updated.UpdatedAt = time.Now().UTC()
		if err := svc.SaveContactRequest(&updated); err != nil {
			return contactRequestDecisionResultMsg{err: err, accepted: accept, request: &req}
		}
		var contact *identity.Contact
		if accept {
			c, err := svc.ImportContactInviteBundle(req.Bundle, identity.TrustSourceRelayDirectory)
			if err != nil {
				return contactRequestDecisionResultMsg{err: err, accepted: accept, request: &updated}
			}
			contact = c
		}
		return contactRequestDecisionResultMsg{request: &updated, contact: contact, accepted: accept}
	}
}

// Match the constants used by the messaging package's response envelope
// builder. Kept local to avoid exporting them just for the TUI.
const (
	contactRequestDecisionAccept = "accept"
	contactRequestDecisionReject = "reject"
)

func (m *Model) handleContactRequestDecisionResult(msg contactRequestDecisionResultMsg) (*Model, tea.Cmd) {
	m.contactRequests.applyDecisionResult(msg)
	if msg.err != nil {
		m.pushToast(fmt.Sprintf("contact request failed: %v", msg.err), ToastBad)
		return m, nil
	}
	if msg.contact != nil {
		m.upsertContact(msg.contact)
	}
	if msg.request != nil {
		m.upsertContactRequest(*msg.request)
	}
	verb := "accepted"
	if !msg.accepted {
		verb = "rejected"
	}
	account := ""
	if msg.request != nil {
		account = msg.request.AccountID
	}
	m.pushToast(fmt.Sprintf("%s request from %s", verb, account), ToastInfo)
	return m, nil
}

func (m *Model) upsertContactRequest(req store.ContactRequest) {
	items := m.contactRequests.items
	found := false
	for idx := range items {
		if items[idx].AccountID == req.AccountID {
			items[idx] = req
			found = true
			break
		}
	}
	if !found {
		items = append(items, req)
	}
	m.contactRequests.replaceItems(items)
	m.pendingRequestsCount = countPendingIncoming(m.contactRequests.items)
}

func (m *Model) loadContactRequests() {
	if m.messaging == nil {
		return
	}
	items, err := m.messaging.ContactRequests()
	if err != nil {
		m.pushToast(fmt.Sprintf("load contact requests failed: %v", err), ToastBad)
		return
	}
	m.contactRequests.items = sortContactRequests(items)
	m.pendingRequestsCount = countPendingIncoming(m.contactRequests.items)
}

func (m *Model) handleContactRequestUpdate(req *store.ContactRequest) {
	if req == nil {
		return
	}
	m.upsertContactRequest(*req)
	switch {
	case req.Direction == store.ContactRequestDirectionIncoming && req.Status == store.ContactRequestStatusPending:
		m.pushToast(fmt.Sprintf("contact request from %s", req.AccountID), ToastInfo)
	case req.Direction == store.ContactRequestDirectionOutgoing && req.Status == store.ContactRequestStatusAccepted:
		m.pushToast(fmt.Sprintf("%s accepted your contact request", req.AccountID), ToastInfo)
	case req.Direction == store.ContactRequestDirectionOutgoing && req.Status == store.ContactRequestStatusRejected:
		m.pushToast(fmt.Sprintf("%s declined your contact request", req.AccountID), ToastWarn)
	}
}

func (m *contactRequestsModal) Body(width int) string {
	bodyWidth := max(1, width)
	parts := []string{m.renderBody(bodyWidth)}
	if m.error != "" {
		parts = append(parts, style.StatusBad.Width(bodyWidth).Render(m.error))
	}
	return strings.Join(parts, "\n\n")
}

func (m *contactRequestsModal) Subtitle() string {
	if len(m.items) == 0 {
		return "No contact requests yet."
	}
	pending := countPendingIncoming(m.items)
	if pending == 0 {
		return "All caught up — showing request history."
	}
	if pending == 1 {
		return "1 incoming request waiting for your decision."
	}
	return fmt.Sprintf("%d incoming requests waiting for your decision.", pending)
}

func (m *contactRequestsModal) Footer() string {
	if m.busy {
		return "working..."
	}
	if len(m.items) == 0 {
		return "esc to close"
	}
	return "up/down move · a accept · r reject · esc close"
}

func (m contactRequestsModal) renderBody(width int) string {
	if len(m.items) == 0 {
		return style.PaletteMeta.Width(width).Render("You have no contact requests. New requests will appear here.")
	}
	lines := make([]string, 0, len(m.items))
	for idx, item := range m.items {
		lines = append(lines, renderContactRequestRow(width, idx == m.selected, item))
	}
	return strings.Join(lines, "\n")
}

func renderContactRequestRow(width int, selected bool, req store.ContactRequest) string {
	title := req.AccountID
	meta := strings.ToUpper(req.Direction)
	detail := contactRequestDetail(req)
	return renderPaletteListItem(width, selected, title, detail, meta)
}

func contactRequestDetail(req store.ContactRequest) string {
	age := humanizeRequestAge(req.CreatedAt)
	status := strings.ToUpper(req.Status)
	pieces := []string{status, age}
	if note := strings.TrimSpace(req.Note); note != "" {
		pieces = append(pieces, fmt.Sprintf("note: %s", truncateNote(note, 60)))
	}
	return strings.Join(pieces, " · ")
}

func humanizeRequestAge(ts time.Time) string {
	if ts.IsZero() {
		return "unknown"
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncateNote(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
