// Package local implements a durable store.Backend on the local filesystem.
package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/southline-tech/botmemory/internal/store"
)

// Backend stores vault files under Root with mode 0600 files and 0700 dirs.
type Backend struct {
	Root string
}

func New(root string) (*Backend, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Backend{Root: root}, nil
}

func (b *Backend) abs(rel string) (string, error) {
	rel = filepath.Clean(strings.Trim(rel, "/"))
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("store: path escape %q", rel)
	}
	p := filepath.Join(b.Root, filepath.FromSlash(rel))
	if !strings.HasPrefix(p, b.Root+string(filepath.Separator)) && p != b.Root {
		return "", fmt.Errorf("store: path escape %q", rel)
	}
	return p, nil
}

func (b *Backend) Read(ctx context.Context, path string) ([]byte, error) {
	data, _, err := b.ReadRev(ctx, path)
	return data, err
}

func (b *Backend) ReadRev(_ context.Context, path string) ([]byte, string, error) {
	p, err := b.abs(path)
	if err != nil {
		return nil, "", err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", store.ErrNotFound
		}
		return nil, "", err
	}
	if !fi.Mode().IsRegular() {
		return nil, "", store.ErrNotFound
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, "", err
	}
	rev, err := fileRev(fi, p)
	if err != nil {
		return nil, "", err
	}
	return data, rev, nil
}

func (b *Backend) Write(ctx context.Context, path string, data []byte) error {
	p, err := b.abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".bm-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (b *Backend) WriteIf(_ context.Context, path string, data []byte, rev string) error {
	p, err := b.abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if rev != "" {
		_, cur, err := b.ReadRev(context.Background(), path)
		if err != nil {
			return err
		}
		if cur != rev {
			return store.ErrPrecondition
		}
	} else {
		if _, err := os.Lstat(p); err == nil {
			return store.ErrPrecondition
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".bm-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func (b *Backend) Delete(_ context.Context, path string) error {
	p, err := b.abs(path)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b *Backend) List(_ context.Context, dir string) ([]string, error) {
	p, err := b.abs(dir)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !fi.IsDir() {
		return nil, nil
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name()+"/")
		} else {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// CopyTree copies all files from src backend root to dst (for migrate).
func CopyTree(ctx context.Context, src, dst *Backend) error {
	return filepath.WalkDir(src.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src.Root, path)
		if err != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		return dst.Write(ctx, relSlash, data)
	})
}

// IsEmpty reports whether the backend root has no files besides optional _acl.json check skipped.
func (b *Backend) IsEmpty() (bool, error) {
	empty := true
	err := filepath.WalkDir(b.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		empty = false
		return io.EOF
	})
	if err == io.EOF {
		return empty, nil
	}
	return empty, err
}
