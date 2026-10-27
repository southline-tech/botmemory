package store

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Memory is an in-process Backend for tests and local dev. Not durable.
type Memory struct {
	mu    sync.RWMutex
	files map[string]mfile
	// FailWrite, if set, is consulted before every write (fault injection).
	FailWrite func(path string) error
	// MaxRead, if > 0, makes reads of larger files fail with ErrTooLarge
	// (mirrors the Drive backend's read cap).
	MaxRead int
}

type mfile struct {
	data []byte
	rev  int
}

func NewMemory() *Memory { return &Memory{files: map[string]mfile{}} }

func (m *Memory) Read(ctx context.Context, p string) ([]byte, error) {
	b, _, err := m.ReadRev(ctx, p)
	return b, err
}

func (m *Memory) ReadRev(_ context.Context, p string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.files[clean(p)]
	if !ok {
		return nil, "", ErrNotFound
	}
	if m.MaxRead > 0 && len(f.data) > m.MaxRead {
		return nil, "", ErrTooLarge
	}
	return append([]byte(nil), f.data...), strconv.Itoa(f.rev), nil
}

func (m *Memory) Write(ctx context.Context, p string, data []byte) error {
	return m.write(p, data, nil)
}

func (m *Memory) WriteIf(_ context.Context, p string, data []byte, rev string) error {
	return m.write(p, data, &rev)
}

func (m *Memory) write(p string, data []byte, rev *string) error {
	if m.FailWrite != nil {
		if err := m.FailWrite(clean(p)); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[clean(p)]
	if rev != nil {
		cur := ""
		if ok {
			cur = strconv.Itoa(f.rev)
		}
		if cur != *rev {
			return ErrPrecondition
		}
	}
	m.files[clean(p)] = mfile{append([]byte(nil), data...), f.rev + 1}
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
