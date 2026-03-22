package tui

import "github.com/charmbracelet/lipgloss"

var (
	Primary   = lipgloss.Color("#7B68EE")
	Success   = lipgloss.Color("#50C878")
	Warning   = lipgloss.Color("#FFBF00")
	Error     = lipgloss.Color("#FF6B6B")
	Muted     = lipgloss.Color("#666666")
	TextWhite = lipgloss.Color("#FAFAFA")
	TextDim   = lipgloss.Color("#999999")

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary)

	BrandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			PaddingLeft(1)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Success)

	WarningStyle = lipgloss.NewStyle().
			Foreground(Warning)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Error).
			Bold(true)

	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	BoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(1, 2)

	StatusOnline  = SuccessStyle.Render("●")
	StatusOffline = MutedStyle.Render("○")
	StatusActive  = WarningStyle.Render("◷")
)

const (
	Symbol = "⬡"
	Brand  = "⬡ SPECTER"
)

func Logo() string {
	return lipgloss.NewStyle().
		Foreground(Primary).
		Bold(true).
		Render(`     ○
    ╱│╲
   ╱ │ ╲
  │  │  │
   ╲ │ ╱
    ∿∿∿`)
}
