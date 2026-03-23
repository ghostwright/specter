package tui

import "time"

// AgentViewModel is the view-layer representation of an agent.
type AgentViewModel struct {
	Name       string
	Role       string
	ServerType string
	Location   string
	IP         string
	URL        string
	DeployedAt time.Time
	Cost       float64

	// Health data (updated async)
	Status       AgentStatus
	Uptime       string
	Version      string
	LastCheckAt  time.Time
	HealthDetail map[string]interface{}
}

type AgentStatus int

const (
	AgentChecking AgentStatus = iota
	AgentOnline
	AgentOffline
	AgentUnhealthy
)

func (s AgentStatus) String() string {
	switch s {
	case AgentOnline:
		return "online"
	case AgentOffline:
		return "offline"
	case AgentUnhealthy:
		return "unhealthy"
	default:
		return "checking"
	}
}

// AgentsLoadedMsg is sent when the agent list is loaded from config + Hetzner API.
type AgentsLoadedMsg struct {
	Agents      []AgentViewModel
	HasSnapshot bool
	Err         error
}

// HealthResultMsg is sent when a single agent's health check completes.
type HealthResultMsg struct {
	Name    string
	Status  AgentStatus
	Uptime  string
	Version string
}

// AllHealthCheckedMsg is sent after all health checks finish.
type AllHealthCheckedMsg struct{}

// healthTickMsg drives periodic health polling.
type healthTickMsg time.Time

// errMsg wraps generic errors for display.
type errMsg struct {
	Err error
}

// snapshotCheckMsg reports whether a golden snapshot exists.
type snapshotCheckMsg struct {
	Exists bool
}

// -- Deploy flow messages --

// DeployFormCompleteMsg signals that the deploy form was submitted.
type DeployFormCompleteMsg struct {
	Name       string
	Role       string
	ServerType string
	Location   string
	EnvVars    map[string]string
}

// DeployFormCancelMsg signals that the deploy form was cancelled.
type DeployFormCancelMsg struct{}

// TUIDeployPhaseMsg updates deploy progress in the dashboard.
type TUIDeployPhaseMsg struct {
	Phase   int
	Name    string
	Status  string // "active", "done", "error"
	Elapsed time.Duration
	Err     error
}

// TUIDeployCompleteMsg signals deploy finished.
type TUIDeployCompleteMsg struct {
	AgentName string
	URL       string
	IP        string
	ServerID  int64
	Elapsed   time.Duration
}

// TUIDeployErrorMsg signals deploy failed.
type TUIDeployErrorMsg struct {
	Err error
}

// -- Destroy flow messages --

// DestroyConfirmMsg signals user confirmed destroy.
type DestroyConfirmMsg struct {
	AgentName string
}

// DestroyCancelMsg signals user cancelled destroy.
type DestroyCancelMsg struct{}

// DestroyProgressMsg updates destroy progress.
type DestroyProgressMsg struct {
	Step string
	Done bool
	Err  error
}

// DestroyCompleteMsg signals destroy finished.
type DestroyCompleteMsg struct {
	AgentName string
}

// -- SSH messages --

// SSHExitMsg is sent when SSH session ends.
type SSHExitMsg struct {
	Err error
}

// -- Logs messages --

// LogsLoadedMsg carries fetched log content.
type LogsLoadedMsg struct {
	Content string
	Err     error
}

// -- Update messages --

// UpdateStartMsg signals an update operation started.
type UpdateStartMsg struct {
	AgentName string
}

// UpdateCompleteMsg signals an update operation finished.
type UpdateCompleteMsg struct {
	AgentName string
	Err       error
}

// -- Setup wizard messages --

// SetupWizardCompleteMsg signals the setup wizard finished successfully.
type SetupWizardCompleteMsg struct {
	Cfg interface{} // *config.Config, typed as interface to avoid import cycle
}

// SetupWizardCancelMsg signals the setup wizard was cancelled.
type SetupWizardCancelMsg struct{}

// -- Image build messages --

// ImageBuildPhaseMsg updates image build progress in the dashboard.
type ImageBuildPhaseMsg struct {
	Phase   int
	Status  string // "active", "done", "error"
	Sub     string // sub-status from provisioning script (=== lines)
	Elapsed time.Duration
	Err     error
}

// ImageBuildCompleteMsg signals image build finished.
type ImageBuildCompleteMsg struct {
	SnapshotID int64
	Version    string
	DiskSize   float32
	Elapsed    time.Duration
}

// ImageBuildErrorMsg signals image build failed.
type ImageBuildErrorMsg struct {
	Err error
}

// -- Status flash --

// StatusFlashMsg shows a temporary status message.
type StatusFlashMsg struct {
	Text string
}

// StatusFlashClearMsg clears the status flash.
type StatusFlashClearMsg struct{}
