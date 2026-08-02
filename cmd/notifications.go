package cmd

import (
	"fmt"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
)

var (
	notificationsUnreadOnly bool
	notificationsJSONOutput bool
)

var notificationsCmd = &cobra.Command{
	Use:     "notifications",
	Short:   "View account notifications",
	Aliases: []string{"notification"},
	Long: `View notifications for your account — payments received, invocation
errors, withdrawals, and calls blocked pending approval
(policy.approvalThreshold — see 'gora8 policy set --approval-above').

There's no separate approval queue to act on: a blocked call is rejected
immediately, and a notification is how you find out it happened. Raise the
agent's threshold with 'gora8 policy set' if the caller should be allowed
through next time.`,
	RunE: runNotificationsList,
}

var notificationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent notifications",
	RunE:  runNotificationsList,
}

func runNotificationsList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	spin := ui.NewSpinner("Fetching notifications...")
	spin.Start()
	client := api.New(cfg)
	resp, err := client.ListNotifications()
	if err != nil {
		spin.Fail("Failed to fetch notifications")
		return err
	}
	spin.Stop("")

	notifications := resp.Notifications
	if notificationsUnreadOnly {
		filtered := make([]api.Notification, 0, len(notifications))
		for _, n := range notifications {
			if !n.Read {
				filtered = append(filtered, n)
			}
		}
		notifications = filtered
	}

	if notificationsJSONOutput {
		return ui.PrintJSON(notifications)
	}

	if len(notifications) == 0 {
		ui.Success("No notifications.")
		return nil
	}

	ui.Header(fmt.Sprintf("Notifications (%d unread of %d)", resp.UnreadCount, len(resp.Notifications)))

	headers := []string{"", "TYPE", "TITLE", "AGE"}
	rows := make([][]string, 0, len(notifications))
	for _, n := range notifications {
		mark := " "
		if !n.Read {
			mark = ui.Cyan("●")
		}
		rows = append(rows, []string{mark, n.Type, n.Title, humanizeNotificationAge(n.CreatedAt)})
	}
	ui.Table(headers, rows)

	unread := false
	for _, n := range notifications {
		if !n.Read {
			unread = true
			break
		}
	}
	if unread {
		fmt.Println()
		ui.Info("Mark one read with: gora8 notifications read <id>")
		ui.Info("Mark all read with: gora8 notifications read-all")
	}
	return nil
}

var notificationsReadCmd = &cobra.Command{
	Use:   "read [notification-id]",
	Short: "Mark a notification as read",
	Args:  cobra.ExactArgs(1),
	RunE:  runNotificationsRead,
}

func runNotificationsRead(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	if err := client.MarkNotificationRead(args[0]); err != nil {
		return err
	}
	ui.Success("Marked as read.")
	return nil
}

var notificationsReadAllCmd = &cobra.Command{
	Use:   "read-all",
	Short: "Mark all notifications as read",
	RunE:  runNotificationsReadAll,
}

func runNotificationsReadAll(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: gora8 auth login")
		return nil
	}

	client := api.New(cfg)
	if err := client.MarkAllNotificationsRead(); err != nil {
		return err
	}
	ui.Success("All notifications marked as read.")
	return nil
}

// humanizeNotificationAge converts an ISO8601 timestamp to a human-readable age string.
func humanizeNotificationAge(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func init() {
	notificationsCmd.AddCommand(notificationsListCmd, notificationsReadCmd, notificationsReadAllCmd)
	notificationsCmd.PersistentFlags().BoolVar(&notificationsUnreadOnly, "unread", false, "Show only unread notifications")
	notificationsCmd.PersistentFlags().BoolVar(&notificationsJSONOutput, "json", false, "Output as JSON")
}
