package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/store"
)

func newVault(t *testing.T) (*Vault, *store.Memory) {
	t.Helper()
	h := acl.HashToken("x")
	a, err := acl.Parse([]byte(fmt.Sprintf(`{"bots":{
	 "pm":{"token_sha256":%q,"roles":{"vc":"owner"}},
	 "impl":{"token_sha256":%q,"roles":{"vc":"write"}},
	 "ro":{"token_sha256":%q,"roles":{"vc":"read"}},
	 "grok":{"token_sha256":%q,"roles":{"*":"write"}}}}`, h, h, h, h)))
	if err != nil {
		t.Fatal(err)
	}
	m := store.NewMemory()
	v := New(m, func() *acl.ACL { return a })
	n := 0
	v.SetClock(func() time.Time { n++; return time.Date(2026, 10, 26, 0, 0, n, 0, time.UTC) })
	return v, m
}

func iv(n int) *int { return &n }

var ctx = context.Background()

func put(v *Vault, bot string, ifv *int, body string) (*Record, error) {
	return v.Put(ctx, bot, PutInput{Project: "vc", Kind: "decision", Key: "k1", Title: "T", Body: body, IfVersion: ifv})
}

func TestPutGetAttributionAndHistory(t *testing.T) {
	v, m := newVault(t)
	r1, err := put(v, "pm", iv(0), "one")
	if err != nil || r1.Version != 1 || r1.CreatedBy != "pm" || r1.UpdatedBy != "pm" || r1.Status != "active" {
		t.Fatalf("%+v %v", r1, err)
	}
	r2, err := put(v, "impl", iv(1), "two")
	if err != nil || r2.Version != 2 || r2.CreatedBy != "pm" || r2.UpdatedBy != "impl" || !r2.CreatedAt.Equal(r1.CreatedAt) {
		t.Fatalf("%+v %v", r2, err)
	}
	if _, err := m.Read(ctx, "projects/vc/history/decision/k1/v1.json"); err != nil {
		t.Fatal("history copy missing:", err)
	}
	old, err := v.Get(ctx, "ro", "vc", "decision", "k1", 1)
	if err != nil || old.Body != "one" {
		t.Fatalf("%+v %v", old, err)
	}
	cur, _ := v.Get(ctx, "ro", "vc", "decision", "k1", 0)
	if cur.Body != "two" || cur.ETag != "k1:2" {
		t.Fatalf("%+v", cur)
	}
	if _, err := v.Get(ctx, "ro", "vc", "decision", "k1", 9); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestStaleWriteFails(t *testing.T) {
	v, _ := newVault(t)
	put(v, "pm", nil, "a")
	put(v, "pm", nil, "b") // now v2
	if _, err := put(v, "impl", iv(1), "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if _, err := put(v, "impl", iv(0), "create-only"); !errors.Is(err, ErrConflict) {
		t.Fatalf("create-only on existing: %v", err)
	}
	if r, _ := v.Get(ctx, "pm", "vc", "decision", "k1", 0); r.Body != "b" || r.Version != 2 {
		t.Fatalf("stale write mutated record: %+v", r)
	}
}

func TestConcurrentIfVersionOneWinner(t *testing.T) {
	v, _ := newVault(t)
	put(v, "pm", nil, "base")
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := put(v, "impl", iv(1), "race"); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins=%d", wins)
	}
}

func TestRoles(t *testing.T) {
	v, _ := newVault(t)
	if _, err := put(v, "ro", nil, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatal("reader wrote")
	}
	if _, err := put(v, "stranger", nil, "x"); !errors.Is(err, ErrForbidden) {
		t.Fatal("stranger wrote")
	}
	if _, err := v.Get(ctx, "stranger", "vc", "decision", "k1", 0); !errors.Is(err, ErrForbidden) {
		t.Fatal("stranger read")
	}
	put(v, "impl", nil, "x")
	if _, err := v.Delete(ctx, "impl", "vc", "decision", "k1", nil); !errors.Is(err, ErrForbidden) {
		t.Fatal("writer deleted")
	}
	if _, err := v.Delete(ctx, "grok", "vc", "decision", "k1", nil); !errors.Is(err, ErrForbidden) {
		t.Fatal("wildcard writer deleted")
	}
	if _, err := v.Put(ctx, "grok", PutInput{Project: "other", Kind: "note", Key: "n", Title: "t"}); err != nil {
		t.Fatal("wildcard write:", err)
	}
}

func TestDeleteArchivesOnly(t *testing.T) {
	v, m := newVault(t)
	put(v, "pm", nil, "keep me")
	if _, err := v.Delete(ctx, "pm", "vc", "decision", "k1", iv(5)); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	r, err := v.Delete(ctx, "pm", "vc", "decision", "k1", iv(1))
	if err != nil || r.Status != StatusArchived || r.Version != 2 || r.UpdatedBy != "pm" || r.Body != "keep me" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := m.Read(ctx, "projects/vc/records/decision/k1.json"); err != nil {
		t.Fatal("record removed from storage")
	}
	if l, _ := v.List(ctx, "pm", "vc", ListFilter{}); len(l) != 0 {
		t.Fatal("archived shown by default")
	}
	if l, _ := v.List(ctx, "pm", "vc", ListFilter{Status: StatusArchived}); len(l) != 1 {
		t.Fatal("archived not listable")
	}
	if _, err := put(v, "impl", nil, "x"); !errors.Is(err, ErrConflict) {
		t.Fatal("blind put revived archived record")
	}
	if _, err := v.Append(ctx, "impl", "vc", "decision", "k1", "x"); !errors.Is(err, ErrConflict) {
		t.Fatal("append to archived")
	}
	if r, err := put(v, "pm", iv(2), "restored"); err != nil || r.Status != "active" {
		t.Fatal("restore with if_version failed", err)
	}
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "z", Title: "t", Status: StatusArchived}); !errors.Is(err, ErrInvalid) {
		t.Fatal("put with status archived")
	}
}

func TestAppend(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Append(ctx, "impl", "vc", "decision", "k1", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	put(v, "pm", nil, "start")
	r, err := v.Append(ctx, "impl", "vc", "decision", "k1", "more")
	if err != nil || r.Version != 2 || r.UpdatedBy != "impl" || !strings.Contains(r.Body, "— impl, ") || !strings.HasSuffix(r.Body, "more\n") || !strings.HasPrefix(r.Body, "start") {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := v.Append(ctx, "ro", "vc", "decision", "k1", "x"); !errors.Is(err, ErrForbidden) {
		t.Fatal("reader appended")
	}
}

func TestListAndProjects(t *testing.T) {
	v, _ := newVault(t)
	v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "spec", Key: "a", Title: "A", Body: "bodyA", Tags: []string{"m1"}})
	v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "b", Title: "B", Status: "draft"})
	v.Put(ctx, "grok", PutInput{Project: "secret", Kind: "note", Key: "c", Title: "C"})
	l, err := v.List(ctx, "ro", "vc", ListFilter{})
	if err != nil || len(l) != 2 || l[0].Key != "b" || l[1].Body != "" {
		t.Fatalf("%+v %v", l, err)
	}
	if l, _ := v.List(ctx, "ro", "vc", ListFilter{Tag: "m1"}); len(l) != 1 || l[0].Key != "a" {
		t.Fatal(l)
	}
	if l, _ := v.List(ctx, "ro", "vc", ListFilter{Kind: "note", Status: "draft"}); len(l) != 1 {
		t.Fatal(l)
	}
	if _, err := v.List(ctx, "ro", "vc", ListFilter{Kind: "bogus"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if p, _ := v.ListProjects(ctx, "ro"); len(p) != 1 || p[0] != "vc" {
		t.Fatal("ro projects", p)
	}
	if p, _ := v.ListProjects(ctx, "grok"); len(p) != 2 {
		t.Fatal("grok projects", p)
	}
}

func TestValidation(t *testing.T) {
	v, _ := newVault(t)
	for _, in := range []PutInput{
		{Project: "../x", Kind: "note", Key: "k"},
		{Project: "vc", Kind: "secret", Key: "k"},
		{Project: "vc", Kind: "note", Key: "a/b"},
		{Project: "vc", Kind: "note", Key: "UPPER"},
	} {
		if _, err := v.Put(ctx, "pm", in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", in, err)
		}
	}
}
