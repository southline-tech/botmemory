package vault

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	if _, _, err := v.Reindex(ctx, "impl", "vc"); !errors.Is(err, ErrForbidden) {
		t.Fatal("non-owner reindexed")
	}
	if n, _, err := v.Reindex(ctx, "pm", "vc"); err != nil || n != 1 {
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
	if n, _, _ := v.Reindex(ctx, "pm", "vc"); n != 1 {
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
	if v.isDirty(ctx, "vc") {
		t.Fatal("still dirty")
	}
	if _, err := m.Read(ctx, dirtyPath("vc")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("dirty marker not removed")
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

// R2-1: only unparseable records are skipped (and reported by reindex); any
// other read error aborts the rebuild without saving and leaves it dirty.
func TestRebuildSkipsOnlyCorrupt(t *testing.T) {
	v, m := newVault(t)
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "alpha"})
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "b", Title: "beta"})
	m.Write(ctx, recPath("vc", "bad"), []byte("{not json"))
	n, skipped, err := v.Reindex(ctx, "pm", "vc")
	if err != nil || n != 2 || len(skipped) != 1 || skipped[0] != "bad.json" {
		t.Fatalf("reindex n=%d skipped=%v err=%v", n, skipped, err)
	}
	before, _ := m.Read(ctx, indexPath("vc"))
	for _, tc := range []struct {
		name string
		max  int
		ctx  func() context.Context
	}{
		{"too large", 20, func() context.Context { return ctx }},
		{"cancelled", 0, func() context.Context { c, cancel := context.WithCancel(ctx); cancel(); return c }},
	} {
		m.MaxRead = tc.max
		if _, _, err := v.Reindex(tc.ctx(), "pm", "vc"); err == nil {
			t.Fatalf("%s: rebuild did not abort", tc.name)
		}
		m.MaxRead = 0
		after, _ := m.Read(ctx, indexPath("vc"))
		if !bytes.Equal(before, after) {
			t.Fatalf("%s: partial index saved", tc.name)
		}
		if !v.isDirty(ctx, "vc") {
			t.Fatalf("%s: not left dirty", tc.name)
		}
	}
	// Transient backend error on a record read (e.g. Drive 503 after retries).
	m.FailRead = func(p string) error {
		if strings.HasSuffix(p, "records/a.json") {
			return errors.New("drive: upstream error (HTTP 503)")
		}
		return nil
	}
	if _, err := v.Search(ctx, "pm", "vc", "beta", ListFilter{}, 0); err == nil {
		t.Fatal("search over a dirty index with a transient error must fail, not return a partial index")
	}
	m.FailRead = nil
	after, _ := m.Read(ctx, indexPath("vc"))
	if !bytes.Equal(before, after) || !v.isDirty(ctx, "vc") {
		t.Fatal("transient: partial index saved or dirty cleared")
	}
	if hits, err := v.Search(ctx, "pm", "vc", "alpha", ListFilter{}, 0); err != nil || len(hits) != 1 || v.isDirty(ctx, "vc") {
		t.Fatalf("recovery: %v %v", hits, err)
	}
}

// R2-2: the dirty mark is durable. A failed index update leaves
// _index.dirty in storage; a fresh Vault (restart) sees it and rebuilds.
func TestDirtyMarkerSurvivesRestart(t *testing.T) {
	v, m := newVault(t)
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "alpha"})
	if _, err := m.Read(ctx, dirtyPath("vc")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("marker left after a clean write")
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
	if _, err := m.Read(ctx, dirtyPath("vc")); err != nil {
		t.Fatalf("no durable marker: %v", err)
	}
	v2 := New(m, v.acl) // restart: empty in-process state, same storage
	hits, err := v2.Search(ctx, "pm", "vc", "alpha", ListFilter{}, 0)
	if err != nil || len(hits) != 0 {
		t.Fatalf("restart served stale index: %+v %v", hits, err)
	}
	if _, err := m.Read(ctx, dirtyPath("vc")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("marker not removed after rebuild")
	}
}

// R3-1: a normal put updates the index incrementally; storage reads per put
// must not grow with the number of records (no rebuild per write).
func TestReadsPerPutConstant(t *testing.T) {
	per := func(n int) int64 {
		v, m := newVault(t)
		for i := 0; i < n; i++ {
			if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: fmt.Sprintf("k%03d", i), Title: "t"}); err != nil {
				t.Fatal(err)
			}
		}
		m.Reads.Store(0)
		if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "new", Title: "t"}); err != nil {
			t.Fatal(err)
		}
		r := m.Reads.Load()
		if _, err := m.Read(ctx, dirtyPath("vc")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("n=%d: marker left after a clean put", n)
		}
		return r
	}
	r5, r50 := per(5), per(50)
	t.Logf("reads per put: 5 records=%d, 50 records=%d", r5, r50)
	if r5 != r50 || r50 > 6 {
		t.Fatalf("reads per put not constant: 5 records=%d, 50 records=%d", r5, r50)
	}
}

// R3-2: concurrent writers share the marker safely: it exists while any
// write is in flight, is gone when all finish cleanly, and the index has
// every record without a rebuild.
func TestConcurrentWritersMarker(t *testing.T) {
	v, m := newVault(t)
	v.Put(ctx, "pm", PutInput{Project: "vc", Key: "seed", Title: "t"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: fmt.Sprintf("c%02d", i), Title: "t"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if _, err := m.Read(ctx, dirtyPath("vc")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("marker left after concurrent clean writes")
	}
	idx, _, err := v.loadIndex(ctx, "vc", false)
	if err != nil || len(idx.Entries) != 21 {
		t.Fatalf("index has %d entries, err %v", len(idx.Entries), err)
	}
	// One writer's index failure: the marker must survive unless a later
	// rebuild repaired the index, and a restart must see all records.
	var fail atomic.Bool
	fail.Store(true)
	m.FailWrite = func(p string) error {
		if strings.HasSuffix(p, "_index.json") && fail.CompareAndSwap(true, false) {
			return errors.New("boom")
		}
		return nil
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v.Put(ctx, "pm", PutInput{Project: "vc", Key: fmt.Sprintf("d%02d", i), Title: "t"})
		}(i)
	}
	wg.Wait()
	m.FailWrite = nil
	if _, err := m.Read(ctx, dirtyPath("vc")); err != nil && v.isDirty(ctx, "vc") {
		t.Fatal("dirty but marker gone")
	}
	v2 := New(m, v.acl)
	// Another writer may have rebuilt after the failure (then the marker is
	// legitimately removed); either way a restart must see a complete index.
	if idx, _, err := v2.loadIndex(ctx, "vc", false); err != nil || len(idx.Entries) != 31 || v2.isDirty(ctx, "vc") {
		t.Fatalf("after restart: index %v, err %v", idx, err)
	}
}

// R3-3: if the marker cannot be written the write fails closed.
func TestMarkerWriteFailsClosed(t *testing.T) {
	v, m := newVault(t)
	if !FailClosedOnMarkerError {
		t.Fatal("PM ruling: fail closed")
	}
	m.FailWrite = func(p string) error {
		if strings.HasSuffix(p, "_index.dirty") {
			return errors.New("drive down")
		}
		return nil
	}
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "t"}); !errors.Is(err, ErrMarker) {
		t.Fatalf("want ErrMarker, got %v", err)
	}
	if _, err := m.Read(ctx, recPath("vc", "a")); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("record written despite marker failure")
	}
}

// R3-4: the log line is appended even when the index update fails.
func TestLogAppendedWhenIndexFails(t *testing.T) {
	v, m := newVault(t)
	m.FailWrite = func(p string) error {
		if strings.HasSuffix(p, "_index.json") {
			return errors.New("boom")
		}
		return nil
	}
	if _, err := v.Put(ctx, "pm", PutInput{Project: "vc", Key: "a", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	m.FailWrite = nil
	b, err := m.Read(ctx, "projects/vc/log.jsonl")
	if err != nil || !strings.Contains(string(b), `"key":"a"`) {
		t.Fatalf("log not appended: %q %v", b, err)
	}
	if _, err := m.Read(ctx, dirtyPath("vc")); err != nil {
		t.Fatal("marker not kept after index failure")
	}
}

// R4-1: a panic while saving the index after the record committed must keep
// the marker, so a restart treats the project as dirty and rebuilds it to
// the committed version (no stale index trusted).
func TestIndexSavePanicKeepsMarker(t *testing.T) {
	v, m := newVault(t)
	if _, err := put(v, "pm", nil, "one"); err != nil {
		t.Fatal(err)
	}
	m.FailWrite = func(p string) error {
		if strings.HasSuffix(p, "_index.json") {
			panic("index save blew up")
		}
		return nil
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed; want re-panic")
			}
		}()
		_, _ = put(v, "pm", iv(1), "two")
	}()
	m.FailWrite = nil
	if _, err := m.Read(ctx, dirtyPath("vc")); err != nil {
		t.Fatalf("marker removed after panic: %v", err)
	}
	if !v.isDirty(ctx, "vc") {
		t.Fatal("project not dirty in-process after panic")
	}
	v2 := New(m, v.acl)
	if !v2.isDirty(ctx, "vc") {
		t.Fatal("restart did not see the project as dirty")
	}
	idx, _, err := v2.loadIndex(ctx, "vc", false)
	if err != nil || idx.Entries["k1"].Version != 2 {
		t.Fatalf("after restart: want k1 v2, got %+v err %v", idx.Entries["k1"], err)
	}
	if v2.isDirty(ctx, "vc") {
		t.Fatal("still dirty after rebuild")
	}
	if _, err := m.Read(ctx, dirtyPath("vc")); err == nil {
		t.Fatal("marker not removed after rebuild")
	}
}

// R3-2, fixed order: the marker stays while any writer is in flight, stays
// after a failed writer even if a later writer succeeds, and goes only once
// the index has been rebuilt with nothing in flight.
func TestMarkerFixedOrder(t *testing.T) {
	v, m := newVault(t)
	present := func() bool { _, err := m.Read(ctx, dirtyPath("vc")); return err == nil }
	step := func(name string, want bool) {
		t.Helper()
		if got := present(); got != want {
			t.Fatalf("%s: marker present=%v, want %v", name, got, want)
		}
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(v.beginWrite(ctx, "vc")) // A
	step("A begun", true)
	must(v.beginWrite(ctx, "vc")) // B
	v.endWrite(ctx, "vc", true)   // A ok, B still in flight
	step("A done, B in flight", true)
	v.endWrite(ctx, "vc", true) // B ok, nothing in flight
	step("A and B done", false)

	must(v.beginWrite(ctx, "vc")) // C
	must(v.beginWrite(ctx, "vc")) // D
	v.endWrite(ctx, "vc", false)  // C index failed
	step("C failed", true)
	v.endWrite(ctx, "vc", true) // D ok but project dirty
	step("D ok after C failed", true)
	if !v.isDirty(ctx, "vc") {
		t.Fatal("not dirty after C failed")
	}
	if _, _, err := v.loadIndex(ctx, "vc", false); err != nil {
		t.Fatal(err)
	}
	step("rebuilt, nothing in flight", false)
}

// saveThenFail saves records, then reports an error or panics, the way a
// Drive PATCH can succeed while its response is lost or the deadline fires.
type saveThenFail struct {
	*store.Memory
	panicAfter bool
}

func (s *saveThenFail) WriteIf(ctx context.Context, p string, data []byte, rev string) error {
	if err := s.Memory.WriteIf(ctx, p, data, rev); err != nil {
		return err
	}
	if strings.Contains(p, "/records/") {
		if s.panicAfter {
			panic("lost after save")
		}
		return errors.New("context deadline exceeded (after save)")
	}
	return nil
}

// R5-1: a WriteIf error (or panic) after the record was saved must keep the
// marker; a restart must see the project dirty and rebuild to the new version.
func TestWriteIfErrorAfterSaveKeepsMarker(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic=%v", panics), func(t *testing.T) {
			v0, m := newVault(t)
			if _, err := put(v0, "pm", nil, "one"); err != nil {
				t.Fatal(err)
			}
			v := New(&saveThenFail{Memory: m, panicAfter: panics}, v0.acl)
			func() {
				defer func() {
					if p := recover(); (p != nil) != panics {
						t.Fatalf("panic=%v, want %v", p, panics)
					}
				}()
				if _, err := put(v, "pm", iv(1), "two"); err == nil {
					t.Fatal("want error from WriteIf")
				}
			}()
			if _, err := m.Read(ctx, dirtyPath("vc")); err != nil {
				t.Fatalf("marker removed: %v", err)
			}
			if n := inflight(v, "vc"); n != 0 {
				t.Fatalf("inflight=%d, want 0", n)
			}
			if !v.isDirty(ctx, "vc") {
				t.Fatal("not dirty in-process")
			}
			v2 := New(m, v0.acl)
			if !v2.isDirty(ctx, "vc") {
				t.Fatal("restart not dirty")
			}
			idx, _, err := v2.loadIndex(ctx, "vc", false)
			if err != nil || idx.Entries["k1"].Version != 2 {
				t.Fatalf("after restart: want k1 v2, got %+v err %v", idx.Entries["k1"], err)
			}
			if _, err := m.Read(ctx, dirtyPath("vc")); err == nil {
				t.Fatal("marker not removed after rebuild")
			}
		})
	}
}

// preconditionFails reports a lost race at WriteIf time (nothing saved).
type preconditionFails struct{ *store.Memory }

func (s preconditionFails) WriteIf(ctx context.Context, p string, data []byte, rev string) error {
	if strings.Contains(p, "/records/") {
		return store.ErrPrecondition
	}
	return s.Memory.WriteIf(ctx, p, data, rev)
}

func inflight(v *Vault, p string) int {
	s := v.markState(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inflight
}

// R5-1 converse / R6-1: a WriteIf ErrPrecondition proves nothing was saved,
// so it reaches the committed=false branch and does not dirty the project.
func TestConflictDoesNotDirty(t *testing.T) {
	v0, m := newVault(t)
	if _, err := put(v0, "pm", nil, "one"); err != nil {
		t.Fatal(err)
	}
	v := New(preconditionFails{m}, v0.acl)
	if _, err := put(v, "pm", iv(1), "two"); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if v.isDirty(ctx, "vc") {
		t.Fatal("conflict dirtied the project")
	}
	if _, err := m.Read(ctx, dirtyPath("vc")); err == nil {
		t.Fatal("marker left after conflict")
	}
	if n := inflight(v, "vc"); n != 0 {
		t.Fatalf("inflight=%d, want 0", n)
	}
}
