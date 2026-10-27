package vault

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/radiancelux/botvault/internal/store"
)

func TestIndexAndLogMaintained(t *testing.T) {
	v, m := newVault(t)
	long := strings.Repeat("é", 600)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "Alpha", Body: long, Tags: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Append(ctx, "impl", "vc", "a", "more"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Delete(ctx, "pm", "vc", "a", iv(2)); err != nil {
		t.Fatal(err)
	}
	data, _ := m.Read(ctx, indexPath("vc"))
	var idx Index
	json.Unmarshal(data, &idx)
	e := idx.Entries["a"]
	if e.Version != 3 || e.Status != StatusArchived || len([]rune(e.Summary)) != summaryLen || e.UpdatedBy != "pm" {
		t.Fatalf("index entry %+v", e)
	}
	logb, _ := m.Read(ctx, logPath("vc"))
	var ops []string
	sc := bufio.NewScanner(bytes.NewReader(logb))
	for sc.Scan() {
		var le LogEntry
		if err := json.Unmarshal(sc.Bytes(), &le); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, le.Op+":"+le.Bot)
	}
	if strings.Join(ops, ",") != "put:pm,append:impl,delete:pm" {
		t.Fatalf("log %v", ops)
	}
}

func TestSearch(t *testing.T) {
	v, _ := newVault(t)
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "auth-plan", Kind: "decision", Title: "Auth Plan", Body: "use OAuth refresh tokens", Tags: []string{"sec"}})
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "ui", Title: "UI notes", Body: "dark mode"})
	v.Put(ctx, "grok", PutInput{Project: "other", Key: "o1", Title: "oauth elsewhere", Body: "x"})
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "old", Title: "oauth old"})
	v.Delete(ctx, "pm", "vc", "old", iv(1))

	hits, err := v.Search(ctx, "pm", "vc", "OAUTH tokens", ListFilter{}, 0)
	if err != nil || len(hits) != 1 || hits[0].Key != "auth-plan" {
		t.Fatalf("all-terms: %v %v", hits, err)
	}
	if hits, _ := v.Search(ctx, "pm", "vc", "oauth", ListFilter{Status: StatusArchived}, 0); len(hits) != 1 || hits[0].Key != "old" {
		t.Fatalf("archived filter %v", hits)
	}
	if hits, _ := v.Search(ctx, "pm", "vc", "oauth", ListFilter{Kind: "decision", Tag: "sec"}, 0); len(hits) != 1 {
		t.Fatalf("kind/tag %v", hits)
	}
	// "*" only spans readable projects: grok reads vc+other, pm only vc.
	if hits, _ := v.Search(ctx, "grok", "*", "oauth", ListFilter{}, 0); len(hits) != 2 {
		t.Fatalf("grok * %v", hits)
	}
	if hits, _ := v.Search(ctx, "pm", "*", "elsewhere", ListFilter{}, 0); len(hits) != 0 {
		t.Fatalf("pm leaked other project %v", hits)
	}
	if _, err := v.Search(ctx, "pm", "other", "x", ListFilter{}, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("forbidden: %v", err)
	}
	if _, err := v.Search(ctx, "pm", "vc", "  ", ListFilter{}, 0); !errors.Is(err, ErrInvalid) {
		t.Fatal("empty query accepted")
	}
	if hits, _ := v.Search(ctx, "pm", "vc", "a", ListFilter{}, 1); len(hits) != 1 {
		t.Fatalf("limit %v", hits)
	}
}

func TestIndexSelfHealsAndReindex(t *testing.T) {
	v, m := newVault(t)
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "findme"})
	m.Write(ctx, indexPath("vc"), []byte("{corrupt"))
	if hits, err := v.Search(ctx, "pm", "vc", "findme", ListFilter{}, 0); err != nil || len(hits) != 1 {
		t.Fatalf("corrupt index not rebuilt: %v %v", hits, err)
	}
	if _, err := v.Reindex(ctx, "impl", "vc"); !errors.Is(err, ErrForbidden) {
		t.Fatal("non-owner reindexed")
	}
	if n, err := v.Reindex(ctx, "pm", "vc"); err != nil || n != 1 {
		t.Fatalf("reindex %d %v", n, err)
	}
}

func TestDerivedFailureDoesNotFailWrite(t *testing.T) {
	v, m := newVault(t)
	m.FailWrite = func(p string) error {
		if strings.HasSuffix(p, "_index.json") {
			return errors.New("boom")
		}
		return nil
	}
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "t"}); err != nil {
		t.Fatalf("write failed due to index: %v", err)
	}
	m.FailWrite = nil
	if n, _ := v.Reindex(ctx, "pm", "vc"); n != 1 {
		t.Fatal("reindex did not recover")
	}
}

func TestHistoryAndGetByVersion(t *testing.T) {
	v, _ := newVault(t)
	put(v, "pm", iv(0), "one")
	put(v, "impl", iv(1), "two")
	put(v, "pm", iv(2), "three")
	h, err := v.History(ctx, "ro", "vc", "k1")
	if err != nil || len(h) != 3 || h[0].Version != 3 || !h[0].Current || h[1].UpdatedBy != "impl" || h[2].Version != 1 {
		t.Fatalf("history %+v %v", h, err)
	}
	r, err := v.Get(ctx, "ro", "vc", "k1", 2)
	if err != nil || r.Body != "two" {
		t.Fatalf("get v2 %v %v", r, err)
	}
	if _, err := v.Get(ctx, "ro", "vc", "k1", 9); !errors.Is(err, ErrNotFound) {
		t.Fatal("future version")
	}
	if _, err := v.History(ctx, "nobody", "vc", "k1"); err == nil {
		t.Fatal("unauthorized history")
	}
}

// R1-3: a failed derived update marks the index dirty; the next access
// rebuilds it, so an archived record never shows as active.
func TestStaleIndexRebuiltAfterDerivedFailure(t *testing.T) {
	v, m := newVault(t)
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "alpha"}); err != nil {
		t.Fatal(err)
	}
	m.FailWrite = func(p string) error {
		if strings.HasSuffix(p, "_index.json") {
			return errors.New("boom")
		}
		return nil
	}
	if _, err := v.Delete(ctx, "pm", "vc", "a", iv(1)); err != nil {
		t.Fatal(err)
	}
	m.FailWrite = nil
	hits, err := v.Search(ctx, "pm", "vc", "alpha", ListFilter{}, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("archived record surfaced as active: %+v %v", hits, err)
	}
	hits, _ = v.Search(ctx, "pm", "vc", "alpha", ListFilter{Status: StatusArchived}, 0)
	if len(hits) != 1 || hits[0].Version != 2 {
		t.Fatalf("archived search %+v", hits)
	}
	// The rebuilt index was persisted and the dirty mark cleared.
	data, _ := m.Read(ctx, indexPath("vc"))
	var idx Index
	json.Unmarshal(data, &idx)
	if idx.Entries["a"].Status != StatusArchived {
		t.Fatalf("index not saved: %+v", idx.Entries["a"])
	}
	if _, d := v.dirty.Load("vc"); d {
		t.Fatal("still dirty")
	}
}

// R1-4: search saves a rebuilt index, and one corrupt record does not break
// "*" search or later index updates.
func TestSearchSavesRebuildAndSkipsCorrupt(t *testing.T) {
	v, m := newVault(t)
	v.Put(ctx, "grok", PutInput{Project: "vc", Key: "a", Title: "alpha"})
	v.Put(ctx, "grok", PutInput{Project: "other", Key: "b", Title: "alpha two"})
	m.Write(ctx, recPath("vc", "bad"), []byte("{not json"))
	m.Write(ctx, indexPath("vc"), []byte("garbage"))
	hits, err := v.Search(ctx, "grok", "*", "alpha", ListFilter{}, 0)
	if err != nil || len(hits) != 2 {
		t.Fatalf("* search: %+v %v", hits, err)
	}
	data, _ := m.Read(ctx, indexPath("vc"))
	var idx Index
	if json.Unmarshal(data, &idx) != nil || len(idx.Entries) != 1 {
		t.Fatalf("rebuilt index not saved: %q", data)
	}
	m.Write(ctx, indexPath("vc"), []byte("garbage"))
	if _, err := v.Put(ctx, "grok", PutInput{Project: "vc", Key: "c", Title: "gamma"}); err != nil {
		t.Fatal(err)
	}
	data, _ = m.Read(ctx, indexPath("vc"))
	idx = Index{}
	json.Unmarshal(data, &idx)
	if _, ok := idx.Entries["c"]; !ok || len(idx.Entries) != 2 {
		t.Fatalf("index update with corrupt record: %+v", idx.Entries)
	}
}

// R1-2: log.jsonl rotates into log-NNNN.jsonl before growing past the
// segment size, and a log over the backend read cap errors (no truncation).
func TestLogRotationAndCap(t *testing.T) {
	v, m := newVault(t)
	old := LogSegmentMax
	LogSegmentMax = 300
	defer func() { LogSegmentMax = old }()
	for i := 0; i < 6; i++ {
		if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "k" + string(rune('a'+i)), Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	names, _ := m.List(ctx, "projects/vc")
	segs, total := 0, 0
	for _, n := range names {
		if strings.HasPrefix(n, "log") {
			b, _ := m.Read(ctx, "projects/vc/"+n)
			if len(b) > LogSegmentMax {
				t.Fatalf("%s is %d bytes", n, len(b))
			}
			total += bytes.Count(b, []byte("\n"))
			if n != "log.jsonl" {
				segs++
			}
		}
	}
	if segs < 1 || total != 6 {
		t.Fatalf("segments %d lines %d (%v)", segs, total, names)
	}
	// Over the read cap: appendLog errors; the record write still succeeds.
	m.MaxRead = 10
	if err := v.appendLog(ctx, "vc", []byte("x\n")); !errors.Is(err, store.ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}
