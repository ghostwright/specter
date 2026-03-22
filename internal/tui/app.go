package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
)

// AppState tracks the top-level dashboard state.
type AppState int

const (
	stateLoading AppState = iota
	stateDashboard
	stateHelp
)

// PanelID identifies which panel has focus.
type PanelID int

const (
	panelAgentList PanelID = iota
	panelDetail
	panelCount // sentinel for modular arithmetic
)

const (
	healthPollInterval = 30 * time.Second
	minLeftWidth       = 25
	titleBarRows       = 1
	statusBarRows      = 1
)

// AppModel is the root Bubbletea model for the Specter dashboard.
type AppModel struct {
	state      AppState
	focusPanel PanelID

	// Data
	agents      []AgentViewModel
	selectedIdx int
	hasSnapshot bool
	loading     bool
	loadErr     error

	// Sub-panels (render methods, not independent models)
	agentList   AgentListPanel
	agentDetail AgentDetailPanel
	statusBar   StatusBarPanel
	helpOverlay HelpOverlay

	// Config
	cfg *config.Config

	// Layout
	width  int
	height int

	// Spinner for loading state
	spinnerIdx int
}

var spinChars = []string{"\u280b", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}

// NewAppModel creates the dashboard model.
func NewAppModel(cfg *config.Config) AppModel {
	return AppModel{
		state:       stateLoading,
		focusPanel:  panelAgentList,
		loading:     true,
		cfg:         cfg,
		agentList:   NewAgentListPanel(),
		agentDetail: NewAgentDetailPanel(),
		statusBar:   NewStatusBarPanel(),
		helpOverlay: NewHelpOverlay(),
	}
}

// Init starts loading agents and health polling.
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		loadAgentsCmd(m.cfg),
		healthTickCmd(),
		spinTickCmd(),
	)
}

// Update handles all messages.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
		return m, nil

	case tea.KeyPressMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}

		// State-specific routing
		switch m.state {
		case stateHelp:
			return m.updateHelp(msg)
		case stateDashboard:
			return m.updateDashboard(msg)
		}

	// Data messages (state-independent)
	case AgentsLoadedMsg:
		m.loading = false
		if msg.Err != nil {
			m.loadErr = msg.Err
		} else {
			m.agents = msg.Agents
			m.hasSnapshot = msg.HasSnapshot
			m.loadErr = nil
			if m.selectedIdx >= len(m.agents) {
				m.selectedIdx = max(0, len(m.agents)-1)
			}
		}
		m.state = stateDashboard
		// Immediately check health
		return m, checkAllHealthCmd(m.agents)

	case HealthResultMsg:
		for i := range m.agents {
			if m.agents[i].Name == msg.Name {
				m.agents[i].Status = msg.Status
				m.agents[i].Uptime = msg.Uptime
				m.agents[i].Version = msg.Version
				m.agents[i].LastCheckAt = time.Now()
				break
			}
		}
		return m, nil

	case healthTickMsg:
		return m, tea.Batch(
			checkAllHealthCmd(m.agents),
			healthTickCmd(),
		)

	case snapshotCheckMsg:
		m.hasSnapshot = msg.Exists
		return m, nil

	case spinTickMsg:
		m.spinnerIdx = (m.spinnerIdx + 1) % len(spinChars)
		if m.loading {
			return m, spinTickCmd()
		}
		return m, nil

	case errMsg:
		m.loadErr = msg.Err
		return m, nil
	}

	return m, nil
}

// View renders the full dashboard.
func (m AppModel) View() tea.View {
	var content string

	switch m.state {
	case stateLoading:
		content = m.loadingView()
	case stateHelp:
		content = m.helpOverlay.View()
	default:
		content = m.dashboardView()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// -- State-specific update handlers --

func (m AppModel) updateDashboard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?":
		m.state = stateHelp
		return m, nil

	case "q":
		return m, tea.Quit

	case "r":
		m.loading = true
		return m, tea.Batch(loadAgentsCmd(m.cfg), spinTickCmd())

	case "tab":
		m.focusPanel = (m.focusPanel + 1) % panelCount
		return m, nil

	case "j", "down":
		if len(m.agents) > 0 {
			m.selectedIdx = min(m.selectedIdx+1, len(m.agents)-1)
		}
		return m, nil

	case "k", "up":
		if len(m.agents) > 0 {
			m.selectedIdx = max(m.selectedIdx-1, 0)
		}
		return m, nil

	case "g":
		m.selectedIdx = 0
		return m, nil

	case "G":
		if len(m.agents) > 0 {
			m.selectedIdx = len(m.agents) - 1
		}
		return m, nil
	}

	return m, nil
}

func (m AppModel) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "escape", "esc", "q":
		m.state = stateDashboard
		return m, nil
	}
	return m, nil
}

// -- View rendering methods --

func (m AppModel) loadingView() string {
	spinner := lipgloss.NewStyle().Foreground(primaryColor).Render(spinChars[m.spinnerIdx])
	text := lipgloss.NewStyle().Foreground(whiteColor).Render(" Loading agents...")

	content := lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		spinner+text,
	)
	return content
}

func (m AppModel) dashboardView() string {
	if m.width < 20 || m.height < 8 {
		return "Terminal too small"
	}

	// Calculate panel sizes
	leftW, rightW, contentH := m.panelSizes()

	// Title bar
	title := m.titleBar()

	// Banners (snapshot warning, errors)
	banners := m.bannerView()
	bannerLines := 0
	if banners != "" {
		bannerLines = strings.Count(banners, "\n") + 1
	}

	// Adjust content height for banners
	adjustedH := contentH - bannerLines
	if adjustedH < 3 {
		adjustedH = 3
	}

	// Left panel content
	m.agentList.SetSize(leftW-2, adjustedH-2)
	leftContent := m.agentList.View(m.agents, m.selectedIdx, m.focusPanel == panelAgentList)

	// Right panel content
	var selectedAgent *AgentViewModel
	if len(m.agents) > 0 && m.selectedIdx < len(m.agents) {
		selectedAgent = &m.agents[m.selectedIdx]
	}
	m.agentDetail.SetSize(rightW-2, adjustedH-2)
	rightContent := m.agentDetail.View(selectedAgent, m.focusPanel == panelDetail)

	// Apply borders to panels
	var leftStyle, rightStyle lipgloss.Style
	if m.focusPanel == panelAgentList {
		leftStyle = panelBorderActive.Width(leftW - 2).Height(adjustedH - 2).Padding(0, 1)
		rightStyle = panelBorderInactive.Width(rightW - 2).Height(adjustedH - 2).Padding(0, 1)
	} else {
		leftStyle = panelBorderInactive.Width(leftW - 2).Height(adjustedH - 2).Padding(0, 1)
		rightStyle = panelBorderActive.Width(rightW - 2).Height(adjustedH - 2).Padding(0, 1)
	}

	leftPanel := leftStyle.Render(leftContent)
	rightPanel := rightStyle.Render(rightContent)

	// Join panels horizontally
	panels := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Status bar
	hasAgent := selectedAgent != nil
	totalCost := m.totalCost()
	m.statusBar.SetWidth(m.width)
	bar := m.statusBar.View(m.state, hasAgent, len(m.agents), totalCost)

	// Compose everything vertically
	parts := []string{title}
	if banners != "" {
		parts = append(parts, banners)
	}
	parts = append(parts, panels, bar)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m AppModel) titleBar() string {
	left := titleBarStyle.Render("\u2b21 SPECTER")

	// Right side: online count
	onlineCount := 0
	for _, a := range m.agents {
		if a.Status == AgentOnline {
			onlineCount++
		}
	}

	right := ""
	if len(m.agents) > 0 {
		if onlineCount == len(m.agents) {
			right = lipgloss.NewStyle().Foreground(successColor).Render(
				fmt.Sprintf("%d online", onlineCount),
			)
		} else {
			right = titleCountStyle.Render(
				fmt.Sprintf("%d/%d online", onlineCount, len(m.agents)),
			)
		}
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return left + strings.Repeat(" ", gap) + right
}

func (m AppModel) bannerView() string {
	var banners []string

	if m.loadErr != nil {
		banners = append(banners,
			"  "+lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render(
				fmt.Sprintf("Error: %v", m.loadErr),
			),
		)
	}

	if !m.hasSnapshot && len(m.agents) == 0 && m.loadErr == nil && m.state == stateDashboard {
		banners = append(banners,
			"  "+bannerWarningStyle.Render("No golden snapshot. Press [b] to build one."),
		)
	}

	if len(banners) == 0 {
		return ""
	}
	return strings.Join(banners, "\n")
}

// -- Layout helpers --

func (m AppModel) panelSizes() (leftW, rightW, contentH int) {
	leftW = max(minLeftWidth, m.width/3)
	rightW = m.width - leftW
	if rightW < 20 {
		rightW = 20
	}
	contentH = m.height - titleBarRows - statusBarRows
	if contentH < 5 {
		contentH = 5
	}
	return
}

func (m AppModel) selectedAgent() *AgentViewModel {
	if len(m.agents) == 0 || m.selectedIdx >= len(m.agents) {
		return nil
	}
	return &m.agents[m.selectedIdx]
}

func (m AppModel) totalCost() float64 {
	var total float64
	for _, a := range m.agents {
		total += a.Cost
	}
	return total
}

func (m *AppModel) recalcLayout() {
	m.helpOverlay.SetSize(m.width, m.height)
}

// -- Commands --

type spinTickMsg struct{}

func spinTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinTickMsg{}
	})
}

func healthTickCmd() tea.Cmd {
	return tea.Tick(healthPollInterval, func(t time.Time) tea.Msg {
		return healthTickMsg(t)
	})
}

// loadAgentsCmd loads agents from config state and Hetzner API.
func loadAgentsCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Load local state for metadata
		state, _ := config.LoadState()
		stCache, _ := config.LoadServerTypeCache()

		// Load servers from Hetzner
		hc := hetzner.NewClient(cfg.Hetzner.Token)
		servers, err := hc.ListSpecterServers(ctx)
		if err != nil {
			return AgentsLoadedMsg{Err: err}
		}

		// Check for golden snapshot
		snapshot, _ := hc.FindSpecterSnapshot(ctx)
		hasSnapshot := snapshot != nil

		agents := make([]AgentViewModel, 0, len(servers))
		for _, s := range servers {
			name := s.Labels["agent_name"]
			role := s.Labels["role"]
			if name == "" {
				name = s.Name
			}

			agent := AgentViewModel{
				Name:       name,
				Role:       role,
				ServerType: s.ServerType.Name,
				IP:         s.PublicNet.IPv4.IP.String(),
				Status:     AgentChecking,
				Cost:       config.GetMonthlyPrice(s.ServerType.Name, stCache),
			}

			// Enrich from local state
			if state != nil {
				if a, ok := state.GetAgent(name); ok {
					agent.URL = a.URL
					agent.Location = a.Location
					agent.DeployedAt = a.DeployedAt
				}
			}

			// Fallback URL
			if agent.URL == "" {
				agent.URL = fmt.Sprintf("https://%s.%s", name, cfg.Domain)
			}

			// Fallback location from server datacenter
			if agent.Location == "" {
				agent.Location = s.Datacenter.Name
			}

			agents = append(agents, agent)
		}

		return AgentsLoadedMsg{
			Agents:      agents,
			HasSnapshot: hasSnapshot,
		}
	}
}

// checkAllHealthCmd checks health of all agents concurrently.
func checkAllHealthCmd(agents []AgentViewModel) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, a := range agents {
		cmds = append(cmds, checkOneHealthCmd(a))
	}
	return tea.Batch(cmds...)
}

// checkOneHealthCmd checks health for a single agent.
func checkOneHealthCmd(agent AgentViewModel) tea.Cmd {
	return func() tea.Msg {
		if agent.URL == "" {
			return HealthResultMsg{Name: agent.Name, Status: AgentOffline}
		}

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(agent.URL + "/health")
		if err != nil {
			return HealthResultMsg{Name: agent.Name, Status: AgentOffline}
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return HealthResultMsg{Name: agent.Name, Status: AgentUnhealthy}
		}

		var data map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&data)

		msg := HealthResultMsg{
			Name:   agent.Name,
			Status: AgentOnline,
		}
		if uptime, ok := data["uptime"].(float64); ok {
			msg.Uptime = formatUptimeDashboard(int(uptime))
		}
		if v, ok := data["version"].(string); ok {
			msg.Version = v
		}
		return msg
	}
}

// formatUptimeDashboard formats seconds into a human-readable uptime string.
func formatUptimeDashboard(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	if seconds < 3600 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	if seconds < 86400 {
		return fmt.Sprintf("%dh %dm", seconds/3600, (seconds%3600)/60)
	}
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	return fmt.Sprintf("%dd %dh", days, hours)
}
