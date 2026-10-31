package vault

import (
	"sync"
	"time"
)

// IndexCacheTTL is how long list/search may serve a cached index before
// re-reading _index.json from storage (cross-token freshness on one host).
var IndexCacheTTL = 3 * time.Second

type indexCacheEntry struct {
	idx      *Index
	loadedAt time.Time
}

type indexCache struct {
	mu sync.Map // project -> *indexCacheEntry
}

func cloneIndex(src *Index) *Index {
	if src == nil {
		return &Index{Entries: map[string]IndexEntry{}}
	}
	out := &Index{Entries: make(map[string]IndexEntry, len(src.Entries))}
	for k, e := range src.Entries {
		out.Entries[k] = e
	}
	return out
}

func (c *indexCache) get(now time.Time, project string) (*Index, bool) {
	v, ok := c.mu.Load(project)
	if !ok {
		return nil, false
	}
	ent := v.(*indexCacheEntry)
	if IndexCacheTTL > 0 && now.Sub(ent.loadedAt) > IndexCacheTTL {
		c.mu.Delete(project)
		return nil, false
	}
	return cloneIndex(ent.idx), true
}

func (c *indexCache) put(now time.Time, project string, idx *Index) {
	c.mu.Store(project, &indexCacheEntry{idx: cloneIndex(idx), loadedAt: now})
}

func (c *indexCache) drop(project string) {
	c.mu.Delete(project)
}

func (v *Vault) dropIndexCache(project string) {
	v.ic.drop(project)
}

func (v *Vault) putIndexCache(project string, idx *Index) {
	v.ic.put(v.now(), project, idx)
}
