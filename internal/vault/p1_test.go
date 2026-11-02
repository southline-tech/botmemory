package vault

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestKeyRejectsSlash(t *testing.T) {
	v, _ := newVault(t)
	_, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "feedback/foo", Title: "T", Body: "b"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want invalid key, got %v", err)
	}
}

func TestBriefTruncationAndEtag(t *testing.T) {
	v, _ := newVault(t)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("k%03d", i)
		if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: key, Title: key, Body: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	b1, err := v.Brief(ctx, "ro", "vc", BriefOpts{Limit: 10})
	if err != nil || !b1.Truncated || len(b1.Keys) != 10 || b1.Project != "vc" || b1.Version < 1 {
		t.Fatalf("brief cap: %+v %v", b1, err)
	}
	b2, err := v.Brief(ctx, "ro", "vc", BriefOpts{IfEtag: b1.Etag})
	if err != nil || !b2.Unchanged {
		t.Fatalf("if_etag: %+v", b2)
	}
}

func TestGetPartial(t *testing.T) {
	v, _ := newVault(t)
	body := "line1\nline2\nline3\nline4"
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "doc", Title: "D", Body: body}); err != nil {
		t.Fatal(err)
	}
	out, err := v.GetWithOpts(ctx, "ro", "vc", "doc", 0, GetOpts{Head: 2})
	if err != nil {
		t.Fatal(err)
	}
	g := out.(GetOutput)
	if g.Body != "line1\nline2" || g.Mode != "head" || g.N != 2 || !g.Truncated {
		t.Fatalf("head: %+v", g)
	}
	if _, err := v.GetWithOpts(ctx, "ro", "vc", "doc", 0, GetOpts{Head: 1, Tail: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatal("head+tail", err)
	}
	sum, err := v.GetWithOpts(ctx, "ro", "vc", "doc", 0, GetOpts{Summary: true})
	if err != nil {
		t.Fatal(err)
	}
	e := sum.(IndexEntry)
	if e.Key != "doc" || e.Title != "D" {
		t.Fatalf("summary entry: %+v", e)
	}
}

func TestGetTail(t *testing.T) {
	v, _ := newVault(t)
	body := "line1\nline2\nline3\nline4"
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "doc", Title: "D", Body: body}); err != nil {
		t.Fatal(err)
	}
	out, err := v.GetWithOpts(ctx, "ro", "vc", "doc", 0, GetOpts{Tail: 2})
	if err != nil {
		t.Fatal(err)
	}
	g := out.(GetOutput)
	if g.Body != "line3\nline4" || g.Mode != "tail" || g.N != 2 || !g.Truncated {
		t.Fatalf("tail: %+v", g)
	}
}

func TestGetMaxBytesTruncated(t *testing.T) {
	v, _ := newVault(t)
	body := "hello " + strings.Repeat("🙂", 20)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "wide", Title: "W", Body: body}); err != nil {
		t.Fatal(err)
	}
	out, err := v.GetWithOpts(ctx, "ro", "vc", "wide", 0, GetOpts{MaxBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	g := out.(GetOutput)
	if g.Mode != "max_bytes" || g.N != 16 || !g.Truncated {
		t.Fatalf("meta: %+v", g)
	}
	if !strings.HasSuffix(g.Body, "…") || len(g.Body) > 16 {
		t.Fatalf("body len=%d %q", len(g.Body), g.Body)
	}
	if !utf8.ValidString(g.Body) {
		t.Fatalf("invalid utf8: %q", g.Body)
	}
}

func TestGetSinceDateForm(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "log", Title: "L", Body: "base"}); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	v.SetClock(func() time.Time { return stamp })
	if _, err := v.Append(ctx, "pm", "vc", "log", "new bit", "note"); err != nil {
		t.Fatal(err)
	}
	since, err := ParseSince("2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	out, err := v.GetWithOpts(ctx, "ro", "vc", "log", 0, GetOpts{Since: since, HasSince: true})
	if err != nil {
		t.Fatal(err)
	}
	g := out.(GetOutput)
	if g.Mode != "since" || g.SinceApplied == nil || !*g.SinceApplied {
		t.Fatalf("since get: %+v", g)
	}
	if !strings.Contains(g.Body, "new bit") {
		t.Fatalf("body %q", g.Body)
	}
}

func TestChangedSinceDateForm(t *testing.T) {
	v, _ := newVault(t)
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	v.SetClock(func() time.Time { return day.Add(-time.Hour) })
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "old", Title: "O", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	v.SetClock(func() time.Time { return day.Add(2 * time.Hour) })
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "new", Title: "N", Body: "y"}); err != nil {
		t.Fatal(err)
	}
	since, err := ParseSince("2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := v.Changed(ctx, "ro", "vc", since, ListFilter{})
	if err != nil || !ch.Changed || ch.Count != 1 || ch.Keys[0] != "new" {
		t.Fatalf("%+v %v", ch, err)
	}
}

func TestGetSinceAppend(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "log", Title: "L", Body: "base"}); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	v.SetClock(func() time.Time { return stamp })
	if _, err := v.Append(ctx, "pm", "vc", "log", "new bit", "note"); err != nil {
		t.Fatal(err)
	}
	out, err := v.GetWithOpts(ctx, "ro", "vc", "log", 0, GetOpts{Since: stamp.Add(-time.Second), HasSince: true})
	if err != nil {
		t.Fatal(err)
	}
	g := out.(GetOutput)
	if g.SinceApplied == nil || !*g.SinceApplied {
		t.Fatalf("since not applied: %+v", g)
	}
	if g.Mode != "since" {
		t.Fatalf("mode %q", g.Mode)
	}
	if !strings.Contains(g.Body, "new bit") {
		t.Fatalf("body %q", g.Body)
	}
}

func TestChangedSince(t *testing.T) {
	v, _ := newVault(t)
	since := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	v.SetClock(func() time.Time { return since.Add(-time.Hour) })
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "old", Title: "O", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	v.SetClock(func() time.Time { return since.Add(time.Minute) })
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "new", Title: "N", Body: "y"}); err != nil {
		t.Fatal(err)
	}
	ch, err := v.Changed(ctx, "ro", "vc", since, ListFilter{})
	if err != nil || !ch.Changed || ch.Count != 1 || ch.Keys[0] != "new" {
		t.Fatalf("%+v %v", ch, err)
	}
}

func TestListIfEtag(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Kind: "note", Key: "a", Title: "A", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	entries, err := v.List(ctx, "ro", "vc", ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	etag := indexEtag(entries)
	out, err := v.ListWithEtag(ctx, "ro", "vc", ListFilter{}, etag)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["unchanged"] != true {
		t.Fatalf("%v", out)
	}
}

func TestBriefForbidden(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Brief(ctx, "ro", "secret", BriefOpts{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("got %v", err)
	}
}
