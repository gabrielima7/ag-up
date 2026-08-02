package manifest

import (
	"sync"
	"testing"
)

func TestManifestRace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newManifest()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			MarkChecked(&m, "agy", "etag")
		}(i)
	}
	wg.Wait()
}
