package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/config"
	"github.com/gora8/cli/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with gora8",
	Long:  "Manage authentication credentials for the gora8 CLI.",
}

// ── login ─────────────────────────────────────────────────────────────────────

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to gora8",
	Long: `Log in to gora8 using your API key.

Don't have one yet? Sign up at https://gora8.com/signup (or log in at
https://gora8.com/login if you already have an account) — either flow
emails you a one-time code and shows your API key once you verify it.`,
	RunE: runAuthLogin,
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.IsAuthenticated() {
		ui.Warning(fmt.Sprintf("Already logged in as %s. Log out first with: agentctl auth logout", cfg.UserEmail))
		return nil
	}

	fmt.Print("  Enter your API key (get one at gora8.com/signup or gora8.com/login): ")

	var apiKey string
	// Use terminal raw mode for hidden input when available.
	if term.IsTerminal(int(syscall.Stdin)) {
		data, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read API key: %w", err)
		}
		apiKey = strings.TrimSpace(string(data))
	} else {
		// Fallback for piped input.
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			apiKey = strings.TrimSpace(scanner.Text())
		}
	}

	if apiKey == "" {
		return fmt.Errorf("API key cannot be empty")
	}

	// Validate by calling /v1/me.
	spin := ui.NewSpinner("Verifying API key...")
	spin.Start()

	cfg.APIKey = apiKey
	client := api.New(cfg)
	me, err := client.GetMe()
	if err != nil {
		spin.Fail("Invalid API key")
		return fmt.Errorf("authentication failed: %w", err)
	}
	spin.Stop("")

	// Persist credentials.
	cfg.UserEmail = me.Email
	cfg.UserID = me.ID
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	ui.Success(fmt.Sprintf("Logged in as %s", ui.Bold(me.Email)))
	if me.Name != "" {
		ui.Info(fmt.Sprintf("Welcome, %s!", me.Name))
	}
	if me.Plan != "" {
		ui.Info(fmt.Sprintf("Plan: %s", me.Plan))
	}
	return nil
}

// ── logout ────────────────────────────────────────────────────────────────────

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out of gora8",
	RunE:  runAuthLogout,
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Warning("Not currently logged in.")
		return nil
	}
	if err := config.Clear(); err != nil {
		return fmt.Errorf("clear config: %w", err)
	}
	ui.Success("Logged out")
	return nil
}

// ── whoami ────────────────────────────────────────────────────────────────────

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the currently authenticated user",
	RunE:  runAuthWhoami,
}

func runAuthWhoami(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if !cfg.IsAuthenticated() {
		ui.Error("Not authenticated. Run: agentctl auth login")
		return nil
	}

	spin := ui.NewSpinner("Fetching user info...")
	spin.Start()
	client := api.New(cfg)
	me, err := client.GetMe()
	if err != nil {
		spin.Fail("Failed to fetch user info")
		return err
	}
	spin.Stop("")

	ui.Header("Current User")
	rows := [][]string{
		{"Email", me.Email},
		{"User ID", me.ID},
		{"Plan", me.Plan},
		{"API URL", cfg.APIURL},
	}
	if me.Name != "" {
		rows = append([][]string{{"Name", me.Name}}, rows...)
	}
	for _, row := range rows {
		fmt.Printf("  %s  %s\n", ui.Dim(fmt.Sprintf("%-10s", row[0])), row[1])
	}
	return nil
}

func init() {
	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authWhoamiCmd)
}
