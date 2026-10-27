package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/store"
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

func entryOf(r *Record) IndexEntry {
	sum := r.Body
	if len([]rune(sum)) > summaryLen {
		sum = string([]rune(sum)[:summaryLen])
	}
	return IndexEntry{Key: r.Key, Kind: r.Kind, Title: r.Title, Tags: nz(r.Tags), Status: r.Status,
		Version: r.Version, UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt, Summary: sum}
}

// afterWrite updates the index and appends to the log under the project lock.
// Drive has no append, so log.jsonl is read-modify-write; single instance +
// project lock keep it append-only in effect.
func (v *Vault) afterWrite(ctx context.Context, op, project string, r *Record) error {
	defer v.lock("proj:" + project)()
	idx, err := v.loadIndex(ctx, project, false)
	if err != nil {
		return fmt.Errorf("index: %w", err)
	}
	idx.Entries[r.Key] = entryOf(r)
	if err := v.saveIndex(ctx, project, idx); err != nil {
		return fmt.Errorf("index: %w", err)
	}
	line, _ := json.Marshal(LogEntry{TS: r.UpdatedAt, Bot: r.UpdatedBy, Op: op, Key: r.Key, Kind: r.Kind, Version: r.Version})
	old, err := v.b.Read(ctx, logPath(project))
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("log: %w", err)
	}
	if err := v.b.Write(ctx, logPath(project), append(old, append(line, '\n')...)); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	return nil
}

// loadIndex reads the index, rebuilding from records when missing or corrupt
// (or when rebuild is forced). Caller must hold the project lock.
func (v *Vault) loadIndex(ctx context.Context, project string, rebuild bool) (*Index, error) {
	if !rebuild {
		data, err := v.b.Read(ctx, indexPath(project))
		if err == nil {
			var idx Index
			if json.Unmarshal(data, &idx) == nil && idx.Entries != nil {
				return &idx, nil
			}
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	idx := &Index{Entries: map[string]IndexEntry{}}
	dir := path.Join("projects", project, "records")
	names, err := v.b.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		r, err := v.load(ctx, path.Join(dir, n))
		if err != nil {
			return nil, err
		}
		idx.Entries[r.Key] = entryOf(r)
	}
	return idx, nil
}

func (v *Vault) saveIndex(ctx context.Context, project string, idx *Index) error {
	b, _ := json.MarshalIndent(idx, "", "  ")
	return v.b.Write(ctx, indexPath(project), b)
}

// Reindex rebuilds a project's _index.json from its records. Owners only.
func (v *Vault) Reindex(ctx context.Context, bot, project string) (int, error) {
	if err := validName("project", project); err != nil {
		return 0, err
	}
	if err := v.need(bot, project, acl.Owner); err != nil {
		return 0, err
	}
	defer v.lock("proj:" + project)()
	idx, err := v.loadIndex(ctx, project, true)
	if err != nil {
		return 0, err
	}
	return len(idx.Entries), v.saveIndex(ctx, project, idx)
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
		projects = ps
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
		idx, err := v.loadIndex(ctx, p, false)
		unlock()
		if err != nil {
			return nil, err
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
