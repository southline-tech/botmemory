// Package vault implements botVault record semantics on top of a store.Backend:
// validation, ACL checks, write attribution, versioning with history copies,
// optimistic concurrency (if_version), append, and soft delete (archive).
package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/store"
)

var (
	ErrForbidden = errors.New("forbidden")
	ErrConflict  = errors.New("version conflict")
	ErrNotFound  = errors.New("not found")
	ErrInvalid   = errors.New("invalid argument")
)

// Kinds is the closed set of record kinds (spec: strict kinds).
var Kinds = []string{"decision", "spec", "handoff", "assignment", "note"}

const StatusArchived = "archived"

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type Record struct {
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Tags      []string  `json:"tags"`
	Status    string    `json:"status"`
	Version   int       `json:"version"`
	ETag      string    `json:"etag"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Links     []string  `json:"links"`

	rev string // storage revision this copy was read at (for WriteIf)
}

// PutInput is the caller-controlled part of a record.
type PutInput struct {
	Project, Kind, Key, Title, Body, Status string // Kind optional
	Tags, Links                             []string
	// IfVersion: nil or 0 = create only (CONFLICT if key exists); N = must be at N.
	IfVersion *int
}

type ListFilter struct{ Kind, Tag, Status string }

// Vault is safe for concurrent use within a single instance (per-key locks).
type Vault struct {
	b     store.Backend
	acl   func() *acl.ACL
	now   func() time.Time
	locks sync.Map // record path -> *sync.Mutex
}

func New(b store.Backend, aclFn func() *acl.ACL) *Vault {
	return &Vault{b: b, acl: aclFn, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock overrides the clock (tests).
func (v *Vault) SetClock(f func() time.Time) { v.now = f }

func (v *Vault) need(bot, project string, r acl.Role) error {
	if got := v.acl().RoleFor(bot, project); got < r {
		return fmt.Errorf("%w: %s has %s on %q, needs %s", ErrForbidden, bot, got, project, r)
	}
	return nil
}

func validName(what, s string) error {
	if !nameRE.MatchString(s) {
		return fmt.Errorf("%w: %s %q must match %s", ErrInvalid, what, s, nameRE)
	}
	return nil
}

func validKind(k string) error {
	for _, x := range Kinds {
		if x == k {
			return nil
		}
	}
	return fmt.Errorf("%w: kind %q not in %v", ErrInvalid, k, Kinds)
}

// Keys are unique per project; kind is metadata stored inside the record.
func recPath(project, key string) string {
	return path.Join("projects", project, "records", key+".json")
}
func histPath(project, key string, ver int) string {
	return path.Join("projects", project, "history", key, fmt.Sprintf("v%d.json", ver))
}

func (v *Vault) lock(p string) func() {
	m, _ := v.locks.LoadOrStore(p, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (v *Vault) load(ctx context.Context, p string) (*Record, error) {
	data, rev, err := v.b.ReadRev(ctx, p)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("corrupt record %s: %w", p, err)
	}
	r.rev = rev
	return &r, nil
}

// commit writes the prior version to history (if any), then the new record
// conditionally on the storage revision it was read at.
//
// Crash consistency: history/vN is a copy of record vN, which is immutable
// once written, so the history write is idempotent. If the record write then
// fails, history/vN merely duplicates the still-current vN (history() hides
// versions >= current) and a retry rewrites identical bytes. The record write
// is the commit point; a concurrent writer (another process) fails it with
// ErrPrecondition -> CONFLICT, in addition to the in-process key lock and the
// single-instance lock file.
func (v *Vault) commit(ctx context.Context, prev, next *Record, project string) error {
	rev := ""
	if prev != nil {
		rev = prev.rev
		b, _ := json.MarshalIndent(prev, "", "  ")
		if err := v.b.Write(ctx, histPath(project, prev.Key, prev.Version), b); err != nil {
			return err
		}
	}
	next.ETag = fmt.Sprintf("%s:%d", next.Key, next.Version)
	b, _ := json.MarshalIndent(next, "", "  ")
	err := v.b.WriteIf(ctx, recPath(project, next.Key), b, rev)
	if errors.Is(err, store.ErrPrecondition) {
		return fmt.Errorf("%w: %s changed in storage since it was read; re-get and retry", ErrConflict, next.Key)
	}
	return err
}

func (v *Vault) ListProjects(ctx context.Context, bot string) ([]string, error) {
	names, err := v.b.List(ctx, "projects")
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, n := range names {
		if strings.HasSuffix(n, "/") {
			p := strings.TrimSuffix(n, "/")
			if v.acl().RoleFor(bot, p) >= acl.Read {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (v *Vault) Get(ctx context.Context, bot, project, key string, version int) (*Record, error) {
	if err := v.checkAddr(project, key); err != nil {
		return nil, err
	}
	if err := v.need(bot, project, acl.Read); err != nil {
		return nil, err
	}
	cur, err := v.load(ctx, recPath(project, key))
	if err != nil || version == 0 || version == cur.Version {
		return cur, err
	}
	if version < 0 || version > cur.Version {
		return nil, fmt.Errorf("%w: version %d (current %d)", ErrNotFound, version, cur.Version)
	}
	return v.load(ctx, histPath(project, key, version))
}

func (v *Vault) List(ctx context.Context, bot, project string, f ListFilter) ([]Record, error) {
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
	out := []Record{}
	dir := path.Join("projects", project, "records")
	names, err := v.b.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	{
		for _, n := range names {
			if !strings.HasSuffix(n, ".json") {
				continue
			}
			r, err := v.load(ctx, path.Join(dir, n))
			if err != nil {
				return nil, err
			}
			if f.Status == "" && r.Status == StatusArchived {
				continue // archived hidden unless asked for
			}
			if f.Status != "" && r.Status != f.Status {
				continue
			}
			if f.Kind != "" && r.Kind != f.Kind {
				continue
			}
			if f.Tag != "" && !contains(r.Tags, f.Tag) {
				continue
			}
			r.Body = "" // list is a summary; use get for body
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (v *Vault) Put(ctx context.Context, bot string, in PutInput) (*Record, error) {
	if err := v.checkAddr(in.Project, in.Key); err != nil {
		return nil, err
	}
	if in.Kind != "" {
		if err := validKind(in.Kind); err != nil {
			return nil, err
		}
	}
	if in.Status == StatusArchived {
		return nil, fmt.Errorf("%w: use delete to archive", ErrInvalid)
	}
	if err := v.need(bot, in.Project, acl.Write); err != nil {
		return nil, err
	}
	p := recPath(in.Project, in.Key)
	defer v.lock(p)()
	prev, err := v.load(ctx, p)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	curVer := 0
	if prev != nil {
		curVer = prev.Version
	}
	if in.IfVersion == nil && prev != nil {
		return nil, fmt.Errorf("%w: key exists at version %d; pass if_version to update", ErrConflict, curVer)
	}
	if in.IfVersion != nil && *in.IfVersion != curVer {
		return nil, fmt.Errorf("%w: if_version=%d but current is %d", ErrConflict, *in.IfVersion, curVer)
	}
	now := v.now()
	next := &Record{
		Key: in.Key, Kind: in.Kind, Title: in.Title, Body: in.Body,
		Tags: nz(in.Tags), Links: nz(in.Links), Status: in.Status,
		Version: curVer + 1, CreatedBy: bot, CreatedAt: now, UpdatedBy: bot, UpdatedAt: now,
	}
	if next.Status == "" {
		next.Status = "active"
	}
	if prev != nil {
		next.CreatedBy, next.CreatedAt = prev.CreatedBy, prev.CreatedAt
	}
	if err := v.commit(ctx, prev, next, in.Project); err != nil {
		return nil, err
	}
	return next, nil
}

// Append adds text to an existing record's body without a version check
// (spec: lock-free for callers; serialized server-side by the per-key lock).
func (v *Vault) Append(ctx context.Context, bot, project, key, text string) (*Record, error) {
	if err := v.checkAddr(project, key); err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%w: empty text", ErrInvalid)
	}
	if err := v.need(bot, project, acl.Write); err != nil {
		return nil, err
	}
	p := recPath(project, key)
	defer v.lock(p)()
	prev, err := v.load(ctx, p)
	if err != nil {
		return nil, err
	}
	if prev.Status == StatusArchived {
		return nil, fmt.Errorf("%w: record is archived", ErrConflict)
	}
	next := *prev
	now := v.now()
	sep := ""
	if next.Body != "" && !strings.HasSuffix(next.Body, "\n") {
		sep = "\n"
	}
	next.Body += sep + fmt.Sprintf("\n— %s, %s\n%s\n", bot, now.Format(time.RFC3339), text)
	next.Version++
	next.UpdatedBy, next.UpdatedAt = bot, now
	if err := v.commit(ctx, prev, &next, project); err != nil {
		return nil, err
	}
	return &next, nil
}

// Delete soft-deletes (status=archived, new version). Owners only. Nothing is
// ever removed from storage.
func (v *Vault) Delete(ctx context.Context, bot, project, key string, ifVersion *int) (*Record, error) {
	if err := v.checkAddr(project, key); err != nil {
		return nil, err
	}
	if err := v.need(bot, project, acl.Owner); err != nil {
		return nil, err
	}
	p := recPath(project, key)
	defer v.lock(p)()
	prev, err := v.load(ctx, p)
	if err != nil {
		return nil, err
	}
	if ifVersion != nil && *ifVersion != prev.Version {
		return nil, fmt.Errorf("%w: if_version=%d but current is %d", ErrConflict, *ifVersion, prev.Version)
	}
	if prev.Status == StatusArchived {
		return prev, nil
	}
	next := *prev
	next.Status = StatusArchived
	next.Version++
	next.UpdatedBy, next.UpdatedAt = bot, v.now()
	if err := v.commit(ctx, prev, &next, project); err != nil {
		return nil, err
	}
	return &next, nil
}

func (v *Vault) checkAddr(project, key string) error {
	if err := validName("project", project); err != nil {
		return err
	}
	return validName("key", key)
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func nz(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
