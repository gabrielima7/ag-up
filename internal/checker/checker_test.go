package checker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
)

func TestExtractVersionFromHTML(t *testing.T) {
	tests := []struct {
		name     string
		appID    string
		html     string
		expected string
	}{
		{
			name:     "agy changelog panel",
			appID:    "agy",
			html:     `<html><div class="grid-body" data-list-panel="cli"><p>v1.2.3</p></div></html>`,
			expected: "v1.2.3",
		},
		{
			name:     "agy changelog panel no v",
			appID:    "agy",
			html:     `<html><div class="grid-body" data-list-panel="cli"><p>1.2.3</p></div></html>`,
			expected: "v1.2.3",
		},
		{
			name:     "antigravity-ide JS bundle",
			appID:    "antigravity-ide",
			html:     `var c=[{version:` + "`" + `v2.1.4` + "`" + `}];`,
			expected: "v2.1.4",
		},
		{
			name:     "antigravity-hub 2.0 JS bundle",
			appID:    "antigravity-hub",
			html:     `var s=[{version:` + "`" + `2.3.5` + "`" + `}];`,
			expected: "v2.3.5",
		},
		{
			name:     "antigravity-hub 2.0 html text",
			appID:    "antigravity-hub",
			html:     `<h1>Antigravity 2.0</h1><p>2.5.0</p>`,
			expected: "v2.5.0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractVersionFromHTML(tc.html, tc.appID)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestFetchVersionFromHEAD(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/v1.5.0/app.tar.gz", http.StatusFound)
	})
	mux.HandleFunc("/releases/v1.5.0/app.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "test-etag")
		w.WriteHeader(http.StatusOK)
	})

	ts := httptest.NewUnstartedServer(mux)
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	ctx := context.Background()
	version, etag, finalURL, err := fetchVersionFromHEAD(ctx, ts.URL+"/download")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if version != "v1.5.0" {
		t.Errorf("expected version v1.5.0, got %q", version)
	}
	if etag != "test-etag" {
		t.Errorf("expected etag test-etag, got %q", etag)
	}
	expectedURL := ts.URL + "/releases/v1.5.0/app.tar.gz"
	if finalURL != expectedURL {
		t.Errorf("expected final url %q, got %q", expectedURL, finalURL)
	}
}

func TestCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/changelog", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><div class="grid-body" data-list-panel="cli"><p>v1.5.5</p></div></html>`))
	})
	mux.HandleFunc("/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`var c=[{version:` + "`" + `2.2.0` + "`" + `}];`))
	})
	mux.HandleFunc("/download/cli", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/v1.5.5/agy.tar.gz", http.StatusFound)
	})
    mux.HandleFunc("/releases/v1.5.5/agy.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "cli-etag")
		w.WriteHeader(http.StatusOK)
    })

	ts := httptest.NewServer(mux)
	defer ts.Close()

	spec := config.AppSpec{
		ID:                  "agy",
		Name:                "Google Antigravity CLI",
		ChangelogURL:        ts.URL + "/changelog",
		DownloadURLTemplate: ts.URL + "/download/cli",
	}

	m := manifest.Manifest{
		Apps: map[string]manifest.AppEntry{
			"agy": {
				InstalledVersion: "v1.5.0",
			},
		},
	}

	ctx := context.Background()
	res := Check(ctx, spec, m, 1)

	if res.IsErr() {
		t.Fatalf("Check failed: %v", res.Error())
	}

	cr, _ := res.Unwrap()
	if cr.AppID != "agy" {
		t.Errorf("expected AppID agy, got %q", cr.AppID)
	}
	if cr.RemoteVersion != "v1.5.5" {
		t.Errorf("expected RemoteVersion v1.5.5, got %q", cr.RemoteVersion)
	}
	if !cr.NeedsUpdate {
		t.Errorf("expected NeedsUpdate true")
	}
}

func TestCheck_NetworkFailure(t *testing.T) {
	// A server that returns 500
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	l, _ := net.Listen("tcp", "[::1]:0")
	ts.Listener = l
	ts.Start()
	defer ts.Close()

	spec := config.AppSpec{
		ID:                  "agy",
		ChangelogURL:        ts.URL + "/changelog",
		DownloadURLTemplate: ts.URL + "/download/cli",
	}
	// Fallback mapping is in defaultWebVersions (agy -> v1.1.5)

	ctx := context.Background()
	m := manifest.Manifest{}

	res := Check(ctx, spec, m, 1)

	if res.IsErr() {
		t.Fatalf("expected fallback to defaultWebVersions instead of error: %v", res.Error())
	}

	cr, _ := res.Unwrap()
	if cr.RemoteVersion != "v1.1.5" {
		t.Errorf("expected fallback version v1.1.5, got %q", cr.RemoteVersion)
	}
}
