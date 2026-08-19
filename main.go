// Command ag-up is the Google Antigravity Universal Updater (v0.1.0).
// It manages the installation and update of:
//   - Google Antigravity CLI (agy)
//   - Google Antigravity IDE
//   - Google Antigravity Hub (2.0)
//
// All operations run entirely in user-space following the XDG Base Directory
// Specification — no sudo required. The tool integrates GopherCore packages
// for resilient networking, structured logging, fast JSON I/O, and safe
// concurrent execution.
//
// Graceful shutdown: pressing Ctrl-C or sending SIGTERM cancels all active
// async.Map workers, cleans up temporary files, and exits with code 130.
//
// Usage:
//
//	ag-up                     # Interactive menu
//	ag-up --all               # Update all apps concurrently
//	ag-up --cli               # Update only the CLI (agy)
//	ag-up --ide               # Update only the IDE
//	ag-up --hub               # Update only the Hub
//	ag-up --check             # Dry-run version check (no downloads)
//	ag-up --retries 5         # Set HTTP retry attempts (default: 3)
//	ag-up --version           # Print ag-up version and exit
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gabrielima7/GopherCore/logkit"
	"github.com/gabrielima7/GopherCore/result"
	"github.com/gabrielima7/ag-up/internal/checker"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
	"github.com/gabrielima7/ag-up/internal/printer"
	"github.com/gabrielima7/ag-up/internal/ui"
	"github.com/gabrielima7/ag-up/internal/updater"
	"github.com/gabrielima7/ag-up/pkg/xdg"
)

// Version is the current release of ag-up.
// Override at build time: go build -ldflags "-X main.Version=v0.2.0" .
const Version = "v0.1.0"

func main() {
	// -----------------------------------------------------------------------
	// Flag definitions
	// -----------------------------------------------------------------------
	flagAll := flag.Bool("all", false, "Update all Antigravity applications concurrently")
	flagCLI := flag.Bool("cli", false, "Update only the Antigravity CLI (agy)")
	flagIDE := flag.Bool("ide", false, "Update only the Antigravity IDE")
	flagHub := flag.Bool("hub", false, "Update only the Antigravity Hub (2.0)")
	flagCheck := flag.Bool("check", false, "Dry-run: check versions without downloading")
	flagRetries := flag.Int("retries", 3, "Maximum HTTP retry attempts per operation (1–10)")
	flagVersion := flag.Bool("version", false, "Print ag-up version and exit")

	flag.Usage = func() {
		printer.Fprintf(os.Stderr, "\nUsage: ag-up [OPTIONS]\n\n")
		printer.Fprintf(os.Stderr, "Google Antigravity Universal Updater (ag-up) %s\n", Version)
		printer.Fprintf(os.Stderr, "Manages agy CLI, Antigravity IDE, and Antigravity Hub 2.0\n\n")
		printer.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		printer.Fprintf(os.Stderr, "\nExamples:\n")
		printer.Fprintf(os.Stderr, "  ag-up                    # Launch interactive menu\n")
		printer.Fprintf(os.Stderr, "  ag-up --check            # Check versions (no download)\n")
		printer.Fprintf(os.Stderr, "  ag-up --all              # Update everything concurrently\n")
		printer.Fprintf(os.Stderr, "  ag-up --cli              # Update only the CLI\n")
		printer.Fprintf(os.Stderr, "  ag-up --ide --retries 5  # Update IDE with 5 retries\n\n")
	}

	flag.Parse()

	// -----------------------------------------------------------------------
	// --version: print and exit immediately (before logger init).
	// -----------------------------------------------------------------------
	if *flagVersion {
		printer.Printf("ag-up version %s\n", Version)
		printer.Println("Google Antigravity Universal Updater")
		printer.Println("https://github.com/gabrielima7/ag-up")
		os.Exit(0)
	}

	// -----------------------------------------------------------------------
	// Initialise GopherCore structured logger (JSON output via log/slog).
	// -----------------------------------------------------------------------
	logkit.Initialize(
		logkit.WithLevel(slog.LevelWarn),
	)

	slog.Info("ag-up starting", "version", Version)

	// -----------------------------------------------------------------------
	// Root context with OS signal handling.
	//
	// signal.NotifyContext cancels ctx when os.Interrupt (Ctrl-C) or
	// syscall.SIGTERM is received. All downstream goroutines spawned by
	// async.Map and retry.DoWithValue receive this context and abort cleanly.
	// -----------------------------------------------------------------------
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop() // Release signal resources on normal exit.

	// -----------------------------------------------------------------------
	// Ensure all required XDG directories exist (idempotent).
	// -----------------------------------------------------------------------
	allAppIDs := make([]string, 0, len(config.All()))
	for _, app := range config.All() {
		allAppIDs = append(allAppIDs, app.ID)
	}

	if err := xdg.EnsureDirectories(allAppIDs); err != nil {
		printer.Fprintf(os.Stderr, "ag-up: failed to create XDG directories: %v\n", err)
		os.Exit(1)
	}

	// -----------------------------------------------------------------------
	// Load local manifest.
	// -----------------------------------------------------------------------
	m, err := manifest.Load()
	if err != nil {
		printer.Fprintf(os.Stderr, "ag-up: failed to load manifest: %v\n", err)
		os.Exit(1)
	}

	maxRetries := *flagRetries
	if maxRetries < 1 {
		maxRetries = 1
	}
	if maxRetries > 10 {
		maxRetries = 10
	}

	// -----------------------------------------------------------------------
	// Non-interactive flag mode.
	// -----------------------------------------------------------------------
	nonInteractive := *flagAll || *flagCLI || *flagIDE || *flagHub || *flagCheck

	if nonInteractive {
		code := runFlagMode(ctx, m, *flagAll, *flagCLI, *flagIDE, *flagHub, *flagCheck, maxRetries)
		// If the context was cancelled (signal received), override the exit code.
		if ctx.Err() != nil {
			handleCancellation()
		}
		os.Exit(code)
	}

	// -----------------------------------------------------------------------
	// Interactive terminal menu (default when no flags are provided).
	// -----------------------------------------------------------------------
	if err := ui.RunInteractiveMenu(ctx, m, maxRetries, Version); err != nil {
		// Distinguish user interruption from a real menu error.
		if ctx.Err() != nil {
			handleCancellation()
		}
		printer.Fprintf(os.Stderr, "ag-up: %v\n", err)
		os.Exit(1)
	}

	// Check once more after clean menu exit — user may have triggered Ctrl-C
	// while the menu was printing results.
	if ctx.Err() != nil {
		handleCancellation()
	}
}

// handleCancellation logs the cancellation warning, prints the user-facing
// message, and exits with code 130 (the POSIX convention for Ctrl-C / SIGINT).
func handleCancellation() {
	slog.Warn("ag-up: operation canceled by user, cleaning up temporary files...")
	printer.Fprintln(os.Stderr, "\n[!] Process interrupted. All temporary files cleaned up.")
	os.Exit(130)
}

// runFlagMode executes the appropriate operation based on the provided flags
// and returns an OS exit code (0 = success, 1 = at least one failure).
// It does NOT call os.Exit itself so the caller can inspect ctx.Err() first.
func runFlagMode(
	ctx context.Context,
	m *manifest.Manifest,
	all, cli, ide, hub, check bool,
	maxRetries int,
) int {
	exitCode := 0

	// --- Dry-run check mode ---
	if check {
		var specs []config.AppSpec
		switch {
		case all || (!cli && !ide && !hub):
			specs = config.All()
		default:
			specs = selectedSpecs(cli, ide, hub)
		}

		printer.Println("\n  Checking versions…")
		results, err := checker.CheckAll(ctx, specs, m, maxRetries)
		if err != nil {
			printer.Fprintf(os.Stderr, "ag-up: check failed: %v\n", err)
			return 1
		}
		ui.PrintCheckResults(results)

		for _, r := range results {
			if r.IsErr() {
				exitCode = 1
			}
		}
		return exitCode
	}

	// --- Update mode ---
	var specs []config.AppSpec
	switch {
	case all:
		specs = config.All()
	default:
		specs = selectedSpecs(cli, ide, hub)
	}

	printer.Printf("\n  Updating %d application(s)…\n", len(specs))

	var updateResults []result.Result[updater.AppUpdateSummary]

	if len(specs) == 1 {
		// Single update: call Update directly.
		r := updater.Update(ctx, specs[0], m, maxRetries)
		updateResults = []result.Result[updater.AppUpdateSummary]{r}
	} else {
		// Multiple: run concurrently via async.Map inside UpdateAll.
		var err error
		updateResults, err = updater.UpdateAll(ctx, specs, m, maxRetries)
		if err != nil {
			printer.Fprintf(os.Stderr, "ag-up: update failed: %v\n", err)
			return 1
		}
	}

	ui.PrintUpdateResults(updateResults)

	for _, r := range updateResults {
		if r.IsErr() {
			exitCode = 1
			continue
		}
		if s, _ := r.Unwrap(); !s.Success {
			exitCode = 1
		}
	}

	return exitCode
}

// selectedSpecs builds the list of AppSpecs matching the provided boolean flags.
func selectedSpecs(cli, ide, hub bool) []config.AppSpec {
	var specs []config.AppSpec
	if cli {
		specs = append(specs, config.CLIApp)
	}
	if ide {
		specs = append(specs, config.IDEApp)
	}
	if hub {
		specs = append(specs, config.HubApp)
	}
	return specs
}
