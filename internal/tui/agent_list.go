package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// AgentListPanel renders the left panel showing all deployed agents.
type AgentListPanel struct {
	width  int
	height int
}

// NewAgentListPanel creates a new agent list panel.
func NewAgentListPanel() AgentListPanel {
	return AgentListPanel{}
}

// SetSize updates the panel dimensions.
func (p *AgentListPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// View renders the agent list panel content (no border, that's handled by app.go).
func (p AgentListPanel) View(agents []AgentViewModel, selectedIdx int, focused bool) string {
	var b strings.Builder

	// Panel header
	header := "AGENTS"
	if len(agents) > 0 {
		header = fmt.Sprintf("AGENTS (%d)", len(agents))
	}

	if focused {
		b.WriteString(detailHeaderStyle.Render(header))
	} else {
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).Bold(true).Render(header))
	}
	b.WriteString("\n")

	// Divider
	divWidth := p.width - 2
	if divWidth < 4 {
		divWidth = 4
	}
	if divWidth > 30 {
		divWidth = 30
	}
	b.WriteString(separatorStyle.Render(strings.Repeat("\u2500", divWidth)))
	b.WriteString("\n")

	if len(agents) == 0 {
		b.WriteString("\n")
		b.WriteString(emptyTitleStyle.Render("No agents deployed."))
		b.WriteString("\n\n")
		b.WriteString(emptyHintStyle.Render("Press [d] to deploy"))
		b.WriteString("\n")
		b.WriteString(emptyHintStyle.Render("your first agent."))
		return b.String()
	}

	// Visible rows (leave room for header + divider)
	visibleRows := p.height - 3
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Calculate scroll offset
	startIdx := 0
	if selectedIdx >= visibleRows {
		startIdx = selectedIdx - visibleRows + 1
	}
	endIdx := startIdx + visibleRows
	if endIdx > len(agents) {
		endIdx = len(agents)
	}

	nameWidth := p.width - 16
	if nameWidth < 6 {
		nameWidth = 6
	}
	if nameWidth > 14 {
		nameWidth = 14
	}

	for i := startIdx; i < endIdx; i++ {
		a := agents[i]
		icon := statusIcon(a.Status)
		selected := i == selectedIdx

		name := a.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-1] + "\u2026"
		}

		role := a.Role
		if len(role) > 6 {
			role = role[:6]
		}

		stype := a.ServerType
		if len(stype) > 5 {
			stype = stype[:5]
		}

		var line string
		if selected && focused {
			line = fmt.Sprintf("%s %s %s %s",
				icon,
				agentNameSelectedStyle.Width(nameWidth).Render(name),
				agentRoleStyle.Render(fmt.Sprintf("%-6s", role)),
				agentTypeStyle.Render(stype),
			)
		} else if selected {
			line = fmt.Sprintf("%s %s %s %s",
				icon,
				agentNameStyle.Width(nameWidth).Render(name),
				agentRoleStyle.Render(fmt.Sprintf("%-6s", role)),
				agentTypeStyle.Render(stype),
			)
		} else {
			line = fmt.Sprintf("%s %s %s %s",
				icon,
				lipgloss.NewStyle().Width(nameWidth).Foreground(dimColor).Render(name),
				agentRoleStyle.Render(fmt.Sprintf("%-6s", role)),
				agentTypeStyle.Render(stype),
			)
		}

		b.WriteString(line)
		if i < endIdx-1 {
			b.WriteString("\n")
		}
	}

	// Scroll indicator
	if len(agents) > visibleRows {
		b.WriteString("\n")
		scrollPct := float64(selectedIdx+1) / float64(len(agents)) * 100
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).Render(
			fmt.Sprintf(" %d/%d (%.0f%%)", selectedIdx+1, len(agents), scrollPct),
		))
	}

	return b.String()
}
