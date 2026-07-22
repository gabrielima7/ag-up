// Package config defines the specifications for each Antigravity application
// that ag-up is capable of managing. Each AppSpec fully describes an app:
// its identity, download source, web release pages, binary name, and desktop metadata.
package config

// ---------------------------------------------------------------------------
// Official Antigravity URLs & Endpoints
// ---------------------------------------------------------------------------

const (
	// ReleasesPageURL is the official release notes web page for Antigravity.
	ReleasesPageURL = "https://antigravity.google/releases"

	// ChangelogURL is the official changelog web page for Antigravity.
	ChangelogURL = "https://antigravity.google/changelog"

	// Primary Download Endpoints
	CLIDownloadURL = "https://antigravity.google/download/linux/x64/cli"
	IDEDownloadURL = "https://antigravity.google/download/linux/x64/ide"
	HubDownloadURL = "https://antigravity.google/download/linux/x64/hub"

	// GitHub Fallback Download URL Templates
	CLIFallbackURL = "https://github.com/gabrielima7/agy/releases/download/{version}/agy-linux-amd64.tar.gz"
	IDEFallbackURL = "https://github.com/gabrielima7/antigravity-ide/releases/download/{version}/antigravity-ide-linux-amd64.tar.gz"
	HubFallbackURL = "https://github.com/gabrielima7/antigravity-hub/releases/download/{version}/antigravity-hub-linux-amd64.tar.gz"
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

	// ReleasesPageURL is the official releases web page URL scraped for version info.
	ReleasesPageURL string

	// ChangelogURL is the official changelog web page URL scraped as a fallback.
	ChangelogURL string

	// DownloadURLTemplate is the primary download endpoint or template URL for the release archive.
	DownloadURLTemplate string

	// FallbackURLTemplate is the secondary/fallback download URL template (e.g. GitHub Releases mirror).
	FallbackURLTemplate string

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
var CLIApp = AppSpec{
	ID:                  "agy",
	Name:                "Google Antigravity CLI (agy)",
	BinaryName:          "agy",
	ReleasesPageURL:     ReleasesPageURL,
	ChangelogURL:        ChangelogURL,
	DownloadURLTemplate: CLIDownloadURL,
	FallbackURLTemplate: CLIFallbackURL,
	IsGUI:               false,
}

// IDEApp is the specification for the Google Antigravity IDE.
var IDEApp = AppSpec{
	ID:                  "antigravity-ide",
	Name:                "Google Antigravity IDE",
	BinaryName:          "antigravity-ide",
	ReleasesPageURL:     ReleasesPageURL,
	ChangelogURL:        ChangelogURL,
	DownloadURLTemplate: IDEDownloadURL,
	FallbackURLTemplate: IDEFallbackURL,
	IsGUI:               true,
	DesktopIcon:         "antigravity-ide",
	Categories:          "Development;IDE;",
	Comment:             "Google Antigravity IDE — AI-powered development environment",
	StartupWMClass:      "AntigravityIDE",
}

// HubApp is the specification for the Google Antigravity Hub 2.0.
var HubApp = AppSpec{
	ID:                  "antigravity-hub",
	Name:                "Google Antigravity Hub (2.0)",
	BinaryName:          "antigravity-hub",
	ReleasesPageURL:     ReleasesPageURL,
	ChangelogURL:        ChangelogURL,
	DownloadURLTemplate: HubDownloadURL,
	FallbackURLTemplate: HubFallbackURL,
	IsGUI:               true,
	DesktopIcon:         "antigravity-hub",
	Categories:          "Network;FileTransfer;",
	Comment:             "Google Antigravity Hub 2.0 — Unified collaboration and distribution platform",
	StartupWMClass:      "AntigravityHub",
}
