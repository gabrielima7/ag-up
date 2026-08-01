import re
import os

with open("internal/updater/updater.go", "r") as f:
    content = f.read()

# We need to make the tmp paths unique per goroutine / process.
# In extractCLI, we can use a unique temp path (e.g. including time or pid).
# We can use os.MkdirTemp or a unique suffix. Let's just use a unique suffix based on a timestamp or random number.
# Or better, we can generate a random string, but time.Now().UnixNano() is easier.
# Let's import time if not present and use it. Wait, time is not imported in updater.go.
# We can use os.Getpid() maybe? But wait, what if multiple goroutines in the same process update the same app?
# UpdateAll calls async.Map but each app spec is unique, so spec.ID is unique per Map iteration. Wait... in the test, we launched updater.UpdateAll twice concurrently with the SAME specs!
# That means two goroutines are updating "agy" at the same time.
# So `destPath + ".tmp"` is used by both goroutines! They overwrite each other's tmp file!
# To fix this, we should add a unique identifier to the tmp file/dir name, like `tmpDataDir := dataDir + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())`

# We need to add "time" to imports in updater.go if not there. Let's check imports.
if '"time"' not in content:
    content = content.replace('"archive/tar"', '"archive/tar"\n\t"time"')

content = content.replace('tmpDestPath := destPath + ".tmp"', 'tmpDestPath := destPath + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())')
content = content.replace('tmpDataDir := dataDir + ".tmp"', 'tmpDataDir := dataDir + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())')
content = content.replace('destPath = targetPath + ".tmp"', 'destPath = targetPath + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())')


with open("internal/updater/updater.go", "w") as f:
    f.write(content)
