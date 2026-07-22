// Package desktop generates XDG .desktop launcher files for GUI Antigravity
// applications. The generated files comply with the freedesktop.org Desktop
// Entry Specification v1.5 and are placed in ~/.local/share/applications/
// so that the desktop environment registers them without any elevated privileges.
package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"

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
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("desktop: create %q: %w", destPath, err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("desktop: render %q: %w", destPath, err)
	}

	// Ensure the file is readable/writable by the user only.
	if err := os.Chmod(destPath, 0644); err != nil {
		return fmt.Errorf("desktop: chmod %q: %w", destPath, err)
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
