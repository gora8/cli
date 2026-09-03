package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/skill"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Install the gora8-commerce Agent Skill into a project",
	Long: `The gora8-commerce Agent Skill teaches an LLM-driven coding agent (Claude
Code or any other Agent-Skill-aware host) how to call gora8's real
commerce API — discover, quote, evaluate, plan, commit, execute, verify,
dispute — with exact request/response shapes, not guesses.

'gora8 init' installs it automatically into a new project unless you
pass --no-skill. Run 'gora8 skill install' directly to add it to an
existing project, or to pick up a newer version of the skill after a
CLI upgrade.`,
	RunE: runSkillInstall, // default sub-command
}

var skillInstallCmd = &cobra.Command{
	Use:   "install [directory]",
	Short: "Write SKILL.md and REFERENCE.md into .claude/skills/gora8-commerce/",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSkillInstall,
}

func runSkillInstall(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	target, err := skill.Install(dir)
	if err != nil {
		return fmt.Errorf("install skill: %w", err)
	}
	ui.Success(fmt.Sprintf("Installed gora8-commerce skill to %s", target))
	ui.Info("Any Agent-Skill-aware host (Claude Code, etc.) working in this project now knows how to call gora8's commerce API.")
	return nil
}

func init() {
	skillCmd.AddCommand(skillInstallCmd)
	rootCmd.AddCommand(skillCmd)
}
