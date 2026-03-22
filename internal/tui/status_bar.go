package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// StatusBarPanel renders the bottom status bar with context-sensitive key hints.
type StatusBarPanel struct {
	width int
}

// NewStatusBarPanel creates a new status bar.
func NewStatusBarPanel() StatusBarPanel {
	return StatusBarPanel{}
}

// SetWidth updates the bar width.
func (p *StatusBarPanel) SetWidth(w int) {
	p.width = w
}

type keyHint struct {
	key  string
	desc string
}

// View renders the status bar based on current state and whether an agent is selected.
func (p StatusBarPanel) View(state AppState, hasAgent bool, agentCount int, totalCost float64) string {
	var hints []keyHint

	switch state {
	case stateHelp:
		hints = []keyHint{
			{"?/esc", "close help"},
		}
	default:
		if hasAgent {
			hints = []keyHint{
				{"d", "deploy"},
				{"s", "ssh"},
				{"l", "logs"},
				{"o", "open"},
				{"x", "destroy"},
				{"r", "refresh"},
				{"?", "help"},
			}
		} else {
			hints = []keyHint{
				{"d", "deploy"},
				{"r", "refresh"},
				{"?", "help"},
			}
		}
	}

	var parts []string
	for _, h := range hints {
		part := fmt.Sprintf("%s %s",
			statusBarKeyStyle.Render("["+h.key+"]"),
			statusBarDescStyle.Render(h.desc),
		)
		parts = append(parts, part)
	}

	left := strings.Join(parts, "  ")

	// Right side: summary
	right := ""
	if agentCount > 0 && totalCost > 0 {
		right = lipgloss.NewStyle().Foreground(mutedColor).Render(
			fmt.Sprintf("%d agents  $%.2f/mo", agentCount, totalCost),
		)
	}

	// Combine left and right with spacing
	available := p.width - 4
	if available < 0 {
		available = 0
	}

	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	gap := available - leftLen - rightLen
	if gap < 2 {
		gap = 2
		right = ""
	}

	bar := left + strings.Repeat(" ", gap) + right

	return statusBarStyle.Width(p.width).Render(bar)
}
