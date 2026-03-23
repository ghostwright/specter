package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/cloudflare"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// AppState tracks the top-level dashboard state.
type AppState int

const (
	stateLoading AppState = iota
	stateDashboard
	stateHelp
	stateDeployForm
	stateDeployProgress
	stateConfirmDestroy
	stateDestroying
	stateViewingLogs
	stateUpdating
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
	flashDuration      = 3 * time.Second
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

	// Overlay models
	deployForm     *DeployFormModel
	deployProgress *DeployProgressModel
	confirmDialog  *ConfirmDialogModel
	logsViewport   *LogsViewportModel

	// Flash status message
	flashMsg  string
	flashType string // "success", "error", "info"

	// The running program reference (for deploy progress goroutine to send messages).
	// Set via SetProgram before calling p.Run().
	program *tea.Program

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
func NewAppModel(cfg *config.Config) *AppModel {
	return &AppModel{
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

// SetProgram sets the tea.Program reference (needed for deploy progress).
func (m *AppModel) SetProgram(p *tea.Program) {
	m.program = p
}

// Init starts loading agents and health polling.
func (m *AppModel) Init() tea.Cmd {
	return tea.Batch(
		loadAgentsCmd(m.cfg),
		healthTickCmd(),
		spinTickCmd(),
	)
}

// Update handles all messages.
func (m *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// When in deploy form state, forward ALL messages to the form
	// (huh needs cursor blink, focus, etc. not just key presses).
	if m.state == stateDeployForm && m.deployForm != nil {
		return m.updateDeployFormAll(msg)
	}

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

		// State-specific key routing
		switch m.state {
		case stateHelp:
			return m.updateHelp(msg)
		case stateConfirmDestroy:
			return m.updateConfirmDestroy(msg)
		case stateViewingLogs:
			return m.updateLogs(msg)
		case stateDeployProgress:
			return m.updateDeployProgress(msg)
		case stateDashboard:
			return m.updateDashboard(msg)
		}

	// -- Data messages (state-independent) --

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
		if m.state == stateLoading {
			m.state = stateDashboard
		}
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
		// Also update deploy progress spinner
		if m.deployProgress != nil {
			m.deployProgress.HandleMsg(msg)
		}
		if m.loading || m.state == stateDeployProgress {
			return m, spinTickCmd()
		}
		return m, nil

	case errMsg:
		m.loadErr = msg.Err
		return m, nil

	// -- Deploy form messages --

	case DeployFormCompleteMsg:
		m.deployForm = nil
		location := m.cfg.Hetzner.DefaultLocation
		envVars := msg.EnvVars
		if envVars == nil {
			envVars = make(map[string]string)
		}

		progress := NewDeployProgressModel(msg.Name, msg.Role, msg.ServerType, location)
		progress.SetSize(m.width, m.height)
		m.deployProgress = &progress
		m.state = stateDeployProgress

		return m, tea.Batch(
			spinTickCmd(),
			RunDeployCmd(m.program, m.cfg, msg.Name, msg.Role, msg.ServerType, location, envVars),
		)

	case DeployFormCancelMsg:
		m.deployForm = nil
		m.state = stateDashboard
		return m, nil

	// -- Deploy progress messages --

	case TUIDeployPhaseMsg:
		if m.deployProgress != nil {
			m.deployProgress.HandleMsg(msg)
		}
		return m, nil

	case TUIDeployCompleteMsg:
		if m.deployProgress != nil {
			m.deployProgress.HandleMsg(msg)
		}
		m.flashMsg = fmt.Sprintf("Agent %s deployed", msg.AgentName)
		m.flashType = "success"
		// Reload agents after a short delay to let the flash show
		return m, tea.Batch(
			loadAgentsCmd(m.cfg),
			flashClearCmd(),
		)

	case TUIDeployErrorMsg:
		if m.deployProgress != nil {
			m.deployProgress.HandleMsg(msg)
		}
		m.flashMsg = fmt.Sprintf("Deploy failed: %v", msg.Err)
		m.flashType = "error"
		return m, flashClearCmd()

	// -- Destroy messages --

	case DestroyConfirmMsg:
		m.confirmDialog = nil
		m.state = stateDestroying
		m.flashMsg = fmt.Sprintf("Destroying %s...", msg.AgentName)
		m.flashType = "info"
		return m, destroyAgentCmd(m.cfg, msg.AgentName)

	case DestroyCancelMsg:
		m.confirmDialog = nil
		m.state = stateDashboard
		return m, nil

	case DestroyCompleteMsg:
		m.state = stateDashboard
		m.flashMsg = fmt.Sprintf("Agent %s destroyed", msg.AgentName)
		m.flashType = "success"
		return m, tea.Batch(
			loadAgentsCmd(m.cfg),
			flashClearCmd(),
		)

	case DestroyProgressMsg:
		if msg.Err != nil {
			m.state = stateDashboard
			m.flashMsg = fmt.Sprintf("Destroy error: %v", msg.Err)
			m.flashType = "error"
			return m, flashClearCmd()
		}
		return m, nil

	// -- SSH messages --

	case SSHExitMsg:
		// SSH session ended, return to dashboard
		return m, nil

	// -- Logs messages --

	case LogsLoadedMsg:
		if m.logsViewport != nil {
			updated, cmd := m.logsViewport.Update(msg)
			m.logsViewport = &updated
			return m, cmd
		}
		return m, nil

	// -- Update messages --

	case UpdateCompleteMsg:
		m.state = stateDashboard
		if msg.Err != nil {
			m.flashMsg = fmt.Sprintf("Update failed: %v", msg.Err)
			m.flashType = "error"
		} else {
			m.flashMsg = fmt.Sprintf("Agent %s restarted", msg.AgentName)
			m.flashType = "success"
		}
		return m, tea.Batch(
			checkAllHealthCmd(m.agents),
			flashClearCmd(),
		)

	// -- Flash clear --

	case StatusFlashClearMsg:
		m.flashMsg = ""
		m.flashType = ""
		return m, nil
	}

	return m, nil
}

// View renders the full dashboard.
func (m *AppModel) View() tea.View {
	var content string

	switch m.state {
	case stateLoading:
		content = m.loadingView()
	case stateHelp:
		content = m.helpOverlay.View()
	case stateDeployForm:
		if m.deployForm != nil {
			content = m.deployForm.View()
		}
	case stateConfirmDestroy:
		if m.confirmDialog != nil {
			content = m.confirmDialog.View()
		}
	case stateDeployProgress:
		content = m.deployProgressView()
	case stateViewingLogs:
		content = m.logsView()
	default:
		content = m.dashboardView()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// -- State-specific update handlers --

func (m *AppModel) updateDashboard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

	case "d":
		return m.startDeployForm()

	case "x":
		return m.startDestroyConfirm()

	case "s", "enter":
		return m.startSSH()

	case "o":
		return m.openURL()

	case "l":
		return m.startLogs()

	case "u":
		return m.startUpdate()
	}

	return m, nil
}

func (m *AppModel) updateHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "escape", "esc", "q":
		m.state = stateDashboard
		return m, nil
	}
	return m, nil
}

func (m *AppModel) updateDeployFormAll(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle ctrl+c globally even in form state
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	// Handle window resize
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsMsg.Width
		m.height = wsMsg.Height
		m.recalcLayout()
		return m, nil
	}

	// Handle form completion/cancel messages directly here
	// (these are produced by the form's tea.Cmd and sent back through Update,
	// but the state check at line 129 intercepts them before they reach the
	// main switch. Handle them here to avoid the blank screen bug.)
	switch msg.(type) {
	case DeployFormCompleteMsg:
		m.deployForm = nil
		m.state = stateDeployProgress
		complete := msg.(DeployFormCompleteMsg)
		location := m.cfg.Hetzner.DefaultLocation
		envVars := complete.EnvVars
		if envVars == nil {
			envVars = make(map[string]string)
		}
		progress := NewDeployProgressModel(complete.Name, complete.Role, complete.ServerType, location)
		progress.SetSize(m.width, m.height)
		m.deployProgress = &progress
		return m, tea.Batch(
			spinTickCmd(),
			RunDeployCmd(m.program, m.cfg, complete.Name, complete.Role, complete.ServerType, location, envVars),
		)
	case DeployFormCancelMsg:
		m.deployForm = nil
		m.state = stateDashboard
		return m, nil
	}

	updated, cmd := m.deployForm.Update(msg)
	m.deployForm = &updated
	return m, cmd
}

func (m *AppModel) updateConfirmDestroy(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirmDialog == nil {
		m.state = stateDashboard
		return m, nil
	}
	updated, cmd := m.confirmDialog.Update(msg)
	m.confirmDialog = &updated
	return m, cmd
}

func (m *AppModel) updateLogs(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape", "q":
		m.logsViewport = nil
		m.state = stateDashboard
		return m, nil
	}

	if m.logsViewport != nil {
		updated, cmd := m.logsViewport.Update(msg)
		m.logsViewport = &updated
		return m, cmd
	}
	return m, nil
}

func (m *AppModel) updateDeployProgress(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		if m.deployProgress != nil && m.deployProgress.done {
			m.deployProgress = nil
			m.state = stateDashboard
			return m, nil
		}
	case "q":
		if m.deployProgress != nil && m.deployProgress.done {
			m.deployProgress = nil
			m.state = stateDashboard
			return m, nil
		}
	}
	return m, nil
}

// -- Action starters --

func (m *AppModel) startDeployForm() (tea.Model, tea.Cmd) {
	if m.cfg.Snapshot.ID == 0 {
		m.flashMsg = "No golden snapshot. Run `specter image build` first."
		m.flashType = "error"
		return m, flashClearCmd()
	}

	var existingNames []string
	for _, a := range m.agents {
		existingNames = append(existingNames, a.Name)
	}

	form := NewDeployFormModel(existingNames, m.cfg.Hetzner.DefaultServerType, m.cfg.Hetzner.DefaultLocation)
	form.SetSize(m.width, m.height)
	m.deployForm = &form
	m.state = stateDeployForm

	return m, m.deployForm.Init()
}

func (m *AppModel) startDestroyConfirm() (tea.Model, tea.Cmd) {
	agent := m.getSelectedAgent()
	if agent == nil {
		return m, nil
	}

	dialog := NewDestroyConfirmDialog(agent.Name)
	dialog.SetSize(m.width, m.height)
	m.confirmDialog = &dialog
	m.state = stateConfirmDestroy

	return m, nil
}

func (m *AppModel) startSSH() (tea.Model, tea.Cmd) {
	agent := m.getSelectedAgent()
	if agent == nil {
		return m, nil
	}

	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", agent.IP))

	return m, tea.ExecProcess(sshCmd, func(err error) tea.Msg {
		return SSHExitMsg{Err: err}
	})
}

func (m *AppModel) openURL() (tea.Model, tea.Cmd) {
	agent := m.getSelectedAgent()
	if agent == nil || agent.URL == "" {
		return m, nil
	}

	// macOS: use open command
	exec.Command("open", agent.URL).Start()
	m.flashMsg = fmt.Sprintf("Opened %s", agent.URL)
	m.flashType = "info"
	return m, flashClearCmd()
}

func (m *AppModel) startLogs() (tea.Model, tea.Cmd) {
	agent := m.getSelectedAgent()
	if agent == nil {
		return m, nil
	}

	viewport := NewLogsViewportModel(agent.Name, agent.IP)
	viewport.SetSize(m.width, m.height)
	m.logsViewport = &viewport
	m.state = stateViewingLogs

	return m, viewport.Init()
}

func (m *AppModel) startUpdate() (tea.Model, tea.Cmd) {
	agent := m.getSelectedAgent()
	if agent == nil {
		return m, nil
	}

	m.flashMsg = fmt.Sprintf("Updating %s...", agent.Name)
	m.flashType = "info"
	m.state = stateUpdating

	return m, updateAgentCmd(agent.Name, agent.IP)
}

// -- View rendering methods --

func (m *AppModel) loadingView() string {
	spinner := lipgloss.NewStyle().Foreground(primaryColor).Render(spinChars[m.spinnerIdx])
	text := lipgloss.NewStyle().Foreground(whiteColor).Render(" Loading agents...")

	content := lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		spinner+text,
	)
	return content
}

func (m *AppModel) deployProgressView() string {
	if m.width < 20 || m.height < 8 {
		return "Terminal too small"
	}

	title := m.titleBar()
	m.statusBar.SetWidth(m.width)
	bar := m.statusBar.View(m.state, false, len(m.agents), m.totalCost())

	var body string
	if m.deployProgress != nil {
		body = m.deployProgress.View()
	} else {
		body = "Deploying..."
	}

	boxWidth := m.width - 4
	if boxWidth > 70 {
		boxWidth = 70
	}
	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(boxWidth).
		Render(body)

	centered := lipgloss.Place(m.width, m.height-titleBarRows-statusBarRows,
		lipgloss.Center, lipgloss.Center,
		box,
	)

	return lipgloss.JoinVertical(lipgloss.Left, title, centered, bar)
}

func (m *AppModel) logsView() string {
	if m.width < 20 || m.height < 8 {
		return "Terminal too small"
	}

	title := m.titleBar()
	m.statusBar.SetWidth(m.width)
	bar := m.statusBar.View(m.state, true, len(m.agents), m.totalCost())

	var body string
	if m.logsViewport != nil {
		body = m.logsViewport.View()
	}

	boxWidth := m.width - 4
	if boxWidth > 90 {
		boxWidth = 90
	}
	boxHeight := m.height - titleBarRows - statusBarRows - 4
	if boxHeight < 10 {
		boxHeight = 10
	}

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(boxWidth).
		Height(boxHeight).
		Render(body)

	centered := lipgloss.Place(m.width, m.height-titleBarRows-statusBarRows,
		lipgloss.Center, lipgloss.Center,
		box,
	)

	return lipgloss.JoinVertical(lipgloss.Left, title, centered, bar)
}

func (m *AppModel) dashboardView() string {
	if m.width < 20 || m.height < 8 {
		return "Terminal too small"
	}

	// Calculate panel sizes
	leftW, rightW, contentH := m.panelSizes()

	// Title bar
	title := m.titleBar()

	// Banners (snapshot warning, errors, flash messages)
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
		leftStyle = panelBorderActive.Width(leftW-2).Height(adjustedH-2).Padding(0, 1)
		rightStyle = panelBorderInactive.Width(rightW-2).Height(adjustedH-2).Padding(0, 1)
	} else {
		leftStyle = panelBorderInactive.Width(leftW-2).Height(adjustedH-2).Padding(0, 1)
		rightStyle = panelBorderActive.Width(rightW-2).Height(adjustedH-2).Padding(0, 1)
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

func (m *AppModel) titleBar() string {
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

func (m *AppModel) bannerView() string {
	var banners []string

	// Flash message
	if m.flashMsg != "" {
		var style lipgloss.Style
		switch m.flashType {
		case "success":
			style = flashSuccessStyle
		case "error":
			style = flashErrorStyle
		default:
			style = flashInfoStyle
		}
		banners = append(banners, "  "+style.Render(m.flashMsg))
	}

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

func (m *AppModel) panelSizes() (leftW, rightW, contentH int) {
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

func (m *AppModel) getSelectedAgent() *AgentViewModel {
	if len(m.agents) == 0 || m.selectedIdx >= len(m.agents) {
		return nil
	}
	return &m.agents[m.selectedIdx]
}

func (m *AppModel) totalCost() float64 {
	var total float64
	for _, a := range m.agents {
		total += a.Cost
	}
	return total
}

func (m *AppModel) recalcLayout() {
	m.helpOverlay.SetSize(m.width, m.height)
	if m.deployForm != nil {
		m.deployForm.SetSize(m.width, m.height)
	}
	if m.confirmDialog != nil {
		m.confirmDialog.SetSize(m.width, m.height)
	}
	if m.deployProgress != nil {
		m.deployProgress.SetSize(m.width, m.height)
	}
	if m.logsViewport != nil {
		m.logsViewport.SetSize(m.width, m.height)
	}
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

func flashClearCmd() tea.Cmd {
	return tea.Tick(flashDuration, func(t time.Time) tea.Msg {
		return StatusFlashClearMsg{}
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

// destroyAgentCmd runs the full destroy flow.
func destroyAgentCmd(cfg *config.Config, agentName string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		state, err := config.LoadState()
		if err != nil {
			return DestroyProgressMsg{Err: fmt.Errorf("could not load state: %w", err)}
		}

		agent, exists := state.GetAgent(agentName)
		if !exists {
			return DestroyProgressMsg{Err: fmt.Errorf("agent '%s' not found in state", agentName)}
		}

		hc := hetzner.NewClient(cfg.Hetzner.Token)
		cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)

		// Delete DNS record
		if agent.DNSRecordID != "" {
			if err := cf.DeleteDNSRecord(ctx, agent.DNSRecordID); err != nil {
				if !cloudflare.IsNotFound(err) {
					// Non-critical, continue
				}
			}
		}

		// Delete server
		if err := hc.DeleteServer(ctx, &hcloud.Server{ID: agent.ServerID}); err != nil {
			if !hetzner.IsNotFound(err) {
				return DestroyProgressMsg{Err: fmt.Errorf("failed to delete server: %w", err)}
			}
		}

		// Remove from state
		state.RemoveAgent(agentName)
		if err := state.Save(); err != nil {
			return DestroyProgressMsg{Err: fmt.Errorf("could not save state: %w", err)}
		}

		return DestroyCompleteMsg{AgentName: agentName}
	}
}

// updateAgentCmd restarts the agent service via SSH.
func updateAgentCmd(agentName, ip string) tea.Cmd {
	return func() tea.Msg {
		client, err := hetzner.SSHConnect(ip)
		if err != nil {
			return UpdateCompleteMsg{AgentName: agentName, Err: fmt.Errorf("SSH connect failed: %w", err)}
		}
		defer client.Close()

		_, err = hetzner.SSHRun(client, "systemctl restart specter-agent && sleep 2 && curl -sf http://localhost:3100/health > /dev/null 2>&1")
		if err != nil {
			return UpdateCompleteMsg{AgentName: agentName, Err: fmt.Errorf("restart failed: %w", err)}
		}

		return UpdateCompleteMsg{AgentName: agentName}
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
