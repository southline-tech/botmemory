// Package store defines the blob storage interface botVault persists to.
// Paths are slash-separated and relative to the botVault root folder.
package store

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a path does not exist.
var ErrNotFound = errors.New("store: not found")

// Backend is the minimal file-like storage botVault needs. Implementations:
// Memory (tests/dev) and drive.Backend (Google Drive, production).
type Backend interface {
	// Read returns the file's bytes or ErrNotFound.
	Read(ctx context.Context, path string) ([]byte, error)
	// Write creates or replaces the file, creating parent folders as needed.
	Write(ctx context.Context, path string, data []byte) error
	// List returns the names (not full paths) of direct children of dir.
	// Folders are suffixed with "/". A missing dir yields an empty list.
	List(ctx context.Context, dir string) ([]string, error)
}
