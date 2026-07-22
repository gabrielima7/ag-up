// Package xdg provides helpers for the XDG Base Directory Specification.
// All paths are resolved relative to the current user's home directory,
// ensuring 100% user-space operation — no root permissions required.
package xdg

import (
	"fmt"
	"os"
	"path/filepath"
)

const antigravityDir = "antigravity"

// homeDir returns the current user's home directory.
func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("xdg: cannot determine home directory: %w", err)
	}
	return home, nil
}

// BinDir returns the XDG user binary directory: ~/.local/bin/
func BinDir() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// DataDir returns the XDG app storage directory: ~/.local/share/antigravity/<appID>/
func DataDir(appID string) (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", antigravityDir, appID), nil
}

// ApplicationsDir returns the XDG desktop launchers directory: ~/.local/share/applications/
func ApplicationsDir() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "applications"), nil
}

// ManifestPath returns the path of the local version manifest file:
// ~/.local/share/antigravity/.manifest.json
func ManifestPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", antigravityDir, ".manifest.json"), nil
}

// EnsureDirectories creates all required XDG directories for ag-up if they
// do not already exist. It is safe to call multiple times (idempotent).
func EnsureDirectories(appIDs []string) error {
	binDir, err := BinDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("xdg: cannot create bin dir %q: %w", binDir, err)
	}

	appsDir, err := ApplicationsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(appsDir, 0755); err != nil {
		return fmt.Errorf("xdg: cannot create applications dir %q: %w", appsDir, err)
	}

	for _, id := range appIDs {
		dataDir, err := DataDir(id)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return fmt.Errorf("xdg: cannot create data dir %q: %w", dataDir, err)
		}
	}

	// Ensure parent manifest directory exists.
	manifestPath, err := ManifestPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("xdg: cannot create manifest parent dir: %w", err)
	}

	return nil
}
