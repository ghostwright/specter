package tui

import lipgloss "charm.land/lipgloss/v2"

// Dashboard styles using lipgloss v2. These mirror the hex values from
// theme.go (which uses lipgloss v1) so the visual identity is consistent.

var (
	primaryColor = lipgloss.Color("#F97316")
	accentColor  = lipgloss.Color("#FB923C")
	deepColor    = lipgloss.Color("#EA580C")
	successColor = lipgloss.Color("#22C55E")
	warningColor = lipgloss.Color("#EAB308")
	errorColor   = lipgloss.Color("#EF4444")
	mutedColor   = lipgloss.Color("#71717A")
	whiteColor   = lipgloss.Color("#FAFAFA")
	dimColor     = lipgloss.Color("#999999")
	surfaceColor = lipgloss.Color("#27272A")
	bgDarkColor  = lipgloss.Color("#18181B")

	// Title bar
	titleBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	titleCountStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	// Panel frames
	panelBorderActive = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor)

	panelBorderInactive = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(mutedColor)

	// Agent list styles
	agentNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(whiteColor)

	agentNameSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(primaryColor)

	agentRoleStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	agentTypeStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	// Status icons (v2)
	statusOnlineIcon  = lipgloss.NewStyle().Foreground(successColor).Render("\u25cf")
	statusOfflineIcon = lipgloss.NewStyle().Foreground(mutedColor).Render("\u25cb")
	statusSickIcon    = lipgloss.NewStyle().Foreground(warningColor).Render("\u25f7")
	statusCheckIcon   = lipgloss.NewStyle().Foreground(accentColor).Render("\u25cb")

	// Detail panel styles
	detailLabelStyle = lipgloss.NewStyle().
				Foreground(mutedColor).
				Width(10)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(whiteColor)

	detailHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(primaryColor)

	detailSubStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	// Status bar
	statusBarStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	statusBarKeyStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true)

	statusBarDescStyle = lipgloss.NewStyle().
				Foreground(mutedColor)

	// Help overlay
	helpTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true).
			Width(12)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(whiteColor)

	helpSectionStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true).
				Padding(1, 0, 0, 0)

	// Empty state
	emptyTitleStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	emptyHintStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	// Banner
	bannerWarningStyle = lipgloss.NewStyle().
				Foreground(warningColor).
				Bold(true)

	// Separator
	separatorStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	// Flash message
	flashSuccessStyle = lipgloss.NewStyle().
				Foreground(successColor).
				Bold(true)

	flashErrorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	flashInfoStyle = lipgloss.NewStyle().
			Foreground(accentColor)
)

// DashboardLogo returns the 5-line ghost logo using lipgloss v2 colors.
func DashboardLogo() string {
	body := lipgloss.NewStyle().Foreground(primaryColor).Bold(true)
	accent := lipgloss.NewStyle().Foreground(accentColor)
	eyes := lipgloss.NewStyle().Foreground(whiteColor).Bold(true)
	mouth := lipgloss.NewStyle().Foreground(deepColor)

	return accent.Render("    .") + body.Render("oOOOo") + accent.Render(".") + "\n" +
		body.Render("   /  ") + eyes.Render("\u25cf") + body.Render(" ") + eyes.Render("\u25cf") + body.Render("  \\") + "\n" +
		body.Render("  |    ") + mouth.Render("\u25e1") + body.Render("    |") + "\n" +
		body.Render("   \\ .___. /") + "\n" +
		accent.Render("    'v~v~v'")
}

// DashboardLogoSmall returns a compact 2-line ghost for inline headers.
func DashboardLogoSmall() string {
	body := lipgloss.NewStyle().Foreground(primaryColor).Bold(true)
	eyes := lipgloss.NewStyle().Foreground(whiteColor).Bold(true)

	return body.Render(" /") + eyes.Render("\u25cf\u25cf") + body.Render("\\") + "\n" +
		body.Render(" '~~'")
}

// statusIcon returns the appropriate icon for an agent status.
func statusIcon(s AgentStatus) string {
	switch s {
	case AgentOnline:
		return statusOnlineIcon
	case AgentOffline:
		return statusOfflineIcon
	case AgentUnhealthy:
		return statusSickIcon
	default:
		return statusCheckIcon
	}
}
