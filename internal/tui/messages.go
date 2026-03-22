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
