// Package store defines the blob storage interface botVault persists to.
// Paths are slash-separated and relative to the botVault root folder.
package store

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is returned when a path does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrPrecondition is returned by WriteIf when the file's revision changed.
	ErrPrecondition = errors.New("store: precondition failed")
)

// Backend is the minimal file-like storage botVault needs. Implementations:
// Memory (tests/dev) and drive.Backend (Google Drive, production).
type Backend interface {
	// Read returns the file's bytes or ErrNotFound.
	Read(ctx context.Context, path string) ([]byte, error)
	// ReadRev is Read plus an opaque revision token for WriteIf.
	ReadRev(ctx context.Context, path string) ([]byte, string, error)
	// Write creates or replaces the file, creating parent folders as needed.
	Write(ctx context.Context, path string, data []byte) error
	// WriteIf writes only if the file is still at rev ("" = must not exist),
	// else ErrPrecondition. This is the storage-side guard for if_version.
	WriteIf(ctx context.Context, path string, data []byte, rev string) error
	// List returns the names (not full paths) of direct children of dir.
	// Folders are suffixed with "/". A missing dir yields an empty list.
	List(ctx context.Context, dir string) ([]string, error)
}
