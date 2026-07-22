// Package updater handles the complete lifecycle of extracting, verifying,
// and installing Antigravity application releases. Download logic lives in
// downloader.go; this file owns extract-and-install and the public Update /
// UpdateAll API. All operations express outcomes as result.Result[T] and use
// GopherCore async.Map for bounded parallel execution.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabrielima7/GopherCore/async"
	"github.com/gabrielima7/GopherCore/guard"
	"github.com/gabrielima7/GopherCore/result"
	"github.com/gabrielima7/ag-up/internal/checker"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/desktop"
	"github.com/gabrielima7/ag-up/internal/manifest"
	"github.com/gabrielima7/ag-up/pkg/xdg"
)

// Version is the current ag-up release version.
const Version = "v0.1.0"

// AppUpdateSummary is the outcome of a single application update attempt.
type AppUpdateSummary struct {
	// AppID matches config.AppSpec.ID.
	AppID string

	// AppName is the human-readable display name.
	AppName string

	// OldVersion is the previously installed version (empty if not installed).
	OldVersion string

	// NewVersion is the version installed by this run.
	NewVersion string

	// Success is true when the update completed without errors.
	Success bool

	// Skipped is true when the remote version matches the local version and
	// --force was not passed.
	Skipped bool

	// Error holds the first error encountered, nil on success.
	Error error
}

// extractAndInstall extracts a .tar.gz archive and installs the binary and
// any supporting files to the appropriate XDG directories.
func extractAndInstall(tarGzPath string, spec config.AppSpec) error {
	binDir, err := xdg.BinDir()
	if err != nil {
		return fmt.Errorf("updater: resolve bin dir: %w", err)
	}
	dataDir, err := xdg.DataDir(spec.ID)
	if err != nil {
		return fmt.Errorf("updater: resolve data dir: %w", err)
	}

	f, err := os.Open(tarGzPath)
	if err != nil {
		return fmt.Errorf("updater: open tarball %q: %w", tarGzPath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("updater: gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("updater: read tar entry: %w", err)
		}

		// Sanitise the path to prevent directory traversal attacks.
		cleanName := filepath.Clean(guard.SanitizeString(hdr.Name))
		if strings.HasPrefix(cleanName, "..") {
			slog.Warn("updater: skipping potentially unsafe tar entry", "name", hdr.Name)
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create sub-directories inside dataDir.
			dirPath := filepath.Join(dataDir, cleanName)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return fmt.Errorf("updater: mkdir %q: %w", dirPath, err)
			}

		case tar.TypeReg:
			baseName := filepath.Base(cleanName)
			var destPath string

			// The primary binary goes to ~/.local/bin/; everything else to dataDir.
			if baseName == spec.BinaryName {
				destPath = filepath.Join(binDir, baseName)
			} else {
				destPath = filepath.Join(dataDir, cleanName)
				// Ensure parent directory exists.
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return fmt.Errorf("updater: mkdir for %q: %w", destPath, err)
				}
			}

			outFile, err := os.Create(destPath)
			if err != nil {
				return fmt.Errorf("updater: create %q: %w", destPath, err)
			}

			// #nosec G110 — tarball size is capped by the download timeout.
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("updater: write %q: %w", destPath, err)
			}
			outFile.Close()

			// Apply execute permission to the main binary.
			if baseName == spec.BinaryName {
				if err := os.Chmod(destPath, 0755); err != nil {
					return fmt.Errorf("updater: chmod %q: %w", destPath, err)
				}
				slog.Info("updater: installed binary",
					"app_id", spec.ID,
					"path", destPath,
				)
			}
		}
	}

	return nil
}

// Update performs the complete update lifecycle for a single AppSpec:
//  1. Check the remote version (with retries).
//  2. Skip if already up-to-date (unless --force).
//  3. Download the release tarball via DownloadTarGz (with fallback URL support).
//  4. Extract and install to XDG paths.
//  5. Generate a .desktop launcher for GUI apps.
//  6. Persist updated version to the local manifest.
//
// Returns result.Result[AppUpdateSummary] — Ok on success, Err on failure.
func Update(
	ctx context.Context,
	spec config.AppSpec,
	m *manifest.Manifest,
	force bool,
	maxRetries int,
) result.Result[AppUpdateSummary] {
	localEntry, _ := manifest.Get(*m, spec.ID)

	summary := AppUpdateSummary{
		AppID:      spec.ID,
		AppName:    spec.Name,
		OldVersion: localEntry.InstalledVersion,
	}

	// --- Step 1: Remote version check ---
	checkResult := checker.Check(ctx, spec, *m, maxRetries)
	if checkResult.IsErr() {
		summary.Error = checkResult.Error()
		return result.Err[AppUpdateSummary](summary.Error)
	}

	cr, _ := checkResult.Unwrap()

	// Persist the fresh ETag even on a skip so future runs benefit from caching.
	_ = manifest.MarkChecked(m, spec.ID, cr.ETag)

	// --- Step 2: Skip if already up-to-date ---
	if !cr.NeedsUpdate && !force {
		slog.Info("updater: already up-to-date, skipping",
			"app_id", spec.ID,
			"version", cr.RemoteVersion,
		)
		summary.NewVersion = cr.RemoteVersion
		summary.Skipped = true
		summary.Success = true
		return result.Ok(summary)
	}

	slog.Info("updater: starting update",
		"app_id", spec.ID,
		"old_version", cr.LocalVersion,
		"new_version", cr.RemoteVersion,
		"download_url", cr.DownloadURL,
		"force", force,
	)

	// --- Step 3: Download tarball ---
	tarGzPath, err := DownloadTarGz(ctx, cr.DownloadURL, spec.ID, maxRetries)
	if err != nil && spec.FallbackURLTemplate != "" && ctx.Err() == nil {
		// Attempt fallback download URL if primary URL returned error
		fallbackURL := spec.FallbackURLTemplate
		if strings.Contains(fallbackURL, "{version}") {
			fallbackURL = strings.ReplaceAll(fallbackURL, "{version}", cr.RemoteVersion)
		}
		slog.Info("updater: primary download endpoint failed, trying fallback URL",
			"app_id", spec.ID,
			"primary_error", err,
			"fallback_url", fallbackURL,
		)
		tarGzPath, err = DownloadTarGz(ctx, fallbackURL, spec.ID, maxRetries)
	}

	if err != nil {
		if ctx.Err() != nil {
			summary.Error = fmt.Errorf("updater: download cancelled for %s", spec.ID)
		} else {
			summary.Error = fmt.Errorf("updater: download failed for %s: %w", spec.ID, err)
		}
		slog.Error("updater: download failed",
			"app_id", spec.ID,
			"error", err,
		)
		return result.Err[AppUpdateSummary](summary.Error)
	}
	defer os.Remove(tarGzPath)

	// --- Step 4: Extract and install ---
	if err := extractAndInstall(tarGzPath, spec); err != nil {
		summary.Error = fmt.Errorf("updater: install failed for %s: %w", spec.ID, err)
		slog.Error("updater: install failed",
			"app_id", spec.ID,
			"error", err,
		)
		return result.Err[AppUpdateSummary](summary.Error)
	}

	// --- Step 5: Generate .desktop launcher for GUI apps ---
	if spec.IsGUI {
		binDir, binErr := xdg.BinDir()
		if binErr == nil {
			binaryPath := filepath.Join(binDir, spec.BinaryName)
			if desktopErr := desktop.Generate(spec, binaryPath); desktopErr != nil {
				slog.Warn("updater: desktop file generation failed",
					"app_id", spec.ID,
					"error", desktopErr,
				)
			}
		}
	}

	// --- Step 6: Persist manifest ---
	if err := manifest.MarkInstalled(m, spec.ID, cr.RemoteVersion, cr.ETag); err != nil {
		slog.Warn("updater: manifest update failed",
			"app_id", spec.ID,
			"error", err,
		)
	}

	summary.NewVersion = cr.RemoteVersion
	summary.Success = true

	slog.Info("updater: update complete",
		"app_id", spec.ID,
		"version", cr.RemoteVersion,
	)

	return result.Ok(summary)
}

// UpdateAll concurrently updates all provided AppSpecs using async.Map.
func UpdateAll(
	ctx context.Context,
	specs []config.AppSpec,
	m *manifest.Manifest,
	force bool,
	maxRetries int,
) ([]result.Result[AppUpdateSummary], error) {
	results, err := async.Map(
		ctx,
		specs,
		3,
		func(ctx context.Context, spec config.AppSpec) (result.Result[AppUpdateSummary], error) {
			r := Update(ctx, spec, m, force, maxRetries)
			return r, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("updater: async.Map failed: %w", err)
	}
	return results, nil
}
