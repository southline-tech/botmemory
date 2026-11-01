package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/southline-tech/botmemory/internal/acl"
)

const briefMaxBytes = 8 << 10
const briefStateMax = 1024

// BriefOpts controls brief assembly.
type BriefOpts struct {
	Limit  int
	IfEtag string
	Kind   string
	Tag    string
}

// BriefResult is the brief tool payload (SPEC v0.3 P1.1).
type BriefResult struct {
	Keys      []string  `json:"keys"`
	Decisions []string  `json:"decisions,omitempty"`
	Open      []string  `json:"open,omitempty"`
	State     string    `json:"state,omitempty"`
	Etag      string    `json:"etag"`
	UpdatedAt time.Time `json:"updated_at"`
	Truncated bool      `json:"truncated,omitempty"`
	Unchanged bool      `json:"unchanged,omitempty"`
}

// ChangedResult is the changed tool payload.
type ChangedResult struct {
	Changed bool     `json:"changed"`
	Count   int      `json:"count"`
	Etag    string   `json:"etag"`
	Keys    []string `json:"keys"`
}

// GetOpts partial read options.
type GetOpts struct {
	Head, Tail int
	Since      time.Time
	HasSince   bool
	MaxBytes   int
	Summary    bool
}

// GetOutput is get with optional partial fields.
type GetOutput struct {
	Record
	SinceApplied *bool `json:"since_applied,omitempty"`
}

func (v *Vault) indexEntries(ctx context.Context, bot, project string, f ListFilter) ([]IndexEntry, error) {
	if err := validName("project", project); err != nil {
		return nil, err
	}
	if err := v.need(bot, project, acl.Read); err != nil {
		return nil, err
	}
	if f.Kind != "" {
		if err := validKind(f.Kind); err != nil {
			return nil, err
		}
	}
	defer v.lock("proj:" + project)()
	idx, _, err := v.loadIndex(ctx, project, false)
	if err != nil {
		return nil, err
	}
	return filterIndexEntries(idx, f), nil
}

func briefLine(e IndexEntry) string {
	sum := e.Summary
	if sum == "" {
		sum = e.Title
	}
	if len([]rune(sum)) > 120 {
		sum = string([]rune(sum)[:120])
	}
	return fmt.Sprintf("%s | %s | %s | %s", e.Key, e.Kind, e.Title, sum)
}

func (v *Vault) Brief(ctx context.Context, bot, project string, opts BriefOpts) (*BriefResult, error) {
	f := ListFilter{Kind: opts.Kind, Tag: opts.Tag}
	entries, err := v.indexEntries(ctx, bot, project, f)
	if err != nil {
		return nil, err
	}
	etag := indexEtag(entries)
	if opts.IfEtag != "" && opts.IfEtag == etag {
		return &BriefResult{Unchanged: true, Etag: etag}, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 40
	}
	if limit > len(entries) {
		limit = len(entries)
	}
	truncated := len(entries) > limit
	entries = entries[:limit]

	res := &BriefResult{
		Etag:      etag,
		UpdatedAt: maxUpdatedAt(entries),
		Truncated: truncated,
	}
	for _, e := range entries {
		line := briefLine(e)
		res.Keys = append(res.Keys, line)
		switch e.Kind {
		case "decision":
			res.Decisions = append(res.Decisions, line)
		case "handoff", "assignment":
			res.Open = append(res.Open, line)
		}
	}
	for _, sk := range []string{"brief", "state"} {
		if _, err := v.Get(ctx, bot, project, sk, 0); err == nil {
			if snip, err := v.stateSnippet(ctx, project, sk, 0); err == nil && snip != "" {
				res.State = snip
				break
			}
		}
	}
	res = capBriefJSON(res)
	return res, nil
}

func (v *Vault) stateSnippet(ctx context.Context, project, key string, version int) (string, error) {
	r, err := v.load(ctx, recPath(project, key))
	if err != nil {
		return "", err
	}
	if version > 0 && r.Version != version {
		return "", nil
	}
	b := r.Body
	if len(b) > briefStateMax {
		b = b[:briefStateMax]
	}
	return b, nil
}

func capBriefJSON(res *BriefResult) *BriefResult {
	for {
		b, err := json.Marshal(res)
		if err != nil || len(b) <= briefMaxBytes {
			return res
		}
		res.Truncated = true
		if len(res.Keys) > 1 {
			res.Keys = res.Keys[:len(res.Keys)-1]
			trimKindLists(res)
			continue
		}
		if res.State != "" {
			res.State = shrinkUTF8(res.State, len(res.State)/2)
			continue
		}
		break
	}
	return res
}

func trimKindLists(res *BriefResult) {
	res.Decisions = filterLines(res.Decisions, res.Keys)
	res.Open = filterLines(res.Open, res.Keys)
}

func filterLines(from, allowed []string) []string {
	set := map[string]struct{}{}
	for _, a := range allowed {
		set[a] = struct{}{}
	}
	out := []string{}
	for _, l := range from {
		if _, ok := set[l]; ok {
			out = append(out, l)
		}
	}
	return out
}

func shrinkUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

func (v *Vault) Changed(ctx context.Context, bot, project string, since time.Time, f ListFilter) (*ChangedResult, error) {
	entries, err := v.indexEntries(ctx, bot, project, f)
	if err != nil {
		return nil, err
	}
	etag := indexEtag(entries)
	var keys []string
	for _, e := range entries {
		if e.UpdatedAt.After(since) {
			keys = append(keys, e.Key)
		}
	}
	sort.Strings(keys)
	return &ChangedResult{
		Changed: len(keys) > 0,
		Count:   len(keys),
		Etag:    etag,
		Keys:    keys,
	}, nil
}

func (v *Vault) ListWithEtag(ctx context.Context, bot, project string, f ListFilter, ifEtag string) (any, error) {
	entries, err := v.indexEntries(ctx, bot, project, f)
	if err != nil {
		return nil, err
	}
	etag := indexEtag(entries)
	if ifEtag != "" && ifEtag == etag {
		return map[string]any{"unchanged": true, "etag": etag}, nil
	}
	return entries, nil
}

func (v *Vault) GetWithOpts(ctx context.Context, bot, project, key string, version int, opts GetOpts) (any, error) {
	if opts.Head > 0 && opts.Tail > 0 {
		return nil, fmt.Errorf("%w: head and tail are mutually exclusive", ErrInvalid)
	}
	if opts.Summary {
		entries, err := v.indexEntries(ctx, bot, project, ListFilter{})
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Key == key {
				return e, nil
			}
		}
		return nil, ErrNotFound
	}
	r, err := v.Get(ctx, bot, project, key, version)
	if err != nil {
		return nil, err
	}
	if opts.Head == 0 && opts.Tail == 0 && !opts.HasSince && opts.MaxBytes <= 0 {
		return r, nil
	}
	out := GetOutput{Record: *r}
	body := r.Body
	applied := false
	if opts.HasSince {
		var ok bool
		body, ok = bodySince(body, opts.Since)
		applied = ok
		out.SinceApplied = &applied
	}
	if opts.Head > 0 {
		body = headLines(body, opts.Head)
	}
	if opts.Tail > 0 {
		body = tailLines(body, opts.Tail)
	}
	if opts.MaxBytes > 0 && len(body) > opts.MaxBytes {
		body = body[:opts.MaxBytes]
	}
	out.Body = body
	return out, nil
}

func headLines(body string, n int) string {
	if n <= 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= n {
		return body
	}
	return strings.Join(lines[:n], "\n")
}

func tailLines(body string, n int) string {
	if n <= 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= n {
		return body
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// bodySince returns text after the last append stamp at or after since (best-effort).
func bodySince(body string, since time.Time) (string, bool) {
	marker := "\n— "
	idx := 0
	lastStart := -1
	for {
		i := strings.Index(body[idx:], marker)
		if i < 0 {
			break
		}
		start := idx + i
		lineStart := start + 1 // skip '\n' before em dash
		lineEnd := strings.Index(body[lineStart:], "\n")
		if lineEnd < 0 {
			break
		}
		line := body[lineStart : lineStart+lineEnd]
		parts := strings.SplitN(line, ", ", 2)
		if len(parts) >= 2 {
			tsStr := strings.TrimSpace(parts[1])
			if t, err := time.Parse(time.RFC3339, tsStr); err == nil && !t.Before(since) {
				lastStart = start
			}
		}
		idx = start + lineEnd + 1
	}
	if lastStart < 0 {
		return body, false
	}
	return strings.TrimSpace(body[lastStart:]), true
}
