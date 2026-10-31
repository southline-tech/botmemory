package store

import (
	"os"
	"path/filepath"
	"runtime"
)

// DefaultDataDir returns the platform default botMemory data directory.
func DefaultDataDir() string {
	if d := os.Getenv("BOTMEMORY_DATA_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "botmemory")
		}
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "botmemory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "botmemory"
	}
	return filepath.Join(home, ".local", "share", "botmemory")
}
