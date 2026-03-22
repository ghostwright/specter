package commands

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ghostwright/specter/internal/config"
	"github.com/spf13/cobra"
)

var shortDuration = regexp.MustCompile(`^(\d+)([smhd])$`)

// convertSinceValue converts short duration strings (5m, 1h, 30s, 2d) to
// journalctl-compatible format ("5 min ago", "1 hour ago", etc.)
func convertSinceValue(s string) string {
	s = strings.TrimSpace(s)
	match := shortDuration.FindStringSubmatch(s)
	if match == nil {
		return s
	}
	num := match[1]
	switch match[2] {
	case "s":
		return num + " seconds ago"
	case "m":
		return num + " min ago"
	case "h":
		return num + " hour ago"
	case "d":
		return num + " days ago"
	}
	return s
}

var (
	logsFollow bool
	logsLines  int
	logsSince  string
)

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "View agent logs",
	Long: `View logs from a deployed agent's systemd journal.

Examples:
  specter logs scout                     # last 100 lines
  specter logs scout -f                  # follow mode
  specter logs scout -n 500              # last 500 lines
  specter logs scout --since 30m         # last 30 minutes
  specter logs scout --since "2026-03-22 17:00"  # since a specific time`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().IntVarP(&logsLines, "lines", "n", 100, "Number of lines to show")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show logs since (e.g., '10m', '1h', '2026-03-22')")
}

func runLogs(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	state, err := config.LoadState()
	if err != nil {
		return err
	}

	agent, exists := state.GetAgent(agentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found. Run `specter list` to see deployed agents", agentName)
	}

	journalCmd := "journalctl -u specter-agent --no-pager"
	if logsFollow {
		journalCmd = "journalctl -u specter-agent -f"
	} else {
		journalCmd += fmt.Sprintf(" -n %d", logsLines)
	}
	if logsSince != "" {
		journalCmd += fmt.Sprintf(" --since '%s'", convertSinceValue(logsSince))
	}

	sshExec := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("specter@%s", agent.IP),
		"sudo "+journalCmd,
	)
	sshExec.Stdin = os.Stdin
	sshExec.Stdout = os.Stdout
	sshExec.Stderr = os.Stderr

	return sshExec.Run()
}
