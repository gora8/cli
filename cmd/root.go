package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agentctl",
	Short: "agentplane CLI — the economic operating system for AI agents",
	Long: `agentctl lets you deploy AI agents as first-class economic participants
on the agentplane network.

Deploy an agent, attach a wallet, configure spending policies, and publish
to agent registries — all from the command line.

Get started:
  agentctl auth login
  agentctl deploy ./my-agent/

Documentation: https://docs.agentplane.ai
`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

// Execute runs the root command.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

func init() {
	rootCmd.AddCommand(
		authCmd,
		deployCmd,
		agentsCmd,
		walletCmd,
		identityCmd,
		publishCmd,
		earningsCmd,
		seoCmd,
		policyCmd,
		logsCmd,
		approvalsCmd,
		versionCmd,
	)
}
