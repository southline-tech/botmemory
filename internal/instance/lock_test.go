package instance

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSingleInstance(t *testing.T) {
	p := filepath.Join(t.TempDir(), "botvault.lock")
	l, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(p); !errors.Is(err, ErrRunning) {
		t.Fatalf("second instance allowed: %v", err)
	}
	l.Release()
	l2, err := Acquire(p)
	if err != nil {
		t.Fatal("not reacquirable after release:", err)
	}
	l2.Release()
}

// M3: a symlink at the lock path is refused and its target is untouched.
func TestLockRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("precious\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "botvault.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if l, err := Acquire(link); err == nil {
		l.Release()
		t.Fatal("Acquire followed a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "precious\n" {
		t.Fatalf("symlink target modified: %q", b)
	}
}
