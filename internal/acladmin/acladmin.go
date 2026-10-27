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
	"syscall"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/store"
)

// Path is the ACL document's path relative to the vault root.
const Path = "_acl.json"

// MaxAttempts bounds the read-modify-write retries on a version conflict.
const MaxAttempts = 5

var (
	ErrNoBot     = errors.New("acladmin: no such bot")
	ErrTokenOut  = errors.New("acladmin: --token-out is required when a token is minted")
	ErrConflicts = errors.New("acladmin: _acl.json kept changing underneath us; retry")
)

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
		return err
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
}

// GrantResult says what Grant did; it never contains the token.
type GrantResult struct {
	Minted bool // a new token was written to TokenOut
}

// Grant gives bot role on project. A new bot (or Rotate) gets a fresh token,
// written to opts.TokenOut before the ACL changes; if the ACL write fails the
// token file is removed. An existing bot keeps its token. A revoked bot needs
// Rotate (its old token must never come back).
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
		d.Bots[bot] = b
		return nil
	})
	if err != nil {
		if mint {
			os.Remove(opts.TokenOut)
		}
		return GrantResult{}, err
	}
	return GrantResult{Minted: mint}, nil
}

// Revoke marks bot revoked (its token stops authenticating at the server's
// next ACL reload, within 60s). Roles are kept for the record.
func (a *Admin) Revoke(ctx context.Context, bot string) error {
	return a.update(ctx, func(d *acl.Doc) error {
		b, ok := d.Bots[bot]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNoBot, bot)
		}
		b.Revoked = true
		d.Bots[bot] = b
		return nil
	})
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

// writeSecretFile creates path exclusively with mode 0600 (never follows a
// symlink, never overwrites) and writes s.
func writeSecretFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("acladmin: token file: %w", err)
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
