package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

type PhaseStatus int

const (
	PhasePending PhaseStatus = iota
	PhaseActive
	PhaseDone
	PhaseFailed
)

type Phase struct {
	Name     string
	Estimate time.Duration
	Elapsed  time.Duration
	Status   PhaseStatus
}

type DeployResult struct {
	URL      string
	IP       string
	ServerID int64
	Elapsed  time.Duration
}

// Messages sent from the deploy goroutine to the TUI
type PhaseStartMsg int
type PhaseDoneMsg struct {
	Index   int
	Elapsed time.Duration
}
type PhaseFailMsg struct {
	Index int
	Err   error
}
type DeployDoneMsg struct {
	Result DeployResult
}
type DeployErrMsg struct {
	Err error
}

type tickMsg time.Time

type DeployModel struct {
	AgentName string
	Role      string
	Location  string
	ServerType string

	phases    []Phase
	current   int
	startTime time.Time
	done      bool
	err       error
	result    *DeployResult
	quitting  bool
}

func NewDeployModel(agentName, role, location, serverType string, phases []Phase) DeployModel {
	return DeployModel{
		AgentName:  agentName,
		Role:       role,
		Location:   location,
		ServerType: serverType,
		phases:     phases,
		current:    -1,
		startTime:  time.Now(),
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m DeployModel) Init() tea.Cmd {
	return tickCmd()
}

func (m DeployModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}

	case tickMsg:
		return m, tickCmd()

	case PhaseStartMsg:
		idx := int(msg)
		if idx >= 0 && idx < len(m.phases) {
			m.current = idx
			m.phases[idx].Status = PhaseActive
		}
		return m, nil

	case PhaseDoneMsg:
		if msg.Index >= 0 && msg.Index < len(m.phases) {
			m.phases[msg.Index].Status = PhaseDone
			m.phases[msg.Index].Elapsed = msg.Elapsed
		}
		return m, nil

	case PhaseFailMsg:
		if msg.Index >= 0 && msg.Index < len(m.phases) {
			m.phases[msg.Index].Status = PhaseFailed
		}
		m.err = msg.Err
		m.done = true
		return m, tea.Quit

	case DeployDoneMsg:
		m.done = true
		m.result = &msg.Result
		return m, tea.Quit

	case DeployErrMsg:
		m.err = msg.Err
		m.done = true
		return m, tea.Quit
	}

	return m, nil
}

func (m DeployModel) View() tea.View {
	return tea.NewView(m.viewString())
}

func (m DeployModel) viewString() string {
	if m.quitting {
		return "\n  Deploy interrupted. Cleaning up...\n"
	}

	var b strings.Builder

	b.WriteString(fmt.Sprintf("  %s\n\n", TitleStyle.Render(Brand)))
	b.WriteString(fmt.Sprintf("  Deploying %s (%s) to %s on %s...\n\n",
		TitleStyle.Render(m.AgentName),
		m.Role,
		m.Location,
		m.ServerType,
	))

	for i, p := range m.phases {
		var icon, detail string
		switch p.Status {
		case PhaseDone:
			icon = SuccessStyle.Render("done")
			detail = fmt.Sprintf("  %s", MutedStyle.Render(p.Elapsed.Round(time.Second).String()))
		case PhaseActive:
			elapsed := time.Since(m.startTime)
			// Subtract elapsed time of previous phases
			for j := 0; j < i; j++ {
				elapsed -= m.phases[j].Elapsed
			}
			icon = WarningStyle.Render("...")
			detail = fmt.Sprintf("  %s / ~%s",
				WarningStyle.Render(elapsed.Round(time.Second).String()),
				MutedStyle.Render(p.Estimate.Round(time.Second).String()))
		case PhaseFailed:
			icon = ErrorStyle.Render("ERR")
			detail = ""
		default:
			icon = MutedStyle.Render(" - ")
			detail = ""
		}
		b.WriteString(fmt.Sprintf("  %s %-30s%s\n", icon, p.Name, detail))
	}

	b.WriteString(fmt.Sprintf("\n  Elapsed: %s\n", time.Since(m.startTime).Round(time.Second)))

	if m.done && m.result != nil {
		b.WriteString(fmt.Sprintf("\n  %s Agent deployed successfully!\n\n", SuccessStyle.Render("done")))
		b.WriteString(fmt.Sprintf("  URL:       %s\n", TitleStyle.Render(m.result.URL)))
		b.WriteString(fmt.Sprintf("  IP:        %s\n", m.result.IP))
		b.WriteString(fmt.Sprintf("  Server ID: %d\n", m.result.ServerID))
		b.WriteString(fmt.Sprintf("  Total:     %s\n\n", m.result.Elapsed))
	}

	if m.done && m.err != nil {
		b.WriteString(fmt.Sprintf("\n  %s %v\n\n", ErrorStyle.Render("Error:"), m.err))
	}

	return BoxStyle.Render(b.String()) + "\n"
}

// Quitting returns whether the user pressed Ctrl+C.
func (m DeployModel) Quitting() bool {
	return m.quitting
}

// Err returns the error if the deploy failed.
func (m DeployModel) Err() error {
	return m.err
}
