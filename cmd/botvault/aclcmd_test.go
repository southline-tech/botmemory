package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiancelux/botvault/internal/acl"
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
	if err := runACL(ctx, m, []string{"grant", "x", "p", "read"}, &out); err == nil {
		t.Fatal("grant without --token-out accepted")
	}
	if err := runACL(ctx, m, []string{"grant", "x", "p", "read", "--bogus"}, &out); err == nil {
		t.Fatal("unknown flag accepted")
	}
}
