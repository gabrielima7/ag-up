package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
	"github.com/gabrielima7/ag-up/pkg/xdg"
)

// Helper to create a valid .tar.gz bundle for testing in memory
func createTestTarball(t *testing.T, files map[string]string) string {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "ag-up-test-*.tar.gz")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer tmpFile.Close()

	gw := gzip.NewWriter(tmpFile)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header for %s failed: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write content for %s failed: %v", name, err)
		}
	}

	return tmpFile.Name()
}

func setupTestEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := xdg.EnsureDirectories([]string{"agy", "antigravity-ide", "antigravity-hub"}); err != nil {
		t.Fatalf("failed to setup XDG dirs: %v", err)
	}
}

func TestDownloadTarGz_Success(t *testing.T) {
	expectedContent := "dummy tarball content"
	mux := http.NewServeMux()
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expectedContent))
	})
	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	ctx := context.Background()
	path, err := DownloadTarGz(ctx, ts.URL+"/download", "agy", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer os.Remove(path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file failed: %v", err)
	}
	if string(content) != expectedContent {
		t.Errorf("expected content %q, got %q", expectedContent, string(content))
	}
}

func TestDownloadTarGz_NetworkFailureFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	ctx := context.Background()
	path, err := DownloadTarGz(ctx, ts.URL+"/download", "agy", 1)
	if err != nil {
		t.Fatalf("unexpected error on fallback: %v", err)
	}
	defer os.Remove(path)

	// Since 404 triggers createLocalPackage, we verify it created a tarball.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open local fallback package: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("fallback package is not valid gzip: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("fallback package tar read error: %v", err)
	}
	if hdr.Name != "agy" {
		t.Errorf("expected binary name 'agy' in fallback package, got %q", hdr.Name)
	}
}

func TestDownloadTarGz_ContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		time.Sleep(2 * time.Second) // delay so we can cancel
		w.Write([]byte("some data"))
	})
	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	path, err := DownloadTarGz(ctx, ts.URL+"/download", "agy", 1)
	if err == nil {
		defer os.Remove(path)
		t.Fatalf("expected error due to context cancellation, got nil")
	}

	// Verify the temp file doesn't exist
	if path != "" {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("temp file %q was not removed after context cancellation", path)
		}
	}
}

func TestExtractAndInstall(t *testing.T) {
	setupTestEnv(t)

	tarballPath := createTestTarball(t, map[string]string{
		"agy":        "#!/bin/sh\necho test",
		"config.txt": "dummy config",
		"dir/file":   "nested file",
	})
	defer os.Remove(tarballPath)

	spec := config.AppSpec{
		ID:         "agy",
		BinaryName: "agy",
	}

	if err := extractAndInstall(tarballPath, spec); err != nil {
		t.Fatalf("extractAndInstall failed: %v", err)
	}

	home := os.Getenv("HOME")
	binPath := filepath.Join(home, ".local", "bin", "agy")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Errorf("expected binary %q to be extracted", binPath)
	}

	configPath := filepath.Join(home, ".local", "share", "antigravity", "agy", "config.txt")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("expected data file %q to be extracted", configPath)
	}

	nestedPath := filepath.Join(home, ".local", "share", "antigravity", "agy", "dir", "file")
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Errorf("expected nested data file %q to be extracted", nestedPath)
	}
}

func TestUpdate(t *testing.T) {
	setupTestEnv(t)

	// We serve everything via a mocked remote mirror.
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`var c=[{version:` + "`" + `v2.2.0` + "`" + `}];`))
	})

	tarballBytes, err := func() ([]byte, error) {
		// create in-memory tarball
		tmpFile := createTestTarball(t, map[string]string{
			"antigravity-ide": "#!/bin/sh\necho IDE",
			"icon.png": "png data",
		})
		defer os.Remove(tmpFile)
		return os.ReadFile(tmpFile)
	}()
	if err != nil {
		t.Fatalf("failed to create dummy tarball bytes: %v", err)
	}

	mux.HandleFunc("/download/ide", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "ide-etag-v2.2.0")
		w.WriteHeader(http.StatusOK)
		w.Write(tarballBytes)
	})

	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	spec := config.AppSpec{
		ID:                  "antigravity-ide",
		Name:                "Google Antigravity IDE",
		BinaryName:          "antigravity-ide",
		ReleasesPageURL:     ts.URL + "/releases",
		DownloadURLTemplate: ts.URL + "/download/ide",
		IsGUI:               true,
		DesktopIcon:         "antigravity-ide",
		Categories:          "Development;IDE;",
		Comment:             "IDE test",
		StartupWMClass:      "AntigravityIDE",
	}

	m := &manifest.Manifest{
		Apps: map[string]manifest.AppEntry{
			"antigravity-ide": {
				InstalledVersion: "v1.0.0",
			},
		},
	}

	ctx := context.Background()
	res := Update(ctx, spec, m, false, 1)

	if res.IsErr() {
		t.Fatalf("Update failed: %v", res.Error())
	}

	summary, _ := res.Unwrap()
	if !summary.Success {
		t.Errorf("expected success to be true")
	}
	if summary.NewVersion != "v2.2.0" {
		t.Errorf("expected new version v2.2.0, got %q", summary.NewVersion)
	}

	// Verify artifacts
	home := os.Getenv("HOME")
	binPath := filepath.Join(home, ".local", "bin", "antigravity-ide")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		t.Errorf("expected binary %q to exist", binPath)
	}

	desktopPath := filepath.Join(home, ".local", "share", "applications", "antigravity-ide.desktop")
	if _, err := os.Stat(desktopPath); os.IsNotExist(err) {
		t.Errorf("expected .desktop file %q to exist", desktopPath)
	}

	manifestPath := filepath.Join(home, ".local", "share", "antigravity", ".manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		// Wait, Update writes to memory manifest `m`, not disk directly if we just check `m`.
		// But MarkInstalled saves to disk. We can check it.
		// manifest is saved to xdg.ManifestPath()
	}

	// Read saved manifest
	savedM, err := manifest.Load()
	if err != nil {
		t.Fatalf("failed to load saved manifest: %v", err)
	}
	entry, ok := savedM.Apps["antigravity-ide"]
	if !ok {
		t.Fatalf("expected manifest to have entry for antigravity-ide")
	}
	if entry.InstalledVersion != "v2.2.0" {
		t.Errorf("expected saved manifest version v2.2.0, got %q", entry.InstalledVersion)
	}
}

func TestUpdate_SkippedAndForce(t *testing.T) {
	setupTestEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`var c=[{version:` + "`" + `v1.0.0` + "`" + `}];`))
	})

	mux.HandleFunc("/download/ide", func(w http.ResponseWriter, r *http.Request) {
		tmpFile := createTestTarball(t, map[string]string{
			"antigravity-ide": "#!/bin/sh\necho IDE",
		})
		defer os.Remove(tmpFile)
		b, _ := os.ReadFile(tmpFile)
		w.Header().Set("ETag", "ide-etag-v1.0.0")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	})

	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	spec := config.AppSpec{
		ID:                  "antigravity-ide",
		BinaryName:          "antigravity-ide",
		ReleasesPageURL:     ts.URL + "/releases",
		DownloadURLTemplate: ts.URL + "/download/ide",
	}

	m := &manifest.Manifest{
		Apps: map[string]manifest.AppEntry{
			"antigravity-ide": {
				InstalledVersion: "v1.0.0", // Same as remote
			},
		},
	}

	ctx := context.Background()

	// 1. Should skip without force
	resSkip := Update(ctx, spec, m, false, 1)
	if resSkip.IsErr() {
		t.Fatalf("Update skipped failed: %v", resSkip.Error())
	}
	summarySkip, _ := resSkip.Unwrap()
	if !summarySkip.Skipped {
		t.Errorf("expected update to be skipped")
	}

	// 2. Should update with force
	resForce := Update(ctx, spec, m, true, 1)
	if resForce.IsErr() {
		t.Fatalf("Update forced failed: %v", resForce.Error())
	}
	summaryForce, _ := resForce.Unwrap()
	if summaryForce.Skipped {
		t.Errorf("expected update to not be skipped with force=true")
	}
}

func TestUpdateAll(t *testing.T) {
	setupTestEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`var c=[{version:` + "`" + `v2.2.0` + "`" + `}];`))
	})
	mux.HandleFunc("/changelog", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><div class="grid-body" data-list-panel="cli"><p>v1.5.5</p></div></html>`))
	})

	mux.HandleFunc("/download/ide", func(w http.ResponseWriter, r *http.Request) {
		tmpFile := createTestTarball(t, map[string]string{
			"antigravity-ide": "#!/bin/sh\necho IDE",
		})
		defer os.Remove(tmpFile)
		b, _ := os.ReadFile(tmpFile)
		w.Header().Set("ETag", "ide-etag-v2.2.0")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	})

	mux.HandleFunc("/download/cli", func(w http.ResponseWriter, r *http.Request) {
		tmpFile := createTestTarball(t, map[string]string{
			"agy": "#!/bin/sh\necho CLI",
		})
		defer os.Remove(tmpFile)
		b, _ := os.ReadFile(tmpFile)
		w.Header().Set("ETag", "cli-etag-v1.5.5")
		w.WriteHeader(http.StatusOK)
		w.Write(b)
	})

	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	specs := []config.AppSpec{
		{
			ID:                  "antigravity-ide",
			BinaryName:          "antigravity-ide",
			ReleasesPageURL:     ts.URL + "/releases",
			DownloadURLTemplate: ts.URL + "/download/ide",
		},
		{
			ID:                  "agy",
			BinaryName:          "agy",
			ChangelogURL:        ts.URL + "/changelog",
			DownloadURLTemplate: ts.URL + "/download/cli",
		},
	}

	m := &manifest.Manifest{
		Apps: map[string]manifest.AppEntry{},
	}

	ctx := context.Background()
	results, err := UpdateAll(ctx, specs, m, false, 1)
	if err != nil {
		t.Fatalf("UpdateAll failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	for _, res := range results {
		if res.IsErr() {
			t.Errorf("expected successful update for app, got error: %v", res.Error())
		}
	}
}

func TestExtractAndInstall_CorruptedArchive(t *testing.T) {
	setupTestEnv(t)
	tmpFile, err := os.CreateTemp("", "ag-up-corrupted-*.tar.gz")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write invalid data
	tmpFile.Write([]byte("this is not a valid gzip or tar archive"))
	tmpFile.Close()

	spec := config.AppSpec{
		ID:         "agy",
		BinaryName: "agy",
	}

	err = extractAndInstall(tmpFile.Name(), spec)
	if err == nil {
		t.Fatalf("expected error when extracting corrupted archive, got nil")
	}
}

func TestUpdate_NetworkFailureReturnsError(t *testing.T) {
	setupTestEnv(t)
	// Server always returns 500
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	spec := config.AppSpec{
		ID:                  "agy",
		BinaryName:          "agy",
		// No fallback will be found in config defaultWebVersions for download failure as DownloadURLTemplate points to failing server
		// Wait, the checker defaults to "v1.1.5" mapping, so it *will* return success for the check.
		// However, we want the *download* to fail to test updater error result propagation.
		DownloadURLTemplate: ts.URL + "/download",
	}

	// We make it think the remote is 1.2.0, while local is 1.0.0, so it proceeds to download.
	// But checker uses the same server and falls back to 1.1.5, which is still > 1.0.0.

	m := &manifest.Manifest{
		Apps: map[string]manifest.AppEntry{
			"agy": {
				InstalledVersion: "v1.0.0",
			},
		},
	}

	// This should fail because the download will fail.
	ctx := context.Background()
	res := Update(ctx, spec, m, false, 1)

	if !res.IsErr() {
		t.Fatalf("expected Update to return error result on download failure, but it succeeded")
	}
}
