// Package acladmin edits _acl.json safely: every change is a read-modify-write
// guarded by the backend's revision check (Drive file version, or the memory
// backend's revision), validated with acl.Parse before it is written, and
// retried a few times if another writer got there first.
//
// Raw tokens are never returned to callers for printing: Grant writes a newly
// minted token only to a caller-chosen file (mode 0600, created exclusively),
// and only the SHA-256 hash goes into the ACL.
package acladmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/store"
)

// Path is the ACL document's path relative to the vault root.
const Path = "_acl.json"

// MaxAttempts bounds the read-modify-write retries on a version conflict.
const MaxAttempts = 5

var (
	ErrNoBot     = errors.New("acladmin: no such bot")
	ErrTokenOut  = errors.New("acladmin: --token-out is required when a token is minted")
	ErrConflicts = errors.New("acladmin: _acl.json kept changing underneath us; retry")
	// ErrLastOwner: the revoke/remove would leave a project with no active
	// owner. The returned error names the operation.
	ErrLastOwner = errors.New("last active owner")
	// ErrOutcomeUnknown: the ACL write failed in a way that may or may not have
	// been applied and the ACL could not be re-read to tell. The token file
	// is kept.
	ErrOutcomeUnknown = errors.New("acladmin: ACL write outcome unknown")
	// ErrNotVerified: the write was acknowledged but a re-read does not
	// show the change (e.g. a concurrent writer on another host overwrote it).
	ErrNotVerified = errors.New("acladmin: change not visible on re-read")
)

// writeUnknownError wraps a WriteIf failure other than ErrPrecondition: the
// backend (e.g. Drive after a lost response) may have saved the document.
// Every other error from update means nothing was written.
type writeUnknownError struct{ err error }

func (e *writeUnknownError) Error() string { return "acladmin: write " + Path + ": " + e.err.Error() }
func (e *writeUnknownError) Unwrap() error { return e.err }

// Admin edits the ACL stored at Path in B.
type Admin struct {
	B store.Backend
	// beforeWrite is a test hook run between read and conditional write.
	beforeWrite func()
}

func New(b store.Backend) *Admin { return &Admin{B: b} }

// update applies fn to the current document and writes it with the
// revision read, retrying on store.ErrPrecondition.
func (a *Admin) update(ctx context.Context, fn func(*acl.Doc) error) error {
	for i := 0; i < MaxAttempts; i++ {
		data, rev, err := a.B.ReadRev(ctx, Path)
		if errors.Is(err, store.ErrNotFound) {
			data, rev, err = []byte(`{"bots":{}}`), "", nil
		}
		if err != nil {
			return fmt.Errorf("acladmin: read %s: %w", Path, err)
		}
		var d acl.Doc
		if err := json.Unmarshal(data, &d); err != nil {
			return fmt.Errorf("acladmin: %s is not valid JSON (fix it by hand first): %w", Path, err)
		}
		if d.Bots == nil {
			d.Bots = map[string]acl.Bot{}
		}
		if err := fn(&d); err != nil {
			return err
		}
		out, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		if _, err := acl.Parse(out); err != nil { // never write an ACL the server would reject
			return err
		}
		if a.beforeWrite != nil {
			a.beforeWrite()
		}
		err = a.B.WriteIf(ctx, Path, append(out, '\n'), rev)
		if errors.Is(err, store.ErrPrecondition) {
			continue
		}
		if err != nil {
			return &writeUnknownError{err}
		}
		return nil
	}
	return ErrConflicts
}

// GrantOpts controls Grant.
type GrantOpts struct {
	// TokenOut is where a newly minted token is written (0600, must not
	// exist). Required whenever a token is minted.
	TokenOut string
	// Rotate mints a new token for an existing bot (and clears revoked).
	Rotate bool
	// DefaultProject sets the bot's default_project (must be granted).
	DefaultProject string
}

// GrantResult says what Grant did; it never contains the token.
type GrantResult struct {
	Minted bool // a new token was written to TokenOut
}

// Grant gives bot role on project. A new bot (or Rotate) gets a fresh token,
// written to opts.TokenOut before the ACL changes. An existing bot keeps its
// token. A revoked bot needs Rotate (its old token must never come back).
//
// Whether to mint is decided from a first read (the token file must exist
// before the ACL can reference its hash) and re-checked inside the edit: a
// bot that appears concurrently without Rotate, or disappears/gets revoked
// concurrently, fails the grant rather than silently changing its token.
//
// Token file on failure: removed when nothing can have been written
// (validation, edit refusal, conflicts). If the write itself failed (it may
// have landed, or land later), the ACL is re-read: hash present => success;
// otherwise the file is kept and ErrOutcomeUnknown names it. After an
// acknowledged write the ACL is re-read and the hash/role must be present
// (else ErrNotVerified, file kept).
func (a *Admin) Grant(ctx context.Context, bot, project, role string, opts GrantOpts) (GrantResult, error) {
	if _, err := acl.ParseRole(role); err != nil {
		return GrantResult{}, err
	}
	if project == "" || project == "*" {
		return GrantResult{}, errors.New("acladmin: grant one explicit project (no wildcard)")
	}
	// Decide up front whether we mint, from the current document.
	data, err := a.B.Read(ctx, Path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return GrantResult{}, fmt.Errorf("acladmin: read %s: %w", Path, err)
	}
	var cur acl.Doc
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cur); err != nil {
			return GrantResult{}, fmt.Errorf("acladmin: %s is not valid JSON: %w", Path, err)
		}
	}
	existing, exists := cur.Bots[bot]
	if exists && existing.Revoked && !opts.Rotate {
		return GrantResult{}, fmt.Errorf("acladmin: bot %q is revoked; use --rotate to issue a new token", bot)
	}
	mint := !exists || opts.Rotate
	var hash string
	if mint {
		if opts.TokenOut == "" {
			return GrantResult{}, ErrTokenOut
		}
		tok, h, err := acl.NewToken()
		if err != nil {
			return GrantResult{}, err
		}
		if err := writeSecretFile(opts.TokenOut, tok+"\n"); err != nil {
			return GrantResult{}, err
		}
		hash = h
	}
	err = a.update(ctx, func(d *acl.Doc) error {
		b, ok := d.Bots[bot]
		switch {
		case mint && ok && !opts.Rotate:
			return fmt.Errorf("acladmin: bot %q was created concurrently; re-run (add --rotate to replace its token)", bot)
		case mint:
			b.TokenSHA256, b.Revoked = hash, false
		case !ok:
			return fmt.Errorf("acladmin: bot %q disappeared concurrently; retry", bot)
		case b.Revoked:
			return fmt.Errorf("acladmin: bot %q was revoked concurrently", bot)
		}
		if b.Roles == nil {
			b.Roles = map[string]string{}
		}
		b.Roles[project] = role
		if dp := strings.TrimSpace(opts.DefaultProject); dp != "" {
			if err := acl.ValidProjectName(dp); err != nil {
				return fmt.Errorf("acladmin: default_project: %w", err)
			}
			if _, ok := b.Roles[dp]; !ok {
				return fmt.Errorf("acladmin: default_project %q is not granted to bot %q", dp, bot)
			}
			b.DefaultProject = dp
		}
		d.Bots[bot] = b
		return nil
	})
	if err == nil {
		if verr := a.verify(ctx, func(d *acl.Doc) bool {
			b, ok := d.Bots[bot]
			return ok && !b.Revoked && b.Roles[project] == role && (!mint || b.TokenSHA256 == hash)
		}); verr != nil {
			if mint {
				return GrantResult{}, fmt.Errorf("%w; token file kept at %s (harmless if its hash is absent): run `botmemory acl list`", verr, opts.TokenOut)
			}
			return GrantResult{}, verr
		}
		return GrantResult{Minted: mint}, nil
	}
	if !mint {
		return GrantResult{}, err
	}
	var wu *writeUnknownError
	if !errors.As(err, &wu) {
		os.Remove(opts.TokenOut) // nothing was written
		return GrantResult{}, err
	}
	// The write may have landed, or may still land later (Drive can apply a
	// request after reporting an error). So the file is only ever kept here:
	// a token whose hash is not in the ACL cannot authenticate, so keeping it
	// is harmless, while deleting it could orphan a hash that lands later.
	landed, rerr := a.hashPresent(ctx, bot, hash)
	if rerr == nil && landed {
		return GrantResult{Minted: true}, nil // applied despite the error
	}
	why := "the new hash is not (yet) in the ACL"
	if rerr != nil {
		why = "re-read failed: " + rerr.Error()
	}
	return GrantResult{}, fmt.Errorf("%w: write failed (%v) and %s; token file kept at %s — later run `botmemory acl list`: if %s shows the grant the token is live, otherwise delete the file", ErrOutcomeUnknown, wu.err, why, opts.TokenOut, bot)
}

// verify re-reads the ACL after an acknowledged write and checks ok.
func (a *Admin) verify(ctx context.Context, ok func(*acl.Doc) bool) error {
	data, err := a.B.Read(ctx, Path)
	if err != nil {
		return fmt.Errorf("%w: re-read failed: %v", ErrNotVerified, err)
	}
	var d acl.Doc
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("%w: %v", ErrNotVerified, err)
	}
	if !ok(&d) {
		return ErrNotVerified
	}
	return nil
}

// hashPresent re-reads the ACL and reports whether bot now carries hash.
func (a *Admin) hashPresent(ctx context.Context, bot, hash string) (bool, error) {
	data, err := a.B.Read(ctx, Path)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil // no ACL at all: our write certainly did not land
	}
	if err != nil {
		return false, err
	}
	var d acl.Doc
	if err := json.Unmarshal(data, &d); err != nil {
		return false, err
	}
	b, ok := d.Bots[bot]
	return ok && b.TokenSHA256 == hash, nil
}

// Revoke marks bot revoked as a whole: its single token stops authenticating
// for EVERY project at the server's next ACL reload (within 60s). Roles are
// kept for the record. Without force it refuses if bot is the last active
// owner of any project (which would leave that project unadministrable).
func (a *Admin) Revoke(ctx context.Context, bot string, force bool) error {
	err := a.update(ctx, func(d *acl.Doc) error {
		b, ok := d.Bots[bot]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNoBot, bot)
		}
		if err := lastOwnerCheck(d, bot, force, "revoke"); err != nil {
			return err
		}
		b.Revoked = true
		d.Bots[bot] = b
		return nil
	})
	revoked := func(d *acl.Doc) bool { b, ok := d.Bots[bot]; return ok && b.Revoked }
	var wu *writeUnknownError
	if errors.As(err, &wu) {
		if a.verify(ctx, revoked) == nil {
			return nil
		}
		return fmt.Errorf("%w: write failed (%v); run `botmemory acl list` later to see whether %s is revoked", ErrOutcomeUnknown, wu.err, bot)
	}
	if err != nil {
		return err
	}
	// Report success only once a re-read shows the bot revoked.
	return a.verify(ctx, revoked)
}

// Remove deletes bot's entry from the ACL entirely (token hash and roles).
// Same last-owner rule as Revoke. A failed write returns ErrOutcomeUnknown
// unless a re-read settles it; success is reported only once a re-read
// shows the entry gone (else ErrNotVerified).
func (a *Admin) Remove(ctx context.Context, bot string, force bool) error {
	err := a.update(ctx, func(d *acl.Doc) error {
		if _, ok := d.Bots[bot]; !ok {
			return fmt.Errorf("%w: %q", ErrNoBot, bot)
		}
		if err := lastOwnerCheck(d, bot, force, "remove"); err != nil {
			return err
		}
		delete(d.Bots, bot)
		return nil
	})
	gone := func(d *acl.Doc) bool { _, ok := d.Bots[bot]; return !ok }
	var wu *writeUnknownError
	if errors.As(err, &wu) {
		if a.verify(ctx, gone) == nil {
			return nil
		}
		return fmt.Errorf("%w: write failed (%v); run `botmemory acl list` later to see whether %s is still present", ErrOutcomeUnknown, wu.err, bot)
	}
	if err != nil {
		return err
	}
	return a.verify(ctx, gone)
}

// lastOwnerCheck refuses (unless force) when bot is active and the only
// active owner of some project.
func lastOwnerCheck(d *acl.Doc, bot string, force bool, op string) error {
	b := d.Bots[bot]
	if force || b.Revoked {
		return nil
	}
	var orphaned []string
	for p, r := range b.Roles {
		if r != "owner" {
			continue
		}
		other := false
		for id, ob := range d.Bots {
			if id != bot && !ob.Revoked && ob.Roles[p] == "owner" {
				other = true
				break
			}
		}
		if !other {
			orphaned = append(orphaned, p)
		}
	}
	if len(orphaned) > 0 {
		sort.Strings(orphaned)
		return fmt.Errorf("acladmin: refusing to %s %q, the %w of %v; grant another owner first or use --force", op, bot, ErrLastOwner, orphaned)
	}
	return nil
}

// Entry is one bot as shown by List. It deliberately has no hash.
type Entry struct {
	Bot     string
	Revoked bool
	Roles   map[string]string
}

func (a *Admin) List(ctx context.Context) ([]Entry, error) {
	data, err := a.B.Read(ctx, Path)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d acl.Doc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(d.Bots))
	for id, b := range d.Bots {
		out = append(out, Entry{Bot: id, Revoked: b.Revoked, Roles: b.Roles})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bot < out[j].Bot })
	return out, nil
}

// writeSecretFile creates path exclusively with mode 0600 (never
// overwrites), writes s and fsyncs it; on any failure the partial file is
// removed. O_NOFOLLOW only protects the LAST path component: symlinked parent
// directories are followed, so put token files in a directory you own (the
// pilot script uses a 0700 per-bot directory).
// syncFile is replaceable in tests to simulate an fsync failure.
var syncFile = (*os.File).Sync

func writeSecretFile(path, s string) error {
	f, err := openSecretFile(path)
	if err != nil {
		return fmt.Errorf("acladmin: token file: %w", err)
	}
	_, err = f.WriteString(s)
	if err == nil {
		err = syncFile(f)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return fmt.Errorf("acladmin: token file: %w", err)
	}
	return nil
}
