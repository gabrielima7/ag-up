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
	fmt.Println("    " + colorCyan + "6)" + colorReset + " Force Reinstall All        (ignore version)")
	fmt.Println("    " + colorCyan + "7)" + colorReset + " Exit")
	fmt.Println()
	fmt.Print(colorBold + "  → " + colorReset)
}

// prompt reads a single line from stdin via a buffered scanner.
func prompt(scanner *bufio.Scanner) string {
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
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
func RunInteractiveMenu(
	ctx context.Context,
	m *manifest.Manifest,
	maxRetries int,
	version string,
) error {
	scanner := bufio.NewScanner(os.Stdin)
	allSpecs := config.All()

	for {
		banner(version)
		menu()

		choice := prompt(scanner)

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

		case "2":
			// Update all apps concurrently.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating all applications..." + colorReset)
			results, err := updater.UpdateAll(ctx, allSpecs, m, false, maxRetries)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s  Error: %v%s\n", colorRed, err, colorReset)
				continue
			}
			PrintUpdateResults(results)

		case "3":
			// Update only the CLI.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating Antigravity CLI (agy)..." + colorReset)
			r := updater.Update(ctx, config.CLIApp, m, false, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})

		case "4":
			// Update only the IDE.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating Antigravity IDE..." + colorReset)
			r := updater.Update(ctx, config.IDEApp, m, false, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})

		case "5":
			// Update only the Hub.
			fmt.Println()
			fmt.Println(colorCyan + "  Updating Antigravity Hub (2.0)..." + colorReset)
			r := updater.Update(ctx, config.HubApp, m, false, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})

		case "6":
			// Force reinstall all apps, ignoring cached versions.
			fmt.Println()
			fmt.Println(colorYellow + "  Force reinstalling all applications..." + colorReset)
			results, err := updater.UpdateAll(ctx, allSpecs, m, true, maxRetries)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s  Error: %v%s\n", colorRed, err, colorReset)
				continue
			}
			PrintUpdateResults(results)

		case "7", "q", "Q", "exit":
			fmt.Println()
			fmt.Println(colorGray + "  Goodbye." + colorReset)
			fmt.Println()
			return nil

		default:
			fmt.Printf("\n  %sInvalid choice %q — please enter 1–7.%s\n\n",
				colorYellow, choice, colorReset,
			)
		}
	}
}
