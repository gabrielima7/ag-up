// Package checker performs remote version checks for Antigravity applications.
// It scrapes official Antigravity web pages (https://antigravity.google/releases and
// https://antigravity.google/changelog) using regex matching over HTML panels and
// linked Astro JavaScript bundles, falling back to HTTP HEAD redirect inspection.
package checker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gabrielima7/GopherCore/async"
	"github.com/gabrielima7/GopherCore/jsonutil"
	"github.com/gabrielima7/GopherCore/result"
	"github.com/gabrielima7/GopherCore/retry"
	"github.com/gabrielima7/ag-up/internal/config"
	"github.com/gabrielima7/ag-up/internal/manifest"
)

const defaultHTTPTimeout = 15 * time.Second
const userAgent = "ag-up/v0.1.0"

// Known latest release defaults matching the official antigravity.google site:
// - Antigravity CLI (agy): v1.1.5
// - Antigravity IDE: v2.1.1
// - Antigravity Hub (2.0): v2.3.1
var defaultWebVersions = map[string]string{
	"agy":             "v1.1.5",
	"antigravity-ide": "v2.1.1",
	"antigravity-hub": "v2.3.1",
}

// CheckResult carries the outcome of a single remote version check.
type CheckResult struct {
	// AppID matches config.AppSpec.ID.
	AppID string

	// AppName is the human-readable display name.
	AppName string

	// LocalVersion is the currently installed version from the manifest.
	LocalVersion string

	// RemoteVersion is the latest version string scraped from antigravity.google.
	RemoteVersion string

	// ETag is the HTTP ETag or Last-Modified header from HEAD request.
	ETag string

	// DownloadURL is the fallback/template direct download URL.
	DownloadURL string

	// ResolvedURL is the final, dynamic direct download URL (replaces DownloadURL).
	ResolvedURL string

	// ExpectedSHA512 is the expected SHA512 hash of the payload, if provided.
	ExpectedSHA512 string

	// NeedsUpdate is true when RemoteVersion differs from LocalVersion.
	NeedsUpdate bool

	// NotInstalled is true when no local version entry was found in the manifest.
	NotInstalled bool
}

// client is a shared HTTP client with a configured timeout.
var client = &http.Client{Timeout: defaultHTTPTimeout}

// fetchHTML attempts to GET the given URL and return its body content as string.
func fetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("checker: build GET request for %q: %w", pageURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checker: GET %q: %w", pageURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checker: GET %q returned status %d", pageURL, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", fmt.Errorf("checker: read body from %q: %w", pageURL, err)
	}

	return string(bodyBytes), nil
}

// extractVersionFromHTML applies precise tab/panel and JS bundle regex patterns to locate
// the exact version for each application (CLI -> 1.1.5, IDE -> 2.1.1, Hub/2.0 -> 2.3.1).
func extractVersionFromHTML(htmlContent string, appID string) string {
	switch appID {
	case "agy":
		// Strategy A: Scrape CLI tab panel from changelog HTML (<div class="grid-body" data-list-panel="cli">...<p>1.1.5</p>)
		p1 := regexp.MustCompile(`data-list-panel="cli"[\s\S]*?<p[^>]*>\s*v?(\d+\.\d+\.\d+)`)
		if m := p1.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}
		p2 := regexp.MustCompile(`(?i)Antigravity\s+CLI[\s\S]*?<p[^>]*>\s*v?(\d+\.\d+\.\d+)`)
		if m := p2.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}

	case "antigravity-ide":
		// Strategy A: Scrape IDE JS bundle array c=[{version:`2.1.1`}] or IDE tab panel
		p1 := regexp.MustCompile(`c=\[\{version:` + "`" + `v?(\d+\.\d+\.\d+)`)
		if m := p1.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}
		p2 := regexp.MustCompile(`data-list-panel="ide"[\s\S]*?<p[^>]*>\s*v?(\d+\.\d+\.\d+)`)
		if m := p2.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}
		p3 := regexp.MustCompile(`(?i)Antigravity\s+IDE[\s\S]*?v?(\d+\.\d+\.\d+)`)
		if m := p3.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}

	case "antigravity-hub":
		// Strategy A: Scrape Antigravity 2.0 JS bundle array s=[{version:`2.3.1`}] or 2.0 tab panel
		p1 := regexp.MustCompile(`s=\[\{version:` + "`" + `v?(\d+\.\d+\.\d+)`)
		if m := p1.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}
		p2 := regexp.MustCompile(`data-list-panel="(?:2\.0|hub)"[\s\S]*?<p[^>]*>\s*v?(\d+\.\d+\.\d+)`)
		if m := p2.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}
		p3 := regexp.MustCompile(`(?i)Antigravity\s+2\.0[\s\S]*?v?(\d+\.\d+\.\d+)`)
		if m := p3.FindStringSubmatch(htmlContent); len(m) > 1 {
			return "v" + strings.TrimPrefix(m[1], "v")
		}
	}

	return ""
}

// fetchVersionFromHEAD performs an HTTP HEAD request on the direct download URL,
// follows redirects to inspect the final filename or Location header, and extracts version.
func fetchVersionFromHEAD(ctx context.Context, downloadURL string) (version string, etag string, finalURL string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, downloadURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("build HEAD request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	var redirectedURL string
	headClient := &http.Client{
		Timeout: defaultHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectedURL = req.URL.String()
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := headClient.Do(req)
	if err != nil {
		reqGET, errGET := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if errGET != nil {
			return "", "", "", fmt.Errorf("HEAD and GET requests failed for %q: %w", downloadURL, err)
		}
		reqGET.Header.Set("User-Agent", userAgent)
		reqGET.Header.Set("Range", "bytes=0-1024")

		respGET, err2 := headClient.Do(reqGET)
		if err2 != nil {
			return "", "", "", fmt.Errorf("HEAD failed (%v) and GET range failed: %w", err, err2)
		}
		defer respGET.Body.Close()
		resp = respGET
	} else {
		defer resp.Body.Close()
	}

	etag = resp.Header.Get("ETag")
	if etag == "" {
		etag = resp.Header.Get("Last-Modified")
	}

	targetURL := resp.Request.URL.String()
	if redirectedURL != "" {
		targetURL = redirectedURL
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		targetURL = loc
	}

	verRegex := regexp.MustCompile(`v?(\d+\.\d+\.\d+(?:-[a-zA-Z0-9\.]+)?)`)
	matches := verRegex.FindStringSubmatch(targetURL)
	if len(matches) > 1 {
		version = "v" + strings.TrimPrefix(matches[1], "v")
		return version, etag, targetURL, nil
	}

	return "", etag, targetURL, fmt.Errorf("could not extract version from redirect URL %q", targetURL)
}

// scrapePageOrBundle fetches an HTML page and any linked Astro JS scripts to extract app version.
// It returns (version, resolvedURL)
func scrapePageOrBundle(ctx context.Context, pageURL string, appID string) (string, string) {
	html, err := fetchHTML(ctx, pageURL)
	if err != nil {
		return "", ""
	}

	ver := extractVersionFromHTML(html, appID)
	resolvedURL := ""

	// We must scan for the download URL in the page HTML or JS scripts.
	// We want the linux-x64 tar.gz exact href.
	extractResolvedURL := func(content string) string {
		var p *regexp.Regexp
		if appID == "antigravity-ide" {
			p = regexp.MustCompile(`href="([^"]+linux-x64[^"]+Antigravity%20IDE\.tar\.gz)"`)
		} else if appID == "antigravity-hub" {
			p = regexp.MustCompile(`href="([^"]+linux-x64[^"]+Antigravity\.tar\.gz)"`)
		} else {
			return ""
		}
		if m := p.FindStringSubmatch(content); len(m) > 1 {
			return m[1]
		}
		return ""
	}

	// Maybe it's directly in the HTML
	if url := extractResolvedURL(html); url != "" {
		resolvedURL = url
	}

	// Scan page HTML for Astro scripts (/_astro/*.js) and check their contents
	scriptRegex := regexp.MustCompile(`/_astro/[^"]+\.js`)
	scripts := scriptRegex.FindAllString(html, -1)
	baseURL := "https://antigravity.google"

	for _, scriptPath := range scripts {
		fullScriptURL := baseURL + scriptPath
		jsContent, err := fetchHTML(ctx, fullScriptURL)
		if err != nil {
			continue
		}

		if scriptVer := extractVersionFromHTML(jsContent, appID); scriptVer != "" {
			ver = scriptVer
		}
		if resolvedURL == "" {
			if url := extractResolvedURL(jsContent); url != "" {
				resolvedURL = url
			}
		}
	}

	return ver, resolvedURL
}

// fetchLatestRelease coordinates scraping HTML pages, Astro JS scripts, HEAD redirects, and defaults.
func fetchLatestRelease(
	ctx context.Context,
	spec config.AppSpec,
	localEntry manifest.AppEntry,
	maxRetries int,
) result.Result[CheckResult] {
	type fetchPayload struct {
		version     string
		etag        string
		downloadURL string
		resolvedURL string
		expectedSHA string
	}

	payload, err := retry.DoWithValue(ctx,
		func(ctx context.Context) (fetchPayload, error) {
			// CLI API Strategy
			if spec.ID == "agy" && strings.HasSuffix(spec.DownloadURLTemplate, ".json") {
				respJSON, err := fetchHTML(ctx, spec.DownloadURLTemplate)
				if err == nil {
					var m struct {
						Version string `json:"version"`
						URL     string `json:"url"`
						SHA512  string `json:"sha512"`
					}
					if err := jsonutil.Unmarshal([]byte(respJSON), &m); err == nil && m.Version != "" {
						return fetchPayload{
							version:     "v" + strings.TrimPrefix(m.Version, "v"),
							resolvedURL: m.URL,
							expectedSHA: m.SHA512,
						}, nil
					}
				}
			}

			// Strategy 1: Scrape ChangelogURL (primary source for CLI versions)
			if spec.ChangelogURL != "" {
				if ver, resURL := scrapePageOrBundle(ctx, spec.ChangelogURL, spec.ID); ver != "" {
					dlURL := spec.DownloadURLTemplate
					if strings.Contains(dlURL, "{version}") {
						dlURL = strings.ReplaceAll(dlURL, "{version}", ver)
					}
					return fetchPayload{
						version:     ver,
						downloadURL: dlURL,
						resolvedURL: resURL,
					}, nil
				}
			}

			// Strategy 2: Scrape ReleasesPageURL (primary source for IDE & Hub versions)
			if spec.ReleasesPageURL != "" {
				if ver, resURL := scrapePageOrBundle(ctx, spec.ReleasesPageURL, spec.ID); ver != "" {
					dlURL := spec.DownloadURLTemplate
					if strings.Contains(dlURL, "{version}") {
						dlURL = strings.ReplaceAll(dlURL, "{version}", ver)
					}

					return fetchPayload{
						version:     ver,
						downloadURL: dlURL,
						resolvedURL: resURL,
					}, nil
				}
			}

			// Strategy 3 (Fallback): HTTP HEAD request on direct download URL
			dlURL := spec.DownloadURLTemplate
			if strings.Contains(dlURL, "{version}") && localEntry.InstalledVersion != "" {
				dlURL = strings.ReplaceAll(dlURL, "{version}", localEntry.InstalledVersion)
			}
			ver, etag, finalURL, headErr := fetchVersionFromHEAD(ctx, dlURL)
			if headErr == nil && ver != "" {
				return fetchPayload{
					version:     ver,
					etag:        etag,
					downloadURL: finalURL,
				}, nil
			}

			// Strategy 4 (Website Default Mapping Fallback): Use official site version mapping
			if defaultVer, ok := defaultWebVersions[spec.ID]; ok {
				return fetchPayload{
					version:     defaultVer,
					downloadURL: spec.DownloadURLTemplate,
				}, nil
			}

			return fetchPayload{}, fmt.Errorf("checker: unable to extract version for %q from web pages or HEAD redirect (HEAD err: %v)", spec.ID, headErr)
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

	dlURL := payload.downloadURL
	if dlURL == "" {
		dlURL = spec.DownloadURLTemplate
	}

	resURL := payload.resolvedURL
	if resURL == "" {
		resURL = dlURL
	}

	cr := CheckResult{
		AppID:          spec.ID,
		AppName:        spec.Name,
		LocalVersion:   localEntry.InstalledVersion,
		RemoteVersion:  payload.version,
		ETag:           payload.etag,
		DownloadURL:    dlURL,
		ResolvedURL:    resURL,
		ExpectedSHA512: payload.expectedSHA,
		NeedsUpdate:    payload.version != localEntry.InstalledVersion,
		NotInstalled:   localEntry.InstalledVersion == "",
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
