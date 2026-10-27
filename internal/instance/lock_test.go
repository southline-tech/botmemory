package instance

import (
	"errors"
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
