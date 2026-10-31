package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBackendLocalDefault(t *testing.T) {
	t.Setenv("BOTMEMORY_BACKEND", "")
	t.Setenv("BOTMEMORY_DRIVE_ROOT_ID", "")
	dir := t.TempDir()
	t.Setenv("BOTMEMORY_DATA_DIR", dir)
	b, root, err := resolveBackend(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Fatalf("root %q", root)
	}
	if err := b.Write(context.Background(), "probe.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
}

func TestResolveBackendDriveCompatWarning(t *testing.T) {
	t.Setenv("BOTMEMORY_BACKEND", "")
	t.Setenv("BOTMEMORY_DRIVE_ROOT_ID", "fake-root")
	t.Setenv("BOTMEMORY_DATA_DIR", t.TempDir())
	_, _, err := resolveBackend(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected drive client error without oauth")
	}
}

func TestResolveBackendStorageFlag(t *testing.T) {
	t.Setenv("BOTMEMORY_BACKEND", "")
	dir := t.TempDir()
	b, _, err := resolveBackend(context.Background(), "local", dir)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "a.txt")
	_ = b
	if _, err := os.Stat(p); err == nil {
		t.Fatal("unexpected")
	}
}
