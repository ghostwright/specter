package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ghostwright/specter/internal/config"
	"github.com/spf13/cobra"
)

var sshRoot bool

var sshCmd = &cobra.Command{
	Use:   "ssh <name>",
	Short: "Connect to an agent's server",
	Long:  "SSH into a deployed agent. Connects as 'specter' user by default. Use --root for admin access.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSSH,
}

func init() {
	sshCmd.Flags().BoolVar(&sshRoot, "root", false, "Connect as root instead of specter user")
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

	user := "specter"
	if sshRoot {
		user = "root"
	}

	sshExec := exec.Command("ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", user, agent.IP),
	)
	sshExec.Stdin = os.Stdin
	sshExec.Stdout = os.Stdout
	sshExec.Stderr = os.Stderr

	return sshExec.Run()
}
