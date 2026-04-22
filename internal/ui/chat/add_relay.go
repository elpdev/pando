package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/elpdev/pando/internal/config"
	"github.com/elpdev/pando/internal/ui/style"
)

// addRelayModal renders both the Add relay and Edit relay flows inside the
// command palette. Which flow is active is derived from the palette path at
// Open time: a path ending in "edit-relay/<name>" triggers edit mode for that
// saved relay profile; otherwise the view shows empty inputs for adding a
// new profile.
type addRelayModal struct {
	m         *Model
	editing   bool
	original  string
	inputs    []textinput.Model
	focused   int
	error     string
	maskToken bool
}

func newAddRelayModal(m *Model) addRelayModal {
	name := textinput.New()
	name.Placeholder = "name"
	url := textinput.New()
	url.Placeholder = "ws://localhost:8080/ws"
	token := textinput.New()
	token.Placeholder = "optional token"
	token.EchoMode = textinput.EchoPassword
	token.EchoCharacter = '*'
	return addRelayModal{m: m, inputs: []textinput.Model{name, url, token}, maskToken: true}
}

func (m *addRelayModal) Open(ctx viewOpenCtx) tea.Cmd {
	model := m.m
	*m = newAddRelayModal(model)
	// If the path routed us through "edit-relay/<name>", prefill the inputs.
	if len(ctx.path) >= 3 && ctx.path[len(ctx.path)-2] == paletteNodeIDEditRelay {
		relay, ok := model.lookupRelayProfile(ctx.path[len(ctx.path)-1])
		if !ok {
			return completePaletteCmd(fmt.Sprintf("relay %s not found", ctx.path[len(ctx.path)-1]), ToastBad)
		}
		m.editing = true
		m.original = relay.Name
		m.inputs[0].SetValue(relay.Name)
		m.inputs[1].SetValue(relay.URL)
		m.inputs[2].SetValue(relay.Token)
	}
	return m.inputs[0].Focus()
}

func (m *addRelayModal) Close() {
	*m = newAddRelayModal(m.m)
}

func (m *addRelayModal) Update(msg tea.Msg) (bool, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if keyMsg.Type == tea.KeyEsc {
		return false, nil
	}
	switch keyMsg.Type {
	case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown:
		return true, m.moveFocus(keyMsg)
	case tea.KeyEnter:
		relay, err := m.profile()
		if err != nil {
			m.error = err.Error()
			return true, nil
		}
		if m.editing {
			return true, func() tea.Msg { return editRelaySavedMsg{original: m.original, relay: relay} }
		}
		return true, func() tea.Msg { return addRelaySavedMsg{relay: relay} }
	case tea.KeyCtrlT:
		m.maskToken = !m.maskToken
		if m.maskToken {
			m.inputs[2].EchoMode = textinput.EchoPassword
		} else {
			m.inputs[2].EchoMode = textinput.EchoNormal
		}
		return true, nil
	}
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	m.error = ""
	return true, cmd
}

func (m *addRelayModal) Body(width, _ int) string {
	bodyWidth := max(1, width)
	lines := []string{
		style.PaletteMeta.Width(bodyWidth).Render("Save a named relay profile and switch to it immediately."),
		renderRelayInput(bodyWidth, "Name", m.inputs[0], m.focused == 0),
		renderRelayInput(bodyWidth, "URL", m.inputs[1], m.focused == 1),
		renderRelayInput(bodyWidth, "Token", m.inputs[2], m.focused == 2),
	}
	if m.error != "" {
		lines = append(lines, style.StatusBad.Width(bodyWidth).Render(m.error))
	}
	return strings.Join(lines, "\n\n")
}

func (m *addRelayModal) Subtitle() string {
	if m.editing {
		return "Update a saved relay profile and keep the active relay in sync."
	}
	return "Relay profiles persist to device config."
}

func (m *addRelayModal) Footer() string {
	if m.editing {
		return "tab move · enter save changes · ctrl+t show/hide token · esc cancel"
	}
	return "tab move · enter save · ctrl+t show/hide token · esc cancel"
}

func (m *addRelayModal) moveFocus(msg tea.KeyMsg) tea.Cmd {
	m.inputs[m.focused].Blur()
	if msg.Type == tea.KeyShiftTab || msg.Type == tea.KeyUp {
		m.focused = (m.focused + len(m.inputs) - 1) % len(m.inputs)
	} else {
		m.focused = (m.focused + 1) % len(m.inputs)
	}
	return m.inputs[m.focused].Focus()
}

func (m addRelayModal) profile() (config.RelayProfile, error) {
	name := strings.TrimSpace(m.inputs[0].Value())
	url := strings.TrimSpace(m.inputs[1].Value())
	token := strings.TrimSpace(m.inputs[2].Value())
	if name == "" {
		return config.RelayProfile{}, fmt.Errorf("relay name is required")
	}
	if url == "" {
		return config.RelayProfile{}, fmt.Errorf("relay url is required")
	}
	return config.RelayProfile{Name: name, URL: url, Token: token}, nil
}

func renderRelayInput(width int, label string, input textinput.Model, focused bool) string {
	input.Width = max(1, width-2)
	heading := style.Muted.Render(label)
	if focused {
		heading = style.Bright.Render(label)
	}
	return heading + "\n" + style.PaletteInput.Width(width).Padding(0, 1).Render(input.View())
}

func (m *Model) handleAddRelaySavedMsg(msg addRelaySavedMsg) (*Model, tea.Cmd) {
	if err := m.addRelayProfile(msg.relay); err != nil {
		m.addRelay.error = err.Error()
		return m, nil
	}
	m.commandPalette.Close()
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
	m.pushToast(fmt.Sprintf("saved relay %s", msg.relay.Name), ToastInfo)
	return m, m.switchRelay(msg.relay.Name)
}

func (m *Model) handleEditRelaySavedMsg(msg editRelaySavedMsg) (*Model, tea.Cmd) {
	if err := m.updateRelayProfile(msg.original, msg.relay); err != nil {
		m.addRelay.error = err.Error()
		return m, nil
	}
	m.commandPalette.Close()
	if m.ui.focus == focusChat {
		m.input.Focus()
	}
	m.pushToast(fmt.Sprintf("updated relay %s", msg.relay.Name), ToastInfo)
	if m.relay.active == msg.original || m.relay.active == msg.relay.Name {
		return m, m.switchRelay(msg.relay.Name)
	}
	return m, nil
}
