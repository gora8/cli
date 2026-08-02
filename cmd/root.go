package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agentctl",
	Short: "gora8 CLI — the economic operating system for AI agents",
	Long: `agentctl lets you deploy AI agents as first-class economic participants
on the gora8 network.

Deploy an agent, attach a wallet, configure spending policies, and publish
to agent registries — all from the command line.

Get started:
  agentctl auth login
  agentctl deploy ./my-agent/

Documentation: https://gora8.com/docs
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
