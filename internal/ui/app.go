package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/elpdev/pando/internal/ui/chat"
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

	headerText := "pando  mailbox=" + a.chat.Mailbox()
	if a.chat.RecipientMailbox() != "" {
		headerText += "  chat=" + a.chat.RecipientMailbox()
	}
	header := lipgloss.NewStyle().Bold(true).Render(headerText)
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(a.chat.Status())

	return strings.Join([]string{header, status, a.chat.View()}, "\n")
}
