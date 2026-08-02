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

var authLoginAPIKeyFlag string

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to gora8",
	Long: `Log in to gora8.

New here? This walks you through it — enter your email, we send you a
6-digit code, and you're in. No separate signup step needed.

Already have an API key (e.g. from a teammate or a CI secret)? Pass it
directly: gora8 auth login --api-key <key>`,
	RunE: runAuthLogin,
}

func init() {
	authLoginCmd.Flags().StringVar(&authLoginAPIKeyFlag, "api-key", "", "Log in with an API key you already have, skipping the email flow")
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.IsAuthenticated() {
		ui.Warning(fmt.Sprintf("Already logged in as %s. Log out first with: gora8 auth logout", cfg.UserEmail))
		return nil
	}

	// Non-interactive paths: an explicit --api-key flag, or a key piped in
	// via stdin (for scripts/CI where there's no TTY to run the email flow).
	if authLoginAPIKeyFlag != "" {
		return loginWithAPIKey(cfg, authLoginAPIKeyFlag)
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			if key := strings.TrimSpace(scanner.Text()); key != "" {
				return loginWithAPIKey(cfg, key)
			}
		}
		return fmt.Errorf("no API key piped on stdin; run interactively or pass --api-key")
	}

	return loginWithEmailOTP(cfg)
}

// loginWithEmailOTP prompts for an email, sends a one-time code, and
// exchanges it for an API key — this is the real primary flow, since most
// people running `gora8 auth login` for the first time have no key yet.
func loginWithEmailOTP(cfg *config.Config) error {
	client := api.New(cfg)

	fmt.Print("  Email: ")
	reader := bufio.NewReader(os.Stdin)
	email, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read email: %w", err)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}

	spin := ui.NewSpinner("Sending code...")
	spin.Start()
	sent, err := client.SendOTP(email)
	if err != nil {
		spin.Fail("Failed to send code")
		return fmt.Errorf("send code: %w", err)
	}
	spin.Stop("")
	if sent.DevMode {
		ui.Info("Dev mode — use code 000000")
	} else {
		ui.Info(fmt.Sprintf("Sent a 6-digit code to %s", email))
	}

	fmt.Print("  Code: ")
	code, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read code: %w", err)
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("code cannot be empty")
	}

	spin = ui.NewSpinner("Verifying...")
	spin.Start()
	verified, err := client.VerifyOTP(email, code)
	if err != nil {
		spin.Fail("Invalid or expired code")
		return fmt.Errorf("verify code: %w", err)
	}
	spin.Stop("")

	cfg.APIKey = verified.APIKey
	cfg.UserEmail = verified.User.Email
	cfg.UserID = verified.User.ID
	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	if verified.IsNew {
		ui.Success(fmt.Sprintf("Welcome to gora8, %s!", ui.Bold(verified.User.Email)))
	} else {
		ui.Success(fmt.Sprintf("Logged in as %s", ui.Bold(verified.User.Email)))
	}
	if verified.User.Plan != "" {
		ui.Info(fmt.Sprintf("Plan: %s", verified.User.Plan))
	}
	return nil
}

// loginWithAPIKey validates and persists a key the caller already has.
func loginWithAPIKey(cfg *config.Config, apiKey string) error {
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
		ui.Error("Not authenticated. Run: gora8 auth login")
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
