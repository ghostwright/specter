package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/ghostwright/specter/internal/cloudflare"
	"github.com/ghostwright/specter/internal/config"
	"github.com/ghostwright/specter/internal/hetzner"
)

type setupPhase int

const (
	setupPhaseForm setupPhase = iota
	setupPhaseValidating
	setupPhaseDone
)

type setupStepInfo struct {
	name   string
	status string // "", "active", "done", "error"
	detail string
}

// SetupWizardModel is the huh-based setup form for first-run configuration.
type SetupWizardModel struct {
	form   *huh.Form
	phase  setupPhase
	width  int
	height int

	// Form values
	hetznerToken string
	cfToken      string
	cfZoneID     string
	domain       string

	// Validation progress
	steps   []setupStepInfo
	spinIdx int
	err     error

	// Result
	cfg *config.Config
}

// NewSetupWizardModel creates the first-run setup wizard.
func NewSetupWizardModel() SetupWizardModel {
	m := SetupWizardModel{
		domain: "specter.tools",
	}

	m.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("hetzner_token").
				Title("Hetzner Cloud API Token").
				Description("Get one at console.hetzner.cloud - Security - API Tokens").
				Placeholder("your-hetzner-token").
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("token is required")
					}
					if len(s) < 10 {
						return fmt.Errorf("token looks too short")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewInput().
				Key("cf_token").
				Title("Cloudflare DNS API Token").
				Description("Token with Zone:DNS:Edit permissions").
				Placeholder("your-cloudflare-token").
				EchoMode(huh.EchoModePassword).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("token is required")
					}
					return nil
				}),

			huh.NewInput().
				Key("cf_zone_id").
				Title("Cloudflare Zone ID").
				Description("Found in your domain's overview page on Cloudflare").
				Placeholder("zone-id").
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("zone ID is required")
					}
					return nil
				}),

			huh.NewInput().
				Key("domain").
				Title("Base Domain").
				Description("Agents will be deployed as <name>.<domain>").
				Placeholder("specter.tools").
				Value(&m.domain),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title("Start setup?").
				Description("This will validate your tokens, create a firewall, and write config.").
				Affirmative("Set up Specter").
				Negative("Cancel"),
		),
	).
		WithWidth(55).
		WithShowHelp(true).
		WithShowErrors(true).
		WithTheme(huh.ThemeFunc(specterFormTheme))

	return m
}

func (m SetupWizardModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m SetupWizardModel) Update(msg tea.Msg) (SetupWizardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			if m.phase == setupPhaseForm {
				return m, func() tea.Msg { return SetupWizardCancelMsg{} }
			}
		case "ctrl+c":
			return m, tea.Quit
		}
	case spinTickMsg:
		m.spinIdx = (m.spinIdx + 1) % len(spinChars)
		return m, nil
	}

	if m.phase == setupPhaseValidating {
		return m, nil
	}

	form, cmd := m.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		m.form = f
	}

	if m.form.State == huh.StateCompleted {
		confirmed := m.form.GetBool("confirm")
		if !confirmed {
			return m, func() tea.Msg { return SetupWizardCancelMsg{} }
		}

		m.hetznerToken = m.form.GetString("hetzner_token")
		m.cfToken = m.form.GetString("cf_token")
		m.cfZoneID = m.form.GetString("cf_zone_id")
		m.domain = m.form.GetString("domain")
		if m.domain == "" {
			m.domain = "specter.tools"
		}

		m.phase = setupPhaseValidating
		m.steps = []setupStepInfo{
			{name: "Validating Hetzner token"},
			{name: "Validating Cloudflare token"},
			{name: "Fetching SSH keys"},
			{name: "Creating firewall"},
			{name: "Fetching server types"},
			{name: "Checking for golden snapshot"},
			{name: "Saving config"},
		}

		return m, tea.Batch(
			spinTickCmd(),
			m.runSetupCmd(),
		)
	}

	if m.form.State == huh.StateAborted {
		return m, func() tea.Msg { return SetupWizardCancelMsg{} }
	}

	return m, cmd
}

func (m SetupWizardModel) View() string {
	switch m.phase {
	case setupPhaseValidating, setupPhaseDone:
		return m.validatingView()
	default:
		return m.formView()
	}
}

func (m SetupWizardModel) formView() string {
	logo := DashboardLogo()

	title := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).
		Render("\u2b21 SPECTER SETUP")

	subtitle := lipgloss.NewStyle().Foreground(mutedColor).
		Render("First-run configuration wizard")

	formView := strings.TrimSuffix(m.form.View(), "\n\n")

	content := logo + "\n\n" + title + "\n" + subtitle + "\n\n" + formView

	boxWidth := 60
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

func (m SetupWizardModel) validatingView() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).
		Render("\u2b21 SETTING UP SPECTER")
	b.WriteString(title)
	b.WriteString("\n\n")

	for _, step := range m.steps {
		var icon, detail string
		switch step.status {
		case "done":
			icon = lipgloss.NewStyle().Foreground(successColor).Render("\u2713")
			if step.detail != "" {
				detail = lipgloss.NewStyle().Foreground(mutedColor).Render("  " + step.detail)
			}
		case "active":
			icon = lipgloss.NewStyle().Foreground(primaryColor).Render(spinChars[m.spinIdx])
		case "error":
			icon = lipgloss.NewStyle().Foreground(errorColor).Render("\u2717")
			if step.detail != "" {
				detail = lipgloss.NewStyle().Foreground(errorColor).Render("  " + step.detail)
			}
		default:
			icon = lipgloss.NewStyle().Foreground(mutedColor).Render("\u25cb")
		}
		b.WriteString(fmt.Sprintf("  %s %-30s%s\n", icon, step.name, detail))
	}

	if m.phase == setupPhaseDone && m.err == nil {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(successColor).Bold(true).
			Render("  \u2713 Setup complete! Launching dashboard..."))
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(errorColor).Bold(true).
			Render(fmt.Sprintf("  Error: %v", m.err)))
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(mutedColor).
			Render("  Press esc to quit and try again."))
	}

	content := b.String()

	boxWidth := 60
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

func (m *SetupWizardModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.form.WithWidth(min(55, w-8))
}

// runSetupCmd runs the full setup pipeline (validate, create firewall, cache, save config).
// It sends updates through the program and returns a completion message.
func (m *SetupWizardModel) runSetupCmd() tea.Cmd {
	hToken := m.hetznerToken
	cfToken := m.cfToken
	cfZoneID := m.cfZoneID
	domain := m.domain

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		cfg := config.DefaultConfig()
		cfg.Hetzner.Token = hToken
		cfg.Cloudflare.Token = cfToken
		cfg.Cloudflare.ZoneID = cfZoneID
		cfg.Domain = domain

		// Step 0: Validate Hetzner token
		hc := hetzner.NewClient(cfg.Hetzner.Token)
		if err := hc.ValidateToken(ctx); err != nil {
			return setupValidationResult{step: 0, err: fmt.Errorf("Hetzner token invalid: %w", err)}
		}

		// Step 1: Validate Cloudflare token
		cf := cloudflare.NewClient(cfg.Cloudflare.Token, cfg.Cloudflare.ZoneID)
		if err := cf.ValidateToken(ctx); err != nil {
			return setupValidationResult{step: 1, err: fmt.Errorf("Cloudflare token invalid: %w", err)}
		}

		// Step 2: Fetch SSH keys and pick one
		keys, err := hc.ListSSHKeys(ctx)
		if err != nil {
			return setupValidationResult{step: 2, err: fmt.Errorf("could not list SSH keys: %w", err)}
		}
		if len(keys) == 0 {
			return setupValidationResult{step: 2, err: fmt.Errorf("no SSH keys found on Hetzner. Upload one at console.hetzner.cloud")}
		}
		// Auto-select first key (most common case; user can re-run init for different key)
		cfg.Hetzner.SSHKeyName = keys[0].Name
		sshDetail := keys[0].Name
		if len(keys) > 1 {
			sshDetail = fmt.Sprintf("%s (of %d)", keys[0].Name, len(keys))
		}

		// Step 3: Create firewall
		fw, err := hc.CreateFirewall(ctx, "specter-default")
		if err != nil {
			return setupValidationResult{step: 3, err: err}
		}
		cfg.Hetzner.FirewallID = fw.ID

		// Step 4: Fetch server types
		serverTypes, err := hc.ListServerTypes(ctx)
		stDetail := ""
		if err != nil {
			stDetail = "failed (will use fallback)"
		} else {
			cache := &config.ServerTypeCache{
				Types:     serverTypes,
				FetchedAt: time.Now(),
			}
			if cacheErr := cache.Save(); cacheErr != nil {
				stDetail = "could not cache"
			} else {
				stDetail = fmt.Sprintf("%d types", len(serverTypes))
			}
		}

		// Step 5: Check for existing snapshot
		snap, err := hc.FindSpecterSnapshot(ctx)
		snapDetail := ""
		if err != nil {
			return setupValidationResult{step: 5, err: err}
		}
		if snap != nil {
			cfg.Snapshot.ID = snap.ID
			if v, ok := snap.Labels["version"]; ok {
				cfg.Snapshot.Version = v
			}
			cfg.Snapshot.DiskSize = snap.DiskSize
			snapDetail = fmt.Sprintf("found (ID: %d)", snap.ID)
		} else {
			snapDetail = "none (build with [b])"
		}

		// Step 6: Save config
		if err := cfg.Save(); err != nil {
			return setupValidationResult{step: 6, err: err}
		}

		return setupValidationResult{
			cfg:        cfg,
			sshDetail:  sshDetail,
			stDetail:   stDetail,
			snapDetail: snapDetail,
		}
	}
}

// setupValidationResult carries the result of the async setup pipeline.
type setupValidationResult struct {
	step       int // which step failed (only meaningful on error)
	err        error
	cfg        *config.Config
	sshDetail  string
	stDetail   string
	snapDetail string
}

// HandleSetupResult processes the validation result and updates the wizard state.
// Returns the message to send to the parent model.
func (m *SetupWizardModel) HandleSetupResult(result setupValidationResult) tea.Msg {
	if result.err != nil {
		for i := 0; i < result.step; i++ {
			m.steps[i].status = "done"
		}
		m.steps[result.step].status = "error"
		m.steps[result.step].detail = result.err.Error()
		m.err = result.err
		return nil
	}

	// All steps succeeded
	for i := range m.steps {
		m.steps[i].status = "done"
	}
	m.steps[2].detail = result.sshDetail
	m.steps[4].detail = result.stDetail
	m.steps[5].detail = result.snapDetail
	m.steps[6].detail = "~/.specter/config.yaml"
	m.phase = setupPhaseDone
	m.cfg = result.cfg

	return SetupWizardCompleteMsg{Cfg: result.cfg}
}
