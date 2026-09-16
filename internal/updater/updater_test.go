package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
	"github.com/gabrielima7/ag-up/pkg/xdg"
)

func TestUpdateAllChaosRace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// manifest.Load() will create a new empty manifest if it doesn't exist
	m, err := manifest.Load()
	if err != nil && err.Error() != "updater: async.Map failed: context canceled" {
		t.Fatalf("failed to load manifest: %v", err)
	}

	// Spin up a dummy HTTP server for checker
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"version": "1.2.3", "url": "http://%s/agy.tar.gz", "sha512": ""}`+"\n", r.Host)
			return
		}
		if r.URL.Path == "/agy.tar.gz" {
			// Delay slightly to ensure context cancellation occurs during the download phase
			time.Sleep(50 * time.Millisecond)
			w.Write([]byte("fake tarball data"))
			return
		}
	}))
	defer ts.Close()

	specs := []config.AppSpec{
		{
			ID:               "agy",
			Name:             "Google Antigravity CLI",
			BinaryName:       "agy",
			ManifestURL:      ts.URL + "/manifest.json",
			TarballInnerName: "antigravity",
			IsGUI:            false,
		},
		{
			ID:               "agy2",
			Name:             "Google Antigravity CLI 2",
			BinaryName:       "agy2",
			ManifestURL:      ts.URL + "/manifest.json",
			TarballInnerName: "antigravity",
			IsGUI:            false,
		},
		{
			ID:               "agy3",
			Name:             "Google Antigravity CLI 3",
			BinaryName:       "agy3",
			ManifestURL:      ts.URL + "/manifest.json",
			TarballInnerName: "antigravity",
			IsGUI:            false,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	results, err := UpdateAll(ctx, specs, m, 1)

	wg.Wait()

	if err != nil && err.Error() != "updater: async.Map failed: context canceled" {
		t.Fatalf("UpdateAll returned error: %v", err)
	}

	for _, res := range results {
		if res.IsOk() {
			t.Log("Warning: test completed too quickly and succeeded instead of cancelling.")
		}
	}
}

func TestExtractAndInstallZipSlipAndHardening(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	// Create test tar.gz
	tarPath := filepath.Join(tempHome, "test_package.tar.gz")
	f, err := os.Create(tarPath)
	if err != nil {
		t.Fatalf("create tar: %v", err)
	}

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// 1. Regular binary "antigravity" with SUID bit (04755)
	binContent := []byte("#!/bin/sh\necho test\n")
	binHdr := &tar.Header{
		Name:     "antigravity",
		Mode:     04755,
		Size:     int64(len(binContent)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(binHdr); err != nil {
		t.Fatalf("write bin header: %v", err)
	}
	if _, err := tw.Write(binContent); err != nil {
		t.Fatalf("write bin content: %v", err)
	}

	// 2. Malicious Zip Slip entry with relative traversal
	slipContent := []byte("pwned")
	slipHdr := &tar.Header{
		Name:     "../evil_slip.txt",
		Mode:     0644,
		Size:     int64(len(slipContent)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(slipHdr); err != nil {
		t.Fatalf("write slip header: %v", err)
	}
	if _, err := tw.Write(slipContent); err != nil {
		t.Fatalf("write slip content: %v", err)
	}

	// 3. Subdirectory and safe file
	subContent := []byte("hello safe file")
	subHdr := &tar.Header{
		Name:     "sub/file.txt",
		Mode:     0644,
		Size:     int64(len(subContent)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(subHdr); err != nil {
		t.Fatalf("write sub header: %v", err)
	}
	if _, err := tw.Write(subContent); err != nil {
		t.Fatalf("write sub content: %v", err)
	}

	// 4. Safe symlink
	safeSymHdr := &tar.Header{
		Name:     "sub/safelink",
		Linkname: "file.txt",
		Typeflag: tar.TypeSymlink,
	}
	if err := tw.WriteHeader(safeSymHdr); err != nil {
		t.Fatalf("write safe symlink header: %v", err)
	}

	// 5. Escaping symlink
	badSymHdr := &tar.Header{
		Name:     "sub/escapelink",
		Linkname: "../../outside.txt",
		Typeflag: tar.TypeSymlink,
	}
	if err := tw.WriteHeader(badSymHdr); err != nil {
		t.Fatalf("write bad symlink header: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	_ = f.Close()

	spec := config.AppSpec{
		ID:               "test-app",
		Name:             "Test Application",
		BinaryName:       "test-app",
		TarballInnerName: "antigravity",
		IsGUI:            false,
	}

	ctx := context.Background()
	binPath, err := extractAndInstall(ctx, tarPath, spec)
	if err != nil {
		t.Fatalf("extractAndInstall failed: %v", err)
	}

	if binPath == "" {
		t.Fatalf("expected binary path, got empty string")
	}

	// Check SUID bit is masked on extracted binary
	fi, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if fi.Mode()&os.ModeSetuid != 0 {
		t.Errorf("expected SUID bit to be masked, got mode: %v", fi.Mode())
	}

	// Check that evil_slip was NOT created outside dataDir
	dataDir, err := xdg.DataDir(spec.ID)
	if err != nil {
		t.Fatalf("get data dir: %v", err)
	}
	escapedPath := filepath.Join(filepath.Dir(dataDir), "evil_slip.txt")
	if _, err := os.Stat(escapedPath); !os.IsNotExist(err) {
		t.Errorf("security failure: Zip Slip file was extracted to %q", escapedPath)
	}

	// Check that escaping symlink was not created outside or pointing outside
	badSymPath := filepath.Join(dataDir, "sub", "escapelink")
	if _, err := os.Lstat(badSymPath); err == nil {
		t.Errorf("security failure: escaping symlink %q was created", badSymPath)
	}
}

