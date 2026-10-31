package main

import (
	"context"
	"testing"

	"github.com/southline-tech/botmemory/internal/store/local"
)

func TestMigrateDryRunNeedsDrive(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	b, err := local.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Write(ctx, "_acl.json", []byte(`{"bots":{}}`)); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(ctx, "projects/demo/records/k.json", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOTMEMORY_DATA_DIR", dir)
	t.Setenv("BOTMEMORY_DRIVE_ROOT_ID", "")
	if err := migrateMain([]string{"--from", "local", "--to", "drive", "--dry-run"}); err == nil {
		t.Fatal("expected error without drive credentials")
	}
}
