package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
)

// ConfirmDialogModel shows a yes/no confirmation dialog.
type ConfirmDialogModel struct {
	title    string
	message  string
	selected bool // true = yes, false = no
	width    int
	height   int

	onConfirm func() tea.Msg
	onCancel  func() tea.Msg
}

// NewDestroyConfirmDialog creates a confirmation dialog for destroying an agent.
func NewDestroyConfirmDialog(agentName string) ConfirmDialogModel {
	return ConfirmDialogModel{
		title:    fmt.Sprintf("Destroy \"%s\"?", agentName),
		message:  "This will delete the VM and DNS record.",
		selected: false,
		onConfirm: func() tea.Msg {
			return DestroyConfirmMsg{AgentName: agentName}
		},
		onCancel: func() tea.Msg {
			return DestroyCancelMsg{}
		},
	}
}

func (m ConfirmDialogModel) Init() tea.Cmd {
	return nil
}

func (m ConfirmDialogModel) Update(msg tea.Msg) (ConfirmDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "y", "Y":
			return m, m.onConfirm
		case "n", "N", "esc", "escape", "q":
			return m, m.onCancel
		case "left", "h":
			m.selected = true
			return m, nil
		case "right", "l":
			m.selected = false
			return m, nil
		case "enter":
			if m.selected {
				return m, m.onConfirm
			}
			return m, m.onCancel
		case "tab":
			m.selected = !m.selected
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m ConfirmDialogModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(errorColor)

	msgStyle := lipgloss.NewStyle().
		Foreground(whiteColor)

	var yesBtn, noBtn string
	if m.selected {
		yesBtn = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(errorColor).
			Padding(0, 2).
			Render("Yes")
		noBtn = lipgloss.NewStyle().
			Foreground(whiteColor).
			Background(lipgloss.Color("#3F3F46")).
			Padding(0, 2).
			Render("No")
	} else {
		yesBtn = lipgloss.NewStyle().
			Foreground(whiteColor).
			Background(lipgloss.Color("#3F3F46")).
			Padding(0, 2).
			Render("Yes")
		noBtn = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(primaryColor).
			Padding(0, 2).
			Render("No")
	}

	content := titleStyle.Render(m.title) + "\n\n" +
		msgStyle.Render(m.message) + "\n\n" +
		yesBtn + "  " + noBtn + "\n\n" +
		lipgloss.NewStyle().Foreground(mutedColor).Render("[y]es  [n]o  [esc] cancel")

	boxWidth := 44
	if m.width > 0 && boxWidth > m.width-4 {
		boxWidth = m.width - 4
	}

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(errorColor).
		Padding(1, 3).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (m *ConfirmDialogModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}
