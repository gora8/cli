package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

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

Opens your browser to confirm a short code — approve it there (signing up
first if you're new) and you're in. No separate signup step needed. Works
over SSH too: if a browser can't open locally, copy the printed URL to any
device.

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

	return loginWithDeviceFlow(cfg)
}

// loginWithDeviceFlow implements the OAuth 2.0 Device Authorization Grant
// (RFC 8628) — the same pattern the Vercel/GitHub/Stripe CLIs use. The CLI
// requests a code pair with no account context, opens (or prints) a browser
// URL for a human to confirm, and polls until that happens. This is the
// real primary flow now: most people running `gora8 auth login` for the
// first time have no key yet, and this handles both login and signup in
// one browser step (unlike the old email/OTP-in-terminal flow it replaces).
func loginWithDeviceFlow(cfg *config.Config) error {
	client := api.New(cfg)

	spin := ui.NewSpinner("Requesting login code...")
	spin.Start()
	auth, err := client.DeviceAuthorize()
	if err != nil {
		spin.Fail("Failed to start login")
		return fmt.Errorf("device authorize: %w", err)
	}
	spin.Stop("")

	ui.Info(fmt.Sprintf("Confirm code: %s", ui.Bold(auth.UserCode)))
	if openBrowser(auth.VerificationURIComplete) {
		ui.Info("Opened your browser. If it didn't open, visit:")
	} else {
		ui.Info("Open this URL on any device to continue:")
	}
	fmt.Printf("  %s\n\n", ui.Cyan(auth.VerificationURIComplete))

	spin = ui.NewSpinner("Waiting for approval in the browser...")
	spin.Start()

	interval := time.Duration(auth.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(auth.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)
		result, err := client.DeviceToken(auth.DeviceCode)
		if err != nil {
			spin.Fail("Login failed")
			return fmt.Errorf("device token: %w", err)
		}
		if result == nil {
			continue // still pending — keep polling
		}

		spin.Stop("")
		cfg.APIKey = result.APIKey
		cfg.UserEmail = result.User.Email
		cfg.UserID = result.User.ID
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		ui.Success(fmt.Sprintf("Logged in as %s", ui.Bold(result.User.Email)))
		if result.User.Plan != "" {
			ui.Info(fmt.Sprintf("Plan: %s", result.User.Plan))
		}
		return nil
	}

	spin.Fail("Login code expired")
	return fmt.Errorf("timed out waiting for browser approval; run 'gora8 auth login' again")
}

// openBrowser best-effort opens url in the system's default browser.
// Returns false (never an error) when it can't — headless/SSH sessions are
// an expected, fully-supported case for device flow: the caller falls back
// to printing the URL for the user to open on any other device.
func openBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start() == nil
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
