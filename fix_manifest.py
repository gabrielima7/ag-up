import re

with open("internal/manifest/manifest.go", "r") as f:
    content = f.read()

# Add sync import
content = content.replace('"time"\n\n\t"github.com/gabrielima7/GopherCore/jsonutil"', '"sync"\n\t"time"\n\n\t"github.com/gabrielima7/GopherCore/jsonutil"')

# Add mutex to struct
content = content.replace('UpdatedAt time.Time `json:"updated_at"`\n}', 'UpdatedAt time.Time `json:"updated_at"`\n\n\tmu *sync.RWMutex\n}')

# Update newManifest
content = content.replace('func newManifest() Manifest {', 'func newManifest() *Manifest {')
content = content.replace('return Manifest{\n\t\tApps:      make(map[string]AppEntry),\n\t\tUpdatedAt: time.Now(),\n\t}', 'return &Manifest{\n\t\tApps:      make(map[string]AppEntry),\n\t\tUpdatedAt: time.Now(),\n\t\tmu:        &sync.RWMutex{},\n\t}')

# Update Load
content = content.replace('func Load() (Manifest, error) {', 'func Load() (*Manifest, error) {')
content = content.replace('var m Manifest', 'var m Manifest\n\tm.mu = &sync.RWMutex{}')
content = content.replace('return m, nil', 'return &m, nil')

# Update Save
content = content.replace('func Save(m Manifest) error {', 'func Save(m *Manifest) error {\n\tm.mu.Lock()\n\tdefer m.mu.Unlock()')

# Update Get
content = content.replace('func Get(m Manifest, appID string) (AppEntry, bool) {', 'func Get(m *Manifest, appID string) (AppEntry, bool) {\n\tm.mu.RLock()\n\tdefer m.mu.RUnlock()')

# Update Set
content = content.replace('func Set(m *Manifest, appID string, entry AppEntry) error {\n\tm.Apps[appID] = entry\n\treturn Save(*m)', 'func Set(m *Manifest, appID string, entry AppEntry) error {\n\tm.mu.Lock()\n\tm.Apps[appID] = entry\n\tm.mu.Unlock()\n\treturn Save(m)')

# Update MarkChecked
content = content.replace('func MarkChecked(m *Manifest, appID, etag string) error {\n\tentry := m.Apps[appID]\n\tentry.LastChecked = time.Now()\n\tif etag != "" {\n\t\tentry.ETag = etag\n\t}\n\treturn Set(m, appID, entry)', 'func MarkChecked(m *Manifest, appID, etag string) error {\n\tm.mu.Lock()\n\tentry := m.Apps[appID]\n\tentry.LastChecked = time.Now()\n\tif etag != "" {\n\t\tentry.ETag = etag\n\t}\n\tm.mu.Unlock()\n\treturn Set(m, appID, entry)')

# Update MarkInstalled
content = content.replace('func MarkInstalled(m *Manifest, appID, version, etag string) error {\n\tentry := m.Apps[appID]\n\tentry.InstalledVersion = version\n\tentry.ETag = etag\n\tentry.LastChecked = time.Now()\n\tentry.LastUpdated = time.Now()\n\treturn Set(m, appID, entry)', 'func MarkInstalled(m *Manifest, appID, version, etag string) error {\n\tm.mu.Lock()\n\tentry := m.Apps[appID]\n\tentry.InstalledVersion = version\n\tentry.ETag = etag\n\tentry.LastChecked = time.Now()\n\tentry.LastUpdated = time.Now()\n\tm.mu.Unlock()\n\treturn Set(m, appID, entry)')

with open("internal/manifest/manifest.go", "w") as f:
    f.write(content)
