package drive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/acladmin"
)

// M3: `botvault acl` against the (fake) Drive backend: the version check
// rejects a stale write and the retry keeps a concurrent edit.
func TestACLAdminOnDrive(t *testing.T) {
	_, nb := newFake(t)
	ctx := context.Background()
	dir := t.TempDir()
	a := acladmin.New(nb())
	for _, g := range []struct{ bot, role string }{
		{"pm-verifychain", "owner"}, {"implementor-verifychain", "write"}, {"reviewer-verifychain", "write"},
	} {
		if _, err := a.Grant(ctx, g.bot, "verifychain", g.role, acladmin.GrantOpts{TokenOut: filepath.Join(dir, g.bot)}); err != nil {
			t.Fatal(g.bot, err)
		}
	}
	// Another process edits _acl.json between our read and write.
	b := nb()
	data, rev, err := b.ReadRev(ctx, acladmin.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := acladmin.New(nb()).Revoke(ctx, "reviewer-verifychain"); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteIf(ctx, acladmin.Path, data, rev); err == nil {
		t.Fatal("stale ACL write accepted; version check missing")
	}
	raw, _ := b.Read(ctx, acladmin.Path)
	parsed, err := acl.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for bot, want := range map[string]acl.Role{"pm-verifychain": acl.Owner, "implementor-verifychain": acl.Write, "reviewer-verifychain": acl.Write} {
		tok, _ := os.ReadFile(filepath.Join(dir, bot))
		id, err := parsed.Authenticate(strings.TrimSpace(string(tok)))
		if bot == "reviewer-verifychain" {
			if err == nil {
				t.Fatal("revoked reviewer still authenticates")
			}
		} else if err != nil || id != bot || parsed.RoleFor(bot, "verifychain") != want || parsed.RoleFor(bot, "other") != acl.None {
			t.Fatalf("%s: id=%q err=%v", bot, id, err)
		}
		if strings.Contains(string(raw), strings.TrimSpace(string(tok))) {
			t.Fatal("raw token in Drive ACL")
		}
	}
}
