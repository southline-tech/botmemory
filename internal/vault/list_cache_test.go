package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/southline-tech/botmemory/internal/store"
)

// readCountBackend counts Read/ReadRev calls (Drive list cost proxy).
type readCountBackend struct {
	store.Backend
	n atomic.Int64
}

func (c *readCountBackend) Read(ctx context.Context, path string) ([]byte, error) {
	c.n.Add(1)
	return c.Backend.Read(ctx, path)
}

func (c *readCountBackend) ReadRev(ctx context.Context, path string) ([]byte, string, error) {
	c.n.Add(1)
	return c.Backend.ReadRev(ctx, path)
}

func seedProject(t *testing.T, v *Vault, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		key := fmtKey(i)
		if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: key, Title: key, Body: "x"}); err != nil {
			t.Fatal(err)
		}
	}
}

func fmtKey(i int) string {
	return fmt.Sprintf("k%03d", i)
}

func TestListUsesIndexCache(t *testing.T) {
	v, m := newVault(t)
	IndexCacheTTL = time.Hour
	t.Cleanup(func() { IndexCacheTTL = 3 * time.Second })
	inner := &readCountBackend{Backend: m}
	v.b = inner
	seedProject(t, v, 5)
	v.dropIndexCache("vc")
	inner.n.Store(0)
	if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
		t.Fatal(err)
	}
	warmReads := inner.n.Load()
	if warmReads == 0 {
		t.Fatal("first list after cache drop should read _index.json from storage")
	}
	inner.n.Store(0)
	for i := 0; i < 20; i++ {
		if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
			t.Fatal(err)
		}
	}
	if inner.n.Load() != 0 {
		t.Fatalf("warm list should not read storage, got %d reads", inner.n.Load())
	}
}

func TestListAfterOwnWrite(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "fresh", Title: "Fresh", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	l, err := v.List(ctx, "ro", "vc", ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range l {
		if e.Key == "fresh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("list after own write: %v", l)
	}
}

func TestListAfterExternalWriteAfterTTL(t *testing.T) {
	v, m := newVault(t)
	var tick atomic.Int64
	v.SetClock(func() time.Time { return time.Unix(tick.Load(), 0) })
	IndexCacheTTL = 2 * time.Second
	t.Cleanup(func() { IndexCacheTTL = 3 * time.Second })

	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "a", Title: "A", Body: "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
		t.Fatal(err)
	}
	// Simulate another token/process updating index on shared storage.
	data, _ := m.Read(ctx, indexPath("vc"))
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	idx.Entries["ext"] = IndexEntry{Key: "ext", Title: "External", Status: "active", Version: 1, Summary: "s"}
	b, _ := json.MarshalIndent(idx, "", "  ")
	if err := m.Write(ctx, indexPath("vc"), b); err != nil {
		t.Fatal(err)
	}
	l, _ := v.List(ctx, "ro", "vc", ListFilter{})
	if len(l) != 1 {
		t.Fatalf("cache should hide external entry before TTL: %v", l)
	}
	tick.Add(3)
	l, _ = v.List(ctx, "ro", "vc", ListFilter{})
	if len(l) != 2 {
		t.Fatalf("after TTL list should reload index: %v", l)
	}
}

func TestListHidesArchivedUnlessFilter(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "live", Title: "L", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "gone", Title: "G", Body: "y"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Delete(ctx, "pm", "vc", "gone", iv(1)); err != nil {
		t.Fatal(err)
	}
	l, err := v.List(ctx, "ro", "vc", ListFilter{})
	if err != nil || len(l) != 1 || l[0].Key != "live" {
		t.Fatalf("default list: %v %v", l, err)
	}
	l, err = v.List(ctx, "ro", "vc", ListFilter{Status: StatusArchived})
	if err != nil || len(l) != 1 || l[0].Key != "gone" {
		t.Fatalf("archived filter: %v %v", l, err)
	}
}

func BenchmarkListWarmLocal(b *testing.B) {
	v, _ := newVault(b)
	IndexCacheTTL = time.Hour
	b.Cleanup(func() { IndexCacheTTL = 3 * time.Second })
	for i := 0; i < 100; i++ {
		key := fmtKey(i)
		if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: key, Title: key, Body: "body"}); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListWarmSimulatedDrive(b *testing.B) {
	v, m := newVault(b)
	const perRead = 25 * time.Millisecond
	slow := &slowReadBackend{Backend: m, d: perRead}
	v.b = slow
	IndexCacheTTL = time.Hour
	b.Cleanup(func() { IndexCacheTTL = 3 * time.Second })
	for i := 0; i < 100; i++ {
		key := fmtKey(i)
		if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: key, Title: key, Body: "body"}); err != nil {
			b.Fatal(err)
		}
	}
	if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.List(ctx, "ro", "vc", ListFilter{}); err != nil {
			b.Fatal(err)
		}
	}
}

type slowReadBackend struct {
	store.Backend
	d time.Duration
}

func (s *slowReadBackend) Read(ctx context.Context, path string) ([]byte, error) {
	time.Sleep(s.d)
	return s.Backend.Read(ctx, path)
}

func (s *slowReadBackend) ReadRev(ctx context.Context, path string) ([]byte, string, error) {
	time.Sleep(s.d)
	return s.Backend.ReadRev(ctx, path)
}

// BenchmarkListColdSimulatedDrive measures first list after seed (index read / rebuild).
func BenchmarkListColdSimulatedDrive(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		v, m := newVault(b)
		slow := &slowReadBackend{Backend: m, d: 25 * time.Millisecond}
		v.b = slow
		IndexCacheTTL = 0
		for j := 0; j < 100; j++ {
			key := "k" + string(rune('a'+j%26))
			v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: key, Title: key, Body: "b"})
		}
		v.dropIndexCache("vc")
		b.StartTimer()
		v.List(ctx, "ro", "vc", ListFilter{})
	}
}
