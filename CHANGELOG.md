# Changelog

All notable changes to `ag-up` (Google Antigravity Universal Updater) will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

## [v0.2.1] - 2026-10-16

### Summary
`v0.2.1` is a resilience, security hardening, and reliability maintenance release. It incorporates 5 merged Pull Requests resolving potential goroutine leaks in the interactive UI, race conditions in manifest serialization, Zip/Tar decompression bomb vulnerabilities (CWE-409), filesystem durability during atomic file operations via `fsync`, and automatic creation of the XDG applications directory. In addition, it integrates `GopherCore v0.5.0` (dual Go 1.26/1.27 compatibility, ultimate chaos load verification, and context-aware circuit breaking) alongside updates across all direct and indirect dependencies.

### Added
- **Decompression Bomb (Zip/Tar Bomb) Defense (`internal/updater`):** Implemented `hardLimitReader` to enforce a 2GB upper bound (`maxFileExtractionLimit`) on any single extracted uncompressed file, neutralizing decompression bomb attacks (CWE-409) while avoiding silent truncation ([PR #52](https://github.com/gabrielima7/ag-up/pull/52)).
- **Comprehensive Unit Tests for Manifest (`internal/manifest`):** Added a dedicated test suite (`internal/manifest/manifest_test.go`) covering 0-byte manifest self-healing, corrupt JSON recovery with structured warning logs, round-trip persistence, and in-memory rollback on disk write failures ([PR #43](https://github.com/gabrielima7/ag-up/pull/43), [PR #44](https://github.com/gabrielima7/ag-up/pull/44)).
- **Decompression Bomb & Hard Limit Unit Tests (`internal/updater`):** Added unit tests in `internal/updater/updater_test.go` (`TestHardLimitReader`, `TestDecompressionBombProtection`) verifying exact-limit reads, EOF handling, beyond-limit error escalation, and reentrant safety ([PR #52](https://github.com/gabrielima7/ag-up/pull/52)).
- **Automatic Directory Provisioning (`internal/desktop`):** Added `os.MkdirAll(appsDir, 0750)` in `desktop.Generate` to guarantee that `~/.local/share/applications/` exists before attempting to write `.desktop` files, preventing installation errors on clean or minimalist Linux systems ([PR #55](https://github.com/gabrielima7/ag-up/pull/55)).

### Changed
- **Version bump:** Updated version constants to `v0.2.1` across `main.go`, `internal/updater/updater.go`, `internal/checker/checker.go`, and documentation.
- **Download Timeouts & Retry Budgets (`internal/updater`):** Increased download per-attempt timeout from 2 minutes to 5 minutes and expanded total download timeout from 5 minutes to 10 minutes in `downloader.go` to provide ample headroom for large downloads over slow network connections and allow full retry loops ([PR #52](https://github.com/gabrielima7/ag-up/pull/52)).
- **Checker Retry & Context Timeouts (`internal/checker`):** Introduced a 15-second per-attempt timeout in the `fetchLatestRelease` retry loop and expanded top-level `Check` context timeout to 2 minutes to accommodate exponential backoff without stalling ([PR #43](https://github.com/gabrielima7/ag-up/pull/43), [PR #44](https://github.com/gabrielima7/ag-up/pull/44)).

### Fixed (Security, Concurrency & Resilience)
- **Physical Durability via `fsync` (`internal/desktop`, `internal/manifest`, `internal/updater`):** Added explicit `f.Sync()` calls prior to `f.Close()` for all temporary files across desktop entry generation (`Generate`, `maybeUpdateDesktopExec`), manifest persistence (`saveLocked`), tarball downloading (`DownloadTarGz`), and application extraction (`extractCLI`, `extractAndInstall`). This guarantees dirty buffers are explicitly flushed to physical disk before atomic `os.Rename` operations, making atomic writes resilient against power outages or system crashes ([PR #50](https://github.com/gabrielima7/ag-up/pull/50)).
- **Data Races in Manifest Persistence (`internal/manifest`):** Cloned `m.Apps` into an isolated map (`dtoApps`) inside `saveLocked` before passing to DTO, preventing race conditions with concurrent readers during JSON serialization reflection ([PR #50](https://github.com/gabrielima7/ag-up/pull/50)).
- **Manifest Self-Healing & In-Memory State Rollback (`internal/manifest`):**
  - Handled empty (0-byte) and corrupted JSON manifests gracefully by resetting to a clean manifest with structured warning logs (`slog.Warn`), preventing fatal updater startup failures ([PR #43](https://github.com/gabrielima7/ag-up/pull/43), [PR #44](https://github.com/gabrielima7/ag-up/pull/44)).
  - Implemented automatic in-memory rollback of application entries in `Set()`, `MarkChecked()`, and `MarkInstalled()` if disk writes (`saveLocked`) fail ([PR #43](https://github.com/gabrielima7/ag-up/pull/43), [PR #44](https://github.com/gabrielima7/ag-up/pull/44)).
- **UI Goroutine Leak Remediation (`internal/ui`):** Fixed potential goroutine leaks in the interactive input reader by passing explicit arguments (`bgCtx`, `reader`, `reqCh`, `readDone`) to goroutine closures to optimize escape analysis, closing `readDone` on exit, and cleanly unblocking upon context cancellation ([PR #42](https://github.com/gabrielima7/ag-up/pull/42)).
- **File Descriptor & Temporary File Cleanup on Windows (`internal/updater`):** Guaranteed `outFile.Close()` is deferred before temporary file removal (`os.Remove`), preventing `Access is denied` file lock errors on Windows during extraction aborts, and tracked successful renames via `removeOnExit` to avoid unnecessary cleanup attempts ([PR #43](https://github.com/gabrielima7/ag-up/pull/43), [PR #44](https://github.com/gabrielima7/ag-up/pull/44)).
- **HTTP Response Nil Pointer Guards (`internal/checker`):** Added explicit `resp == nil` defensive validation checks in `fetchJSON` and `fetchHTML` before response header and body processing, achieving 100% NilAway static analysis compliance.

### Performance & Optimization
- **Pre-Compiled Desktop Entry Template (`internal/desktop`):** Optimized template execution in `desktop.go` by pre-compiling `desktopTemplate` at package initialization via `template.Must(template.New("desktop").Parse(desktopTemplate))`, eliminating redundant compilation allocations on every `.desktop` generation ([PR #42](https://github.com/gabrielima7/ag-up/pull/42)).
- **Desktop Directory Slice Allocation (`internal/desktop`):** Replaced slice loop iteration with direct variadic `append` (staticcheck S1011) when scanning localized Desktop directory candidates.
- **Closure Escape Analysis Optimization (`internal/ui`):** Refactored interactive reader closures to accept parameters explicitly rather than capturing outer pointers, improving stack allocation and reducing heap pressure ([PR #42](https://github.com/gabrielima7/ag-up/pull/42)).

### Dependencies
- Updated `github.com/gabrielima7/GopherCore`: `v0.4.1` → `v0.5.0` (Dual Go 1.26/1.27 compatibility matrix, Ultimate Chaos verification scaling up to 10k goroutines, context-aware circuit breaking, and goroutine leak hardening via `goleak`).
- Updated Go toolchain target: `1.26` → `1.26.9` (mitigating standard library HTTP/2 vulnerabilities).
- Updated direct and indirect dependencies to their latest compatible patch releases:
  - `github.com/go-playground/locales`: `v0.14.1` → `v0.14.2`
  - `github.com/goccy/go-json`: `v0.10.6` → `v0.11.2`
  - `github.com/redis/go-redis/v9`: `v9.22.0` → `v9.23.0`
  - `golang.org/x/crypto`: `v0.57.0` → `v0.58.0`
  - `golang.org/x/net`: `v0.59.0` → `v0.61.0` (via `v0.60.0` in PR #52)
  - `golang.org/x/sys`: `v0.48.0` → `v0.49.0`
  - `golang.org/x/text`: `v0.42.0` → `v0.43.0`

---

### Merged Pull Requests (since v0.2.0)

The following 5 Pull Requests were reviewed, validated, and merged into the project between `v0.2.0` and `v0.2.1`:

| PR # | Title | Merge Date | Details |
| :---: | :--- | :---: | :--- |
| [#55](https://github.com/gabrielima7/ag-up/pull/55) | **fix: ensure applications directory exists before creating desktop shortcut** | 2026-10-16 | Automatically provisions `~/.local/share/applications` with `0750` permissions if missing before generating `.desktop` entries. |
| [#52](https://github.com/gabrielima7/ag-up/pull/52) | **fix: implement zip-bomb protection, update vulnerable dependencies, and harden timeouts** | 2026-10-13 | Introduces `hardLimitReader` to enforce 2GB uncompressed limit per file against decompression bombs, increases download timeouts to 5m/10m, updates `golang.org/x/net` to `v0.60.0` and Go to `1.26.9`, and adds comprehensive unit tests. |
| [#50](https://github.com/gabrielima7/ag-up/pull/50) | **fix(core): Resolve data races in manifest and enforce atomic durability via fsync** | 2026-10-08 | Deep-copies `m.Apps` during serialization to prevent JSON reflection data races, and adds `f.Sync()` calls before `Close()` across all temporary files for atomic write crash resilience. |
| [#43](https://github.com/gabrielima7/ag-up/pull/43), [#44](https://github.com/gabrielima7/ag-up/pull/44) | **fix: harden updater, checker resilience, and manifest self-healing** | 2026-09-30 | Adds 15s per-attempt checker timeouts, self-heals 0-byte/corrupted manifests, implements in-memory rollback on save failure, ensures Windows file unlock before temp file deletion, and adds full manifest unit tests. |
| [#42](https://github.com/gabrielima7/ag-up/pull/42) | **refactor: fix goroutine leak in ui.go and optimize template parsing in desktop.go** | 2026-09-28 | Fixes goroutine leak and escape analysis in interactive input reader via explicit closure arguments and context cancellation; pre-compiles desktop entry template at package initialization. |

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
