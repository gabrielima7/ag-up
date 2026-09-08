import re

with open("internal/manifest/manifest.go", "r") as f:
    content = f.read()

# Fix data races in map accesses in manifest.go
# Wait, the code says `Get`, `Set`, `MarkChecked`, `MarkInstalled` use RWMutex.
# But `saveLocked` doesn't protect reading from `m` because it assumes caller holds lock.
