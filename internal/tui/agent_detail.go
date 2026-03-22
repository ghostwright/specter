package tui

import (
	"fmt"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
)

// AgentDetailPanel renders the right panel showing details of the selected agent.
type AgentDetailPanel struct {
	width  int
	height int
}

// NewAgentDetailPanel creates a new detail panel.
func NewAgentDetailPanel() AgentDetailPanel {
	return AgentDetailPanel{}
}

// SetSize updates the panel dimensions.
func (p *AgentDetailPanel) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// View renders the detail panel for the selected agent.
func (p AgentDetailPanel) View(agent *AgentViewModel, focused bool) string {
	if agent == nil {
		return p.emptyView()
	}

	var b strings.Builder

	// Header: name (role) + status
	statusStr := statusIcon(agent.Status) + " " + agent.Status.String()
	header := fmt.Sprintf("%s (%s) %s",
		agent.Name,
		agent.Role,
		statusStr,
	)
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
	if divWidth > 50 {
		divWidth = 50
	}
	b.WriteString(separatorStyle.Render(strings.Repeat("\u2500", divWidth)))
	b.WriteString("\n")

	// Detail fields
	fields := []struct {
		label string
		value string
	}{
		{"URL", agent.URL},
		{"IP", agent.IP},
		{"Type", fmt.Sprintf("%s (%s)", agent.ServerType, agent.Location)},
	}

	// Deployed time
	if !agent.DeployedAt.IsZero() {
		fields = append(fields, struct {
			label string
			value string
		}{"Deployed", formatTimeAgo(agent.DeployedAt)})
	}

	// Uptime
	if agent.Uptime != "" {
		fields = append(fields, struct {
			label string
			value string
		}{"Up", agent.Uptime})
	}

	// Version
	if agent.Version != "" {
		fields = append(fields, struct {
			label string
			value string
		}{"Version", agent.Version})
	}

	// Cost
	if agent.Cost > 0 {
		fields = append(fields, struct {
			label string
			value string
		}{"Cost", fmt.Sprintf("$%.2f/mo", agent.Cost)})
	}

	for _, f := range fields {
		val := f.value
		maxVal := p.width - 13
		if maxVal > 0 && len(val) > maxVal {
			val = val[:maxVal-1] + "\u2026"
		}
		b.WriteString(fmt.Sprintf("%s %s\n",
			detailLabelStyle.Render(f.label+":"),
			detailValueStyle.Render(val),
		))
	}

	// Health status section
	b.WriteString("\n")
	healthLine := fmt.Sprintf("Health: %s %s", statusIcon(agent.Status), agent.Status.String())
	if !agent.LastCheckAt.IsZero() {
		ago := time.Since(agent.LastCheckAt).Round(time.Second)
		healthLine += fmt.Sprintf(" (%s ago)", ago)
	}
	b.WriteString(detailSubStyle.Render(healthLine))

	return b.String()
}

// emptyView renders when no agent is selected.
func (p AgentDetailPanel) emptyView() string {
	var b strings.Builder
	b.WriteString(emptyTitleStyle.Render("No agent selected"))
	b.WriteString("\n\n")
	b.WriteString(emptyHintStyle.Render("Select an agent from the list"))
	b.WriteString("\n")
	b.WriteString(emptyHintStyle.Render("to view its details."))
	return b.String()
}

// formatTimeAgo formats a time as a human-readable "ago" string.
func formatTimeAgo(t time.Time) string {
	d := time.Since(t)

	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		if mins > 0 {
			return fmt.Sprintf("%dh %dm ago", hours, mins)
		}
		return fmt.Sprintf("%dh ago", hours)
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if hours > 0 {
		return fmt.Sprintf("%dd %dh ago", days, hours)
	}
	return fmt.Sprintf("%dd ago", days)
}
