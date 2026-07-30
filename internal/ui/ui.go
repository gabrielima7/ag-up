// Package ui renders the interactive terminal menu for ag-up and formats all
// output tables and progress messages. It uses only the standard library
// (bufio, fmt, strings) to keep the dependency footprint minimal.
package ui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gabrielima7/GopherCore/result"
	"github.com/gabrielima7/ag-up/internal/checker"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
	"github.com/gabrielima7/ag-up/internal/updater"
)

// ANSI colour codes for terminal output.
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorGray   = "\033[90m"
)

// banner prints the ag-up ASCII art header.
func banner(version string) {
	verStr := "v" + strings.TrimPrefix(version, "v")
	fmt.Println()
	fmt.Println(colorCyan + colorBold + "  ╔═══════════════════════════════════════════════╗" + colorReset)
	fmt.Println(colorCyan + colorBold + "  ║       ag-up · Google Antigravity Updater      ║" + colorReset)
	fmt.Printf(colorCyan+colorBold+"  ║              Universal Updater %-15s║\n"+colorReset, verStr+" ")
	fmt.Println(colorCyan + colorBold + "  ╚═══════════════════════════════════════════════╝" + colorReset)
	fmt.Println()
}

// menu prints the interactive menu options.
func menu() {
	fmt.Println(colorBold + "  Choose an option:" + colorReset)
	fmt.Println()
	fmt.Println("    " + colorCyan + "1)" + colorReset + " Check for updates          (dry-run)")
	fmt.Println("    " + colorCyan + "2)" + colorReset + " Update All                 (parallel)")
	fmt.Println("    " + colorCyan + "3)" + colorReset + " Update Antigravity CLI     (agy)")
	fmt.Println("    " + colorCyan + "4)" + colorReset + " Update Antigravity IDE")
	fmt.Println("    " + colorCyan + "5)" + colorReset + " Update Antigravity Hub     (2.0)")
	fmt.Println("    " + colorCyan + "6)" + colorReset + " Exit")
	fmt.Println()
	fmt.Print(colorBold + "  → " + colorReset)
}

// readLine reads a single line from reader, aggressively trimming all leading
// and trailing whitespace including \r\n (important for TTY and piped input).
// Returns an empty string on EOF or read error.
func readLine(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		// EOF with partial data is still valid (e.g. last line without trailing newline).
		// Return whatever was read after trimming.
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(line)
}

// pressEnterToContinue blocks until the user presses Enter, preventing the
// menu loop from instantly redrawing and pushing result tables off-screen.
// It creates its own reader so it does not consume bytes from the shared
// menu reader.
func pressEnterToContinue() {
	fmt.Print("\nPress [Enter] to return to the menu...")
	bufio.NewReader(os.Stdin).ReadBytes('\n') //nolint:errcheck
}

// PrintCheckResults renders a formatted table of version check results.
func PrintCheckResults(results []result.Result[checker.CheckResult]) {
	fmt.Println()
	fmt.Println(colorBold + "  ┌─────────────────────────────────────────────────────────────┐" + colorReset)
	fmt.Println(colorBold + "  │                    VERSION CHECK REPORT                     │" + colorReset)
	fmt.Println(colorBold + "  └─────────────────────────────────────────────────────────────┘" + colorReset)
	fmt.Println()
	fmt.Printf("  %-28s %-14s %-14s %s\n",
		colorBold+"Application"+colorReset,
		colorBold+"Installed"+colorReset,
		colorBold+"Latest"+colorReset,
		colorBold+"Status"+colorReset,
	)
	fmt.Println("  " + strings.Repeat("─", 65))

	for _, r := range results {
		if r.IsErr() {
			fmt.Printf("  %-28s %-14s %-14s %s\n",
				colorRed+"[error]"+colorReset,
				"—",
				"—",
				colorRed+"✗ check failed"+colorReset,
			)
			continue
		}
		cr, _ := r.Unwrap()

		localVer := cr.LocalVersion
		if localVer == "" {
			localVer = colorGray + "not installed" + colorReset
		}

		var statusStr string
		switch {
		case cr.NotInstalled:
			statusStr = colorYellow + "⬇ not installed" + colorReset
		case cr.NeedsUpdate:
			statusStr = colorGreen + "↑ update available" + colorReset
		default:
			statusStr = colorGray + "✓ up-to-date" + colorReset
		}

		fmt.Printf("  %-28s %-14s %-14s %s\n",
			cr.AppName,
			localVer,
			cr.RemoteVersion,
			statusStr,
		)
	}
	fmt.Println()
}

// PrintUpdateResults renders a formatted summary of update outcomes.
func PrintUpdateResults(results []result.Result[updater.AppUpdateSummary]) {
	fmt.Println()
	fmt.Println(colorBold + "  ┌─────────────────────────────────────────────────────────────┐" + colorReset)
	fmt.Println(colorBold + "  │                      UPDATE REPORT                          │" + colorReset)
	fmt.Println(colorBold + "  └─────────────────────────────────────────────────────────────┘" + colorReset)
	fmt.Println()
	fmt.Printf("  %-28s %-14s %-14s %s\n",
		colorBold+"Application"+colorReset,
		colorBold+"Before"+colorReset,
		colorBold+"After"+colorReset,
		colorBold+"Status"+colorReset,
	)
	fmt.Println("  " + strings.Repeat("─", 65))

	successCount, skipCount, failCount := 0, 0, 0

	for _, r := range results {
		if r.IsErr() {
			failCount++
			fmt.Printf("  %-28s %-14s %-14s %s\n",
				colorRed+"[error]"+colorReset, "—", "—",
				colorRed+"✗ failed"+colorReset,
			)
			fmt.Printf("       %s%s%s\n", colorRed, r.Error().Error(), colorReset)
			continue
		}

		s, _ := r.Unwrap()

		oldVer := s.OldVersion
		if oldVer == "" {
			oldVer = colorGray + "—" + colorReset
		}

		switch {
		case s.Skipped:
			skipCount++
			fmt.Printf("  %-28s %-14s %-14s %s\n",
				s.AppName, oldVer, s.NewVersion,
				colorGray+"↔ skipped (up-to-date)"+colorReset,
			)
		case s.Success:
			successCount++
			fmt.Printf("  %-28s %-14s %-14s %s\n",
				s.AppName, oldVer, s.NewVersion,
				colorGreen+"✓ updated"+colorReset,
			)
		default:
			failCount++
			fmt.Printf("  %-28s %-14s %-14s %s\n",
				s.AppName, oldVer, "—",
				colorRed+"✗ failed"+colorReset,
			)
			if s.Error != nil {
				fmt.Printf("       %s%s%s\n", colorRed, s.Error.Error(), colorReset)
			}
		}
	}

	fmt.Println()
	fmt.Printf("  %sTotal:%s %d updated · %d skipped · %d failed\n",
		colorBold, colorReset, successCount, skipCount, failCount,
	)
	fmt.Println()
}

// RunInteractiveMenu displays the main menu loop until the user chooses to
// exit. It dispatches to the checker and updater packages based on selection.
//
// Input is read via bufio.Reader.ReadString('\n') + strings.TrimSpace, which
// is more reliable than bufio.Scanner for interactive TTY sessions because it
// handles \r\n line endings and does not stall waiting for a second newline.
func RunInteractiveMenu(
	ctx context.Context,
	m *manifest.Manifest,
	maxRetries int,
	version string,
) error {
	reader := bufio.NewReader(os.Stdin)
	allSpecs := config.All()

	for {
		// Check for context cancellation at the top of each loop iteration so
		// that a Ctrl-C received while the menu is printing exits cleanly.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		banner(version)
		menu()

		choice := readLine(reader)

		switch choice {
		case "1":
			// Dry-run check for all apps.
			fmt.Println()
			fmt.Println(colorCyan + "  Checking versions (this may take a moment)..." + colorReset)
			results, err := checker.CheckAll(ctx, allSpecs, *m, maxRetries)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s  Error: %v%s\n", colorRed, err, colorReset)
				continue
			}
			PrintCheckResults(results)
			pressEnterToContinue()

		case "2":
			// Update all apps concurrently.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating all applications..." + colorReset)
			results, err := updater.UpdateAll(ctx, allSpecs, m, maxRetries)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s  Error: %v%s\n", colorRed, err, colorReset)
				continue
			}
			PrintUpdateResults(results)
			pressEnterToContinue()

		case "3":
			// Update only the CLI.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating Antigravity CLI (agy)..." + colorReset)
			r := updater.Update(ctx, config.CLIApp, m, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})
			pressEnterToContinue()

		case "4":
			// Update only the IDE.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating Antigravity IDE..." + colorReset)
			r := updater.Update(ctx, config.IDEApp, m, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})
			pressEnterToContinue()

		case "5":
			// Update only the Hub.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating Antigravity Hub (2.0)..." + colorReset)
			r := updater.Update(ctx, config.HubApp, m, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})
			pressEnterToContinue()

		case "6", "q", "Q", "exit":
			fmt.Println()
			fmt.Println(colorGray + "  Goodbye." + colorReset)
			fmt.Println()
			return nil

		default:
			fmt.Printf("\n  %sInvalid choice %q — please enter 1–6.%s\n\n",
				colorYellow, choice, colorReset,
			)
		}
	}
}
