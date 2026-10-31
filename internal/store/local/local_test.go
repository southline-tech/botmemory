package local_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/store/local"
)

func TestLocalBackendReadWrite(t *testing.T) {
	dir := t.TempDir()
	b, err := local.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := b.Write(ctx, "a/b.json", []byte("1")); err != nil {
		t.Fatal(err)
	}
	data, rev, err := b.ReadRev(ctx, "a/b.json")
	if err != nil || string(data) != "1" || rev == "" {
		t.Fatalf("read %q rev=%q err=%v", data, rev, err)
	}
	if err := b.WriteIf(ctx, "a/b.json", []byte("2"), rev); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteIf(ctx, "a/b.json", []byte("3"), rev); !errors.Is(err, store.ErrPrecondition) {
		t.Fatalf("want precondition, got %v", err)
	}
}

func TestLocalPathEscape(t *testing.T) {
	dir := t.TempDir()
	b, _ := local.New(dir)
	ctx := context.Background()
	if err := b.Write(ctx, "../etc/passwd", []byte("x")); err == nil {
		t.Fatal("expected path escape error")
	}
}

func TestLocalFileMode(t *testing.T) {
	dir := t.TempDir()
	b, _ := local.New(dir)
	ctx := context.Background()
	_ = b.Write(ctx, "f.json", []byte("{}"))
	fi, err := os.Stat(filepath.Join(dir, "f.json"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err=%v", fi.Mode(), err)
	}
}
