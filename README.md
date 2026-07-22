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
| 🌐 **Web Page Scraper** | Regex parsing on `https://antigravity.google/releases` & `/changelog` |
| 🔀 **HTTP HEAD Fallback** | Resolves redirect locations & tarball filenames for version tags |
| ⚡ **Parallel Updates** | Bounded concurrency via GopherCore `async.Map` |
| 🔁 **Resilient Networking** | Exponential backoff + jitter via GopherCore `retry` |
| 🛡️ **Input Sanitisation** | All paths sanitised via GopherCore `guard` |
| 📋 **Structured Logging** | JSON logs via GopherCore `logkit` / `log/slog` |
| 📁 **XDG Compliant** | Binaries in `~/.local/bin/`, data in `~/.local/share/antigravity/` |
| 🖥️ **Desktop Integration** | Auto-generates `.desktop` launchers for GUI apps |
| 🗺️ **Version Manifest** | Fast JSON persistence via GopherCore `jsonutil` |
| 🎛️ **Dual Mode** | Interactive terminal menu **or** non-interactive flags |

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
go build -ldflags "-X main.Version=1.0.0" -o ag-up .

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
  ║              Universal Updater v1.0.0         ║
  ╚═══════════════════════════════════════════════╝

  Choose an option:

    1) Check for updates          (dry-run)
    2) Update All                 (parallel)
    3) Update Antigravity CLI     (agy)
    4) Update Antigravity IDE
    5) Update Antigravity Hub     (2.0)
    6) Force Reinstall All        (ignore version)
    7) Exit

  →
```

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

# Update all, even if versions match (force reinstall)
ag-up --all --force

# Update CLI with 5 retry attempts
ag-up --cli --retries 5

# Force reinstall the IDE with custom retries
ag-up --ide --force --retries 10

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
| `--force` | bool | false | Force re-download regardless of version match |
| `--retries` | int | 3 | Maximum HTTP retry attempts (range: 1–10) |
| `--version` | bool | false | Print `ag-up` version and exit |

---

## XDG Directory Layout

```
~/.local/
├── bin/
│   ├── agy                          ← Antigravity CLI binary
│   ├── antigravity-ide              ← IDE binary
│   └── antigravity-hub              ← Hub binary
└── share/
    ├── antigravity/
    │   ├── .manifest.json           ← Version manifest (jsonutil)
    │   ├── agy/                     ← CLI app data
    │   ├── antigravity-ide/         ← IDE app data / resources
    │   └── antigravity-hub/         ← Hub app data / resources
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
      "installed_version": "v1.5.0",
      "etag": "\"abc123\"",
      "last_checked": "2026-07-22T10:00:00Z",
      "last_updated": "2026-07-20T09:00:00Z"
    },
    "antigravity-ide": {
      "installed_version": "v2.0.1",
      "etag": "\"def456\"",
      "last_checked": "2026-07-22T10:00:00Z",
      "last_updated": "2026-07-21T14:00:00Z"
    }
  },
  "updated_at": "2026-07-22T10:00:00Z"
}
```

---

## Automated Updates with Crontab

Schedule ag-up to run automatically using `crontab -e`:

```cron
# Check for updates daily at 08:00 and update everything
0 8 * * * /home/$USER/.local/bin/ag-up --all >> /home/$USER/.local/share/antigravity/update.log 2>&1

# Check versions every Monday at 09:00 (dry-run only, no downloads)
0 9 * * 1 /home/$USER/.local/bin/ag-up --check >> /home/$USER/.local/share/antigravity/check.log 2>&1

# Force reinstall all apps on the 1st of every month at 02:00
0 2 1 * * /home/$USER/.local/bin/ag-up --all --force >> /home/$USER/.local/share/antigravity/update.log 2>&1

# Update only the CLI (agy) every night at midnight
0 0 * * * /home/$USER/.local/bin/ag-up --cli >> /home/$USER/.local/share/antigravity/agy-update.log 2>&1
```

> **Tip:** Use `systemd --user` timers as a modern alternative to cron:
>
> ```bash
> # Create a user systemd timer for ag-up
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

ag-up deeply integrates `github.com/gabrielima7/GopherCore` v0.4.1:

| Package | Where Used | Purpose |
|---------|-----------|---------|
| `result` | `checker`, `updater`, `main` | Wrap step outputs as `Result[T]` — no panics |
| `retry` | `checker`, `updater` | Exponential backoff + jitter for HTTP calls |
| `async` | `checker.CheckAll`, `updater.UpdateAll` | Bounded parallel execution (concurrency=3) |
| `logkit` | `main` | Initialise global structured JSON logger |
| `jsonutil` | `manifest` | Fast JSON encoding/decoding of `.manifest.json` |
| `guard` | `updater`, `desktop` | Sanitise all paths and user-controlled strings |

---

## Architecture

```
ag-up/
├── main.go                    ← Entry point, flag parsing, mode dispatch
├── go.mod
├── go.sum
├── README.md
├── internal/
│   ├── config/config.go       ← AppSpec definitions (agy, IDE, Hub)
│   ├── manifest/manifest.go   ← Local JSON manifest R/W (jsonutil)
│   ├── checker/checker.go     ← Remote version checks (retry + result)
│   ├── updater/updater.go     ← Download → extract → install (async.Map)
│   ├── desktop/desktop.go     ← XDG .desktop launcher generator (guard)
│   └── ui/ui.go               ← Interactive terminal menu (bufio/fmt)
└── pkg/
    └── xdg/xdg.go             ← XDG Base Directory path helpers
```

---

## License

MIT License — see [LICENSE](LICENSE) for details.

---

## Contributing

Contributions welcome! Please open an issue or PR on GitHub.