package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/instance"
	"github.com/radiancelux/botvault/internal/store"
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
	if strings.Contains(s, token) || strings.Contains(s, acl.HashToken(token)) || strings.Contains(s, "bv_") {
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
	t.Setenv("BOTVAULT_ACL_LOCK_FILE", lp)
	m := store.NewMemory()
	held, err := instance.Acquire(lp)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	tp := filepath.Join(dir, "b.token")
	err = runACLLocked(ctx, m, "root", []string{"grant", "b", "p", "write", "--token-out", tp}, &out)
	if err == nil || !strings.Contains(err.Error(), "another `botvault acl` run") {
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
	t.Setenv("BOTVAULT_ACL_LOCK_FILE", "")
	p := aclLockPath("someroot")
	if !strings.HasPrefix(p, "/var/tmp/botvault-acl-") || p == aclLockPath("other") || p == lockPath("someroot") {
		t.Fatalf("bad acl lock path %q", p)
	}
}
