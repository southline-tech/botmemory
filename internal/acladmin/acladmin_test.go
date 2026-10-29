package acladmin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/store"
)

var ctx = context.Background()

func load(t *testing.T, m store.Backend) *acl.ACL {
	t.Helper()
	data, err := m.Read(ctx, Path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := acl.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func readTok(t *testing.T, p string) string {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v, want 0600", st.Mode().Perm())
	}
	b, _ := os.ReadFile(p)
	return strings.TrimSpace(string(b))
}

func TestGrantRevokeList(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	dir := t.TempDir()
	tokPM := filepath.Join(dir, "pm.token")

	r, err := a.Grant(ctx, "pm-verifychain", "verifychain", "owner", GrantOpts{TokenOut: tokPM})
	if err != nil || !r.Minted {
		t.Fatalf("grant: %+v %v", r, err)
	}
	tok := readTok(t, tokPM)
	raw, _ := m.Read(ctx, Path)
	if strings.Contains(string(raw), tok) {
		t.Fatal("raw token written to _acl.json")
	}
	acl1 := load(t, m)
	if id, err := acl1.Authenticate(tok); err != nil || id != "pm-verifychain" {
		t.Fatalf("auth: %q %v", id, err)
	}
	if acl1.RoleFor("pm-verifychain", "verifychain") != acl.Owner {
		t.Fatal("role not owner")
	}

	// Existing bot: adds a role, keeps its token, needs no --token-out.
	if r, err := a.Grant(ctx, "pm-verifychain", "other", "read", GrantOpts{}); err != nil || r.Minted {
		t.Fatalf("second grant: %+v %v", r, err)
	}
	if id, _ := load(t, m).Authenticate(tok); id != "pm-verifychain" {
		t.Fatal("token changed on role grant")
	}

	if err := a.Revoke(ctx, "pm-verifychain", false); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("revoke of sole owner without force: %v", err)
	}
	if err := a.Revoke(ctx, "pm-verifychain", true); err != nil {
		t.Fatal(err)
	}
	if _, err := load(t, m).Authenticate(tok); err == nil {
		t.Fatal("revoked token still authenticates")
	}
	if _, err := a.Grant(ctx, "pm-verifychain", "verifychain", "owner", GrantOpts{TokenOut: filepath.Join(dir, "x")}); err == nil {
		t.Fatal("grant to revoked bot without --rotate succeeded")
	}
	tok2p := filepath.Join(dir, "pm2.token")
	if r, err := a.Grant(ctx, "pm-verifychain", "verifychain", "owner", GrantOpts{TokenOut: tok2p, Rotate: true}); err != nil || !r.Minted {
		t.Fatalf("rotate: %+v %v", r, err)
	}
	acl2 := load(t, m)
	if _, err := acl2.Authenticate(tok); err == nil {
		t.Fatal("old token works after rotate")
	}
	if id, _ := acl2.Authenticate(readTok(t, tok2p)); id != "pm-verifychain" {
		t.Fatal("rotated token rejected")
	}

	es, err := a.List(ctx)
	if err != nil || len(es) != 1 || es[0].Revoked || es[0].Roles["other"] != "read" {
		t.Fatalf("list: %+v %v", es, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", es), acl.HashToken(readTok(t, tok2p))) {
		t.Fatal("list exposes a hash")
	}
	if err := a.Revoke(ctx, "nobody", false); !errors.Is(err, ErrNoBot) {
		t.Fatalf("revoke unknown: %v", err)
	}
}

func TestGrantRefusals(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	dir := t.TempDir()
	if _, err := a.Grant(ctx, "b", "p", "write", GrantOpts{}); !errors.Is(err, ErrTokenOut) {
		t.Fatalf("missing --token-out: %v", err)
	}
	if _, err := a.Grant(ctx, "b", "p", "admin", GrantOpts{TokenOut: filepath.Join(dir, "t")}); err == nil {
		t.Fatal("bad role accepted")
	}
	if _, err := a.Grant(ctx, "b", "*", "read", GrantOpts{TokenOut: filepath.Join(dir, "t")}); err == nil {
		t.Fatal("wildcard accepted")
	}
	if _, err := a.Grant(ctx, "Bad Bot", "p", "read", GrantOpts{TokenOut: filepath.Join(dir, "t")}); err == nil {
		t.Fatal("invalid bot id accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "t")); !os.IsNotExist(err) {
		t.Fatal("token file left behind after a refused grant")
	}
	exist := filepath.Join(dir, "exists")
	os.WriteFile(exist, []byte("keep"), 0o600)
	if _, err := a.Grant(ctx, "b", "p", "read", GrantOpts{TokenOut: exist}); err == nil {
		t.Fatal("overwrote an existing token file")
	}
	if b, _ := os.ReadFile(exist); string(b) != "keep" {
		t.Fatal("existing file modified")
	}
	link := filepath.Join(dir, "link")
	os.Symlink(filepath.Join(dir, "target"), link)
	if _, err := a.Grant(ctx, "b", "p", "read", GrantOpts{TokenOut: link}); err == nil {
		t.Fatal("followed a symlink for the token file")
	}
	if _, err := m.Read(ctx, Path); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("ACL written by a refused grant")
	}
}

// A concurrent edit between read and write is detected by the revision check;
// the retry keeps both changes.
func TestConcurrentEditRetried(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	dir := t.TempDir()
	if _, err := a.Grant(ctx, "other", "p", "read", GrantOpts{TokenOut: filepath.Join(dir, "o")}); err != nil {
		t.Fatal(err)
	}
	other := New(m)
	once := false
	a.beforeWrite = func() {
		if !once {
			once = true
			if err := other.Revoke(ctx, "other", false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := a.Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: filepath.Join(dir, "b")}); err != nil {
		t.Fatal(err)
	}
	es, _ := a.List(ctx)
	if len(es) != 2 || es[1].Bot != "other" || !es[1].Revoked || es[0].Roles["p"] != "write" {
		t.Fatalf("lost update: %+v", es)
	}
}

// If the ACL can never be written, the minted token file is removed.
func TestPersistentConflictRemovesTokenFile(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	other := New(m)
	dir := t.TempDir()
	n := 0
	a.beforeWrite = func() {
		n++
		other.Grant(ctx, fmt.Sprintf("x%d", n), "p", "read", GrantOpts{TokenOut: filepath.Join(dir, fmt.Sprintf("x%d", n))})
	}
	tp := filepath.Join(dir, "b")
	if _, err := a.Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: tp}); !errors.Is(err, ErrConflicts) {
		t.Fatalf("want ErrConflicts, got %v", err)
	}
	if _, err := os.Stat(tp); !os.IsNotExist(err) {
		t.Fatal("token file left after failed grant")
	}
}

// saveThenFail applies WriteIf to the inner store and then reports an error,
// like a Drive upload whose response was lost. readFails makes later reads
// fail too.
type saveThenFail struct {
	store.Backend
	failWrites bool
	readFails  bool // reads fail once a write has been attempted
	wrote      bool
}

func (s *saveThenFail) WriteIf(ctx context.Context, p string, d []byte, rev string) error {
	if !s.failWrites {
		return s.Backend.WriteIf(ctx, p, d, rev)
	}
	s.wrote = true
	if err := s.Backend.WriteIf(ctx, p, d, rev); err != nil {
		return err
	}
	return errors.New("connection reset (response lost)")
}

func (s *saveThenFail) Read(ctx context.Context, p string) ([]byte, error) {
	if s.readFails && s.wrote {
		return nil, errors.New("drive: upstream error (HTTP 503)")
	}
	return s.Backend.Read(ctx, p)
}

// failNoSave reports an error without saving anything.
type failNoSave struct{ store.Backend }

func (failNoSave) WriteIf(context.Context, string, []byte, string) error {
	return errors.New("connection reset before upload")
}

// R1-1: a write that landed despite an error keeps the token file and succeeds.
func TestGrantSavedThenErrorKeepsToken(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(fmt.Sprintf("rotate=%v", rotate), func(t *testing.T) {
			m := store.NewMemory()
			dir := t.TempDir()
			if rotate {
				if _, err := New(m).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: filepath.Join(dir, "old")}); err != nil {
					t.Fatal(err)
				}
			}
			tp := filepath.Join(dir, "b.token")
			r, err := New(&saveThenFail{Backend: m, failWrites: true}).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: tp, Rotate: rotate})
			if err != nil || !r.Minted {
				t.Fatalf("grant: %+v %v", r, err)
			}
			if id, err := load(t, m).Authenticate(readTok(t, tp)); err != nil || id != "b" {
				t.Fatalf("saved token does not authenticate: %q %v", id, err)
			}
		})
	}
}

// R1-1: write failed and the hash is absent => file removed; re-read fails => kept, outcome unknown.
func TestGrantWriteErrorOutcomes(t *testing.T) {
	for _, rotate := range []bool{false, true} {
		t.Run(fmt.Sprintf("rotate=%v", rotate), func(t *testing.T) {
			m := store.NewMemory()
			dir := t.TempDir()
			if rotate {
				if _, err := New(m).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: filepath.Join(dir, "old")}); err != nil {
					t.Fatal(err)
				}
			}
			tp := filepath.Join(dir, "absent.token")
			// R2-1: hash absent after an ambiguous error => kept, outcome unknown.
			if _, err := New(failNoSave{m}).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: tp, Rotate: rotate}); !errors.Is(err, ErrOutcomeUnknown) || !strings.Contains(err.Error(), tp) {
				t.Fatalf("want ErrOutcomeUnknown naming the file, got %v", err)
			}
			readTok(t, tp) // kept, 0600
			up := filepath.Join(dir, "unknown.token")
			_, err := New(&saveThenFail{Backend: m, failWrites: true, readFails: true}).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: up, Rotate: rotate})
			if !errors.Is(err, ErrOutcomeUnknown) || !strings.Contains(err.Error(), up) {
				t.Fatalf("want ErrOutcomeUnknown naming the file, got %v", err)
			}
			readTok(t, up) // kept, still 0600
			if id, _ := load(t, m).Authenticate(readTok(t, up)); id != "b" {
				t.Fatal("expected the write to have landed in this scenario")
			}
		})
	}
}

// R1-3: a bot created concurrently (without --rotate) fails the grant; nothing is overwritten.
func TestGrantBotAppearsConcurrently(t *testing.T) {
	m := store.NewMemory()
	dir := t.TempDir()
	a, other := New(m), New(m)
	otherTok := filepath.Join(dir, "other.token")
	a.beforeWrite = func() {
		a.beforeWrite = nil
		if _, err := other.Grant(ctx, "b", "p", "read", GrantOpts{TokenOut: otherTok}); err != nil {
			t.Fatal(err)
		}
	}
	tp := filepath.Join(dir, "b.token")
	if _, err := a.Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: tp}); err == nil || !strings.Contains(err.Error(), "concurrently") {
		t.Fatalf("want concurrent-creation error, got %v", err)
	}
	if _, err := os.Stat(tp); !os.IsNotExist(err) {
		t.Fatal("token file left after refused grant")
	}
	l := load(t, m)
	if id, _ := l.Authenticate(readTok(t, otherTok)); id != "b" || l.RoleFor("b", "p") != acl.Read {
		t.Fatal("concurrent creator's grant was overwritten")
	}
}

// R1-2: last active owner of any project is protected; another active owner lifts it.
func TestRevokeLastOwner(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	dir := t.TempDir()
	g := func(bot, p, r string) {
		t.Helper()
		if _, err := a.Grant(ctx, bot, p, r, GrantOpts{TokenOut: filepath.Join(dir, bot+p)}); err != nil {
			t.Fatal(err)
		}
	}
	g("pm", "p1", "owner")
	g("pm", "p2", "owner")
	g("pm2", "p1", "owner")
	g("dev", "p1", "write")
	if err := a.Revoke(ctx, "pm", false); !errors.Is(err, ErrLastOwner) || !strings.Contains(err.Error(), "p2") || strings.Contains(err.Error(), "p1") {
		t.Fatalf("want last-owner refusal naming p2 only, got %v", err)
	}
	if err := a.Revoke(ctx, "dev", false); err != nil {
		t.Fatalf("non-owner revoke: %v", err)
	}
	g("pm2", "p2", "owner")
	if err := a.Revoke(ctx, "pm", false); err != nil {
		t.Fatalf("revoke with co-owners: %v", err)
	}
	// pm is revoked, so pm2 is now last owner of both; a revoked co-owner does not count.
	if err := a.Revoke(ctx, "pm2", false); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("revoked co-owner counted: %v", err)
	}
	if err := a.Revoke(ctx, "pm2", true); err != nil {
		t.Fatal(err)
	}
}

// R1-4: an fsync failure removes the partial file; success leaves 0600 content.
func TestWriteSecretFileCleanup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad")
	syncFile = func(*os.File) error { return errors.New("EIO") }
	err := writeSecretFile(p, "tok\n")
	syncFile = (*os.File).Sync
	if err == nil {
		t.Fatal("sync failure not reported")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("partial token file left after sync failure")
	}
	ok := filepath.Join(dir, "ok")
	if err := writeSecretFile(ok, "tok\n"); err != nil {
		t.Fatal(err)
	}
	if readTok(t, ok) != "tok" {
		t.Fatal("content")
	}
}

// lateWrite errors on WriteIf without saving, then applies the write after
// the caller's re-read (Drive applying a request late).
type lateWrite struct {
	store.Backend
	pending func()
}

func (l *lateWrite) WriteIf(ctx context.Context, p string, d []byte, rev string) error {
	l.pending = func() { l.Backend.WriteIf(ctx, p, d, rev) }
	return errors.New("timeout awaiting response")
}

// R2-1: the write lands after the re-read: the file must still be there and work.
func TestGrantLateWriteKeepsToken(t *testing.T) {
	m := store.NewMemory()
	lw := &lateWrite{Backend: m}
	tp := filepath.Join(t.TempDir(), "b.token")
	if _, err := New(lw).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: tp}); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("want ErrOutcomeUnknown, got %v", err)
	}
	if lw.pending == nil {
		t.Fatal("no write attempted")
	}
	lw.pending() // Drive applies it late
	if id, err := load(t, m).Authenticate(readTok(t, tp)); err != nil || id != "b" {
		t.Fatalf("late-applied token unusable: %q %v", id, err)
	}
}

// dropWrite acknowledges WriteIf but never saves (an overwrite from another host).
type dropWrite struct{ store.Backend }

func (dropWrite) WriteIf(context.Context, string, []byte, string) error { return nil }

// R2-2: revoke and grant report success only after a re-read confirms the change.
func TestChangesVerifiedBeforeSuccess(t *testing.T) {
	m := store.NewMemory()
	dir := t.TempDir()
	if _, err := New(m).Grant(ctx, "b", "p", "write", GrantOpts{TokenOut: filepath.Join(dir, "b")}); err != nil {
		t.Fatal(err)
	}
	if err := New(dropWrite{m}).Revoke(ctx, "b", false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("unverified revoke reported: %v", err)
	}
	if load(t, m).RoleFor("b", "p") != acl.Write {
		t.Fatal("setup")
	}
	if err := New(m).Revoke(ctx, "b", false); err != nil {
		t.Fatalf("verified revoke: %v", err)
	}
	tp := filepath.Join(dir, "c")
	if _, err := New(dropWrite{m}).Grant(ctx, "c", "p", "read", GrantOpts{TokenOut: tp}); !errors.Is(err, ErrNotVerified) || !strings.Contains(err.Error(), tp) {
		t.Fatalf("unverified grant reported: %v", err)
	}
	if _, err := New(dropWrite{m}).Grant(ctx, "b", "q", "read", GrantOpts{Rotate: false}); err == nil {
		t.Fatal("grant to revoked bot accepted")
	}
}

// M3 follow-up: Remove deletes the entry, honours last-owner protection,
// and reports ErrNotVerified / ErrOutcomeUnknown like the other edits.
func TestRemove(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	dir := t.TempDir()
	for _, g := range [][3]string{{"pm", "p", "owner"}, {"dev", "p", "write"}, {"x", "p", "read"}} {
		if _, err := a.Grant(ctx, g[0], g[1], g[2], GrantOpts{TokenOut: filepath.Join(dir, g[0])}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Remove(ctx, "pm", false); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("last owner removed without force: %v", err)
	}
	if err := a.Remove(ctx, "nobody", false); !errors.Is(err, ErrNoBot) {
		t.Fatalf("missing bot: %v", err)
	}
	if err := New(dropWrite{m}).Remove(ctx, "dev", false); !errors.Is(err, ErrNotVerified) {
		t.Fatalf("unverified remove reported: %v", err)
	}
	if err := New(failNoSave{m}).Remove(ctx, "dev", false); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("failed write: %v", err)
	}
	if err := New(&saveThenFail{Backend: m, failWrites: true}).Remove(ctx, "dev", false); err != nil {
		t.Fatalf("landed write with lost response: %v", err)
	}
	if err := a.Remove(ctx, "x", false); err != nil {
		t.Fatal(err)
	}
	es, _ := a.List(ctx)
	if len(es) != 1 || es[0].Bot != "pm" {
		t.Fatalf("after remove: %+v", es)
	}
	if err := a.Remove(ctx, "pm", true); err != nil {
		t.Fatal(err)
	}
	if es, _ := a.List(ctx); len(es) != 0 {
		t.Fatalf("not empty: %+v", es)
	}
}

// R4-3: the last-owner error names the operation.
func TestLastOwnerMessageNamesOp(t *testing.T) {
	m := store.NewMemory()
	a := New(m)
	if _, err := a.Grant(ctx, "pm", "p", "owner", GrantOpts{TokenOut: filepath.Join(t.TempDir(), "pm")}); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"revoke", "remove"} {
		var err error
		if op == "revoke" {
			err = a.Revoke(ctx, "pm", false)
		} else {
			err = a.Remove(ctx, "pm", false)
		}
		if !errors.Is(err, ErrLastOwner) || !strings.Contains(err.Error(), "refusing to "+op+" ") {
			t.Fatalf("%s: %v", op, err)
		}
	}
}

// R4-4: an ambiguous revoke write is ErrOutcomeUnknown unless a re-read settles it.
func TestRevokeOutcomeUnknown(t *testing.T) {
	m := store.NewMemory()
	if _, err := New(m).Grant(ctx, "dev", "p", "write", GrantOpts{TokenOut: filepath.Join(t.TempDir(), "d")}); err != nil {
		t.Fatal(err)
	}
	if err := New(failNoSave{m}).Revoke(ctx, "dev", false); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("failed revoke write: %v", err)
	}
	if err := New(&saveThenFail{Backend: m, failWrites: true}).Revoke(ctx, "dev", false); err != nil {
		t.Fatalf("landed revoke with lost response: %v", err)
	}
}
