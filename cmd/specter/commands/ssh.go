package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ghostwright/specter/internal/config"
	"github.com/spf13/cobra"
)

var sshCmd = &cobra.Command{
	Use:   "ssh <name>",
	Short: "Connect to an agent's server",
	Args:  cobra.ExactArgs(1),
	RunE:  runSSH,
}

func runSSH(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	state, err := config.LoadState()
	if err != nil {
		return err
	}

	agent, exists := state.GetAgent(agentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found. Run `specter list` to see deployed agents", agentName)
	}

	sshCmd := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("root@%s", agent.IP),
	)
	sshCmd.Stdin = os.Stdin
	sshCmd.Stdout = os.Stdout
	sshCmd.Stderr = os.Stderr

	return sshCmd.Run()
}
