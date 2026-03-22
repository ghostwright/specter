package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ghostwright/specter/internal/config"
	"github.com/spf13/cobra"
)

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Stream agent logs",
	Args:  cobra.ExactArgs(1),
	RunE:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", true, "Follow log output")
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

	journalCmd := "journalctl -u specter-agent --no-pager -n 100"
	if logsFollow {
		journalCmd = "journalctl -u specter-agent -f"
	}

	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", agent.IP),
		journalCmd,
	)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	return sshCmd.Run()
}
