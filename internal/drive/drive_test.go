package drive

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/radiancelux/botvault/internal/store"
)

// fakeDrive implements just enough of Drive v3 REST for the backend.
type fakeDrive struct {
	mu    sync.Mutex
	n     int
	files map[string]*ffile
	fail  []int // status codes returned (in order) before serving normally
	hits  int
}
type ffile struct {
	id, name, mime, parent string
	data                   []byte
	seq, version           int
}

var qRE = regexp.MustCompile(`^'([^']+)' in parents(?: and name = '((?:[^'\\]|\\.)*)')? and trashed = false(?: and mimeType (=|!=) '([^']+)')?$`)

func (f *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits++
	if len(f.fail) > 0 {
		code := f.fail[0]
		f.fail = f.fail[1:]
		if code == 429 {
			w.Header().Set("Retry-After", "0")
		}
		http.Error(w, "transient", code)
		return
	}
	if r.Header.Get("Authorization") != "Bearer test" {
		http.Error(w, "noauth", 401)
		return
	}
	p := r.URL.Path
	switch {
	case r.Method == "GET" && p == "/drive/v3/files":
		m := qRE.FindStringSubmatch(r.URL.Query().Get("q"))
		if m == nil {
			http.Error(w, "bad q "+r.URL.Query().Get("q"), 400)
			return
		}
		name := strings.NewReplacer(`\'`, `'`, `\\`, `\`).Replace(m[2])
		if r.URL.Query().Get("orderBy") != "createdTime" {
			http.Error(w, "missing orderBy", 400)
			return
		}
		xs := []*ffile{}
		for _, x := range f.files {
			xs = append(xs, x)
		}
		sort.Slice(xs, func(i, j int) bool { return xs[i].seq < xs[j].seq })
		out := []map[string]string{}
		for _, x := range xs {
			if x.parent != m[1] || (m[2] != "" && x.name != name) {
				continue
			}
			if m[3] == "=" && x.mime != m[4] || m[3] == "!=" && x.mime == m[4] {
				continue
			}
			out = append(out, map[string]string{"id": x.id, "name": x.name, "mimeType": x.mime, "version": strconv.Itoa(x.version)})
		}
		json.NewEncoder(w).Encode(map[string]any{"files": out})
	case r.Method == "GET" && strings.HasPrefix(p, "/drive/v3/files/"):
		x := f.files[strings.TrimPrefix(p, "/drive/v3/files/")]
		if x == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(x.data)
	case r.Method == "POST" && p == "/drive/v3/files":
		var meta struct {
			Name, MimeType string
			Parents        []string
		}
		json.NewDecoder(r.Body).Decode(&meta)
		id := f.add(meta.Name, meta.MimeType, meta.Parents[0], nil)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	case strings.HasPrefix(p, "/upload/drive/v3/files"):
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		mp, _ := mr.NextPart()
		var meta struct {
			Name, MimeType string
			Parents        []string
		}
		json.NewDecoder(mp).Decode(&meta)
		dp, _ := mr.NextPart()
		data, _ := io.ReadAll(dp)
		if r.Method == "PATCH" {
			x := f.files[strings.TrimPrefix(p, "/upload/drive/v3/files/")]
			x.data = data
			x.version++
			w.Write([]byte(`{}`))
			return
		}
		id := f.add(meta.Name, meta.MimeType, meta.Parents[0], data)
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	default:
		http.Error(w, "unhandled "+r.Method+" "+p, 500)
	}
}

func (f *fakeDrive) add(name, mt, parent string, data []byte) string {
	f.n++
	id := "id" + strconv.Itoa(f.n)
	f.files[id] = &ffile{id: id, name: name, mime: mt, parent: parent, data: data, seq: f.n, version: 1}
	return id
}

type authRT struct{}

func (authRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer test")
	return http.DefaultTransport.RoundTrip(r)
}

func TestDriveBackend(t *testing.T) {
	fd := &fakeDrive{files: map[string]*ffile{}}
	srv := httptest.NewServer(fd)
	defer srv.Close()
	b := New(&http.Client{Transport: authRT{}}, "ROOT")
	b.Base = srv.URL
	ctx := context.Background()

	if _, err := b.Read(ctx, "projects/vc/records/note/x.json"); err != store.ErrNotFound {
		t.Fatal(err)
	}
	if l, err := b.List(ctx, "projects"); err != nil || len(l) != 0 {
		t.Fatal(l, err)
	}
	if err := b.Write(ctx, "projects/vc/records/note/o'brien.json", []byte(`v1`)); err != nil {
		t.Fatal(err)
	}
	if err := b.Write(ctx, "projects/vc/records/note/o'brien.json", []byte(`v2`)); err != nil {
		t.Fatal(err)
	}
	b.Write(ctx, "projects/vc/records/spec/s.json", []byte(`s`))
	// fresh backend (no folder cache) must resolve existing folders, not dup them
	b2 := New(&http.Client{Transport: authRT{}}, "ROOT")
	b2.Base = srv.URL
	got, err := b2.Read(ctx, "projects/vc/records/note/o'brien.json")
	if err != nil || string(got) != "v2" {
		t.Fatal(string(got), err)
	}
	l, _ := b2.List(ctx, "projects/vc/records")
	if len(l) != 2 {
		t.Fatal(l)
	}
	l, _ = b2.List(ctx, "projects/vc/records/note")
	if !reflect.DeepEqual(l, []string{"o'brien.json"}) {
		t.Fatal(l)
	}
	if len(fd.files) != 7 { // projects, vc, records, note, spec + 2 files
		t.Fatalf("files=%d (duplicate folders?)", len(fd.files))
	}
}

func newFake(t *testing.T) (*fakeDrive, func() *Backend) {
	fd := &fakeDrive{files: map[string]*ffile{}}
	srv := httptest.NewServer(fd)
	t.Cleanup(srv.Close)
	return fd, func() *Backend {
		b := New(&http.Client{Transport: authRT{}}, "ROOT")
		b.Base = srv.URL
		b.Retry = fastRetry(nil)
		return b
	}
}

// Finding 4: WriteIf honours the Drive version precondition.
func TestDriveWriteIf(t *testing.T) {
	_, nb := newFake(t)
	b, ctx := nb(), context.Background()
	if err := b.WriteIf(ctx, "p/k.json", []byte("a"), ""); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteIf(ctx, "p/k.json", []byte("b"), ""); err != store.ErrPrecondition {
		t.Fatal("create-only overwrote:", err)
	}
	_, rev, _ := b.ReadRev(ctx, "p/k.json")
	nb().Write(ctx, "p/k.json", []byte("other process")) // separate instance
	if err := b.WriteIf(ctx, "p/k.json", []byte("c"), rev); err != store.ErrPrecondition {
		t.Fatal("stale rev accepted:", err)
	}
	got, rev, _ := b.ReadRev(ctx, "p/k.json")
	if string(got) != "other process" {
		t.Fatal(string(got))
	}
	if err := b.WriteIf(ctx, "p/k.json", []byte("d"), rev); err != nil {
		t.Fatal(err)
	}
}

// Finding 5: concurrent first writes create each folder exactly once, and
// pre-existing duplicates resolve to the oldest.
func TestDriveFolderRaceAndDuplicates(t *testing.T) {
	fd, nb := newFake(t)
	b, ctx := nb(), context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := b.Write(ctx, "projects/new/records/k"+strconv.Itoa(i)+".json", []byte("x")); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	folders := map[string]int{}
	for _, x := range fd.files {
		if x.mime == folderMime {
			folders[x.parent+"/"+x.name]++
		}
	}
	for k, n := range folders {
		if n != 1 {
			t.Fatalf("folder %s created %d times", k, n)
		}
	}
	// Inject a newer duplicate "dup" folder; the oldest must win everywhere.
	b.Write(ctx, "dup/a.json", []byte("old"))
	fd.mu.Lock()
	fd.add("dup", folderMime, "ROOT", nil)
	fd.mu.Unlock()
	if got, err := nb().Read(ctx, "dup/a.json"); err != nil || string(got) != "old" {
		t.Fatal("duplicate not resolved to oldest:", string(got), err)
	}
}

// Finding 9: Drive error bodies never reach callers.
func TestDriveErrorSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"secret detail ROOT-folder-id"}`, 403)
	}))
	defer srv.Close()
	b := New(srv.Client(), "ROOT")
	b.Base = srv.URL
	b.Retry = fastRetry(nil)
	_, err := b.Read(context.Background(), "x/y.json")
	if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "/drive/") || err.Error() != "drive: upstream error (HTTP 403)" {
		t.Fatalf("leaky error: %v", err)
	}
}

func fastRetry(slept *[]time.Duration) Retry {
	return Retry{MaxAttempts: 4, Base: 100 * time.Millisecond, Max: time.Second,
		Jitter: func() float64 { return 0.5 },
		Sleep: func(ctx context.Context, d time.Duration) error {
			if slept != nil {
				*slept = append(*slept, d)
			}
			return ctx.Err()
		}}
}

func TestRetryTransient(t *testing.T) {
	fd, mk := newFake(t)
	b := mk()
	var slept []time.Duration
	b.Retry = fastRetry(&slept)
	ctx := context.Background()
	// 503,500 on a write (multipart body must be re-sent intact), then success.
	fd.fail = []int{503, 500}
	if err := b.Write(ctx, "a/b.json", []byte(`{"x":1}`)); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Read(ctx, "a/b.json"); err != nil || string(got) != `{"x":1}` {
		t.Fatalf("body not re-sent: %q %v", got, err)
	}
	// Exponential with jitter 0.5: 50ms, 100ms.
	if !reflect.DeepEqual(slept, []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}) {
		t.Fatalf("backoff %v", slept)
	}
	// 429 honours Retry-After: 0.
	slept = nil
	fd.fail = []int{429}
	if _, err := b.Read(ctx, "a/b.json"); err != nil || len(slept) != 1 || slept[0] != 0 {
		t.Fatalf("429: %v %v", slept, err)
	}
}

func TestRetryBoundedAndNonRetryable(t *testing.T) {
	fd, mk := newFake(t)
	b := mk()
	ctx := context.Background()
	b.Write(ctx, "a.json", []byte("1"))
	fd.hits = 0
	fd.fail = []int{500, 500, 500, 500, 500, 500}
	if _, err := b.Read(ctx, "a.json"); err == nil || err.Error() != "drive: upstream error (HTTP 500)" {
		t.Fatalf("want sanitized 500 after bounded retries, got %v", err)
	}
	if fd.hits != 4 {
		t.Fatalf("attempts = %d, want 4", fd.hits)
	}
	fd.fail, fd.hits = []int{403}, 0
	if _, err := b.Read(ctx, "a.json"); err == nil || fd.hits != 1 {
		t.Fatalf("403 retried: hits=%d err=%v", fd.hits, err)
	}
}

func TestRetryDelayCap(t *testing.T) {
	r := Retry{Base: time.Second, Max: 4 * time.Second, Jitter: func() float64 { return 0.999 }}
	if d := r.delay(10, nil); d > 4*time.Second {
		t.Fatalf("uncapped %v", d)
	}
	resp := &http.Response{Header: http.Header{"Retry-After": {"3600"}}}
	if d := r.delay(0, resp); d != 4*time.Second {
		t.Fatalf("Retry-After not capped %v", d)
	}
}

func TestBootstrap(t *testing.T) {
	fd, _ := newFake(t)
	srv := httptest.NewServer(fd)
	defer srv.Close()
	hc := &http.Client{Transport: authRT{}}
	ctx := context.Background()
	id, seeded, err := Bootstrap(ctx, hc, srv.URL)
	if err != nil || id == "" || !seeded {
		t.Fatalf("bootstrap: %q %v %v", id, seeded, err)
	}
	if fd.files[id].parent != "root" || fd.files[id].name != RootFolderName || fd.files[id].mime != folderMime {
		t.Fatalf("root folder %+v", fd.files[id])
	}
	b := New(hc, id)
	b.Base = srv.URL
	if got, _ := b.Read(ctx, "_acl.json"); strings.TrimSpace(string(got)) != `{"bots":{}}` {
		t.Fatalf("acl seed %q", got)
	}
	b.Write(ctx, "_acl.json", []byte(`{"bots":{"x":{}}}`))
	id2, seeded2, err := Bootstrap(ctx, hc, srv.URL)
	if err != nil || id2 != id || seeded2 {
		t.Fatalf("not idempotent: %q %v %v", id2, seeded2, err)
	}
	if got, _ := b.Read(ctx, "_acl.json"); string(got) != `{"bots":{"x":{}}}` {
		t.Fatal("bootstrap overwrote existing ACL")
	}
}
