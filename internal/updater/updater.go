// Package updater handles the complete lifecycle of extracting, verifying,
// and installing Antigravity application releases. Download logic lives in
// downloader.go; this file owns SHA-512 validation, app-type-aware extraction,
// post-install hooks, and the public Update / UpdateAll API.
// All operations express outcomes as result.Result[T] and use GopherCore
// async.Map for bounded parallel execution.
package updater

import (
	"archive/tar"
	"time"
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

	// Skipped is retained for API compatibility but will never be true — every
	// invocation of Update performs a clean install regardless of local version.
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
		tmpDestPath := destPath + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())
		_ = os.Remove(tmpDestPath)
		outFile, err := os.Create(tmpDestPath)
		if err != nil {
			return fmt.Errorf("updater: create %q: %w", tmpDestPath, err)
		}

		// #nosec G110 — tarball size is capped by the download timeout.
		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
            _ = os.Remove(tmpDestPath)
			return fmt.Errorf("updater: write %q: %w", tmpDestPath, err)
		}
		outFile.Close()

		if err := os.Chmod(tmpDestPath, 0755); err != nil {
            _ = os.Remove(tmpDestPath)
			return fmt.Errorf("updater: chmod %q: %w", tmpDestPath, err)
		}

        if err := os.Rename(tmpDestPath, destPath); err != nil {
            _ = os.Remove(tmpDestPath)
            return fmt.Errorf("updater: rename %q to %q: %w", tmpDestPath, destPath, err)
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
//
// Before extraction the previous application directory is completely wiped via
// os.RemoveAll so that ghost files from upstream structural changes cannot
// accumulate across versions.
//
// Binary discovery uses a two-phase strategy:
//  1. Exact match: an executable named exactly "antigravity".
//  2. Loose match: an executable whose base name matches spec.ID (e.g.
//     "antigravity-ide"). Entries inside resources/ or locales/ subdirectories
//     are always excluded to prevent accidentally symlinking internal tools such
//     as the language_server.
//
// Returns the absolute path to the discovered main binary, which the caller
// uses to create the ~/.local/bin/<app-id> symlink. Returns an empty string if
// no suitable binary is found.
func extractAndInstall(tarGzPath string, spec config.AppSpec) (string, error) {
	binDir, err := xdg.BinDir()
	if err != nil {
		return "", fmt.Errorf("updater: resolve bin dir: %w", err)
	}
	dataDir, err := xdg.DataDir(spec.ID)
	if err != nil {
		return "", fmt.Errorf("updater: resolve data dir: %w", err)
	}

	// Extract to a temporary directory first.
	tmpDataDir := dataDir + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())
	_ = os.RemoveAll(tmpDataDir)
	if err := os.MkdirAll(tmpDataDir, 0755); err != nil {
		return "", fmt.Errorf("updater: create tmp install dir %q: %w", tmpDataDir, err)
	}

	// We will also accumulate bin files to atomic rename later
	type binRename struct {
		src string
		dst string
	}
	var binRenames []binRename

    // We defer cleanup of the tmp directory in case of failure.
    // If successful, we'll set a flag so it's not removed.
    success := false
    defer func() {
        if !success {
            os.RemoveAll(tmpDataDir)
            for _, r := range binRenames {
                os.Remove(r.src)
            }
        }
    }()

	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", fmt.Errorf("updater: open tarball %q: %w", tarGzPath, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("updater: gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// actualBinaryPath tracks the installed path of the main GUI executable
	// discovered via the exact "antigravity" name match (phase 1).
	var actualBinaryPath string

	// candidateBinaries accumulates all installed executables that are NOT
	// inside a resources/ or locales/ subdirectory. Used for the loose-match
	// fallback (phase 2) when no "antigravity" binary is found.
	var candidateBinaries []string

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("updater: read tar entry: %w", err)
		}

		// Sanitise the path to prevent directory traversal attacks.
		cleanName := filepath.Clean(guard.SanitizeString(hdr.Name))
		if strings.HasPrefix(cleanName, "..") {
			slog.Warn("updater: skipping potentially unsafe tar entry", "name", hdr.Name)
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			// Create sub-directories inside tmpDataDir.
			dirPath := filepath.Join(tmpDataDir, cleanName)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return "", fmt.Errorf("updater: mkdir %q: %w", dirPath, err)
			}

		case tar.TypeReg:
			baseName := filepath.Base(cleanName)
			var destPath string
            var targetPath string

			// The primary binary goes to ~/.local/bin/; everything else to tmpDataDir.
			if baseName == spec.BinaryName {
				targetPath = filepath.Join(binDir, baseName)
                destPath = targetPath + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())
                binRenames = append(binRenames, binRename{src: destPath, dst: targetPath})
			} else {
				destPath = filepath.Join(tmpDataDir, cleanName)
                targetPath = filepath.Join(dataDir, cleanName) // For actualBinaryPath later
				// Ensure parent directory exists.
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return "", fmt.Errorf("updater: mkdir for %q: %w", destPath, err)
				}
			}

			fileMode := hdr.FileInfo().Mode()
			_ = os.Remove(destPath)
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fileMode)
			if err != nil {
				return "", fmt.Errorf("updater: create %q: %w", destPath, err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", fmt.Errorf("updater: write %q: %w", destPath, err)
			}
			outFile.Close()

			if err := os.Chmod(destPath, fileMode); err != nil {
				return "", fmt.Errorf("updater: chmod %q: %w", destPath, err)
			}

			if baseName == spec.BinaryName {
				slog.Info("updater: installed binary",
					"app_id", spec.ID,
					"path", targetPath,
				)
			}

			if fileMode&0111 != 0 {
				// Phase 1: exact match — the Electron host binary used by both Hub and IDE.
				if baseName == "antigravity" && actualBinaryPath == "" {
					actualBinaryPath = targetPath
				}

				// Accumulate candidates for phase 2 (loose match), excluding
				// internal tool directories that must never be symlinked.
				slashPath := filepath.ToSlash(cleanName)
				if !strings.Contains(slashPath, "/resources/") &&
					!strings.Contains(slashPath, "/locales/") {
					candidateBinaries = append(candidateBinaries, targetPath)
				}
			}
		}
	}

	// Phase 2: loose match — triggered only when no "antigravity" binary was
	// found (e.g. the IDE package ships its main binary as "antigravity-ide").
	if actualBinaryPath == "" {
		for _, p := range candidateBinaries {
			if filepath.Base(p) == spec.ID {
				actualBinaryPath = p
				slog.Info("updater: main binary resolved via loose match",
					"app_id", spec.ID,
					"path", actualBinaryPath,
				)
				break
			}
		}
	}

	// --- Perform Atomic Replace ---
    slog.Info("updater: atomic replace of installation directory", "app_id", spec.ID, "path", dataDir)

    // Attempt to remove existing dir
    if err := os.RemoveAll(dataDir); err != nil {
        return "", fmt.Errorf("updater: failed to remove existing data dir %q: %w", dataDir, err)
    }

    if err := os.Rename(tmpDataDir, dataDir); err != nil {
        return "", fmt.Errorf("updater: failed to rename %q to %q: %w", tmpDataDir, dataDir, err)
    }

    for _, r := range binRenames {
        _ = os.Remove(r.dst)
        if err := os.Rename(r.src, r.dst); err != nil {
             return "", fmt.Errorf("updater: failed to rename %q to %q: %w", r.src, r.dst, err)
        }
    }

    success = true
	return actualBinaryPath, nil
}

// createGUISymlink atomically creates a symlink at ~/.local/bin/<app-id> pointing
// to actualBinaryPath. Any pre-existing file or old symlink at that location is
// removed first, making the operation idempotent and safe to run on every update.
func createGUISymlink(binDir, appID, actualBinaryPath string) {
	if actualBinaryPath == "" {
		slog.Warn("updater: main binary not found during extraction, skipping symlink",
			"app_id", appID,
		)
		return
	}

	symlinkPath := filepath.Join(binDir, appID)

	// Remove any existing stub, dummy, or outdated symlink — ignore "not found".
	_ = os.Remove(symlinkPath)

	if err := os.Symlink(actualBinaryPath, symlinkPath); err != nil {
		slog.Warn("updater: failed to create symlink",
			"app_id", appID,
			"symlink", symlinkPath,
			"target", actualBinaryPath,
			"error", err,
		)
		return
	}

	slog.Info("updater: symlink created",
		"app_id", appID,
		"symlink", symlinkPath,
		"target", actualBinaryPath,
	)
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
//  2. Download the release tarball via DownloadTarGz.
//  3. Verify SHA-512 checksum (if provided by manifest).
//  4. Extract and install using app-type-aware logic (always clean-wipes first).
//  5. Generate a .desktop launcher for GUI apps.
//  6. Run post-install hook for CLI (agy install).
//  7. Persist updated version to the local manifest.
//
// Every invocation performs a clean install — there is no "already up-to-date"
// skip. This guarantees file integrity on every run.
//
// Returns result.Result[AppUpdateSummary] — Ok on success, Err on failure.
func Update(
	ctx context.Context,
	spec config.AppSpec,
	m *manifest.Manifest,
	maxRetries int,
) result.Result[AppUpdateSummary] {
	localEntry, _ := manifest.Get(m, spec.ID)

	summary := AppUpdateSummary{
		AppID:      spec.ID,
		AppName:    spec.Name,
		OldVersion: localEntry.InstalledVersion,
	}

	// --- Step 1: Remote version check ---
	fmt.Printf("  [%s] Checking remote version...\n", spec.ID)
	checkResult := checker.Check(ctx, spec, m, maxRetries)
	if checkResult.IsErr() {
		summary.Error = checkResult.Error()
		return result.Err[AppUpdateSummary](summary.Error)
	}

	cr, _ := checkResult.Unwrap()
	summary.ResolvedURL = cr.ResolvedURL

	// Persist the fresh ETag so future check runs benefit from caching.
	_ = manifest.MarkChecked(m, spec.ID, cr.ETag)

	slog.Info("updater: starting update",
		"app_id", spec.ID,
		"old_version", cr.LocalVersion,
		"new_version", cr.RemoteVersion,
		"download_url", cr.ResolvedURL,
	)

	// --- Step 2: Download tarball ---
	fmt.Printf("  [%s] Downloading %s...\n", spec.ID, cr.RemoteVersion)
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

	// --- Step 3: SHA-512 security validation ---
	if cr.ExpectedSHA512 != "" {
		fmt.Printf("  [%s] Verifying integrity (SHA-512)...\n", spec.ID)
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

	// --- Step 4: Extract and install (app-type-aware) ---
	fmt.Printf("  [%s] Extracting...\n", spec.ID)
	var installErr error
	var actualBinaryPath string
	if spec.TarballInnerName != "" {
		// CLI path: extract only the named binary, rename to BinaryName.
		installErr = extractCLI(tarGzPath, spec)
	} else {
		// IDE / Hub path: generic multi-file extraction.
		// Returns the detected path to the main Electron binary for symlink creation.
		actualBinaryPath, installErr = extractAndInstall(tarGzPath, spec)
	}

	if installErr != nil {
		summary.Error = fmt.Errorf("updater: install failed for %s: %w", spec.ID, installErr)
		slog.Error("updater: install failed",
			"app_id", spec.ID,
			"error", installErr,
		)
		return result.Err[AppUpdateSummary](summary.Error)
	}

	// --- Step 5: Generate .desktop launcher and terminal symlink for GUI apps ---
	fmt.Printf("  [%s] Installing...\n", spec.ID)
	if spec.IsGUI {
		binDir, binErr := xdg.BinDir()
		if binErr == nil {
			// Create/replace ~/.local/bin/<app-id> → actual nested binary.
			// This makes `antigravity-hub` / `antigravity-ide` available on PATH
			// without any manual user intervention.
			createGUISymlink(binDir, spec.ID, actualBinaryPath)

			// The .desktop Exec= field points at the symlink for a clean launcher entry.
			symlinkPath := filepath.Join(binDir, spec.ID)
			if desktopErr := desktop.Generate(spec, symlinkPath); desktopErr != nil {
				slog.Warn("updater: desktop file generation failed",
					"app_id", spec.ID,
					"error", desktopErr,
				)
			}
		}
	}

	// --- Step 6: Post-install hook (CLI only: runs `agy install`) ---
	runPostInstallHook(ctx, spec)

	// --- Step 7: Persist manifest ---
	if err := manifest.MarkInstalled(m, spec.ID, cr.RemoteVersion, cr.ETag); err != nil {
		slog.Warn("updater: manifest update failed",
			"app_id", spec.ID,
			"error", err,
		)
	}

	summary.NewVersion = cr.RemoteVersion
	summary.Success = true

	fmt.Printf("  [%s] ✓ Done (%s)\n", spec.ID, cr.RemoteVersion)

	slog.Info("updater: update complete",
		"app_id", spec.ID,
		"version", cr.RemoteVersion,
		"sha512_verified", summary.SHA512Verified,
	)

	return result.Ok(summary)
}

// UpdateAll concurrently updates all provided AppSpecs using async.Map.
// Every spec receives a clean install — no version-match skipping occurs.
func UpdateAll(
	ctx context.Context,
	specs []config.AppSpec,
	m *manifest.Manifest,
	maxRetries int,
) ([]result.Result[AppUpdateSummary], error) {
	results, err := async.Map(
		ctx,
		specs,
		3,
		func(ctx context.Context, spec config.AppSpec) (result.Result[AppUpdateSummary], error) {
			r := Update(ctx, spec, m, maxRetries)
			return r, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("updater: async.Map failed: %w", err)
	}
	return results, nil
}
