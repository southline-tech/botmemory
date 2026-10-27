package store

import (
	"context"
	"reflect"
	"testing"
)

func TestMemory(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	if _, err := m.Read(ctx, "a/b.json"); err != ErrNotFound {
		t.Fatal(err)
	}
	m.Write(ctx, "/a/b.json", []byte("1"))
	m.Write(ctx, "a/c/d.json", []byte("2"))
	b, _ := m.Read(ctx, "a/b.json")
	b[0] = 'x' // must not alias
	if b2, _ := m.Read(ctx, "a/b.json"); string(b2) != "1" {
		t.Fatal("aliasing")
	}
	got, _ := m.List(ctx, "a")
	if !reflect.DeepEqual(got, []string{"b.json", "c/"}) {
		t.Fatal(got)
	}
	if got, _ := m.List(ctx, "missing"); len(got) != 0 {
		t.Fatal(got)
	}
}
