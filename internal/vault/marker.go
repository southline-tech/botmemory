package vault

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"sync"

	"github.com/southline-tech/botvault/internal/store"
)

// FailClosedOnMarkerError decides what happens when the durable
// _index.dirty marker cannot be written before a record commit. true (PM
// ruling, round 3): the write fails with ErrMarker. false: the write
// proceeds and only the in-process dirty flag protects the index.
const FailClosedOnMarkerError = true

func dirtyPath(p string) string { return path.Join("projects", p, "_index.dirty") }

// markState tracks one project's durable marker. All fields are guarded by
// mu, which serialises every marker read/write/delete for the project, so
// concurrent writers cannot race on it (R3-2).
//
// The marker means "writes may be in flight or the index may be stale". It
// is created when the first writer starts (inflight 0->1), kept while any
// writer is in flight or after an index failure, and removed only when
// inflight drops to 0 with the index known good. Finding it on the first
// access after a (re)start means a crash or an unfinished rebuild, so the
// project is rebuilt once.
type markState struct {
	mu       sync.Mutex
	checked  bool // the startup marker check has run
	present  bool // marker believed to exist in storage
	inflight int
}

func (v *Vault) markState(project string) *markState {
	s, _ := v.marks.LoadOrStore(project, &markState{})
	return s.(*markState)
}

// checkLocked runs once per project per process: a marker left in storage
// means a previous process crashed mid-write or before a rebuild finished.
func (v *Vault) checkLocked(ctx context.Context, project string, s *markState) error {
	if s.checked {
		return nil
	}
	_, err := v.b.Read(ctx, dirtyPath(project))
	switch {
	case err == nil:
		s.present = true
		v.dirty.Store(project, true)
		log.Printf("botvault: %s: index dirty marker found at startup; rebuilding on access", project)
	case errors.Is(err, store.ErrNotFound):
		if _, known := v.dirty.Load(project); !known {
			v.dirty.Store(project, false)
		}
	default:
		return err
	}
	s.checked = true
	return nil
}

// beginWrite must precede a record commit; pair with endWrite.
func (v *Vault) beginWrite(ctx context.Context, project string) error {
	s := v.markState(project)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := v.checkLocked(ctx, project, s); err != nil && FailClosedOnMarkerError {
		return fmt.Errorf("%w: %v", ErrMarker, err)
	}
	if !s.present {
		if err := v.b.Write(ctx, dirtyPath(project), []byte("1\n")); err != nil {
			if FailClosedOnMarkerError {
				return fmt.Errorf("%w: %v", ErrMarker, err)
			}
			log.Printf("botvault: %s: writing index dirty marker failed: %v", project, err)
		} else {
			s.present = true
		}
	}
	s.inflight++
	return nil
}

// endWrite finishes a write. On index failure the marker is (re)written so
// it is durable even if someone removed it meanwhile; otherwise it is
// removed once no writer is in flight and the index is not dirty.
func (v *Vault) endWrite(ctx context.Context, project string, indexOK bool) {
	s := v.markState(project)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight--
	if !indexOK {
		v.dirty.Store(project, true)
		if err := v.b.Write(ctx, dirtyPath(project), []byte("1\n")); err != nil {
			log.Printf("botvault: %s: re-writing index dirty marker failed: %v", project, err)
		} else {
			s.present = true
		}
		return
	}
	v.removeMarkerLocked(ctx, project, s)
}

func (v *Vault) removeMarkerLocked(ctx context.Context, project string, s *markState) {
	if s.inflight > 0 || !s.present {
		return
	}
	if d, _ := v.dirty.Load(project); d == true {
		return
	}
	if err := v.b.Delete(context.WithoutCancel(ctx), dirtyPath(project)); err != nil {
		log.Printf("botvault: %s: removing index dirty marker failed (harmless; one rebuild after restart): %v", project, err)
		return
	}
	s.present = false
}

// clearDirty records a successful rebuild+save; the marker is removed if no
// writer is in flight.
func (v *Vault) clearDirty(ctx context.Context, project string) error {
	s := v.markState(project)
	s.mu.Lock()
	defer s.mu.Unlock()
	v.dirty.Store(project, false)
	s.checked = true
	v.removeMarkerLocked(ctx, project, s)
	return nil
}

// isDirty reports whether the index must be rebuilt before use.
func (v *Vault) isDirty(ctx context.Context, project string) bool {
	s := v.markState(project)
	s.mu.Lock()
	err := v.checkLocked(ctx, project, s)
	s.mu.Unlock()
	if err != nil {
		return true // can't tell: rebuild (safe)
	}
	d, _ := v.dirty.Load(project)
	return d == true
}
