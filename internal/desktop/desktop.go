// Package desktop generates XDG .desktop launcher files for GUI Antigravity
// applications. The generated files comply with the freedesktop.org Desktop
// Entry Specification v1.5 and are placed in ~/.local/share/applications/
// so that the desktop environment registers them without any elevated privileges.
package desktop

import (
	"bufio"
	"fmt"
	"log/slog"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/gabrielima7/GopherCore/guard"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/pkg/xdg"
)

// desktopTemplate is the XDG Desktop Entry template.
// All user-supplied values are sanitised before being injected.
const desktopTemplate = `[Desktop Entry]
Version=1.5
Type=Application
Name={{.Name}}
Comment={{.Comment}}
Exec={{.Exec}}
Icon={{.Icon}}
Terminal=false
Categories={{.Categories}}
StartupNotify=true
StartupWMClass={{.StartupWMClass}}
`

// desktopData is the data bag fed into desktopTemplate.
type desktopData struct {
	Name           string
	Comment        string
	Exec           string
	Icon           string
	Categories     string
	StartupWMClass string
}

// Generate writes a valid .desktop launcher file for the given GUI AppSpec.
// The file is placed at ~/.local/share/applications/<appID>.desktop.
// It is safe to call on non-GUI apps — this function returns immediately
// with nil if spec.IsGUI is false.
func Generate(spec config.AppSpec, binaryPath string) error {
	if !spec.IsGUI {
		return nil
	}

	appsDir, err := xdg.ApplicationsDir()
	if err != nil {
		return fmt.Errorf("desktop: resolve applications dir: %w", err)
	}

	// Sanitise all string fields before rendering.
	data := desktopData{
		Name:           guard.SanitizeString(spec.Name),
		Comment:        guard.SanitizeString(spec.Comment),
		Exec:           guard.SanitizeString(binaryPath),
		Icon:           guard.SanitizeString(spec.DesktopIcon),
		Categories:     guard.SanitizeString(spec.Categories),
		StartupWMClass: guard.SanitizeString(spec.StartupWMClass),
	}

	tmpl, err := template.New("desktop").Parse(desktopTemplate)
	if err != nil {
		// Template parsing is deterministic; an error here is a bug.
		return fmt.Errorf("desktop: parse template: %w", err)
	}

	destPath := filepath.Join(appsDir, guard.SanitizeString(spec.ID)+".desktop")

	dir, file := filepath.Split(destPath)
	tmpPath := fmt.Sprintf("%s.tmp.%d.%d", filepath.Join(dir, file), os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("desktop: create temp file in %q: %w", dir, err)
	}
	defer func() { _ = os.Remove(filepath.Clean(tmpPath)) }()

	// Ensure the file is readable by the desktop environment (0644).
	// #nosec G302
	if err := f.Chmod(0644); err != nil {
		_ = f.Close()
		return fmt.Errorf("desktop: chmod %q: %w", tmpPath, err)
	}

	if err := tmpl.Execute(f, data); err != nil {
		_ = f.Close()
		return fmt.Errorf("desktop: render %q: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("desktop: close temp file %q: %w", tmpPath, err)
	}

	// Atomically replace the destination file
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("desktop: rename to %q: %w", destPath, err)
	}

	return nil
}

// Remove deletes the .desktop launcher for the given app ID, if it exists.
// It is safe to call even when the file does not exist (idempotent).
func Remove(appID string) error {
	appsDir, err := xdg.ApplicationsDir()
	if err != nil {
		return fmt.Errorf("desktop: resolve applications dir: %w", err)
	}

	destPath := filepath.Join(appsDir, guard.SanitizeString(appID)+".desktop")
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("desktop: remove %q: %w", destPath, err)
	}
	return nil
}

// SyncLegacyLaunchers finds .desktop files in well-known locations that still
// point to old, unmanaged installations of the given app and updates their
// Exec= line to point at newBinaryPath.
//
// Search locations:
//   - ~/.local/share/applications/<any>.desktop
//   - ~/Desktop/*.desktop  (and localised equivalents under XDG_DESKTOP_DIR)
//
// A file is considered a legacy launcher if:
//  1. It contains the app name (spec.Name) or any known legacy Exec= path.
//  2. Its Exec= line does NOT already point at newBinaryPath.
//
// Errors are logged as warnings and do not abort the update.
func SyncLegacyLaunchers(spec config.AppSpec, newBinaryPath string) {
	if !spec.IsGUI || newBinaryPath == "" {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("desktop: sync legacy: cannot determine home dir", "error", err)
		return
	}

	appsDir, err := xdg.ApplicationsDir()
	if err != nil {
		slog.Warn("desktop: sync legacy: cannot resolve applications dir", "error", err)
		return
	}

	// Build the list of directories to scan for legacy .desktop files.
	scanDirs := []string{appsDir}
	// Add common desktop directory candidates (localised and standard).
	for _, candidate := range []string{
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Área de trabalho"),
		filepath.Join(home, "Área de Trabalho"),
		filepath.Join(home, "Bureau"),       // French
		filepath.Join(home, "Escritorio"),   // Spanish
		filepath.Join(home, "Schreibtisch"), // German
	} {
		scanDirs = append(scanDirs, candidate)
	}

	// Also check XDG_DESKTOP_DIR if set.
	if xdgDesktop := os.Getenv("XDG_DESKTOP_DIR"); xdgDesktop != "" {
		scanDirs = append(scanDirs, xdgDesktop)
	}

	for _, dir := range scanDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if updated := maybeUpdateDesktopExec(path, spec, newBinaryPath); updated {
				slog.Info("desktop: legacy launcher updated",
					"app_id", spec.ID,
					"file", path,
					"new_exec", newBinaryPath,
				)
			}
		}
	}
}

// maybeUpdateDesktopExec reads a single .desktop file, checks if it is a
// legacy launcher for spec, and rewrites the Exec= line if needed.
// Returns true if the file was modified.
func maybeUpdateDesktopExec(path string, spec config.AppSpec, newBinaryPath string) bool {
	path = filepath.Clean(path)

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return false
	}
	data, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return false
	}
	content := string(data)

	// Quick pre-check: the file must mention the app in some way.
	appHint := strings.ToLower(spec.ID)
	if !strings.Contains(strings.ToLower(content), appHint) &&
		!strings.Contains(strings.ToLower(content), strings.ToLower(spec.Name)) {
		return false
	}

	// Parse existing Exec= value.
	var currentExec string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Exec=") {
			currentExec = strings.TrimPrefix(line, "Exec=")
			break
		}
	}

	// No Exec= found, or already points to the correct binary.
	if currentExec == "" || currentExec == newBinaryPath {
		return false
	}

	// Skip desktop files that are already managed by ag-up (pointing to our
	// managed paths) — avoid double-updating.
	managedDir := filepath.Join(func() string { h, _ := os.UserHomeDir(); return h }(),
		".local", "share", "antigravity")
	if strings.HasPrefix(currentExec, managedDir) {
		return false
	}

	// Rewrite the Exec= line.
	var newLines []string
	scanner = bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Exec=") {
			newLines = append(newLines, "Exec="+newBinaryPath)
		} else {
			newLines = append(newLines, line)
		}
	}
	newContent := strings.Join(newLines, "\n")
	// Preserve trailing newline if original had one.
	if strings.HasSuffix(content, "\n") && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}

	// Atomic write.
	dir, base := filepath.Split(path)
	tmpPath := fmt.Sprintf("%s.tmp.%d.%d", filepath.Join(dir, base), os.Getpid(), time.Now().UnixNano())
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode())
	if err != nil {
		slog.Warn("desktop: sync legacy: cannot create temp file", "path", path, "error", err)
		return false
	}
	// #nosec G703
	defer func() { _ = os.Remove(filepath.Clean(tmpPath)) }()

	// Preserve original permissions.
	if err := tmp.Chmod(info.Mode()); err != nil {
		_ = tmp.Close()
		slog.Warn("desktop: sync legacy: chmod failed", "path", tmpPath, "error", err)
		return false
	}

	if _, err := tmp.WriteString(newContent); err != nil {
		_ = tmp.Close()
		slog.Warn("desktop: sync legacy: write failed", "path", tmpPath, "error", err)
		return false
	}
	if err := tmp.Close(); err != nil {
		slog.Warn("desktop: sync legacy: close failed", "path", tmpPath, "error", err)
		return false
	}

	// #nosec G703
	if err := os.Rename(tmpPath, path); err != nil {
		slog.Warn("desktop: sync legacy: rename failed", "path", path, "error", err)
		return false
	}

	return true
}
