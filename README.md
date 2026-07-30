# ag-up · Google Antigravity Universal Updater

[![Go 1.26+](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![GopherCore](https://img.shields.io/badge/powered%20by-GopherCore-orange)](https://github.com/gabrielima7/GopherCore)

> **ag-up** is a production-ready, 100% user-space CLI utility that keeps all
> Google Antigravity products up-to-date on any Linux distribution — without
> ever needing `sudo`.

---

## Features

| Feature | Details |
|---------|---------|
| 🌐 **Web Page Scraper** | Regex parsing on `https://antigravity.google/download` for IDE & Hub links |
| ☁️ **CLI JSON Manifest** | Fetches `{version, url, sha512}` from the Cloud Run auto-updater endpoint |
| ⚡ **Parallel Updates** | Bounded concurrency (3 workers) via GopherCore `async.Map` |
| 🔁 **Resilient Networking** | Exponential backoff + jitter via GopherCore `retry`; fast-fails on HTTP 4xx |
| 🛡️ **Input Sanitisation** | All paths and tar entries sanitised via GopherCore `guard`; directory-traversal protection |
| 🔐 **SHA-512 Verification** | Cryptographic integrity check on every CLI tarball before extraction |
| 🧹 **Always-Clean Install** | Wipes previous `dataDir` with `os.RemoveAll` before every extraction — no ghost files |
| 📋 **Structured Logging** | JSON logs via GopherCore `logkit` / `log/slog` (WARN+ only in interactive mode) |
| 📁 **XDG Compliant** | Binaries in `~/.local/bin/`, data in `~/.local/share/antigravity/` |
| 🖥️ **Desktop Integration** | Auto-generates `.desktop` launchers for GUI apps (IDE & Hub) |
| 🗺️ **Version Manifest** | Fast JSON persistence via GopherCore `jsonutil` |
| 🎛️ **Dual Mode** | Interactive terminal menu **or** non-interactive CLI flags |
| 📊 **Real-time Feedback** | Per-step progress prints (`[app_id] Downloading…`, `Extracting…`, `Installing…`) |

---

## Managed Applications

| App | Binary | Flag |
|-----|--------|------|
| Google Antigravity CLI | `agy` | `--cli` |
| Google Antigravity IDE | `antigravity-ide` | `--ide` |
| Google Antigravity Hub (2.0) | `antigravity-hub` | `--hub` |

---

## Installation

### Prerequisites

- **Go 1.26+** (`go version`)
- Linux x86-64 (any distribution: Debian, Ubuntu, Fedora, Arch, Zorin OS, openSUSE…)

### Build from Source

```bash
# Clone the repository
git clone https://github.com/gabrielima7/ag-up.git
cd ag-up

# Download dependencies
go mod tidy

# Build the binary
go build -o ag-up .

# (Optional) Build with version metadata embedded
go build -ldflags "-X main.Version=v0.1.0" -o ag-up .

# Install to ~/.local/bin/ (XDG user binary directory)
mkdir -p ~/.local/bin
cp ag-up ~/.local/bin/

# Ensure ~/.local/bin is on your PATH
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

---

## Usage

### Interactive Mode (default)

Simply run without flags to launch the menu:

```
$ ag-up

  ╔═══════════════════════════════════════════════╗
  ║       ag-up · Google Antigravity Updater      ║
  ║              Universal Updater v0.1.0         ║
  ╚═══════════════════════════════════════════════╝

  Choose an option:

    1) Check for updates          (dry-run)
    2) Update All                 (parallel)
    3) Update Antigravity CLI     (agy)
    4) Update Antigravity IDE
    5) Update Antigravity Hub     (2.0)
    6) Exit

  →
```

After selecting an update option, real-time per-step feedback is printed:

```
  [antigravity-hub] Checking remote version...
  [antigravity-hub] Downloading v2.4.3...
  [antigravity-hub] Extracting...
  [antigravity-hub] Installing...
  [antigravity-hub] ✓ Done (v2.4.3)
```

Each result table is followed by a `Press [Enter] to return to the menu...` pause so results are never pushed off-screen.

### Non-Interactive Flag Mode

```bash
# Check all versions without downloading anything
ag-up --check

# Update everything concurrently
ag-up --all

# Update only the CLI (agy)
ag-up --cli

# Update only the IDE
ag-up --ide

# Update only the Hub
ag-up --hub

# Update CLI with 5 retry attempts
ag-up --cli --retries 5

# Print ag-up version
ag-up --version
```

### All Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | Update all applications concurrently |
| `--cli` | bool | false | Update Antigravity CLI (`agy`) only |
| `--ide` | bool | false | Update Antigravity IDE only |
| `--hub` | bool | false | Update Antigravity Hub 2.0 only |
| `--check` | bool | false | Dry-run: check versions without downloading |
| `--retries` | int | 3 | Maximum HTTP retry attempts (range: 1–10) |
| `--version` | bool | false | Print `ag-up` version and exit |

> **Note:** The `--force` flag has been removed. Every `ag-up` invocation now
> performs a clean install unconditionally — the previous installation directory
> is wiped before extraction, guaranteeing file integrity on every run.

---

## Update Behaviour

ag-up always performs a **clean install** regardless of whether the remote version
matches the locally recorded version. On every run:

1. **Fetch** the latest version info from the remote source.
2. **Download** the `.tar.gz` tarball.
3. **Verify** SHA-512 checksum (CLI only; provided by the Cloud Run manifest).
4. **Wipe** the previous `~/.local/share/antigravity/<app-id>/` directory entirely
   (`os.RemoveAll`), preventing ghost files from accumulating across releases.
5. **Extract** all files with their original permissions preserved.
6. **Symlink** `~/.local/bin/<app-id>` → detected main binary inside the data directory.
7. **Generate** a `.desktop` launcher (GUI apps only).
8. **Persist** the new version to `~/.local/share/antigravity/.manifest.json`.

---

## XDG Directory Layout

```
~/.local/
├── bin/
│   ├── agy                          ← Antigravity CLI binary
│   ├── antigravity-ide              ← Symlink → IDE Electron binary
│   └── antigravity-hub              ← Symlink → Hub Electron binary
└── share/
    ├── antigravity/
    │   ├── .manifest.json           ← Version manifest (jsonutil)
    │   ├── agy/                     ← CLI app data
    │   ├── antigravity-ide/         ← IDE application files
    │   └── antigravity-hub/         ← Hub application files
    └── applications/
        ├── antigravity-ide.desktop  ← XDG launcher (IDE)
        └── antigravity-hub.desktop  ← XDG launcher (Hub)
```

---

## Manifest File

`~/.local/share/antigravity/.manifest.json` is maintained automatically:

```json
{
  "apps": {
    "agy": {
      "installed_version": "v1.1.8",
      "etag": "\"abc123\"",
      "last_checked": "2026-07-30T12:00:00Z",
      "last_updated": "2026-07-30T12:00:00Z"
    },
    "antigravity-ide": {
      "installed_version": "v2.1.1",
      "etag": "\"def456\"",
      "last_checked": "2026-07-30T12:00:00Z",
      "last_updated": "2026-07-30T12:00:00Z"
    },
    "antigravity-hub": {
      "installed_version": "v2.4.3",
      "etag": "\"ghi789\"",
      "last_checked": "2026-07-30T12:00:00Z",
      "last_updated": "2026-07-30T12:00:00Z"
    }
  },
  "updated_at": "2026-07-30T12:00:00Z"
}
```

---

## Automated Updates with Crontab

Schedule ag-up to run automatically using `crontab -e`:

```cron
# Update everything daily at 08:00
0 8 * * * /home/$USER/.local/bin/ag-up --all >> /home/$USER/.local/share/antigravity/update.log 2>&1

# Check versions every Monday at 09:00 (dry-run only, no downloads)
0 9 * * 1 /home/$USER/.local/bin/ag-up --check >> /home/$USER/.local/share/antigravity/check.log 2>&1

# Update only the CLI (agy) every night at midnight
0 0 * * * /home/$USER/.local/bin/ag-up --cli >> /home/$USER/.local/share/antigravity/agy-update.log 2>&1
```

> **Tip:** Use `systemd --user` timers as a modern alternative to cron:
>
> ```bash
> mkdir -p ~/.config/systemd/user
>
> cat > ~/.config/systemd/user/ag-up.service << 'EOF'
> [Unit]
> Description=Google Antigravity Universal Updater
>
> [Service]
> Type=oneshot
> ExecStart=%h/.local/bin/ag-up --all
> EOF
>
> cat > ~/.config/systemd/user/ag-up.timer << 'EOF'
> [Unit]
> Description=Run ag-up daily
>
> [Timer]
> OnCalendar=daily
> Persistent=true
>
> [Install]
> WantedBy=timers.target
> EOF
>
> systemctl --user enable --now ag-up.timer
> systemctl --user list-timers ag-up.timer
> ```

---

## GopherCore Integration

ag-up deeply integrates [`github.com/gabrielima7/GopherCore`](https://github.com/gabrielima7/GopherCore):

| Package | Where Used | Purpose |
|---------|-----------|---------|
| `result` | `checker`, `updater`, `main` | Wrap step outputs as `Result[T]` — no panics on unwrap |
| `retry` | `checker`, `updater` | Exponential backoff + jitter; fast-fails on permanent HTTP 4xx |
| `async` | `checker.CheckAll`, `updater.UpdateAll` | Bounded parallel execution (concurrency=3) |
| `logkit` | `main` | Initialise global structured JSON logger (WARN+ in interactive mode) |
| `jsonutil` | `manifest` | Fast JSON encoding/decoding of `.manifest.json` |
| `guard` | `updater`, `desktop` | Sanitise all paths and user-controlled strings |

---

## Architecture

```
ag-up/
├── main.go                    ← Entry point, flag parsing, signal handling, mode dispatch
├── go.mod
├── go.sum
├── README.md
├── internal/
│   ├── config/config.go       ← AppSpec definitions (agy, IDE, Hub) + endpoints
│   ├── manifest/manifest.go   ← Local JSON manifest R/W (jsonutil, atomic write)
│   ├── checker/checker.go     ← Remote version checks (retry + result + 4xx fast-fail)
│   ├── updater/
│   │   ├── updater.go         ← Download → verify → extract → symlink → manifest
│   │   └── downloader.go      ← HTTP download with retry, shared client, temp-file cleanup
│   ├── desktop/desktop.go     ← XDG .desktop launcher generator (guard + template)
│   └── ui/ui.go               ← Interactive terminal menu (bufio.Reader, press-Enter pause)
└── pkg/
    └── xdg/xdg.go             ← XDG Base Directory path helpers (os.UserHomeDir)
```

### Key Design Decisions

- **Always-overwrite:** Every update unconditionally wipes and reinstalls. No version-match skip logic exists in the codebase.
- **Delete-before-create:** Both extraction paths (`extractCLI`, `extractAndInstall`) call `os.Remove(destPath)` before opening any file for writing, preventing `ELOOP` errors from dangling symlinks left after `os.RemoveAll(dataDir)`.
- **Package-level HTTP client:** `downloader.go` uses a single `var downloaderClient` shared across all retry attempts, preserving TCP connection pooling and TLS sessions.
- **4xx fast-fail in retry:** Both `checker.go` and `downloader.go` use `isNonRetryableHTTPError` to immediately abort on HTTP 400/401/403/404 instead of burning retry budget.
- **bufio.Reader for TTY input:** The interactive menu uses `bufio.NewReader(os.Stdin).ReadString('\n')` + `strings.TrimSpace`, which handles `\r\n`, partial reads, and TTY-specific buffering edge cases that `bufio.Scanner` is susceptible to.

---

## License

MIT License — see [LICENSE](LICENSE) for details.

---

## Contributing

Contributions welcome! Please open an issue or PR on GitHub.