package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// HelpOverlay renders a full-screen help reference.
type HelpOverlay struct {
	width  int
	height int
}

// NewHelpOverlay creates a new help overlay.
func NewHelpOverlay() HelpOverlay {
	return HelpOverlay{}
}

// SetSize updates dimensions.
func (h *HelpOverlay) SetSize(w, ht int) {
	h.width = w
	h.height = ht
}

// View renders the help content.
func (h HelpOverlay) View() string {
	var b strings.Builder

	b.WriteString(helpTitleStyle.Render("\u2b21 SPECTER HELP"))
	b.WriteString("\n\n")

	sections := []struct {
		title string
		keys  []struct{ key, desc string }
	}{
		{
			title: "Navigation",
			keys: []struct{ key, desc string }{
				{"j / down", "Move cursor down in agent list"},
				{"k / up", "Move cursor up in agent list"},
				{"tab", "Switch focus between panels"},
				{"g", "Jump to first agent"},
				{"G", "Jump to last agent"},
			},
		},
		{
			title: "Actions",
			keys: []struct{ key, desc string }{
				{"d", "Deploy a new agent"},
				{"b", "Build golden image (snapshot)"},
				{"s / enter", "SSH into selected agent"},
				{"l", "View agent logs (scrollable)"},
				{"o", "Open agent URL in browser"},
				{"u", "Restart agent service"},
				{"x", "Destroy selected agent"},
				{"r", "Refresh agent list and health"},
			},
		},
		{
			title: "General",
			keys: []struct{ key, desc string }{
				{"?", "Toggle this help overlay"},
				{"q", "Quit the dashboard"},
				{"ctrl+c", "Force quit"},
			},
		},
	}

	for _, sec := range sections {
		b.WriteString(helpSectionStyle.Render(sec.title))
		b.WriteString("\n")
		for _, k := range sec.keys {
			b.WriteString("  ")
			b.WriteString(helpKeyStyle.Render(k.key))
			b.WriteString(helpDescStyle.Render(k.desc))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).Render("Press ? or esc to close"))

	content := b.String()

	// Center the help in a bordered box
	boxWidth := 50
	if h.width > 0 && boxWidth > h.width-4 {
		boxWidth = h.width - 4
	}

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	// Center horizontally and vertically
	return lipgloss.Place(h.width, h.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}
