import re

with open("internal/manifest/manifest.go", "r") as f:
    content = f.read()

old_man = """	tmpFile, err := os.CreateTemp(dir, file+".tmp.*")
	if err != nil {
		return fmt.Errorf("manifest: create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmpFile.Chmod(0644); err != nil {"""

new_man = """	tmpFile, err := os.CreateTemp(dir, file+".tmp.*")
	if err != nil {
		return fmt.Errorf("manifest: create temp file in %q: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	// #nosec G302
	if err := tmpFile.Chmod(0644); err != nil {"""

content = content.replace(old_man, new_man)

with open("internal/manifest/manifest.go", "w") as f:
    f.write(content)
