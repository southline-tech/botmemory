package store

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Memory is an in-process Backend for tests and local dev. Not durable.
type Memory struct {
	mu    sync.RWMutex
	files map[string][]byte
}

func NewMemory() *Memory { return &Memory{files: map[string][]byte{}} }

func (m *Memory) Read(_ context.Context, p string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.files[clean(p)]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (m *Memory) Write(_ context.Context, p string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[clean(p)] = append([]byte(nil), data...)
	return nil
}

func (m *Memory) List(_ context.Context, dir string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := clean(dir)
	if prefix != "" {
		prefix += "/"
	}
	seen := map[string]bool{}
	for k := range m.files {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			seen[rest[:i+1]] = true
		} else {
			seen[rest] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

func clean(p string) string { return strings.Trim(p, "/") }
