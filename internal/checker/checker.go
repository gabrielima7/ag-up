// Package checker performs remote version checks for Antigravity applications.
//
// Two resolution strategies are supported, dispatched per-AppSpec:
//
//  1. CLI (agy): Fetches the Cloud Run JSON manifest from ManifestURL.
//     Returns version, direct download URL, and SHA-512 checksum.
//
//  2. IDE / Hub: Fetches the HTML download page at WebReleasePage, locates
//     the <section id=SectionID> block, extracts the linux-x64 .tar.gz href,
//     and derives the SemVer tag directly from the URL path.
package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gabrielima7/GopherCore/async"
	"github.com/gabrielima7/GopherCore/result"
	"github.com/gabrielima7/GopherCore/retry"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
)

const defaultHTTPTimeout = 15 * time.Second
const userAgent = "ag-up/v0.1.0"

// CheckResult carries the outcome of a single remote version check.
type CheckResult struct {
	// AppID matches config.AppSpec.ID.
	AppID string

	// AppName is the human-readable display name.
	AppName string

	// LocalVersion is the currently installed version from the manifest.
	LocalVersion string

	// RemoteVersion is the latest version string scraped from the remote source.
	RemoteVersion string

	// ETag is the HTTP ETag or Last-Modified header from HEAD request (may be empty).
	ETag string

	// ResolvedURL is the direct .tar.gz download URL resolved from the remote source.
	ResolvedURL string

	// ExpectedSHA512 is the hex-encoded SHA-512 checksum from the manifest (CLI only).
	// Empty string means no checksum validation is required.
	ExpectedSHA512 string

	// NeedsUpdate is true when RemoteVersion differs from LocalVersion.
	NeedsUpdate bool

	// NotInstalled is true when no local version entry was found in the manifest.
	NotInstalled bool
}

// client is a shared HTTP client with a configured timeout.
// Note: Accept-Encoding is NOT set manually so Go's transport handles
// transparent gzip decompression for compressed responses.
var client = &http.Client{Timeout: defaultHTTPTimeout}

// cliManifest is the JSON structure returned by the Cloud Run auto-updater.
type cliManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA512  string `json:"sha512"`
}

// fetchJSON performs a GET request and decodes the JSON body into dest.
func fetchJSON(ctx context.Context, url string, dest interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("checker: build request for %q: %w", url, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("checker: GET %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checker: GET %q returned status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return fmt.Errorf("checker: read body from %q: %w", url, err)
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("checker: decode JSON from %q: %w", url, err)
	}
	return nil
}

// fetchHTML performs a GET request and returns the response body as a string.
// Accept-Encoding is intentionally omitted so Go's http.Transport automatically
// decompresses gzip responses from antigravity.google/download.
func fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("checker: build GET request for %q: %w", pageURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checker: GET %q: %w", pageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checker: GET %q returned status %d", pageURL, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return "", fmt.Errorf("checker: read body from %q: %w", pageURL, err)
	}

	return string(bodyBytes), nil
}

// fetchCLIManifest fetches the Cloud Run JSON manifest for the CLI app and
// returns the resolved CheckResult payload (version, download URL, SHA-512).
func fetchCLIManifest(ctx context.Context, spec config.AppSpec, localEntry manifest.AppEntry) (CheckResult, error) {
	var m cliManifest
	if err := fetchJSON(ctx, spec.ManifestURL, &m); err != nil {
		return CheckResult{}, fmt.Errorf("cli manifest: %w", err)
	}

	if m.Version == "" || m.URL == "" {
		return CheckResult{}, fmt.Errorf("cli manifest: missing version or url in response")
	}

	remoteVersion := "v" + strings.TrimPrefix(m.Version, "v")

	cr := CheckResult{
		AppID:          spec.ID,
		AppName:        spec.Name,
		LocalVersion:   localEntry.InstalledVersion,
		RemoteVersion:  remoteVersion,
		ResolvedURL:    m.URL,
		ExpectedSHA512: m.SHA512,
		NeedsUpdate:    remoteVersion != localEntry.InstalledVersion,
		NotInstalled:   localEntry.InstalledVersion == "",
	}

	slog.Info("checker: cli manifest resolved",
		"app_id", spec.ID,
		"remote_version", remoteVersion,
		"download_url", m.URL,
		"sha512_present", m.SHA512 != "",
	)

	return cr, nil
}

// semverFromURL extracts the first SemVer tag (vX.Y.Z or X.Y.Z) found in a URL string.
// The version is embedded in path segments like ".../2.3.1-5358163105546240/...".
var semverRe = regexp.MustCompile(`/(\d+\.\d+\.\d+)-\d+/`)

// scrapeDownloadPage fetches the HTML download page for IDE or Hub, finds the
// linux-x64 .tar.gz href within the app's designated <section id=SectionID>,
// and extracts the SemVer tag from the URL path.
func scrapeDownloadPage(ctx context.Context, spec config.AppSpec, localEntry manifest.AppEntry) (CheckResult, error) {
	html, err := fetchHTML(ctx, spec.WebReleasePage)
	if err != nil {
		return CheckResult{}, fmt.Errorf("scrape download page: %w", err)
	}

	// Locate the section for this specific app by finding id="<SectionID>"
	// and then slicing up to the next </section> tag.
	// We use strings.Index to avoid RE2's repeat-count limit.
	anchor := fmt.Sprintf(`id="%s"`, spec.SectionID)
	sectionMatch := html
	if startIdx := strings.Index(html, anchor); startIdx >= 0 {
		slice := html[startIdx:]
		if endIdx := strings.Index(slice, "</section>"); endIdx >= 0 {
			sectionMatch = slice[:endIdx+len("</section>")]
		} else {
			sectionMatch = slice
		}
	} else {
		slog.Warn("checker: section anchor not found, searching whole page",
			"app_id", spec.ID,
			"section_id", spec.SectionID,
		)
	}

	// Within the section, find the linux-x64 .tar.gz href.
	var hrefRe *regexp.Regexp
	switch spec.SectionID {
	case "antigravity-ide":
		// edgedl.me.gvt1.com/.../linux-x64/Antigravity%20IDE.tar.gz
		hrefRe = regexp.MustCompile(`href="(https://edgedl\.me\.gvt1\.com/[^"]*linux-x64/Antigravity(?:%20IDE)?\.tar\.gz)"`)
	default:
		// storage.googleapis.com/.../linux-x64/Antigravity.tar.gz  (Hub / antigravity-2)
		hrefRe = regexp.MustCompile(`href="(https://storage\.googleapis\.com/[^"]*linux-x64/Antigravity\.tar\.gz)"`)
	}

	hrefMatch := hrefRe.FindStringSubmatch(sectionMatch)
	if len(hrefMatch) < 2 {
		return CheckResult{}, fmt.Errorf("scrape download page: linux-x64 tar.gz link not found in section %q", spec.SectionID)
	}
	resolvedURL := hrefMatch[1]

	// Extract SemVer from the URL path segment (e.g. "2.3.1-5358163105546240").
	verMatch := semverRe.FindStringSubmatch(resolvedURL)
	if len(verMatch) < 2 {
		return CheckResult{}, fmt.Errorf("scrape download page: could not extract version from URL %q", resolvedURL)
	}
	remoteVersion := "v" + verMatch[1]

	cr := CheckResult{
		AppID:         spec.ID,
		AppName:       spec.Name,
		LocalVersion:  localEntry.InstalledVersion,
		RemoteVersion: remoteVersion,
		ResolvedURL:   resolvedURL,
		NeedsUpdate:   remoteVersion != localEntry.InstalledVersion,
		NotInstalled:  localEntry.InstalledVersion == "",
	}

	slog.Info("checker: html page scraped",
		"app_id", spec.ID,
		"section_id", spec.SectionID,
		"remote_version", remoteVersion,
		"download_url", resolvedURL,
	)

	return cr, nil
}

// fetchLatestRelease dispatches to the correct resolution strategy based on AppSpec fields.
func fetchLatestRelease(
	ctx context.Context,
	spec config.AppSpec,
	localEntry manifest.AppEntry,
	maxRetries int,
) result.Result[CheckResult] {
	cr, err := retry.DoWithValue(ctx,
		func(ctx context.Context) (CheckResult, error) {
			if spec.ManifestURL != "" {
				return fetchCLIManifest(ctx, spec, localEntry)
			}
			if spec.WebReleasePage != "" {
				return scrapeDownloadPage(ctx, spec, localEntry)
			}
			return CheckResult{}, fmt.Errorf("checker: AppSpec %q has no ManifestURL or WebReleasePage configured", spec.ID)
		},
		retry.WithMaxAttempts(maxRetries),
		retry.WithInitialDelay(500*time.Millisecond),
		retry.WithStrategy(retry.StrategyExponential),
		retry.WithJitter(true),
		retry.WithMaxDelay(10*time.Second),
		retry.WithRetryIf(func(err error) bool {
			return ctx.Err() == nil
		}),
	)

	if err != nil {
		slog.Warn("checker: failed to fetch release info",
			"app_id", spec.ID,
			"error", err,
		)
		return result.Err[CheckResult](fmt.Errorf("checker: %s: %w", spec.ID, err))
	}

	slog.Info("checker: version check complete",
		"app_id", spec.ID,
		"local", cr.LocalVersion,
		"remote", cr.RemoteVersion,
		"needs_update", cr.NeedsUpdate,
	)

	return result.Ok(cr)
}

// Check performs a single remote version check for the given AppSpec.
func Check(
	ctx context.Context,
	spec config.AppSpec,
	m manifest.Manifest,
	maxRetries int,
) result.Result[CheckResult] {
	localEntry, _ := manifest.Get(m, spec.ID)
	return fetchLatestRelease(ctx, spec, localEntry, maxRetries)
}

// CheckAll performs parallel version checks for all provided AppSpecs using async.Map.
func CheckAll(
	ctx context.Context,
	specs []config.AppSpec,
	m manifest.Manifest,
	maxRetries int,
) ([]result.Result[CheckResult], error) {
	results, err := async.Map(
		ctx,
		specs,
		3,
		func(ctx context.Context, spec config.AppSpec) (result.Result[CheckResult], error) {
			r := Check(ctx, spec, m, maxRetries)
			return r, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("checker: async.Map failed: %w", err)
	}
	return results, nil
}
