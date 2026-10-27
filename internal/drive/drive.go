// Package drive is the Google Drive (v3 REST) implementation of store.Backend.
// Vault paths map to nested folders under a root folder id; files are JSON.
// Auth is an oauth2.TokenSource held only by the server (drive.file scope).
package drive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/radiancelux/botvault/internal/store"
)

const folderMime = "application/vnd.google-apps.folder"

type Backend struct {
	HC     *http.Client // must add auth (e.g. oauth2.NewClient)
	RootID string
	Base   string // default https://www.googleapis.com; overridable for tests

	mu      sync.Mutex
	folders map[string]string // vault dir path -> folder id
}

func New(hc *http.Client, rootID string) *Backend {
	return &Backend{HC: hc, RootID: rootID, Base: "https://www.googleapis.com", folders: map[string]string{"": rootID}}
}

type file struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
}

func q(s string) string { return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) }

func (d *Backend) do(req *http.Request, out any) error {
	resp, err := d.HC.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode == http.StatusNotFound {
		return store.ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("drive: %s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, bytes.TrimSpace(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (d *Backend) query(ctx context.Context, qs string) ([]file, error) {
	var all []file
	tok := ""
	for {
		v := url.Values{"q": {qs}, "fields": {"nextPageToken,files(id,name,mimeType)"}, "pageSize": {"1000"},
			"supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"}}
		if tok != "" {
			v.Set("pageToken", tok)
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", d.Base+"/drive/v3/files?"+v.Encode(), nil)
		var res struct {
			NextPageToken string `json:"nextPageToken"`
			Files         []file `json:"files"`
		}
		if err := d.do(req, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Files...)
		if tok = res.NextPageToken; tok == "" {
			return all, nil
		}
	}
}

func (d *Backend) child(ctx context.Context, parent, name string, folder bool) (*file, error) {
	qs := fmt.Sprintf("'%s' in parents and name = '%s' and trashed = false", q(parent), q(name))
	if folder {
		qs += " and mimeType = '" + folderMime + "'"
	} else {
		qs += " and mimeType != '" + folderMime + "'"
	}
	fs, err := d.query(ctx, qs)
	if err != nil {
		return nil, err
	}
	if len(fs) == 0 {
		return nil, store.ErrNotFound
	}
	return &fs[0], nil
}

// folderID resolves (and optionally creates) the folder for dir.
func (d *Backend) folderID(ctx context.Context, dir string, create bool) (string, error) {
	dir = strings.Trim(dir, "/")
	d.mu.Lock()
	id, ok := d.folders[dir]
	d.mu.Unlock()
	if ok {
		return id, nil
	}
	parentDir, name := path.Split(dir)
	parent, err := d.folderID(ctx, parentDir, create)
	if err != nil {
		return "", err
	}
	f, err := d.child(ctx, parent, name, true)
	if err == store.ErrNotFound && create {
		meta, _ := json.Marshal(map[string]any{"name": name, "mimeType": folderMime, "parents": []string{parent}})
		req, _ := http.NewRequestWithContext(ctx, "POST", d.Base+"/drive/v3/files?supportsAllDrives=true&fields=id", bytes.NewReader(meta))
		req.Header.Set("Content-Type", "application/json")
		f = &file{}
		err = d.do(req, f)
	}
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	d.folders[dir] = f.ID
	d.mu.Unlock()
	return f.ID, nil
}

func (d *Backend) Read(ctx context.Context, p string) ([]byte, error) {
	dir, name := path.Split(strings.Trim(p, "/"))
	pid, err := d.folderID(ctx, dir, false)
	if err != nil {
		return nil, err
	}
	f, err := d.child(ctx, pid, name, false)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", d.Base+"/drive/v3/files/"+url.PathEscape(f.ID)+"?alt=media&supportsAllDrives=true", nil)
	resp, err := d.HC.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("drive: download %s: %d", p, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func (d *Backend) Write(ctx context.Context, p string, data []byte) error {
	dir, name := path.Split(strings.Trim(p, "/"))
	pid, err := d.folderID(ctx, dir, true)
	if err != nil {
		return err
	}
	existing, err := d.child(ctx, pid, name, false)
	if err != nil && err != store.ErrNotFound {
		return err
	}
	var meta map[string]any
	method, u := "POST", d.Base+"/upload/drive/v3/files?uploadType=multipart&supportsAllDrives=true&fields=id"
	if existing != nil {
		method = "PATCH"
		u = d.Base + "/upload/drive/v3/files/" + url.PathEscape(existing.ID) + "?uploadType=multipart&supportsAllDrives=true&fields=id"
		meta = map[string]any{}
	} else {
		meta = map[string]any{"name": name, "parents": []string{pid}, "mimeType": "application/json"}
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mb, _ := json.Marshal(meta)
	pw, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json; charset=UTF-8"}})
	pw.Write(mb)
	pw, _ = mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/json"}})
	pw.Write(data)
	mw.Close()
	req, _ := http.NewRequestWithContext(ctx, method, u, &buf)
	req.Header.Set("Content-Type", "multipart/related; boundary="+mw.Boundary())
	return d.do(req, nil)
}

func (d *Backend) List(ctx context.Context, dir string) ([]string, error) {
	pid, err := d.folderID(ctx, dir, false)
	if err == store.ErrNotFound {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	fs, err := d.query(ctx, fmt.Sprintf("'%s' in parents and trashed = false", q(pid)))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		if f.MimeType == folderMime {
			out = append(out, f.Name+"/")
		} else {
			out = append(out, f.Name)
		}
	}
	return out, nil
}
