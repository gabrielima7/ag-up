// Package manifest manages the local version metadata file used by ag-up to
// track installed versions, ETags, and last-checked timestamps for each
// Antigravity application. All I/O is performed via GopherCore's jsonutil
// package for fast, consistent JSON encoding/decoding.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gabrielima7/GopherCore/jsonutil"
	"github.com/gabrielima7/ag-up/pkg/xdg"
)

// AppEntry holds version metadata for a single installed application.
type AppEntry struct {
	// InstalledVersion is the version tag currently on disk (e.g., "v1.2.3").
	InstalledVersion string `json:"installed_version"`

	// ETag is the HTTP ETag from the last successful release API response.
	// Used for lightweight change detection without full version parsing.
	ETag string `json:"etag"`

	// LastChecked is the timestamp of the most recent remote version check.
	LastChecked time.Time `json:"last_checked"`

	// LastUpdated is the timestamp of the most recent successful install.
	LastUpdated time.Time `json:"last_updated"`
}

// Manifest is the top-level structure serialised to .manifest.json.
type Manifest struct {
	mu sync.RWMutex

	// Apps maps each application ID to its local metadata.
	Apps map[string]AppEntry `json:"apps"`

	// UpdatedAt records the last time the manifest file was written.
	UpdatedAt time.Time `json:"updated_at"`
}

// newManifest returns an empty, initialised Manifest.
func newManifest() *Manifest {
	return &Manifest{
		Apps:      make(map[string]AppEntry),
		UpdatedAt: time.Now(),
	}
}

// Load reads the manifest file from disk. If the file does not exist yet a
// fresh, empty Manifest is returned without error — this is the expected
// behaviour on first run.
func Load() (*Manifest, error) {
	path, err := xdg.ManifestPath()
	if err != nil {
		return newManifest(), fmt.Errorf("manifest: resolve path: %w", err)
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// First run — return an empty manifest, not an error.
		return newManifest(), nil
	}
	if err != nil {
		return newManifest(), fmt.Errorf("manifest: read %q: %w", path, err)
	}

	m := newManifest()
	if err := jsonutil.Unmarshal(data, m); err != nil {
		return newManifest(), fmt.Errorf("manifest: parse %q: %w", path, err)
	}

	// Guard against a nil map (e.g., JSON with "apps": null).
	if m.Apps == nil {
		m.Apps = make(map[string]AppEntry)
	}

	return m, nil
}

// Save serialises the manifest to disk, atomically replacing the previous
// version via a write-and-rename strategy to prevent partial writes.
func Save(m *Manifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return saveLocked(m)
}

func saveLocked(m *Manifest) error {
	path, err := xdg.ManifestPath()
	if err != nil {
		return fmt.Errorf("manifest: resolve path: %w", err)
	}

	m.UpdatedAt = time.Now()
	data, err := jsonutil.Marshal(m)
	if err != nil {
		return fmt.Errorf("manifest: marshal: %w", err)
	}

	// Write to a uniquely-named temporary file first, then rename — atomic on Linux.
	// os.CreateTemp prevents predictable filename collisions if multiple ag-up processes run concurrently.
	dir, file := filepath.Split(path)
	tmpFile, err := os.CreateTemp(dir, file+".tmp.*")
	if err != nil {
		return fmt.Errorf("manifest: create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmpFile.Chmod(0644); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("manifest: chmod temp file %q: %w", tmpPath, err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("manifest: write temp file %q: %w", tmpPath, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("manifest: close temp file %q: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("manifest: rename to %q: %w", path, err)
	}

	return nil
}

// Get returns the AppEntry for the given appID, plus a boolean indicating
// whether a record exists (analogous to a Go map lookup).
func Get(m *Manifest, appID string) (AppEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.Apps[appID]
	return entry, ok
}

// Set updates the in-memory AppEntry for appID and persists the manifest to
// disk immediately. Returns an error if the save fails.
func Set(m *Manifest, appID string, entry AppEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Apps[appID] = entry
	return saveLocked(m)
}

// MarkChecked updates the LastChecked timestamp and ETag for appID without
// modifying the InstalledVersion, then saves the manifest.
func MarkChecked(m *Manifest, appID, etag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.Apps[appID]
	entry.LastChecked = time.Now()
	if etag != "" {
		entry.ETag = etag
	}
	m.Apps[appID] = entry
	return saveLocked(m)
}

// MarkInstalled records a successful installation of version for appID,
// updating both InstalledVersion and LastUpdated.
func MarkInstalled(m *Manifest, appID, version, etag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.Apps[appID]
	entry.InstalledVersion = version
	entry.ETag = etag
	entry.LastChecked = time.Now()
	entry.LastUpdated = time.Now()
	m.Apps[appID] = entry
	return saveLocked(m)
}
