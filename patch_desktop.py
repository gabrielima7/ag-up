import re

with open("internal/desktop/desktop.go", "r") as f:
    content = f.read()

old_desk = """	tmp, err := os.CreateTemp(dir, base+".tmp.*")
	if err != nil {
		slog.Warn("desktop: sync legacy: cannot create temp file", "path", path, "error", err)
		return false
	}
	tmpPath := tmp.Name()
	// #nosec G703
	defer func() { _ = os.Remove(filepath.Clean(tmpPath)) }()

	// Preserve original permissions.
	// #nosec G703
	if info, err := os.Stat(path); err == nil {
		_ = tmp.Chmod(info.Mode())
	} else {
		_ = tmp.Chmod(0644)
	}"""

new_desk = """	tmp, err := os.CreateTemp(dir, base+".tmp.*")
	if err != nil {
		slog.Warn("desktop: sync legacy: cannot create temp file", "path", path, "error", err)
		return false
	}
	tmpPath := tmp.Name()
	// #nosec G703
	defer func() { _ = os.Remove(filepath.Clean(tmpPath)) }()

	// Preserve original permissions.
	// #nosec G703
	if info, err := os.Stat(path); err == nil {
		// #nosec G302
		_ = tmp.Chmod(info.Mode())
	} else {
		// #nosec G302
		_ = tmp.Chmod(0644)
	}"""

content = content.replace(old_desk, new_desk)

with open("internal/desktop/desktop.go", "w") as f:
    f.write(content)
