// Package updater handles the complete lifecycle of extracting, verifying,
// and installing Antigravity application releases. Download logic lives in
// downloader.go; this file owns SHA-512 validation, app-type-aware extraction,
// post-install hooks, and the public Update / UpdateAll API.
// All operations express outcomes as result.Result[T] and use GopherCore
// async.Map for bounded parallel execution.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
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

	// ResolvedURL is the direct download URL that was used.
	ResolvedURL string

	// SHA512Verified is true when the downloaded file's hash matched the manifest.
	SHA512Verified bool

	// Success is true when the update completed without errors.
	Success bool

	// Skipped is true when the remote version matches the local version and
	// --force was not passed.
	Skipped bool

	// Error holds the first error encountered, nil on success.
	Error error
}

// verifySHA512 computes the SHA-512 hash of the file at path and compares it
// against expected (hex string). Returns nil if they match or if expected is empty.
func verifySHA512(path, expected string) error {
	if expected == "" {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("sha512: open %q: %w", path, err)
	}
	defer f.Close()

	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("sha512: hash %q: %w", path, err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf(
			"sha512: checksum mismatch for %q\n  expected: %s\n  actual:   %s",
			path, expected, actual,
		)
	}

	slog.Info("updater: sha512 verified", "path", path)
	return nil
}

// extractCLI extracts only the binary named spec.TarballInnerName from the tarball
// and writes it to ~/.local/bin/<spec.BinaryName> with 0755 permissions.
// This handles the CLI quirk where the tarball contains "antigravity" but must
// be installed as "agy".
func extractCLI(tarGzPath string, spec config.AppSpec) error {
	binDir, err := xdg.BinDir()
	if err != nil {
		return fmt.Errorf("updater: resolve bin dir: %w", err)
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

	innerName := spec.TarballInnerName
	if innerName == "" {
		innerName = spec.BinaryName
	}

	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("updater: read tar entry: %w", err)
		}

		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// Match the entry whose base filename equals TarballInnerName.
		cleanName := filepath.Clean(guard.SanitizeString(hdr.Name))
		if strings.HasPrefix(cleanName, "..") {
			slog.Warn("updater: skipping potentially unsafe tar entry", "name", hdr.Name)
			continue
		}

		if filepath.Base(cleanName) != innerName {
			continue
		}

		// Found the target binary — write it to ~/.local/bin/<BinaryName>.
		destPath := filepath.Join(binDir, spec.BinaryName)
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

		if err := os.Chmod(destPath, 0755); err != nil {
			return fmt.Errorf("updater: chmod %q: %w", destPath, err)
		}

		slog.Info("updater: cli binary installed",
			"tarball_name", innerName,
			"installed_as", spec.BinaryName,
			"path", destPath,
		)
		found = true
		break
	}

	if !found {
		return fmt.Errorf("updater: binary %q not found in tarball %q", innerName, tarGzPath)
	}
	return nil
}

// extractAndInstall extracts a .tar.gz archive and installs all files to the
// appropriate XDG directories for GUI apps (IDE and Hub).
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

// runPostInstallHook executes `agy install` after placing the CLI binary.
// Errors are logged as warnings but do not fail the overall update — the
// binary is already correctly installed at this point.
func runPostInstallHook(ctx context.Context, spec config.AppSpec) {
	if spec.ID != "agy" {
		return
	}

	slog.Info("updater: running post-install hook", "app_id", spec.ID, "cmd", "agy install")

	// Resolve the full path to agy from the user's bin dir.
	binDir, err := xdg.BinDir()
	if err != nil {
		slog.Warn("updater: post-install hook: could not resolve bin dir", "error", err)
		return
	}
	agyPath := filepath.Join(binDir, "agy")

	cmd := exec.CommandContext(ctx, agyPath, "install")
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		slog.Info("updater: post-install hook output", "output", string(out))
	}
	if err != nil {
		slog.Warn("updater: post-install hook returned error (non-fatal)",
			"app_id", spec.ID,
			"error", err,
		)
		return
	}
	slog.Info("updater: post-install hook completed successfully", "app_id", spec.ID)
}

// Update performs the complete update lifecycle for a single AppSpec:
//  1. Check the remote version (with retries).
//  2. Skip if already up-to-date (unless --force).
//  3. Download the release tarball via DownloadTarGz.
//  4. Verify SHA-512 checksum (if provided by manifest).
//  5. Extract and install using app-type-aware logic.
//  6. Generate a .desktop launcher for GUI apps.
//  7. Run post-install hook for CLI (agy install).
//  8. Persist updated version to the local manifest.
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
	summary.ResolvedURL = cr.ResolvedURL

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
		"download_url", cr.ResolvedURL,
		"force", force,
	)

	// --- Step 3: Download tarball ---
	tarGzPath, err := DownloadTarGz(ctx, cr.ResolvedURL, spec.ID, maxRetries)
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

	// --- Step 4: SHA-512 security validation ---
	if cr.ExpectedSHA512 != "" {
		slog.Info("updater: verifying sha512 checksum", "app_id", spec.ID)
		if err := verifySHA512(tarGzPath, cr.ExpectedSHA512); err != nil {
			summary.Error = fmt.Errorf("updater: integrity check failed for %s: %w", spec.ID, err)
			slog.Error("updater: sha512 mismatch — aborting installation",
				"app_id", spec.ID,
				"error", err,
			)
			return result.Err[AppUpdateSummary](summary.Error)
		}
		summary.SHA512Verified = true
	}

	// --- Step 5: Extract and install (app-type-aware) ---
	var installErr error
	if spec.TarballInnerName != "" {
		// CLI path: extract only the named binary, rename to BinaryName.
		installErr = extractCLI(tarGzPath, spec)
	} else {
		// IDE / Hub path: generic multi-file extraction.
		installErr = extractAndInstall(tarGzPath, spec)
	}

	if installErr != nil {
		summary.Error = fmt.Errorf("updater: install failed for %s: %w", spec.ID, installErr)
		slog.Error("updater: install failed",
			"app_id", spec.ID,
			"error", installErr,
		)
		return result.Err[AppUpdateSummary](summary.Error)
	}

	// --- Step 6: Generate .desktop launcher for GUI apps ---
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

	// --- Step 7: Post-install hook (CLI only: runs `agy install`) ---
	runPostInstallHook(ctx, spec)

	// --- Step 8: Persist manifest ---
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
		"sha512_verified", summary.SHA512Verified,
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
