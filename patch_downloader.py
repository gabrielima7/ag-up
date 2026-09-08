import re

with open("internal/updater/downloader.go", "r") as f:
    content = f.read()

old_dl = """	f, err := os.CreateTemp(os.TempDir(), fmt.Sprintf("ag-up-%s-*.tar.gz", safeID))
	if err != nil {
		return "", fmt.Errorf("downloader: create temp file for %q: %w", appID, err)
	}
	if err := f.Chmod(0600); err != nil {"""

new_dl = """	f, err := os.CreateTemp(os.TempDir(), fmt.Sprintf("ag-up-%s-*.tar.gz", safeID))
	if err != nil {
		return "", fmt.Errorf("downloader: create temp file for %q: %w", appID, err)
	}
	// #nosec G302
	if err := f.Chmod(0600); err != nil {"""

content = content.replace(old_dl, new_dl)

with open("internal/updater/downloader.go", "w") as f:
    f.write(content)
