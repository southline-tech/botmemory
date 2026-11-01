package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// indexEtag returns a stable hash over active index entries (key:version pairs,
// sorted by key). Archived keys are omitted unless ListFilter.Status targets them.
func indexEtag(entries []IndexEntry) string {
	pairs := make([]string, 0, len(entries))
	for _, e := range entries {
		pairs = append(pairs, e.Key+":"+itoa(e.Version))
	}
	sort.Strings(pairs)
	sum := sha256.Sum256([]byte(strings.Join(pairs, ",")))
	return hex.EncodeToString(sum[:16])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func filterIndexEntries(idx *Index, f ListFilter) []IndexEntry {
	out := []IndexEntry{}
	for _, e := range idx.Entries {
		if f.Status == "" && e.Status == StatusArchived {
			continue
		}
		if f.Status != "" && e.Status != f.Status {
			continue
		}
		if f.Kind != "" && e.Kind != f.Kind {
			continue
		}
		if f.Tag != "" && !contains(e.Tags, f.Tag) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func maxUpdatedAt(entries []IndexEntry) (t time.Time) {
	for _, e := range entries {
		if e.UpdatedAt.After(t) {
			t = e.UpdatedAt
		}
	}
	return t
}
