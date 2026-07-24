// Package config defines the specifications for each Antigravity application
// that ag-up is capable of managing. Each AppSpec fully describes an app:
// its identity, download source, web release pages, binary name, and desktop metadata.
package config

// ---------------------------------------------------------------------------
// Official Antigravity Production Endpoints
// ---------------------------------------------------------------------------

const (
	// CLIManifestURL is the Cloud Run JSON manifest endpoint for the Antigravity CLI.
	// Returns: {"version":"1.1.x","url":"...tar.gz","sha512":"..."}
	CLIManifestURL = "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app/manifests/linux_amd64.json"

	// WebReleasesPageURL is the official download page for Antigravity IDE and Hub.
	// Contains <section id="antigravity-2"> for Hub and <section id="antigravity-ide"> for IDE.
	WebReleasesPageURL = "https://antigravity.google/download"

	// ReleasesPageURL is the official release notes web page for Antigravity.
	ReleasesPageURL = "https://antigravity.google/releases"

	// ChangelogURL is the official changelog web page for Antigravity.
	ChangelogURL = "https://antigravity.google/changelog"
)

// ---------------------------------------------------------------------------
// AppSpec
// ---------------------------------------------------------------------------

// AppSpec describes a single Antigravity application managed by ag-up.
type AppSpec struct {
	// ID is the unique, machine-readable identifier used in manifest keys,
	// XDG data directories, and CLI flags (e.g., "agy", "antigravity-ide", "antigravity-hub").
	ID string

	// Name is the human-readable display name shown in menus and reports.
	Name string

	// BinaryName is the name of the executable file placed in ~/.local/bin/.
	BinaryName string

	// ManifestURL is the JSON manifest endpoint for this app (non-empty for CLIApp only).
	// The manifest returns {version, url, sha512} and is served by the Cloud Run auto-updater.
	ManifestURL string

	// WebReleasePage is the HTML download page URL to scrape for download links (IDE and Hub).
	WebReleasePage string

	// SectionID is the HTML element id= that scopes the download link search within WebReleasePage.
	// Example: "antigravity-2" for Hub, "antigravity-ide" for IDE.
	SectionID string

	// TarballInnerName is the exact filename of the binary inside the downloaded tarball.
	// For CLIApp: "antigravity" (must be renamed to BinaryName="agy" on install).
	// For IDE/Hub: empty string (uses generic multi-file extraction).
	TarballInnerName string

	// IsGUI indicates whether the application is a graphical (GUI) app.
	// When true, a .desktop launcher is generated in ~/.local/share/applications/.
	IsGUI bool

	// DesktopIcon is the icon name or absolute path for the .desktop file.
	DesktopIcon string

	// Categories is the XDG desktop entry Categories value.
	Categories string

	// Comment is the short description placed in the .desktop Comment field.
	Comment string

	// StartupWMClass is the StartupWMClass hint for the .desktop file.
	StartupWMClass string
}

// ---------------------------------------------------------------------------
// Registry Helpers
// ---------------------------------------------------------------------------

// All returns the canonical, ordered list of all Antigravity applications
// managed by ag-up. The order determines display order in menus and parallel ops.
func All() []AppSpec {
	return []AppSpec{CLIApp, IDEApp, HubApp}
}

// ByID returns the AppSpec matching the given ID, and a boolean indicating
// whether it was found. Returns (AppSpec{}, false) for unknown IDs.
func ByID(id string) (AppSpec, bool) {
	for _, app := range All() {
		if app.ID == id {
			return app, true
		}
	}
	return AppSpec{}, false
}

// ---------------------------------------------------------------------------
// Application Definitions
// ---------------------------------------------------------------------------

// CLIApp is the specification for the Google Antigravity CLI tool (agy).
// Downloads via Cloud Run JSON manifest; inner tarball binary is named "antigravity"
// and must be renamed to "agy" on install. Post-install runs `agy install`.
var CLIApp = AppSpec{
	ID:               "agy",
	Name:             "Google Antigravity CLI (agy)",
	BinaryName:       "agy",
	ManifestURL:      CLIManifestURL,
	TarballInnerName: "antigravity",
	IsGUI:            false,
}

// IDEApp is the specification for the Google Antigravity IDE.
// Downloads via HTML link scraping from WebReleasesPageURL, scoped to section id="antigravity-ide".
var IDEApp = AppSpec{
	ID:             "antigravity-ide",
	Name:           "Google Antigravity IDE",
	BinaryName:     "antigravity-ide",
	WebReleasePage: WebReleasesPageURL,
	SectionID:      "antigravity-ide",
	IsGUI:          true,
	DesktopIcon:    "antigravity-ide",
	Categories:     "Development;IDE;",
	Comment:        "Google Antigravity IDE — AI-powered development environment",
	StartupWMClass: "AntigravityIDE",
}

// HubApp is the specification for the Google Antigravity Hub 2.0.
// Downloads via HTML link scraping from WebReleasesPageURL, scoped to section id="antigravity-2".
var HubApp = AppSpec{
	ID:             "antigravity-hub",
	Name:           "Google Antigravity Hub (2.0)",
	BinaryName:     "antigravity-hub",
	WebReleasePage: WebReleasesPageURL,
	SectionID:      "antigravity-2",
	IsGUI:          true,
	DesktopIcon:    "antigravity-hub",
	Categories:     "Network;FileTransfer;",
	Comment:        "Google Antigravity Hub 2.0 — Unified collaboration and distribution platform",
	StartupWMClass: "AntigravityHub",
}
