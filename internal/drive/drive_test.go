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
	"strings"
	"sync"
	"testing"

	"github.com/radiancelux/botvault/internal/store"
)

// fakeDrive implements just enough of Drive v3 REST for the backend.
type fakeDrive struct {
	mu    sync.Mutex
	n     int
	files map[string]*ffile
}
type ffile struct {
	id, name, mime, parent string
	data                   []byte
}

var qRE = regexp.MustCompile(`^'([^']+)' in parents(?: and name = '((?:[^'\\]|\\.)*)')? and trashed = false(?: and mimeType (=|!=) '([^']+)')?$`)

func (f *fakeDrive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
		out := []map[string]string{}
		for _, x := range f.files {
			if x.parent != m[1] || (m[2] != "" && x.name != name) {
				continue
			}
			if m[3] == "=" && x.mime != m[4] || m[3] == "!=" && x.mime == m[4] {
				continue
			}
			out = append(out, map[string]string{"id": x.id, "name": x.name, "mimeType": x.mime})
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
			f.files[strings.TrimPrefix(p, "/upload/drive/v3/files/")].data = data
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
	id := "id" + string(rune('A'+f.n))
	f.files[id] = &ffile{id: id, name: name, mime: mt, parent: parent, data: data}
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
