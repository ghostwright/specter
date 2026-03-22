package tui

import "github.com/charmbracelet/lipgloss"

var (
	Primary = lipgloss.Color("#F97316")
	Accent  = lipgloss.Color("#FB923C")
	Deep    = lipgloss.Color("#EA580C")
	Success = lipgloss.Color("#22C55E")
	Warning = lipgloss.Color("#EAB308")
	Error   = lipgloss.Color("#EF4444")
	Muted   = lipgloss.Color("#71717A")

	TextWhite = lipgloss.Color("#FAFAFA")
	TextDim   = lipgloss.Color("#999999")

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary)

	BrandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			PaddingLeft(1)

	AccentStyle = lipgloss.NewStyle().
			Foreground(Accent)

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
	body := lipgloss.NewStyle().Foreground(Primary).Bold(true)
	tail := lipgloss.NewStyle().Foreground(Accent)
	eyes := lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA")).Bold(true)

	return body.Render("       .-\"\"\"-.") + "\n" +
		body.Render("      /       \\") + "\n" +
		body.Render("     |  ") + eyes.Render("O   O") + body.Render("  |") + "\n" +
		body.Render("     |    ") + lipgloss.NewStyle().Foreground(Deep).Render("o") + body.Render("    |") + "\n" +
		body.Render("     |  \\___/  |") + "\n" +
		body.Render("      \\       /") + "\n" +
		tail.Render("      _) _ _ (_") + "\n" +
		tail.Render("     / /| | |\\ \\") + "\n" +
		tail.Render("    (_/ | | | \\_)")
}
