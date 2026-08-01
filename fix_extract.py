import re
import os

with open("internal/updater/updater.go", "r") as f:
    content = f.read()

# For extractCLI:
# Find: destPath := filepath.Join(binDir, spec.BinaryName)
# Change to write to temp path, then atomic rename.
new_extractCLI = """		// Found the target binary — write it to ~/.local/bin/<BinaryName>.
		destPath := filepath.Join(binDir, spec.BinaryName)
		tmpDestPath := destPath + ".tmp"
		_ = os.Remove(tmpDestPath)
		outFile, err := os.Create(tmpDestPath)
		if err != nil {
			return fmt.Errorf("updater: create %q: %w", tmpDestPath, err)
		}

		// #nosec G110 — tarball size is capped by the download timeout.
		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
            _ = os.Remove(tmpDestPath)
			return fmt.Errorf("updater: write %q: %w", tmpDestPath, err)
		}
		outFile.Close()

		if err := os.Chmod(tmpDestPath, 0755); err != nil {
            _ = os.Remove(tmpDestPath)
			return fmt.Errorf("updater: chmod %q: %w", tmpDestPath, err)
		}

        if err := os.Rename(tmpDestPath, destPath); err != nil {
            _ = os.Remove(tmpDestPath)
            return fmt.Errorf("updater: rename %q to %q: %w", tmpDestPath, destPath, err)
        }"""

# Using regex to replace the old block in extractCLI
old_extractCLI = """		// Found the target binary — write it to ~/.local/bin/<BinaryName>.
		destPath := filepath.Join(binDir, spec.BinaryName)
		// Remove any pre-existing file or dangling symlink at the destination.
		// os.Create follows symlinks; if the symlink is dangling (e.g. after
		// os.RemoveAll on the previous dataDir) it returns ELOOP.
		_ = os.Remove(destPath)
		outFile, err := os.Create(destPath)
		if err != nil {
			return fmt.Errorf("updater: create %q: %w", destPath, err)
		}

		// #nosec G110 — tarball size is capped by the download timeout.
		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			return fmt.Errorf("updater: write %q: %w", destPath, err)
		}
		outFile.Close()

		if err := os.Chmod(destPath, 0755); err != nil {
			return fmt.Errorf("updater: chmod %q: %w", destPath, err)
		}"""

content = content.replace(old_extractCLI, new_extractCLI)


# For extractAndInstall:
# We should extract the data dir to a tmp location.
# Replace:
# 	if err := os.RemoveAll(dataDir); err != nil { ...
# 	if err := os.MkdirAll(dataDir, 0755); err != nil { ...
# With extracting to dataDir + ".tmp"
old_extractAndInstall_setup = """	// --- Clean installation wipe ---
	// Remove the previous version directory entirely so that files deleted or
	// moved in the new release do not persist as ghost entries.
	slog.Info("updater: wiping previous installation directory", "app_id", spec.ID, "path", dataDir)
	if err := os.RemoveAll(dataDir); err != nil {
		return "", fmt.Errorf("updater: remove previous install dir %q: %w", dataDir, err)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("updater: recreate install dir %q: %w", dataDir, err)
	}"""

new_extractAndInstall_setup = """	// Extract to a temporary directory first.
	tmpDataDir := dataDir + ".tmp"
	_ = os.RemoveAll(tmpDataDir)
	if err := os.MkdirAll(tmpDataDir, 0755); err != nil {
		return "", fmt.Errorf("updater: create tmp install dir %q: %w", tmpDataDir, err)
	}

	// We will also accumulate bin files to atomic rename later
	type binRename struct {
		src string
		dst string
	}
	var binRenames []binRename

    // We defer cleanup of the tmp directory in case of failure.
    // If successful, we'll set a flag so it's not removed.
    success := false
    defer func() {
        if !success {
            os.RemoveAll(tmpDataDir)
            for _, r := range binRenames {
                os.Remove(r.src)
            }
        }
    }()"""

content = content.replace(old_extractAndInstall_setup, new_extractAndInstall_setup)

# Inside the loop:
old_extractAndInstall_loop_dir = """		case tar.TypeDir:
			// Create sub-directories inside dataDir.
			dirPath := filepath.Join(dataDir, cleanName)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return "", fmt.Errorf("updater: mkdir %q: %w", dirPath, err)
			}"""

new_extractAndInstall_loop_dir = """		case tar.TypeDir:
			// Create sub-directories inside tmpDataDir.
			dirPath := filepath.Join(tmpDataDir, cleanName)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				return "", fmt.Errorf("updater: mkdir %q: %w", dirPath, err)
			}"""

content = content.replace(old_extractAndInstall_loop_dir, new_extractAndInstall_loop_dir)

old_extractAndInstall_loop_reg = """		case tar.TypeReg:
			baseName := filepath.Base(cleanName)
			var destPath string

			// The primary binary goes to ~/.local/bin/; everything else to dataDir.
			if baseName == spec.BinaryName {
				destPath = filepath.Join(binDir, baseName)
			} else {
				destPath = filepath.Join(dataDir, cleanName)
				// Ensure parent directory exists.
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return "", fmt.Errorf("updater: mkdir for %q: %w", destPath, err)
				}
			}

			// Preserve the exact file mode from the tar header.
			// os.Create hardcodes 0666 and silently drops execute bits on auxiliary
			// binaries (e.g. resources/bin/language_server). Using os.OpenFile with
			// the header's mode followed by an explicit os.Chmod bypasses umask too.
			fileMode := hdr.FileInfo().Mode()
			// Remove any pre-existing file or dangling symlink before writing.
			// This is critical when destPath is in ~/.local/bin/ and points to a
			// file inside dataDir that was just wiped by os.RemoveAll — os.OpenFile
			// would follow the dangling symlink and return ELOOP.
			_ = os.Remove(destPath)
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fileMode)
			if err != nil {
				return "", fmt.Errorf("updater: create %q: %w", destPath, err)
			}

			// #nosec G110 — tarball size is capped by the download timeout.
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", fmt.Errorf("updater: write %q: %w", destPath, err)
			}
			outFile.Close()

			// Explicit chmod after write — safety net against umask stripping +x.
			if err := os.Chmod(destPath, fileMode); err != nil {
				return "", fmt.Errorf("updater: chmod %q: %w", destPath, err)
			}"""

new_extractAndInstall_loop_reg = """		case tar.TypeReg:
			baseName := filepath.Base(cleanName)
			var destPath string
            var targetPath string

			// The primary binary goes to ~/.local/bin/; everything else to tmpDataDir.
			if baseName == spec.BinaryName {
				targetPath = filepath.Join(binDir, baseName)
                destPath = targetPath + ".tmp"
                binRenames = append(binRenames, binRename{src: destPath, dst: targetPath})
			} else {
				destPath = filepath.Join(tmpDataDir, cleanName)
                targetPath = filepath.Join(dataDir, cleanName) // For actualBinaryPath later
				// Ensure parent directory exists.
				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					return "", fmt.Errorf("updater: mkdir for %q: %w", destPath, err)
				}
			}

			fileMode := hdr.FileInfo().Mode()
			_ = os.Remove(destPath)
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, fileMode)
			if err != nil {
				return "", fmt.Errorf("updater: create %q: %w", destPath, err)
			}

			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", fmt.Errorf("updater: write %q: %w", destPath, err)
			}
			outFile.Close()

			if err := os.Chmod(destPath, fileMode); err != nil {
				return "", fmt.Errorf("updater: chmod %q: %w", destPath, err)
			}"""

content = content.replace(old_extractAndInstall_loop_reg, new_extractAndInstall_loop_reg)

old_extractAndInstall_loop_end = """			if baseName == spec.BinaryName {
				slog.Info("updater: installed binary",
					"app_id", spec.ID,
					"path", destPath,
				)
			}

			if fileMode&0111 != 0 {
				// Phase 1: exact match — the Electron host binary used by both Hub and IDE.
				if baseName == "antigravity" && actualBinaryPath == "" {
					actualBinaryPath = destPath
				}

				// Accumulate candidates for phase 2 (loose match), excluding
				// internal tool directories that must never be symlinked.
				slashPath := filepath.ToSlash(cleanName)
				if !strings.Contains(slashPath, "/resources/") &&
					!strings.Contains(slashPath, "/locales/") {
					candidateBinaries = append(candidateBinaries, destPath)
				}
			}
		}
	}"""

new_extractAndInstall_loop_end = """			if baseName == spec.BinaryName {
				slog.Info("updater: installed binary",
					"app_id", spec.ID,
					"path", targetPath,
				)
			}

			if fileMode&0111 != 0 {
				// Phase 1: exact match — the Electron host binary used by both Hub and IDE.
				if baseName == "antigravity" && actualBinaryPath == "" {
					actualBinaryPath = targetPath
				}

				// Accumulate candidates for phase 2 (loose match), excluding
				// internal tool directories that must never be symlinked.
				slashPath := filepath.ToSlash(cleanName)
				if !strings.Contains(slashPath, "/resources/") &&
					!strings.Contains(slashPath, "/locales/") {
					candidateBinaries = append(candidateBinaries, targetPath)
				}
			}
		}
	}"""

content = content.replace(old_extractAndInstall_loop_end, new_extractAndInstall_loop_end)

# We need to do the atomic replace at the end.
old_extractAndInstall_end = """	return actualBinaryPath, nil
}"""

new_extractAndInstall_end = """	// --- Perform Atomic Replace ---
    slog.Info("updater: atomic replace of installation directory", "app_id", spec.ID, "path", dataDir)

    // Attempt to remove existing dir
    if err := os.RemoveAll(dataDir); err != nil {
        return "", fmt.Errorf("updater: failed to remove existing data dir %q: %w", dataDir, err)
    }

    if err := os.Rename(tmpDataDir, dataDir); err != nil {
        return "", fmt.Errorf("updater: failed to rename %q to %q: %w", tmpDataDir, dataDir, err)
    }

    for _, r := range binRenames {
        _ = os.Remove(r.dst)
        if err := os.Rename(r.src, r.dst); err != nil {
             return "", fmt.Errorf("updater: failed to rename %q to %q: %w", r.src, r.dst, err)
        }
    }

    success = true
	return actualBinaryPath, nil
}"""

content = content.replace(old_extractAndInstall_end, new_extractAndInstall_end)

with open("internal/updater/updater.go", "w") as f:
    f.write(content)
