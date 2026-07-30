// Package updater — downloader.go
//
// This file isolates all network download logic from the extract-and-install
// pipeline. It enforces:
//   - Context-aware HTTP requests (respects cancellation from Ctrl-C / SIGTERM).
//   - Immediate defer cleanup registration on every temp file created.
//   - Retry with exponential backoff + jitter via GopherCore retry package.
//   - Fast failure on non-retryable HTTP 4xx responses (404, 403).
package updater

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabrielima7/GopherCore/guard"
	"github.com/gabrielima7/GopherCore/retry"
)

// userAgent is the HTTP User-Agent header sent on all outbound requests.
const userAgent = "ag-up/" + Version

// downloadTimeout caps the total duration allowed for a single tarball download.
const downloadTimeout = 5 * time.Minute

// downloaderClient is a package-level HTTP client shared across all download
// attempts. Reusing a single client and its underlying transport means:
//   - TCP connection pooling is preserved between retry attempts.
//   - TLS handshakes are amortised.
//   - Transport-level timeout errors are not masked by re-initialisation.
var downloaderClient = &http.Client{Timeout: downloadTimeout}

// isNonRetryableError checks if an error represents a permanent client failure
// (such as HTTP 404 Not Found or HTTP 403 Forbidden).
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "HTTP 404") ||
		strings.Contains(errStr, "HTTP 403") ||
		strings.Contains(errStr, "HTTP 400") ||
		strings.Contains(errStr, "HTTP 401")
}

// DownloadTarGz fetches the tarball at rawURL and writes it to a uniquely
// named temporary file under os.TempDir(). Returns the path to the temp file
// on success; the caller is responsible for removing it (via defer os.Remove).
func DownloadTarGz(ctx context.Context, rawURL, appID string, maxRetries int) (string, error) {
	// Sanitise inputs through guard to strip null bytes and control characters.
	safeURL := guard.SanitizeString(rawURL)
	safeID := guard.SanitizeString(appID)

	tmpPath := filepath.Join(
		os.TempDir(),
		fmt.Sprintf("ag-up-%s-%d.tar.gz", safeID, time.Now().UnixNano()),
	)

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("downloader: create temp file for %q: %w", appID, err)
	}

	removeOnExit := true
	defer func() {
		f.Close()
		if removeOnExit {
			if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
				slog.Warn("downloader: failed to remove temp file on error path",
					"path", tmpPath,
					"error", rmErr,
				)
			}
		}
	}()

	err = retry.Do(ctx,
		func(ctx context.Context) error {
			if err := f.Truncate(0); err != nil {
				return fmt.Errorf("downloader: truncate temp file: %w", err)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("downloader: seek temp file: %w", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, safeURL, nil)
			if err != nil {
				return fmt.Errorf("downloader: build request: %w", err)
			}
			req.Header.Set("User-Agent", userAgent)

			resp, err := downloaderClient.Do(req)
			if err != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("downloader: request cancelled for %q: %w", appID, ctx.Err())
				}
				return fmt.Errorf("downloader: GET %q for %q: %w", safeURL, appID, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf(
					"downloader: HTTP %d for %q (url: %s)",
					resp.StatusCode, appID, safeURL,
				)
			}

			written, err := io.Copy(f, resp.Body)
			if err != nil {
				if ctx.Err() != nil {
					return fmt.Errorf("downloader: body copy cancelled for %q: %w", appID, ctx.Err())
				}
				return fmt.Errorf("downloader: write body for %q: %w", appID, err)
			}

			slog.Debug("downloader: tarball fetched",
				"app_id", appID,
				"bytes", written,
				"path", tmpPath,
			)
			return nil
		},
		retry.WithMaxAttempts(maxRetries),
		retry.WithInitialDelay(500*time.Millisecond),
		retry.WithStrategy(retry.StrategyExponential),
		retry.WithJitter(true),
		retry.WithMaxDelay(10*time.Second),
		retry.WithRetryIf(func(err error) bool {
			if ctx.Err() != nil {
				return false
			}
			return !isNonRetryableError(err)
		}),
	)

	if err != nil {
		return "", err
	}

	removeOnExit = false
	return tmpPath, nil
}
