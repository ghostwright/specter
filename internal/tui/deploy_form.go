package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/templates"
)

var validAgentName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// DeployFormModel wraps a huh form for deploying an agent.
type DeployFormModel struct {
	form       *huh.Form
	cancelled  bool
	completed  bool
	width      int
	height     int
	existNames map[string]bool

	// Collected values
	agentName  string
	role       string
	serverType string
	confirm    bool
}

// NewDeployFormModel creates the deploy form. Existing agent names are used
// to validate against duplicates. Server types come from the local cache.
func NewDeployFormModel(existingNames []string, defaultServerType, defaultLocation string) DeployFormModel {
	nameSet := make(map[string]bool, len(existingNames))
	for _, n := range existingNames {
		nameSet[n] = true
	}

	// Build server type options from cache
	serverTypeOpts := buildServerTypeOptions(defaultServerType)

	// Build location options (x86 only - golden snapshot is x86)
	locationOpts := buildLocationOptions(defaultLocation)

	m := DeployFormModel{
		existNames: nameSet,
	}

	m.form = huh.NewForm(
		// Page 1: Name + Role
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title("Agent Name").
				Placeholder("my-agent").
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("name is required")
					}
					if !validAgentName.MatchString(s) {
						return fmt.Errorf("lowercase letters, numbers, hyphens only")
					}
					if nameSet[s] {
						return fmt.Errorf("agent '%s' already exists", s)
					}
					return nil
				}),

			huh.NewSelect[string]().
				Key("role").
				Title("Role").
				Options(
					huh.NewOption("swe", "swe"),
					huh.NewOption("devops", "devops"),
					huh.NewOption("data", "data"),
					huh.NewOption("custom", "custom"),
				),
		),
		// Page 2: Server Type + Location
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("server_type").
				Title("Server Type").
				Options(serverTypeOpts...),

			huh.NewSelect[string]().
				Key("location").
				Title("Location").
				Description("Hetzner datacenter region").
				Options(locationOpts...),
		),
		// Page 3: Env vars + Confirm
		huh.NewGroup(
			huh.NewInput().
				Key("env_file").
				Title("Env File Path").
				Description("Optional .env file to load").
				Placeholder("./agent.env").
				Validate(validateEnvFilePath),

			huh.NewText().
				Key("env_inline").
				Title("Inline Env Vars").
				Description("KEY=VALUE, one per line (overrides file)").
				Placeholder("API_KEY=sk-...\nMODEL=claude-sonnet-4-20250514").
				Lines(4).
				CharLimit(2000).
				ExternalEditor(false),

			huh.NewConfirm().
				Key("confirm").
				Title("Deploy this agent?").
				Affirmative("Deploy").
				Negative("Cancel"),
		),
	).
		WithWidth(50).
		WithShowHelp(true).
		WithShowErrors(true).
		WithTheme(huh.ThemeFunc(specterFormTheme))

	return m
}

// buildLocationOptions creates huh options for x86-compatible Hetzner locations.
func buildLocationOptions(defaultLocation string) []huh.Option[string] {
	if defaultLocation == "" {
		defaultLocation = "nbg1"
	}

	type loc struct {
		id   string
		name string
	}
	locations := []loc{
		{"nbg1", "Nuremberg, DE"},
		{"fsn1", "Falkenstein, DE"},
		{"hel1", "Helsinki, FI"},
	}

	var opts []huh.Option[string]
	for _, l := range locations {
		label := fmt.Sprintf("%s (%s)", l.id, l.name)
		if l.id == defaultLocation {
			label = "* " + label
		} else {
			label = "  " + label
		}
		opts = append(opts, huh.NewOption(label, l.id))
	}
	return opts
}

// validateEnvFilePath checks that a provided env file exists.
// Empty input is valid (the field is optional).
func validateEnvFilePath(s string) error {
	if s == "" {
		return nil
	}
	path := expandHomePath(s)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		return fmt.Errorf("cannot read file: %s", path)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}
	return nil
}

// expandHomePath expands a leading ~/ to the user's home directory.
func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// parseFormEnvVars combines env file and inline vars into a single map.
// Inline vars override file vars (Docker-style precedence).
func parseFormEnvVars(envFilePath, inlineVars string) (map[string]string, error) {
	vars := make(map[string]string)

	// Load from env file if provided
	if envFilePath != "" {
		path := expandHomePath(envFilePath)
		fileVars, err := templates.ParseEnvFile(path)
		if err != nil {
			return nil, err
		}
		for k, v := range fileVars {
			vars[k] = v
		}
	}

	// Parse inline vars, overriding file values
	if inlineVars != "" {
		lines := strings.Split(inlineVars, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" {
				vars[key] = value
			}
		}
	}

	if len(vars) == 0 {
		return nil, nil
	}
	return vars, nil
}

// buildServerTypeOptions creates huh options from the server type cache.
func buildServerTypeOptions(defaultType string) []huh.Option[string] {
	cache, _ := config.LoadServerTypeCache()

	var types []config.ServerTypeInfo
	if cache != nil && len(cache.Types) > 0 {
		types = cache.SortedByPrice()
	} else {
		types = config.FallbackServerTypes
	}

	var opts []huh.Option[string]
	for _, t := range types {
		if t.Architecture == "arm" {
			continue
		}
		label := fmt.Sprintf("%-6s  %2d vCPU  %4.0f GB  $%.2f/mo",
			t.Name, t.Cores, t.Memory, t.PriceMonthly)
		if t.Name == defaultType {
			label = "* " + label
		} else {
			label = "  " + label
		}
		opts = append(opts, huh.NewOption(label, t.Name))
	}

	if len(opts) == 0 {
		opts = append(opts, huh.NewOption("cx33 (default)", "cx33"))
	}
	return opts
}

// specterFormTheme returns the orange-themed huh form styles.
func specterFormTheme(isDark bool) *huh.Styles {
	t := huh.ThemeBase(isDark)

	orange := lipgloss.Color("#F97316")
	accent := lipgloss.Color("#FB923C")
	white := lipgloss.Color("#FAFAFA")
	muted := lipgloss.Color("#71717A")
	dark := lipgloss.Color("#000000")
	surface := lipgloss.Color("#3F3F46")

	t.Focused.Base = lipgloss.NewStyle().
		PaddingLeft(1).
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(orange)
	t.Focused.Title = lipgloss.NewStyle().Foreground(orange).Bold(true)
	t.Focused.Description = lipgloss.NewStyle().Foreground(accent)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(orange).SetString("> ")
	t.Focused.Option = lipgloss.NewStyle().Foreground(white)
	t.Focused.FocusedButton = lipgloss.NewStyle().
		Foreground(dark).
		Background(orange).
		Bold(true).
		Padding(0, 2)
	t.Focused.BlurredButton = lipgloss.NewStyle().
		Foreground(white).
		Background(surface).
		Padding(0, 2)

	t.Blurred.Title = lipgloss.NewStyle().Foreground(muted)
	t.Blurred.Base = lipgloss.NewStyle().
		PaddingLeft(1).
		BorderStyle(lipgloss.HiddenBorder()).
		BorderLeft(true)

	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(orange)
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(white)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(orange)
	t.Focused.TextInput.Placeholder = lipgloss.NewStyle().Foreground(muted)

	return t
}

func (m DeployFormModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m DeployFormModel) Update(msg tea.Msg) (DeployFormModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			m.cancelled = true
			return m, func() tea.Msg { return DeployFormCancelMsg{} }
		case "ctrl+c":
			return m, tea.Quit
		}
	}

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateCompleted {
		confirmed := m.form.GetBool("confirm")
		if !confirmed {
			m.cancelled = true
			return m, func() tea.Msg { return DeployFormCancelMsg{} }
		}
		m.completed = true

		envFilePath := m.form.GetString("env_file")
		inlineVars := m.form.GetString("env_inline")
		envVars, _ := parseFormEnvVars(envFilePath, inlineVars)

		return m, func() tea.Msg {
			return DeployFormCompleteMsg{
				Name:       m.form.GetString("name"),
				Role:       m.form.GetString("role"),
				ServerType: m.form.GetString("server_type"),
				Location:   m.form.GetString("location"),
				EnvVars:    envVars,
			}
		}
	}

	if m.form.State == huh.StateAborted {
		m.cancelled = true
		return m, func() tea.Msg { return DeployFormCancelMsg{} }
	}

	return m, cmd
}

func (m DeployFormModel) View() string {
	if m.completed || m.cancelled {
		return ""
	}

	title := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).
		Render("\u2b21 DEPLOY NEW AGENT")

	formView := strings.TrimSuffix(m.form.View(), "\n\n")

	content := title + "\n\n" + formView

	boxWidth := 54
	if m.width > 0 && boxWidth > m.width-4 {
		boxWidth = m.width - 4
	}

	box := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (m *DeployFormModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.form.WithWidth(min(50, w-8))
}
