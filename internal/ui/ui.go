// Package ui renders the interactive terminal menu for ag-up and formats all
// output tables and progress messages. It uses only the standard library
// (bufio, fmt, strings) to keep the dependency footprint minimal.
package ui

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/gabrielima7/GopherCore/result"
	"github.com/gabrielima7/ag-up/internal/checker"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
	"github.com/gabrielima7/ag-up/internal/printer"
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
	printer.Println()
	printer.Println(colorCyan + colorBold + "  ╔═══════════════════════════════════════════════╗" + colorReset)
	printer.Println(colorCyan + colorBold + "  ║       ag-up · Google Antigravity Updater      ║" + colorReset)
	printer.Printf(colorCyan+colorBold+"  ║              Universal Updater %-15s║\n"+colorReset, verStr+" ")
	printer.Println(colorCyan + colorBold + "  ╚═══════════════════════════════════════════════╝" + colorReset)
	printer.Println()
}

// menu prints the interactive menu options.
func menu() {
	printer.Println(colorBold + "  Choose an option:" + colorReset)
	printer.Println()
	printer.Println("    " + colorCyan + "1)" + colorReset + " Check for updates          (dry-run)")
	printer.Println("    " + colorCyan + "2)" + colorReset + " Update All                 (parallel)")
	printer.Println("    " + colorCyan + "3)" + colorReset + " Update Antigravity CLI     (agy)")
	printer.Println("    " + colorCyan + "4)" + colorReset + " Update Antigravity IDE")
	printer.Println("    " + colorCyan + "5)" + colorReset + " Update Antigravity Hub     (2.0)")
	printer.Println("    " + colorCyan + "6)" + colorReset + " Exit")
	printer.Println()
	printer.Print(colorBold + "  → " + colorReset)
}

// interactiveReader manages a background goroutine for reading lines without leaking.
type interactiveReader struct {
	reader *bufio.Reader
	reqCh  chan chan string
}

func newInteractiveReader(ctx context.Context, r *bufio.Reader) *interactiveReader {
	ir := &interactiveReader{
		reader: r,
		reqCh:  make(chan chan string),
	}

	// Start a continuous reader loop in the background.
	// It reads from the underlying reader and waits for a request.
	// If a request comes in, it hands the line off and starts reading again.
	// We MUST NOT leak goroutines, so it listens to ctx.Done().
	go func() {
		// Dedicated goroutine for bufio.ReadString blocking call
		readDone := make(chan string)
		go func() {
			defer close(readDone)
			for {
				line, err := ir.reader.ReadString('\n')
				// Wait for the dispatcher to be ready to accept, or cancellation
				select {
				case <-ctx.Done():
					return
				case readDone <- line:
				}
				if err != nil {
					return
				}
			}
		}()

		eof := false
		for {
			select {
			case <-ctx.Done():
				return
			case replyCh := <-ir.reqCh:
				if eof {
					select {
					case replyCh <- "":
					default:
					}
					close(replyCh)
					continue
				}
				// We have a request. Now wait for a line from the reader.
				select {
				case <-ctx.Done():
					close(replyCh)
					return
				case line, ok := <-readDone:
					if !ok {
						eof = true
						select {
						case replyCh <- "":
						default:
						}
					} else {
						select {
						case replyCh <- line:
						default:
						}
					}
					close(replyCh)
				}
			}
		}
	}()
	return ir
}

// Close gracefully terminates the background goroutine.
func (ir *interactiveReader) Close() {
	// Let the context handle cancellation. No need to close reqCh to avoid panics on concurrent Close/readLine.
}

// readLine reads a single line from reader, aggressively trimming all leading
// and trailing whitespace including \r\n (important for TTY and piped input).
// Returns an empty string on EOF, read error, or context cancellation.
func (ir *interactiveReader) readLine(ctx context.Context) string {
	replyCh := make(chan string, 1)

	select {
	case <-ctx.Done():
		return ""
	case ir.reqCh <- replyCh:
	}

	select {
	case <-ctx.Done():
		return ""
	case line, ok := <-replyCh:
		if !ok {
			return ""
		}
		return strings.TrimSpace(line)
	}
}

// pressEnterToContinue blocks until the user presses Enter, preventing the
// menu loop from instantly redrawing and pushing result tables off-screen.
// It accepts the context to avoid blocking forever if a shutdown signal is received.
func (ir *interactiveReader) pressEnterToContinue(ctx context.Context) {
	printer.Print("\nPress [Enter] to return to the menu...")

	replyCh := make(chan string, 1)

	select {
	case <-ctx.Done():
		return
	case ir.reqCh <- replyCh:
	}

	select {
	case <-ctx.Done():
	case <-replyCh:
	}
}

// PrintCheckResults renders a formatted table of version check results.
func PrintCheckResults(results []result.Result[checker.CheckResult]) {
	printer.Println()
	printer.Println(colorBold + "  ┌─────────────────────────────────────────────────────────────┐" + colorReset)
	printer.Println(colorBold + "  │                    VERSION CHECK REPORT                     │" + colorReset)
	printer.Println(colorBold + "  └─────────────────────────────────────────────────────────────┘" + colorReset)
	printer.Println()
	printer.Printf("  %-28s %-14s %-14s %s\n",
		colorBold+"Application"+colorReset,
		colorBold+"Installed"+colorReset,
		colorBold+"Latest"+colorReset,
		colorBold+"Status"+colorReset,
	)
	printer.Println("  " + strings.Repeat("─", 65))

	for _, r := range results {
		if r.IsErr() {
			printer.Printf("  %-28s %-14s %-14s %s\n",
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

		printer.Printf("  %-28s %-14s %-14s %s\n",
			cr.AppName,
			localVer,
			cr.RemoteVersion,
			statusStr,
		)
	}
	printer.Println()
}

// PrintUpdateResults renders a formatted summary of update outcomes.
func PrintUpdateResults(results []result.Result[updater.AppUpdateSummary]) {
	printer.Println()
	printer.Println(colorBold + "  ┌─────────────────────────────────────────────────────────────┐" + colorReset)
	printer.Println(colorBold + "  │                      UPDATE REPORT                          │" + colorReset)
	printer.Println(colorBold + "  └─────────────────────────────────────────────────────────────┘" + colorReset)
	printer.Println()
	printer.Printf("  %-28s %-14s %-14s %s\n",
		colorBold+"Application"+colorReset,
		colorBold+"Before"+colorReset,
		colorBold+"After"+colorReset,
		colorBold+"Status"+colorReset,
	)
	printer.Println("  " + strings.Repeat("─", 65))

	successCount, skipCount, failCount := 0, 0, 0

	for _, r := range results {
		if r.IsErr() {
			failCount++
			printer.Printf("  %-28s %-14s %-14s %s\n",
				colorRed+"[error]"+colorReset, "—", "—",
				colorRed+"✗ failed"+colorReset,
			)
			printer.Printf("       %s%s%s\n", colorRed, r.Error().Error(), colorReset)
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
			printer.Printf("  %-28s %-14s %-14s %s\n",
				s.AppName, oldVer, s.NewVersion,
				colorGray+"↔ skipped (up-to-date)"+colorReset,
			)
		case s.Success:
			successCount++
			printer.Printf("  %-28s %-14s %-14s %s\n",
				s.AppName, oldVer, s.NewVersion,
				colorGreen+"✓ updated"+colorReset,
			)
		default:
			failCount++
			printer.Printf("  %-28s %-14s %-14s %s\n",
				s.AppName, oldVer, "—",
				colorRed+"✗ failed"+colorReset,
			)
			if s.Error != nil {
				printer.Printf("       %s%s%s\n", colorRed, s.Error.Error(), colorReset)
			}
		}
	}

	printer.Println()
	printer.Printf("  %sTotal:%s %d updated · %d skipped · %d failed\n",
		colorBold, colorReset, successCount, skipCount, failCount,
	)
	printer.Println()
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
	ir := newInteractiveReader(ctx, bufio.NewReader(os.Stdin))
	defer ir.Close()
	allSpecs := config.All()

	for {
		// Check for context cancellation at the top of each loop iteration so
		// that a Ctrl-C received while the menu is printing exits cleanly.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		banner(version)
		menu()

		choice := ir.readLine(ctx)

		// Check for context cancellation after blocking on readLine
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch choice {
		case "1":
			// Dry-run check for all apps.
			printer.Println()
			printer.Println(colorCyan + "  Checking versions (this may take a moment)..." + colorReset)
			results, err := checker.CheckAll(ctx, allSpecs, m, maxRetries)
			if err != nil {
				_, _ = printer.Fprintf(os.Stderr, "%s  Error: %v%s\n", colorRed, err, colorReset)
				continue
			}
			PrintCheckResults(results)
			ir.pressEnterToContinue(ctx)

		case "2":
			// Update all apps concurrently.
			printer.Println()
			printer.Println(colorCyan + "  Updating all applications..." + colorReset)
			results, err := updater.UpdateAll(ctx, allSpecs, m, maxRetries)
			if err != nil {
				_, _ = printer.Fprintf(os.Stderr, "%s  Error: %v%s\n", colorRed, err, colorReset)
				continue
			}
			PrintUpdateResults(results)
			ir.pressEnterToContinue(ctx)

		case "3":
			// Update only the CLI.
			printer.Println()
			printer.Println(colorCyan + "  Updating Antigravity CLI (agy)..." + colorReset)
			r := updater.Update(ctx, config.CLIApp, m, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})
			ir.pressEnterToContinue(ctx)

		case "4":
			// Update only the IDE.
			printer.Println()
			printer.Println(colorCyan + "  Updating Antigravity IDE..." + colorReset)
			r := updater.Update(ctx, config.IDEApp, m, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})
			ir.pressEnterToContinue(ctx)

		case "5":
			// Update only the Hub.
			printer.Println()
			printer.Println(colorCyan + "  Updating Antigravity Hub (2.0)..." + colorReset)
			r := updater.Update(ctx, config.HubApp, m, maxRetries)
			PrintUpdateResults([]result.Result[updater.AppUpdateSummary]{r})
			ir.pressEnterToContinue(ctx)

		case "6", "q", "Q", "exit":
			printer.Println()
			printer.Println(colorGray + "  Goodbye." + colorReset)
			printer.Println()
			return nil

		default:
			printer.Printf("\n  %sInvalid choice %q — please enter 1–6.%s\n\n",
				colorYellow, choice, colorReset,
			)
		}
	}
}
