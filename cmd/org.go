package cmd

import (
	"fmt"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

// Stage 1/9 of the gap-closure plan: the minimal org-management surface
// deferred until routes/organizations.ts's API existed — it now does.
// Not the full Enterprise Governance feature set (SSO, fine-grained
// per-capability RBAC, invite emails), just create/list/invite/assign.
var orgCmd = &cobra.Command{
	Use:   "org",
	Short: "Manage organizations — shared ownership of a fleet of agents",
	Long: `Organizations let more than one person own and see the same fleet of
agents — invite teammates, assign agents to a shared org instead of each
staying tied to one personal account.

Example:
  gora8 org create "Acme Agents"
  gora8 org invite org_abc123 teammate@acme.com
  gora8 org assign agt_abc123 org_abc123`,
	RunE: runOrgList, // default sub-command
}

var orgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List organizations you belong to",
	RunE:  runOrgList,
}

func runOrgList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	orgs, err := client.ListOrganizations()
	if err != nil {
		return err
	}
	if len(orgs) == 0 {
		ui.Info("No organizations yet. Create one: gora8 org create \"<name>\"")
		return nil
	}

	ui.Header("Organizations")
	headers := []string{"ID", "NAME", "YOUR ROLE", "MEMBERS"}
	rows := make([][]string, 0, len(orgs))
	for _, o := range orgs {
		rows = append(rows, []string{o.ID, o.Name, o.MyRole, fmt.Sprintf("%d", len(o.Members))})
	}
	ui.Table(headers, rows)
	return nil
}

var orgCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new organization — you become its owner",
	Args:  cobra.ExactArgs(1),
	RunE:  runOrgCreate,
}

func runOrgCreate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	org, err := client.CreateOrganization(args[0])
	if err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Created organization %s (%s)", org.Name, org.ID))
	return nil
}

var orgInviteRoleFlag string

var orgInviteCmd = &cobra.Command{
	Use:   "invite <org-id> <email>",
	Short: "Invite an existing gora8 account to an organization you own or admin",
	Long: `Invites a teammate by email — they must already have a gora8 account
(this doesn't send an invite email yet, see routes/organizations.ts).
Requires owner or admin role in the organization.`,
	Args: cobra.ExactArgs(2),
	RunE: runOrgInvite,
}

func init() {
	orgInviteCmd.Flags().StringVar(&orgInviteRoleFlag, "role", "member", "Role to grant: admin or member")
}

func runOrgInvite(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	member, err := client.InviteOrganizationMember(args[0], args[1], orgInviteRoleFlag)
	if err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Added %s as %s", args[1], member.Role))
	return nil
}

var orgAssignCmd = &cobra.Command{
	Use:   "assign <agent-id> <org-id>",
	Short: "Assign one of your agents to an organization you're a member of",
	Long: `Assign <agent-id> to <org-id> — you must own the agent and be a member
of the org. Pass "none" as <org-id> to unassign an agent back to being
a personal agent.

Example:
  gora8 org assign agt_abc123 org_xyz789
  gora8 org assign agt_abc123 none`,
	Args: cobra.ExactArgs(2),
	RunE: runOrgAssign,
}

func runOrgAssign(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	agentID, orgID := args[0], args[1]
	client := api.New(cfg)
	var err2 error
	if orgID == "none" {
		err2 = client.AssignAgentOrganization(agentID, nil)
	} else {
		err2 = client.AssignAgentOrganization(agentID, &orgID)
	}
	if err2 != nil {
		return err2
	}
	if orgID == "none" {
		ui.Success(fmt.Sprintf("Unassigned %s — back to a personal agent", agentID))
	} else {
		ui.Success(fmt.Sprintf("Assigned %s to %s", agentID, orgID))
	}
	return nil
}

func init() {
	orgCmd.AddCommand(orgListCmd, orgCreateCmd, orgInviteCmd, orgAssignCmd)
}
