package manifest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielima7/ag-up/pkg/xdg"
)

func setTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

func TestLoad_NotExist(t *testing.T) {
	setTempHome(t)

	m, err := Load()
	if err != nil {
		t.Fatalf("Load() on missing manifest should not error, got: %v", err)
	}
	if m == nil {
		t.Fatal("Load() returned nil manifest")
	}
	if len(m.Apps) != 0 {
		t.Fatalf("expected 0 apps, got %d", len(m.Apps))
	}
}

func TestLoad_EmptyFile(t *testing.T) {
	setTempHome(t)

	path, err := xdg.ManifestPath()
	if err != nil {
		t.Fatalf("xdg.ManifestPath() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write empty manifest: %v", err)
	}

	m, err := Load()
	if err != nil {
		t.Fatalf("Load() on 0-byte manifest should self-heal without error, got: %v", err)
	}
	if m == nil || len(m.Apps) != 0 {
		t.Fatalf("expected clean empty manifest, got: %+v", m)
	}
}

func TestLoad_CorruptedJSON(t *testing.T) {
	setTempHome(t)

	path, err := xdg.ManifestPath()
	if err != nil {
		t.Fatalf("xdg.ManifestPath() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"apps": { invalid json `), 0644); err != nil {
		t.Fatalf("failed to write corrupt manifest: %v", err)
	}

	m, err := Load()
	if err != nil {
		t.Fatalf("Load() on corrupt manifest should self-heal without error, got: %v", err)
	}
	if m == nil || len(m.Apps) != 0 {
		t.Fatalf("expected clean manifest after corrupt file recovery, got: %+v", m)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	setTempHome(t)

	path, err := xdg.ManifestPath()
	if err != nil {
		t.Fatalf("xdg.ManifestPath() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create manifest dir: %v", err)
	}

	m := newManifest()
	appID := "agy"
	entry := AppEntry{
		InstalledVersion: "v1.2.3",
		ETag:             "\"etag-abc\"",
		LastChecked:      time.Now().Truncate(time.Second),
		LastUpdated:      time.Now().Truncate(time.Second),
	}

	if err := Set(m, appID, entry); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	gotEntry, ok := Get(m, appID)
	if !ok {
		t.Fatalf("Get() returned false for %q", appID)
	}
	if gotEntry.InstalledVersion != entry.InstalledVersion {
		t.Fatalf("expected version %q, got %q", entry.InstalledVersion, gotEntry.InstalledVersion)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	loadedEntry, ok := Get(loaded, appID)
	if !ok {
		t.Fatalf("Get() on loaded manifest returned false for %q", appID)
	}
	if loadedEntry.InstalledVersion != entry.InstalledVersion {
		t.Fatalf("expected loaded version %q, got %q", entry.InstalledVersion, loadedEntry.InstalledVersion)
	}
}

func TestRollbackOnSaveFailure(t *testing.T) {
	home := setTempHome(t)

	// Block temp file creation inside ~/.local/share/antigravity by creating
	// antigravity as a regular file instead of a directory.
	targetDir := filepath.Join(home, ".local", "share", "antigravity")
	if err := os.MkdirAll(filepath.Dir(targetDir), 0755); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(targetDir, []byte("blocking-file"), 0644); err != nil {
		t.Fatalf("failed to write blocking file: %v", err)
	}

	m := newManifest()

	// 1. Set on new app: should fail and delete the key from m.Apps
	err := Set(m, "app-new", AppEntry{InstalledVersion: "v1.0.0"})
	if err == nil {
		t.Fatal("expected Set() to fail due to blocked directory")
	}
	if _, exists := Get(m, "app-new"); exists {
		t.Fatal("expected 'app-new' to be deleted on save failure")
	}

	// Pre-populate an existing app in memory directly
	m.Apps["app-existing"] = AppEntry{
		InstalledVersion: "v1.0.0",
		ETag:             "old-etag",
	}

	// 2. Set on existing app: should fail and revert to v1.0.0
	err = Set(m, "app-existing", AppEntry{InstalledVersion: "v2.0.0"})
	if err == nil {
		t.Fatal("expected Set() to fail due to blocked directory")
	}
	curr, ok := Get(m, "app-existing")
	if !ok || curr.InstalledVersion != "v1.0.0" {
		t.Fatalf("expected rollback to v1.0.0, got: %+v (ok=%v)", curr, ok)
	}

	// 3. MarkChecked on existing app: should fail and revert
	err = MarkChecked(m, "app-existing", "new-etag")
	if err == nil {
		t.Fatal("expected MarkChecked() to fail")
	}
	curr, _ = Get(m, "app-existing")
	if curr.ETag != "old-etag" {
		t.Fatalf("expected ETag to remain 'old-etag', got: %q", curr.ETag)
	}

	// 4. MarkInstalled on existing app: should fail and revert
	err = MarkInstalled(m, "app-existing", "v3.0.0", "v3-etag")
	if err == nil {
		t.Fatal("expected MarkInstalled() to fail")
	}
	curr, _ = Get(m, "app-existing")
	if curr.InstalledVersion != "v1.0.0" {
		t.Fatalf("expected InstalledVersion to remain 'v1.0.0', got: %q", curr.InstalledVersion)
	}
}
