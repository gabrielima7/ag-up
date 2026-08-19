package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
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
