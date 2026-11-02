package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/southline-tech/botoptimizer/internal/store/local"
)

func TestMigrateDryRunLocalOnly(t *testing.T) {
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
	t.Setenv("BOTOPTIMIZER_DATA_DIR", dir)
	t.Setenv("BOTOPTIMIZER_DRIVE_ROOT_ID", "")
	r, w, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = w
	err = migrateMain([]string{"--from", "local", "--to", "drive", "--dry-run"})
	w.Close()
	os.Stdout = oldOut
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "would copy") {
		t.Fatalf("output: %q", out)
	}
	if !strings.Contains(out, "dry-run:") {
		t.Fatalf("output: %q", out)
	}
}
