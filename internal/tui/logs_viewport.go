package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/hetzner"
)

// LogsViewportModel displays scrollable logs from a remote agent.
type LogsViewportModel struct {
	agentName string
	agentIP   string
	content   string
	lines     []string
	scrollOff int
	loading   bool
	err       error
	width     int
	height    int
}

// NewLogsViewportModel creates the logs viewer.
func NewLogsViewportModel(name, ip string) LogsViewportModel {
	return LogsViewportModel{
		agentName: name,
		agentIP:   ip,
		loading:   true,
	}
}

func (m *LogsViewportModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m LogsViewportModel) Init() tea.Cmd {
	return fetchLogsCmd(m.agentIP)
}

func (m LogsViewportModel) Update(msg tea.Msg) (LogsViewportModel, tea.Cmd) {
	switch msg := msg.(type) {
	case LogsLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.content = msg.Content
			m.lines = strings.Split(msg.Content, "\n")
			// Start at the bottom
			visibleLines := m.visibleLines()
			if len(m.lines) > visibleLines {
				m.scrollOff = len(m.lines) - visibleLines
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "j", "down":
			visibleLines := m.visibleLines()
			if m.scrollOff < len(m.lines)-visibleLines {
				m.scrollOff++
			}
			return m, nil
		case "k", "up":
			if m.scrollOff > 0 {
				m.scrollOff--
			}
			return m, nil
		case "g":
			m.scrollOff = 0
			return m, nil
		case "G":
			visibleLines := m.visibleLines()
			if len(m.lines) > visibleLines {
				m.scrollOff = len(m.lines) - visibleLines
			}
			return m, nil
		case "d":
			// Half page down
			visibleLines := m.visibleLines()
			m.scrollOff = min(m.scrollOff+visibleLines/2, max(0, len(m.lines)-visibleLines))
			return m, nil
		case "u":
			// Half page up
			visibleLines := m.visibleLines()
			m.scrollOff = max(0, m.scrollOff-visibleLines/2)
			return m, nil
		}
	}
	return m, nil
}

func (m LogsViewportModel) visibleLines() int {
	h := m.height - 4 // header + footer
	if h < 3 {
		h = 3
	}
	return h
}

func (m LogsViewportModel) View() string {
	var b strings.Builder

	header := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).
		Render(fmt.Sprintf("LOGS: %s", m.agentName))
	b.WriteString(header)
	b.WriteString("\n")

	divWidth := m.width - 4
	if divWidth < 4 {
		divWidth = 4
	}
	if divWidth > 50 {
		divWidth = 50
	}
	b.WriteString(separatorStyle.Render(strings.Repeat("\u2500", divWidth)))
	b.WriteString("\n")

	if m.loading {
		spinner := lipgloss.NewStyle().Foreground(primaryColor).Render("\u25cb")
		b.WriteString(spinner + " Loading logs...")
		return b.String()
	}

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(errorColor).
			Render(fmt.Sprintf("Error: %v", m.err)))
		return b.String()
	}

	if len(m.lines) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
			Render("No logs available"))
		return b.String()
	}

	visibleLines := m.visibleLines()
	endIdx := m.scrollOff + visibleLines
	if endIdx > len(m.lines) {
		endIdx = len(m.lines)
	}

	lineStyle := lipgloss.NewStyle().Foreground(whiteColor)
	for i := m.scrollOff; i < endIdx; i++ {
		line := m.lines[i]
		maxW := m.width - 4
		if maxW > 0 && len(line) > maxW {
			line = line[:maxW]
		}
		b.WriteString(lineStyle.Render(line))
		if i < endIdx-1 {
			b.WriteString("\n")
		}
	}

	// Scroll indicator
	b.WriteString("\n")
	pct := 0
	if len(m.lines) > visibleLines {
		pct = int(float64(m.scrollOff+visibleLines) / float64(len(m.lines)) * 100)
	} else {
		pct = 100
	}
	b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
		Render(fmt.Sprintf("[esc] close  [j/k] scroll  [g/G] top/bottom  %d%%", pct)))

	return b.String()
}

// fetchLogsCmd fetches logs from the remote agent via SSH.
func fetchLogsCmd(ip string) tea.Cmd {
	return func() tea.Msg {
		client, err := hetzner.SSHConnect(ip)
		if err != nil {
			return LogsLoadedMsg{Err: fmt.Errorf("SSH connect failed: %w", err)}
		}
		defer client.Close()

		output, err := hetzner.SSHRun(client, "journalctl -u specter-agent -n 50 --no-pager 2>/dev/null || echo 'No logs available'")
		if err != nil {
			return LogsLoadedMsg{Err: fmt.Errorf("failed to fetch logs: %w", err)}
		}
		return LogsLoadedMsg{Content: output}
	}
}
