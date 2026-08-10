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
	"sync"
	"time"

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

// printMu synchronizes stdout access to prevent interleaved printing during parallel updates.
var printMu sync.Mutex

func safePrint(format string, a ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Printf(format, a...)
}

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
	defer func() { _ = f.Close() }()

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
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("updater: gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

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

		// Wrapping file extraction logic in a closure allows safe defer for os.Remove
		err = func() error {
			// Write to a uniquely-named temporary file first for atomic replacement.
			// Incorporate os.Getpid() to prevent cross-process collisions.
			tmpPath := fmt.Sprintf("%s.tmp.%d.%d", destPath, os.Getpid(), time.Now().UnixNano())
			outFile, err := os.Create(tmpPath)
			if err != nil {
				return fmt.Errorf("updater: create temp file %q: %w", tmpPath, err)
			}
			defer func() { _ = os.Remove(tmpPath) }()

			// #nosec G110 — tarball size is capped by the download timeout.
			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("updater: write %q: %w", tmpPath, err)
			}
			_ = outFile.Close()

			if err := os.Chmod(tmpPath, 0755); err != nil {
				return fmt.Errorf("updater: chmod %q: %w", tmpPath, err)
			}

			// Atomically replace the destination file
			if err := os.Rename(tmpPath, destPath); err != nil {
				return fmt.Errorf("updater: rename to %q: %w", destPath, err)
			}
			return nil
		}()

		if err != nil {
			return err
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

	// Extract to a uniquely-named temporary directory first for atomic replacement.
	tmpDataDir := fmt.Sprintf("%s.tmp.%d.%d", dataDir, os.Getpid(), time.Now().UnixNano())
	if err := os.MkdirAll(tmpDataDir, 0750); err != nil {
		return "", fmt.Errorf("updater: create temp install dir %q: %w", tmpDataDir, err)
	}
	defer func() { _ = os.RemoveAll(tmpDataDir) }()

	f, err := os.Open(tarGzPath)
	if err != nil {
		return "", fmt.Errorf("updater: open tarball %q: %w", tarGzPath, err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("updater: gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

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
			if err := os.MkdirAll(dirPath, 0750); err != nil {
				return "", fmt.Errorf("updater: mkdir %q: %w", dirPath, err)
			}

		case tar.TypeReg:
			baseName := filepath.Base(cleanName)
			var destPath string

			// All files (including the main binary) are extracted to tmpDataDir.
			// createGUISymlink is responsible for pointing ~/.local/bin/<app-id>
			// at the binary inside dataDir — writing the binary directly to binDir
			// would cause createGUISymlink to create a self-referential symlink,
			// overwriting the real binary with a circular link.
			destPath = filepath.Join(tmpDataDir, cleanName)
			// Ensure parent directory exists.
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return "", fmt.Errorf("updater: mkdir for %q: %w", destPath, err)
			}

			// Preserve the exact file mode from the tar header.
			// os.Create hardcodes 0666 and silently drops execute bits on auxiliary
			// binaries (e.g. resources/bin/language_server). Using os.OpenFile with
			// the header's mode followed by an explicit os.Chmod bypasses umask too.
			fileMode := hdr.FileInfo().Mode()

			err = func() error {
				// Write to a uniquely-named temporary file first for atomic replacement.
				// This prevents returning ELOOP if destPath points to a dangling symlink,
				// and ensures a crash doesn't leave corrupted partial files.
				tmpPath := fmt.Sprintf("%s.tmp.%d.%d", destPath, os.Getpid(), time.Now().UnixNano())
				outFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fileMode)
				if err != nil {
					return fmt.Errorf("updater: create temp file %q: %w", tmpPath, err)
				}
				defer func() { _ = os.Remove(tmpPath) }()

				// #nosec G110 — tarball size is capped by the download timeout.
				if _, err := io.Copy(outFile, tr); err != nil {
					_ = outFile.Close()
					return fmt.Errorf("updater: write %q: %w", tmpPath, err)
				}
				_ = outFile.Close()

				// Explicit chmod after write — safety net against umask stripping +x.
				if err := os.Chmod(tmpPath, fileMode); err != nil {
					return fmt.Errorf("updater: chmod %q: %w", tmpPath, err)
				}

				// Atomically replace the destination file
				if err := os.Rename(tmpPath, destPath); err != nil {
					return fmt.Errorf("updater: rename to %q: %w", destPath, err)
				}
				return nil
			}()

			if err != nil {
				return "", err
			}

			if baseName == spec.BinaryName {
				slog.Info("updater: installed binary",
					"app_id", spec.ID,
					"path", destPath,
				)
			}

			if fileMode&0111 != 0 {
				// Phase 1: exact match — the Electron host binary used by both Hub and IDE.
				if baseName == "antigravity" && actualBinaryPath == "" {
					actualBinaryPath = destPath
				}

				// Accumulate candidates for phase 2 (loose match), excluding
				// internal tool directories that must never be symlinked.
				// Also exclude files that are inside binDir — those would create
				// a self-referential symlink if selected as the main binary.
				slashPath := filepath.ToSlash(cleanName)
				if !strings.Contains(slashPath, "/resources/") &&
					!strings.Contains(slashPath, "/locales/") &&
					!strings.HasPrefix(destPath, binDir+string(filepath.Separator)) {
					candidateBinaries = append(candidateBinaries, destPath)
				}
			}
		}
	}

	// Phase 2: loose match — triggered only when no "antigravity" binary was
	// found (e.g. the IDE package ships its main binary as "antigravity-ide").
	// We prefer entries NOT in a "bin/" subdirectory (those are usually wrapper
	// scripts) and fall back to them only if no top-level match exists.
	if actualBinaryPath == "" {
		var fallback string
		for _, p := range candidateBinaries {
			if filepath.Base(p) != spec.BinaryName {
				continue
			}
			// Prefer the top-level entry (not inside a "bin/" sub-directory).
			rel, _ := filepath.Rel(tmpDataDir, p)
			if !strings.HasPrefix(filepath.ToSlash(rel), "bin/") {
				actualBinaryPath = p
				slog.Info("updater: main binary resolved via loose match (top-level)",
					"app_id", spec.ID,
					"path", actualBinaryPath,
				)
				break
			}
			if fallback == "" {
				fallback = p
			}
		}
		if actualBinaryPath == "" && fallback != "" {
			actualBinaryPath = fallback
			slog.Info("updater: main binary resolved via loose match (fallback)",
				"app_id", spec.ID,
				"path", actualBinaryPath,
			)
		}
	}

	// Atomically replace dataDir with tmpDataDir
	_ = os.RemoveAll(dataDir)
	if err := os.Rename(tmpDataDir, dataDir); err != nil {
		return "", fmt.Errorf("updater: rename temp dir to %q: %w", dataDir, err)
	}

	// Adjust actualBinaryPath to point to the new location
	if actualBinaryPath != "" {
		if rel, err := filepath.Rel(tmpDataDir, actualBinaryPath); err == nil {
			actualBinaryPath = filepath.Join(dataDir, rel)
		}
	}

	return actualBinaryPath, nil
}

// createGUISymlink atomically creates a symlink at ~/.local/bin/<app-id> pointing
// to actualBinaryPath using a create-and-rename strategy.
func createGUISymlink(binDir, appID, actualBinaryPath string) {
	if actualBinaryPath == "" {
		slog.Warn("updater: main binary not found during extraction, skipping symlink",
			"app_id", appID,
		)
		return
	}

	symlinkPath := filepath.Join(binDir, appID)
	tmpSymlinkPath := fmt.Sprintf("%s.tmp.%d.%d", symlinkPath, os.Getpid(), time.Now().UnixNano())

	// Create temporary symlink
	if err := os.Symlink(actualBinaryPath, tmpSymlinkPath); err != nil {
		slog.Warn("updater: failed to create temporary symlink",
			"app_id", appID,
			"tmp_symlink", tmpSymlinkPath,
			"target", actualBinaryPath,
			"error", err,
		)
		return
	}
	defer func() { _ = os.Remove(tmpSymlinkPath) }() // Safe cleanup on panic/early return

	// Atomically rename it over the old one
	if err := os.Rename(tmpSymlinkPath, symlinkPath); err != nil {
		slog.Warn("updater: failed to replace symlink atomically",
			"app_id", appID,
			"symlink", symlinkPath,
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

	// Add timeout to prevent dangling goroutines if the post-install hook hangs
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// #nosec G204 — agyPath is constructed securely from XDG user bin directory.
	cmd := exec.CommandContext(timeoutCtx, agyPath, "install")
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
	// Apply a global timeout for the entire update operation for this app to prevent stalls
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	localEntry, _ := manifest.Get(m, spec.ID)

	summary := AppUpdateSummary{
		AppID:      spec.ID,
		AppName:    spec.Name,
		OldVersion: localEntry.InstalledVersion,
	}

	// --- Step 1: Remote version check ---
	safePrint("  [%s] Checking remote version...\n", spec.ID)
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
	safePrint("  [%s] Downloading %s...\n", spec.ID, cr.RemoteVersion)
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
	defer func() { _ = os.Remove(tarGzPath) }()

	// --- Step 3: SHA-512 security validation ---
	if cr.ExpectedSHA512 != "" {
		safePrint("  [%s] Verifying integrity (SHA-512)...\n", spec.ID)
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
	safePrint("  [%s] Extracting...\n", spec.ID)
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
	safePrint("  [%s] Installing...\n", spec.ID)
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

			// Update any legacy .desktop launchers (e.g. created by old official
			// installers in ~/Desktop or ~/.local/share/applications/) so they
			// also point to the symlink — using the symlink path guarantees no
			// spaces in the Exec= value, preventing shell-splitting issues in
			// desktop environments (XFCE, GNOME, KDE) that truncate unquoted paths.
			desktop.SyncLegacyLaunchers(spec, symlinkPath)
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

	safePrint("  [%s] ✓ Done (%s)\n", spec.ID, cr.RemoteVersion)

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
