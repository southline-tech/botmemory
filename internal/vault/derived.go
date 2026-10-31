package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/store"
)

const summaryLen = 500

// IndexEntry is the per-key summary kept in projects/<p>/_index.json.
type IndexEntry struct {
	Key       string    `json:"key"`
	Kind      string    `json:"kind,omitempty"`
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	Status    string    `json:"status"`
	Version   int       `json:"version"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
	Summary   string    `json:"summary"` // first 500 chars of body
}

type Index struct {
	Entries map[string]IndexEntry `json:"entries"`
}

// LogEntry is one line of projects/<p>/log.jsonl.
type LogEntry struct {
	TS      time.Time `json:"ts"`
	Bot     string    `json:"bot"`
	Op      string    `json:"op"` // put | append | delete
	Key     string    `json:"key"`
	Kind    string    `json:"kind,omitempty"`
	Version int       `json:"version"`
}

// VersionInfo is one row of history().
type VersionInfo struct {
	Version   int       `json:"version"`
	Status    string    `json:"status"`
	Title     string    `json:"title"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
	Current   bool      `json:"current,omitempty"`
}

type SearchHit struct {
	Project string `json:"project"`
	IndexEntry
}

func indexPath(p string) string { return path.Join("projects", p, "_index.json") }
func logPath(p string) string   { return path.Join("projects", p, "log.jsonl") }
func logSegPath(p string, n int) string {
	return path.Join("projects", p, fmt.Sprintf("log-%04d.jsonl", n))
}

// LogSegmentMax is the size at which log.jsonl is rotated: its contents move
// to the next log-NNNN.jsonl and log.jsonl starts empty. Kept well under the
// Drive backend's 32 MiB read cap so the live log is always fully readable.
var LogSegmentMax = 8 << 20

func entryOf(r *Record) IndexEntry {
	sum := r.Body
	if len([]rune(sum)) > summaryLen {
		sum = string([]rune(sum)[:summaryLen])
	}
	return IndexEntry{Key: r.Key, Kind: r.Kind, Title: r.Title, Tags: nz(r.Tags), Status: r.Status,
		Version: r.Version, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, Summary: sum}
}

// afterWrite updates the index for r and appends the log line, under the
// project lock. The log line is appended even if the index update fails
// (the log is independent of the index). An index failure marks the project
// dirty (rebuilt on next access) and leaves *indexOK=false (it is set true only after the index saves) so endWrite keeps
// the durable marker.
func (v *Vault) afterWrite(ctx context.Context, op, project string, r *Record, indexOK *bool) error {
	defer v.lock("proj:" + project)()
	var errs []error
	if err := v.updateIndex(ctx, project, r); err != nil {
		v.dirty.Store(project, true)
		errs = append(errs, fmt.Errorf("index: %w", err))
	} else {
		*indexOK = true
	}
	line, _ := json.Marshal(LogEntry{TS: r.UpdatedAt, Bot: r.UpdatedBy, Op: op, Key: r.Key, Kind: r.Kind, Version: r.Version})
	if err := v.appendLog(ctx, project, append(line, '\n')); err != nil {
		errs = append(errs, fmt.Errorf("log: %w", err))
	}
	return errors.Join(errs...)
}

// updateIndex applies r to the index incrementally. A full rebuild happens
// only if the project is dirty (marker found at startup, or an earlier
// index failure) or the index is missing/corrupt.
func (v *Vault) updateIndex(ctx context.Context, project string, r *Record) error {
	idx, rebuilt, err := v.loadIndex(ctx, project, false)
	if err != nil {
		return err
	}
	if rebuilt { // a rebuild already reflects r
		return nil
	}
	idx.Entries[r.Key] = entryOf(r)
	return v.saveIndex(ctx, project, idx)
}

// appendLog appends to log.jsonl, rotating it into log-NNNN.jsonl first when
// the append would pass LogSegmentMax. A log the backend refuses to read in
// full (store.ErrTooLarge) is an error, never silently truncated.
// Crash between the segment write and the reset can duplicate lines across
// the segment and log.jsonl; nothing is lost.
func (v *Vault) appendLog(ctx context.Context, project string, line []byte) error {
	old, err := v.b.Read(ctx, logPath(project))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if len(old) > 0 && len(old)+len(line) > LogSegmentMax {
		n, err := v.nextLogSeg(ctx, project)
		if err != nil {
			return err
		}
		if err := v.b.Write(ctx, logSegPath(project, n), old); err != nil {
			return err
		}
		old = nil
	}
	return v.b.Write(ctx, logPath(project), append(old, line...))
}

func (v *Vault) nextLogSeg(ctx context.Context, project string) (int, error) {
	names, err := v.b.List(ctx, path.Join("projects", project))
	if err != nil {
		return 0, err
	}
	max := 0
	for _, n := range names {
		var k int
		if _, err := fmt.Sscanf(n, "log-%04d.jsonl", &k); err == nil && k > max {
			max = k
		}
	}
	return max + 1, nil
}

// loadIndex reads the index, rebuilding from records when it is missing,
// corrupt, marked dirty, or rebuild is forced. A rebuilt index is saved
// (best effort; on failure the project stays dirty and is rebuilt again next
// time). rebuilt reports whether it came from records. Caller must hold the
// project lock.
func (v *Vault) loadIndex(ctx context.Context, project string, rebuild bool) (idx *Index, rebuilt bool, err error) {
	if !rebuild && !v.isDirty(ctx, project) {
		data, err := v.b.Read(ctx, indexPath(project))
		if err == nil {
			var idx Index
			if json.Unmarshal(data, &idx) == nil && idx.Entries != nil {
				return &idx, false, nil
			}
			log.Printf("botmemory: %s: corrupt index; rebuilding from records", project)
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, false, err
		}
	}
	idx, _, err = v.rebuildIndex(ctx, project)
	if err != nil {
		v.dirty.Store(project, true)
		return nil, false, err
	}
	if err := v.saveIndex(ctx, project, idx); err != nil {
		v.dirty.Store(project, true)
		log.Printf("botmemory: %s: saving rebuilt index failed (will rebuild again): %v", project, err)
	} else if err := v.clearDirty(ctx, project); err != nil {
		log.Printf("botmemory: %s: %v (will rebuild again)", project, err)
	}
	return idx, true, nil
}

// rebuildIndex builds an index from records. Only records that exist but
// cannot be parsed (ErrCorrupt) are skipped and logged, and returned as
// skipped. Any other error (transient Drive 5xx/429 after retries,
// store.ErrTooLarge, ctx cancellation/deadline) aborts the rebuild so a
// partial index is never saved; callers leave the project dirty.
func (v *Vault) rebuildIndex(ctx context.Context, project string) (*Index, []string, error) {
	idx := &Index{Entries: map[string]IndexEntry{}}
	dir := path.Join("projects", project, "records")
	names, err := v.b.List(ctx, dir)
	if err != nil {
		return nil, nil, err
	}
	var skipped []string
	for _, n := range names {
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		r, err := v.load(ctx, path.Join(dir, n))
		if errors.Is(err, ErrCorrupt) {
			log.Printf("botmemory: %s: skipping unparseable record %s in index rebuild: %v", project, n, err)
			skipped = append(skipped, n)
			continue
		}
		if errors.Is(err, ErrNotFound) {
			continue // deleted between List and Read
		}
		if err != nil {
			return nil, nil, fmt.Errorf("rebuild index %s: %w", project, err)
		}
		idx.Entries[r.Key] = entryOf(r)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return idx, skipped, nil
}

func (v *Vault) saveIndex(ctx context.Context, project string, idx *Index) error {
	b, _ := json.MarshalIndent(idx, "", "  ")
	return v.b.Write(ctx, indexPath(project), b)
}

// Reindex rebuilds a project's _index.json from its records. Owners only.
// It returns the entry count and the record files skipped as unparseable.
func (v *Vault) Reindex(ctx context.Context, bot, project string) (int, []string, error) {
	if err := validName("project", project); err != nil {
		return 0, nil, err
	}
	if err := v.need(bot, project, acl.Owner); err != nil {
		return 0, nil, err
	}
	defer v.lock("proj:" + project)()
	idx, skipped, err := v.rebuildIndex(ctx, project)
	if err != nil {
		v.dirty.Store(project, true)
		return 0, nil, err
	}
	if err := v.saveIndex(ctx, project, idx); err != nil {
		v.dirty.Store(project, true)
		return 0, nil, err
	}
	if err := v.clearDirty(ctx, project); err != nil {
		return 0, nil, err
	}
	return len(idx.Entries), skipped, nil
}

// History lists every version of a key, newest first.
func (v *Vault) History(ctx context.Context, bot, project, key string) ([]VersionInfo, error) {
	if err := v.checkAddr(project, key); err != nil {
		return nil, err
	}
	if err := v.need(bot, project, acl.Read); err != nil {
		return nil, err
	}
	cur, err := v.load(ctx, recPath(project, key))
	if err != nil {
		return nil, err
	}
	out := []VersionInfo{{cur.Version, cur.Status, cur.Title, cur.UpdatedBy, cur.UpdatedAt, true}}
	names, err := v.b.List(ctx, path.Join("projects", project, "history", key))
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		num, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(n, "v"), ".json"))
		if err != nil || num >= cur.Version {
			continue
		}
		r, err := v.load(ctx, histPath(project, key, num))
		if err != nil {
			return nil, err
		}
		out = append(out, VersionInfo{r.Version, r.Status, r.Title, r.UpdatedBy, r.UpdatedAt, false})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

// Search matches every whitespace-separated term (case-insensitive) against
// key, kind, title, tags and summary in the index of one project, or of every
// project the bot can read when project == "*". Archived hidden unless
// status=archived. Newest first, capped at limit (default 50).
func (v *Vault) Search(ctx context.Context, bot, project, query string, f ListFilter, limit int) ([]SearchHit, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, fmt.Errorf("%w: empty query", ErrInvalid)
	}
	if f.Kind != "" {
		if err := validKind(f.Kind); err != nil {
			return nil, err
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var projects []string
	if project == "*" {
		ps, err := v.ListProjects(ctx, bot)
		if err != nil {
			return nil, err
		}
		projects = make([]string, len(ps))
		for i, p := range ps {
			projects[i] = p.Name
		}
	} else {
		if err := validName("project", project); err != nil {
			return nil, err
		}
		if err := v.need(bot, project, acl.Read); err != nil {
			return nil, err
		}
		projects = []string{project}
	}
	hits := []SearchHit{}
	for _, p := range projects {
		unlock := v.lock("proj:" + p)
		idx, _, err := v.loadIndex(ctx, p, false)
		unlock()
		if err != nil {
			if project != "*" {
				return nil, err
			}
			// One unreadable project must not fail a cross-project search.
			log.Printf("botmemory: search: skipping project %s: %v", p, err)
			continue
		}
		for _, e := range idx.Entries {
			if f.Status == "" && e.Status == StatusArchived || f.Status != "" && e.Status != f.Status ||
				f.Kind != "" && e.Kind != f.Kind || f.Tag != "" && !contains(e.Tags, f.Tag) {
				continue
			}
			hay := strings.ToLower(strings.Join(append([]string{e.Key, e.Kind, e.Title, e.Summary}, e.Tags...), "\n"))
			ok := true
			for _, t := range terms {
				if !strings.Contains(hay, t) {
					ok = false
					break
				}
			}
			if ok {
				hits = append(hits, SearchHit{Project: p, IndexEntry: e})
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].UpdatedAt.After(hits[j].UpdatedAt) })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}
