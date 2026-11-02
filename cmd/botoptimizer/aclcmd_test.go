package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/southline-tech/botoptimizer/internal/acl"
	"github.com/southline-tech/botoptimizer/internal/instance"
	"github.com/southline-tech/botoptimizer/internal/store"
)

// M3: the CLI never prints a token or a hash, and works on the memory backend.
func TestACLCommandMemory(t *testing.T) {
	ctx := context.Background()
	m := store.NewMemory()
	dir := t.TempDir()
	var out bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		if err := runACL(ctx, m, args, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	tp := filepath.Join(dir, "pm.token")
	run("grant", "pm-verifychain", "verifychain", "owner", "--token-out", tp)
	run("grant", "reviewer-verifychain", "verifychain", "write", "--token-out="+filepath.Join(dir, "rv.token"))
	run("revoke", "reviewer-verifychain")
	run("list")
	tok, _ := os.ReadFile(tp)
	token := strings.TrimSpace(string(tok))
	s := out.String()
	if strings.Contains(s, token) || strings.Contains(s, acl.HashToken(token)) || strings.Contains(s, "bo_") {
		t.Fatal("CLI output contains a token or hash")
	}
	for _, want := range []string{"pm-verifychain", "active", "verifychain=owner", "reviewer-verifychain", "REVOKED", "verifychain=write"} {
		if !strings.Contains(s, want) {
			t.Fatalf("list output missing %q:\n%s", want, s)
		}
	}
	if err := runACL(ctx, m, []string{"revoke", "pm-verifychain"}, &out); err == nil || !strings.Contains(err.Error(), "last active owner") {
		t.Fatalf("sole owner revoked without --force: %v", err)
	}
	if err := runACL(ctx, m, []string{"revoke", "pm-verifychain", "--force"}, &out); err != nil {
		t.Fatal(err)
	}
	run("grant", "tmp-bot", "verifychain", "read", "--token-out", filepath.Join(dir, "tmp.token"))
	run("remove", "tmp-bot")
	out.Reset()
	run("list")
	if strings.Contains(out.String(), "tmp-bot") {
		t.Fatal("removed bot still listed")
	}
	if err := runACL(ctx, m, []string{"remove", "tmp-bot"}, &out); err == nil {
		t.Fatal("removing a missing bot succeeded")
	}
	if err := runACL(ctx, m, []string{"remove", "x", "--token-out", "f"}, &out); err == nil {
		t.Fatal("--token-out accepted on remove")
	}
	if err := runACL(ctx, m, []string{"grant", "x", "p", "read", "--force", "--token-out", filepath.Join(dir, "x")}, &out); err == nil {
		t.Fatal("--force accepted on grant")
	}
	if err := runACL(ctx, m, []string{"grant", "x", "p", "read"}, &out); err == nil {
		t.Fatal("grant without --token-out accepted")
	}
	if err := runACL(ctx, m, []string{"grant", "x", "p", "read", "--bogus"}, &out); err == nil {
		t.Fatal("unknown flag accepted")
	}
}

// R2-2: a second acl run is refused while another holds the ACL lock.
func TestACLLockRefusesConcurrentRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	lp := filepath.Join(dir, "acl.lock")
	t.Setenv("BOTOPTIMIZER_ACL_LOCK_FILE", lp)
	m := store.NewMemory()
	held, err := instance.Acquire(lp)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	tp := filepath.Join(dir, "b.token")
	err = runACLLocked(ctx, m, "root", []string{"grant", "b", "p", "write", "--token-out", tp}, &out)
	if err == nil || !strings.Contains(err.Error(), "another `botoptimizer acl` run") {
		t.Fatalf("concurrent run not refused: %v", err)
	}
	if _, err := os.Stat(tp); !os.IsNotExist(err) {
		t.Fatal("refused run minted a token")
	}
	if _, err := m.Read(ctx, "_acl.json"); err == nil {
		t.Fatal("refused run wrote the ACL")
	}
	held.Release()
	if err := runACLLocked(ctx, m, "root", []string{"grant", "b", "p", "write", "--token-out", tp}, &out); err != nil {
		t.Fatalf("after release: %v", err)
	}
}

func TestACLLockPath(t *testing.T) {
	t.Setenv("BOTOPTIMIZER_ACL_LOCK_FILE", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // must not matter (R4-1)
	rt := filepath.Dir(mustUserDir(t))
	p, err := aclLockPath("someroot")
	if err != nil {
		t.Fatal(err)
	}
	q, _ := aclLockPath("other")
	if !strings.HasPrefix(p, filepath.Join(rt, "botoptimizer", "acl-")) || strings.HasPrefix(p, "/var/tmp") || p == q || p == lockPath("someroot") {
		t.Fatalf("bad acl lock path %q", p)
	}
	if fi, err := os.Stat(filepath.Dir(p)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("lock dir not 0700: %v %v", fi, err)
	}
}

func mustUserDir(t *testing.T) string {
	t.Helper()
	d, err := instance.UserDir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
