package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// ── Prefix symbols ────────────────────────────────────────────────────────────

var (
	checkMark = color.GreenString("✓")
	crossMark = color.RedString("✗")
	infoMark  = color.CyanString("ℹ")
	warnMark  = color.YellowString("⚠")
	arrowMark = color.HiBlackString("→")
)

// Success prints a green success message with a checkmark.
func Success(msg string) {
	fmt.Fprintf(os.Stdout, "%s %s\n", checkMark, msg)
}

// Error prints a red error message with an X.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", crossMark, color.RedString(msg))
}

// Info prints a cyan informational message.
func Info(msg string) {
	fmt.Fprintf(os.Stdout, "%s %s\n", infoMark, msg)
}

// Warning prints a yellow warning message.
func Warning(msg string) {
	fmt.Fprintf(os.Stdout, "%s %s\n", warnMark, color.YellowString(msg))
}

// Step prints an in-progress step message (→ message).
func Step(msg string) {
	fmt.Fprintf(os.Stdout, "%s %s\n", arrowMark, color.HiWhiteString(msg))
}

// Header prints a bold section header.
func Header(msg string) {
	fmt.Fprintf(os.Stdout, "\n%s\n", color.New(color.Bold).Sprint(msg))
}

// PrintJSON pretty-prints a value as JSON.
func PrintJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// Table renders a table to stdout.
func Table(headers []string, rows [][]string) {
	t := tablewriter.NewWriter(os.Stdout)
	t.SetHeader(headers)
	t.SetBorder(false)
	t.SetColumnSeparator("  ")
	t.SetHeaderLine(false)
	t.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	t.SetAlignment(tablewriter.ALIGN_LEFT)
	t.SetTablePadding("  ")
	t.SetNoWhiteSpace(true)
	t.SetAutoWrapText(false)
	t.AppendBulk(rows)
	t.Render()
}

// ── Spinner ───────────────────────────────────────────────────────────────────

// Spinner is a simple terminal spinner.
type Spinner struct {
	frames  []string
	message string
	stop    chan struct{}
	done    chan struct{}
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(msg string) *Spinner {
	return &Spinner{
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		message: msg,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Start begins rendering the spinner in a goroutine.
func (s *Spinner) Start() {
	go func() {
		defer close(s.done)
		i := 0
		for {
			select {
			case <-s.stop:
				// Clear the spinner line.
				fmt.Fprintf(os.Stdout, "\r\033[K")
				return
			default:
				frame := color.CyanString(s.frames[i%len(s.frames)])
				fmt.Fprintf(os.Stdout, "\r%s  %s", frame, s.message)
				time.Sleep(80 * time.Millisecond)
				i++
			}
		}
	}()
}

// Stop halts the spinner and prints a success message.
func (s *Spinner) Stop(successMsg string) {
	close(s.stop)
	<-s.done
	if successMsg != "" {
		Success(successMsg)
	}
}

// Fail halts the spinner and prints an error message.
func (s *Spinner) Fail(errMsg string) {
	close(s.stop)
	<-s.done
	if errMsg != "" {
		Error(errMsg)
	}
}

// ── Color helpers ─────────────────────────────────────────────────────────────

// Green returns a green-colored string.
func Green(s string) string { return color.GreenString(s) }

// Red returns a red-colored string.
func Red(s string) string { return color.RedString(s) }

// Yellow returns a yellow-colored string.
func Yellow(s string) string { return color.YellowString(s) }

// Cyan returns a cyan-colored string.
func Cyan(s string) string { return color.CyanString(s) }

// Bold returns a bold string.
func Bold(s string) string { return color.New(color.Bold).Sprint(s) }

// Dim returns a dimmed string.
func Dim(s string) string { return color.HiBlackString(s) }

// StatusColor returns a colored status string.
func StatusColor(status string) string {
	switch status {
	case "active", "running", "success", "approved":
		return color.GreenString(status)
	case "paused", "pending":
		return color.YellowString(status)
	case "error", "failed", "denied":
		return color.RedString(status)
	default:
		return status
	}
}
