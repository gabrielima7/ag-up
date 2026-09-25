# Changelog

All notable changes to `ag-up` (Google Antigravity Universal Updater) will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [v0.2.0] - 2026-09-25

### Summary
`v0.2.0` is a major reliability, concurrency, performance, and DevSecOps hardening release. It incorporates 18 merged Pull Requests resolving data races, goroutine leaks, channel deadlocks, HTTP connection leaks, Time-of-Check to Time-of-Use (TOCTOU) file vulnerabilities, and Zip Slip risks. It also introduces a dedicated CI/CD security audit pipeline and updates all core dependencies.

### Added
- **Thread-Safe Console Package (`internal/printer`):** Centralized mutex-guarded printer (`printer.Print`, `printer.Printf`, `printer.Println`, `printer.Fprintf`) eliminating race conditions and garbled output when concurrent workers write to standard output/error ([PR #8](https://github.com/gabrielima7/ag-up/pull/8), [PR #10](https://github.com/gabrielima7/ag-up/pull/10), [PR #12](https://github.com/gabrielima7/ag-up/pull/12), [PR #17](https://github.com/gabrielima7/ag-up/pull/17)).
- **Automated CI / DevSecOps Pipeline (`.github/workflows/ci.yml`):**
  - Repository hygiene validation against unauthorized patch scripts.
  - Dependency verification via `go mod verify`.
  - Static AST security vulnerability auditing via `gosec`.
  - Known CVE security auditing via `govulncheck`.
  - Strict concurrency testing with race detection (`go test -race`).
- **Comprehensive Chaos & Race Test Suites:**
  - `internal/manifest/manifest_race_test.go`: High-concurrency race test validating `sync.RWMutex` safety under concurrent reads and writes ([PR #9](https://github.com/gabrielima7/ag-up/pull/9)).
  - `internal/updater/updater_test.go`: Chaos simulation testing network stalls, abrupt context cancellations, atomic rollbacks, and concurrent downloads ([PR #9](https://github.com/gabrielima7/ag-up/pull/9), [PR #12](https://github.com/gabrielima7/ag-up/pull/12)).
  - `internal/ui/ui_test.go`: Unit tests validating interactive terminal input handling, reader lifecycle, and cancellation behavior.
  - `internal/checker/checker_test.go`: HTTP mock server tests verifying fast-fail behaviors, ETag handling, and status parsing.
- **DTO Deserialization Safety (`internal/manifest`):** Introduced `manifestDTO` for `manifest.Load()`, matching `saveLocked()` to isolate `sync.RWMutex` from reflection during JSON decoding ([PR #39](https://github.com/gabrielima7/ag-up/pull/39)).
- **Bounded HTTP Body Draining:** Added post-decode draining (`io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))`) in `checker.go` to ensure persistent HTTP/1.1 TCP keep-alive connection reuse ([PR #40](https://github.com/gabrielima7/ag-up/pull/40)).

### Changed
- **Version bump:** Updated version constants to `v0.2.0` across `main.go`, `internal/updater/updater.go`, `internal/checker/checker.go`, and documentation.
- **Manifest Struct Receivers:** Refactored `Manifest` methods to use pointer receivers exclusively, preventing mutex copy-by-value bugs detected by `go vet` ([PR #6](https://github.com/gabrielima7/ag-up/pull/6), [PR #7](https://github.com/gabrielima7/ag-up/pull/7)).
- **Interactive Reader Architecture (`internal/ui`):** Re-engineered `interactiveReader` to use request/reply channels paired with explicit reader context (`ir.ctx`), cleanly unblocking and terminating goroutines upon context cancellation or reader close ([PR #8](https://github.com/gabrielima7/ag-up/pull/8), [PR #9](https://github.com/gabrielima7/ag-up/pull/9), [PR #39](https://github.com/gabrielima7/ag-up/pull/39), [PR #40](https://github.com/gabrielima7/ag-up/pull/40)).
- **Closure Argument Passing (`internal/updater`):** Refactored file extraction closures (`extractCLI`, `extractAndInstall`) to explicitly receive loop variables (`tr`, `destPath`, `fileMode`) as arguments instead of capturing outer pointers, improving escape analysis and preventing heap escapes ([PR #38](https://github.com/gabrielima7/ag-up/pull/38), [PR #40](https://github.com/gabrielima7/ag-up/pull/40)).

### Fixed (Security, Concurrency & Stability)
- **Data Races in Manifest:** Eliminated data races in `internal/manifest` by guarding map access with `sync.RWMutex` and decoupling struct serialization/deserialization from reflection via DTOs ([PR #6](https://github.com/gabrielima7/ag-up/pull/6), [PR #10](https://github.com/gabrielima7/ag-up/pull/10), [PR #33](https://github.com/gabrielima7/ag-up/pull/33), [PR #39](https://github.com/gabrielima7/ag-up/pull/39)).
- **Nil Pointer Defenses in Manifest:** Added defensive bounds checks (`if m == nil`) across `Get`, `Set`, `MarkChecked`, and `MarkInstalled` to prevent panics on uninitialized states ([PR #25](https://github.com/gabrielima7/ag-up/pull/25), [PR #26](https://github.com/gabrielima7/ag-up/pull/26)).
- **UI Terminal Deadlocks & Goroutine Leaks:** Fixed channel deadlocks where cancelled contexts or EOF left `readLine` and `pressEnterToContinue` hanging on send/receive operations without consumers ([PR #8](https://github.com/gabrielima7/ag-up/pull/8), [PR #9](https://github.com/gabrielima7/ag-up/pull/9), [PR #11](https://github.com/gabrielima7/ag-up/pull/11), [PR #14](https://github.com/gabrielima7/ag-up/pull/14), [PR #17](https://github.com/gabrielima7/ag-up/pull/17), [PR #25](https://github.com/gabrielima7/ag-up/pull/25), [PR #32](https://github.com/gabrielima7/ag-up/pull/32), [PR #39](https://github.com/gabrielima7/ag-up/pull/39), [PR #40](https://github.com/gabrielima7/ag-up/pull/40)).
- **Zip Slip & Directory Traversal Protection:** Enforced strict path confinement using Go standard library `filepath.IsLocal` to reject absolute paths and parent-traversing tar headers (`../`) ([PR #32](https://github.com/gabrielima7/ag-up/pull/32)).
- **Privilege Escalation Prevention:** Masked tar file modes with `& 0777` during extraction to strip SUID and SGID bits, enforcing unprivileged user execution ([PR #32](https://github.com/gabrielima7/ag-up/pull/32)).
- **Atomic File Operations & TOCTOU Elimination:**
  - Replaced non-atomic `os.Stat` checks with direct atomic operations in desktop launcher synchronization ([PR #25](https://github.com/gabrielima7/ag-up/pull/25)).
  - Ensured all file extractions and configuration writes use unique temporary files (`.tmp.<pid>.<nano>` / `os.CreateTemp`), immediately followed by `os.Chmod` and atomic `os.Rename` ([PR #7](https://github.com/gabrielima7/ag-up/pull/7), [PR #8](https://github.com/gabrielima7/ag-up/pull/8), [PR #11](https://github.com/gabrielima7/ag-up/pull/11), [PR #12](https://github.com/gabrielima7/ag-up/pull/12), [PR #14](https://github.com/gabrielima7/ag-up/pull/14), [PR #17](https://github.com/gabrielima7/ag-up/pull/17)).
  - Enforced explicit `outFile.Close()` checks prior to `os.Rename` to guarantee filesystem sync to disk ([PR #34](https://github.com/gabrielima7/ag-up/pull/34)).
- **TCP Socket & Connection Leaks:** Drained unread HTTP response bodies bounded via `io.LimitReader` on both error and success paths, preventing TCP socket leaks and connection stalls ([PR #26](https://github.com/gabrielima7/ag-up/pull/26), [PR #34](https://github.com/gabrielima7/ag-up/pull/34), [PR #38](https://github.com/gabrielima7/ag-up/pull/38), [PR #40](https://github.com/gabrielima7/ag-up/pull/40)).
- **Network Timeouts & Stalls:** Bound all HTTP requests and tar reader iterations with explicit context timeouts to prevent hanging indefinitely on stalled connections ([PR #6](https://github.com/gabrielima7/ag-up/pull/6), [PR #7](https://github.com/gabrielima7/ag-up/pull/7), [PR #14](https://github.com/gabrielima7/ag-up/pull/14), [PR #33](https://github.com/gabrielima7/ag-up/pull/33), [PR #34](https://github.com/gabrielima7/ag-up/pull/34)).
- **HTTP 4xx Fast-Fail Matching:** Corrected HTTP fast-fail detection in `checker.go` to use exact string matching `HTTP %d` ([PR #32](https://github.com/gabrielima7/ag-up/pull/32)).
- **Safe Symlink Extraction:** Added support for `tar.TypeSymlink` and `tar.TypeLink` with safety boundary constraints, preventing dangling symlink ELOOP crashes ([PR #32](https://github.com/gabrielima7/ag-up/pull/32)).

### Performance & Optimization
- **Buffer Pool (`sync.Pool`):** Retained and optimized a 32KB buffer pool (`copyBufPool`) for tarball extraction streams.
- **Escape Analysis Improvements:** Passing explicit arguments to closures in `updater.go` keeps execution stack-allocated.
- **HTTP Keep-Alive Connection Reuse:** Bounded body draining ensures connection reuse across multiple check operations.

### Dependencies
Updated direct and indirect dependencies to their latest patch versions:
- `github.com/gabriel-vasile/mimetype`: `v1.4.13` → `v1.4.15`
- `github.com/go-playground/universal-translator`: `v0.18.1` → `v0.18.2`
- `github.com/go-playground/validator/v10`: `v10.30.3` → `v10.30.5`
- `github.com/leodido/go-urn`: `v1.4.0` → `v1.5.0`
- `github.com/redis/go-redis/v9`: `v9.21.0` → `v9.22.0`
- `go.uber.org/atomic`: `v1.11.0` → `v1.12.0`
- `golang.org/x/crypto`: `v0.54.0` → `v0.57.0`
- `golang.org/x/net`: `v0.57.0` → `v0.59.0`
- `golang.org/x/sys`: `v0.47.0` → `v0.48.0`
- `golang.org/x/text`: `v0.40.0` → `v0.42.0`
- `golang.org/x/time`: `v0.15.0` → `v0.16.0`
- `google.golang.org/protobuf`: `v1.36.11` → `v1.36.12`

---

### Merged Pull Requests (since v0.1.0)

The following 18 Pull Requests were reviewed, validated, and merged into the project between `v0.1.0` and `v0.2.0`:

| PR # | Title | Merge Date | Details |
| :---: | :--- | :---: | :--- |
| [#40](https://github.com/gabrielima7/ag-up/pull/40) | **Fix concurrency deadlocks and connection leaks** | 2026-09-25 | Drains HTTP response bodies with `io.LimitReader` for connection reuse, optimizes closure escape analysis, and cleans linters. |
| [#39](https://github.com/gabrielima7/ag-up/pull/39) | **fix: resolve data race in manifest loading and UI channel deadlock** | 2026-09-25 | Implements `manifestDTO` to isolate mutex from JSON reflection during `Load()`; adds reader context cancellation in `ui.go`. |
| [#38](https://github.com/gabrielima7/ag-up/pull/38) | **Fix goroutine leaks and optimize escape analysis** | 2026-09-23 | Bounds `io.Copy(io.Discard, resp.Body)` with `LimitReader(4096)`; passes slice buffers explicitly to extraction closures. |
| [#34](https://github.com/gabrielima7/ag-up/pull/34) | **chore: harden updater against edge cases, network stalls, and resource leaks** | 2026-09-17 | Sets explicit transport timeouts in checker; enforces disk flush verification (`Close()`) before atomic renames. |
| [#33](https://github.com/gabrielima7/ag-up/pull/33) | **Refactor for robustness: fix data races and network timeouts** | 2026-09-16 | Fixes data races during JSON serialization via DTO; enforces network deadlines and transport timeouts in downloader. |
| [#32](https://github.com/gabrielima7/ag-up/pull/32) | **Harden ag-up updater with extensive chaos engineering fixes** | 2026-09-16 | Eliminates Zip Slip (`filepath.IsLocal`), masks mode bits (`& 0777`) against SUID/SGID elevation, and adds safe symlink handling. |
| [#31](https://github.com/gabrielima7/ag-up/pull/31) | **Fix Vulnerabilities and Optimize Update Allocations** | 2026-09-13 | Introduces atomic streaming practices, eliminates unnecessary heap allocations, and resolves cancellation leaks. |
| [#26](https://github.com/gabrielima7/ag-up/pull/26) | **Fix map panics, connection leaks, and UI glitches** | 2026-09-05 | Prevents map nil panics, drains unread HTTP response bodies, and fixes interactive terminal error rendering. |
| [#25](https://github.com/gabrielima7/ag-up/pull/25) | **fix: harden codebase by resolving memory leaks and potential nil pointers** | 2026-08-30 | Eliminates TOCTOU file checks in desktop symlinks, adds nil pointer guards in manifest, and enforces atomic permissions. |
| [#17](https://github.com/gabrielima7/ag-up/pull/17) | **Fix concurrency and atomicity flaws in updater** | 2026-08-19 | Synchronizes stdout/stderr via `safePrint`/`printer`, guarantees atomic file replacement, and cancels interactive reader on timeout. |
| [#14](https://github.com/gabrielima7/ag-up/pull/14) | **Fix concurrency, data races, deadlocks, and atomicity** | 2026-08-15 | Resolves ELOOP idempotency issues, replaces predictable temp files with `os.CreateTemp`, and fixes manifest lock leaks. |
| [#12](https://github.com/gabrielima7/ag-up/pull/12) | **fix(updater): enhance fault tolerance, atomicity, and synchronization** | 2026-08-11 | Guards directory swaps with rollback mechanism; synchronizes stdout across concurrent operations using mutex. |
| [#11](https://github.com/gabrielima7/ag-up/pull/11) | **fix: goroutine leak and non-atomic extraction** | 2026-08-10 | Implements atomic directory extraction and closes background UI reader goroutines upon cancellation. |
| [#10](https://github.com/gabrielima7/ag-up/pull/10) | **Fix UI concurrent printing data race and Manifest Save deadlock** | 2026-08-10 | Synchronizes concurrent UI printing and eliminates deadlock during manifest save operations. |
| [#9](https://github.com/gabrielima7/ag-up/pull/9) | **fix: resolve goroutine leaks and implement chaos tests for hardening** | 2026-08-07 | Resolves UI goroutine leaks via `interactiveReader`; adds chaos test suite under high concurrency and cancellation. |
| [#8](https://github.com/gabrielima7/ag-up/pull/8) | **Harden atomic IO, resolve context deadlocks, and ensure data race freedom** | 2026-08-05 | Hardens atomic I/O routines, ensures context-aware stdin reads, and eliminates file descriptor leaks. |
| [#7](https://github.com/gabrielima7/ag-up/pull/7) | **Harden updater implementation for edge case stability** | 2026-08-03 | Patches network and hook timeouts; introduces package-level mutex `safePrint`; enforces atomic write-and-rename on XDG paths. |
| [#6](https://github.com/gabrielima7/ag-up/pull/6) | **fix: resolve manifest data races and bound downloader with context timeouts** | 2026-08-02 | Protects manifest with `sync.RWMutex` via pointer receivers; bounds downloads with `context.WithTimeout`. |

---

## [v0.1.0] - 2026-07-30

### Initial Production Release
- Stable release of `ag-up`, the Google Antigravity Universal Updater.
- 6-option interactive terminal menu with real-time per-step progress feedback.
- Non-interactive flag mode (`--all`, `--cli`, `--ide`, `--hub`, `--check`, `--retries`, `--version`).
- Always-clean installation: unconditionally wipes previous application directory before extraction to eliminate ghost files.
- SHA-512 cryptographic integrity verification on CLI tarballs.
- Bounded parallel updates (3 concurrent workers) via GopherCore `async.Map`.
- Exponential backoff + jitter retry with HTTP 4xx fast-fail via GopherCore `retry`.
- XDG Base Directory specification compliance (`~/.local/bin/`, `~/.local/share/antigravity/`).
- Auto-generation of `.desktop` application launchers for IDE and Hub.
- Signal handling for graceful Ctrl-C / SIGTERM cancellation with temporary file cleanup.
- Full GopherCore library integration (`result`, `retry`, `async`, `logkit`, `jsonutil`, `guard`).
