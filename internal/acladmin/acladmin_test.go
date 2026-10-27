package acladmin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/store"
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

	if err := a.Revoke(ctx, "pm-verifychain"); err != nil {
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
	if err := a.Revoke(ctx, "nobody"); !errors.Is(err, ErrNoBot) {
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
			if err := other.Revoke(ctx, "other"); err != nil {
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
