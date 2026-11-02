package store

import (
	"os"
	"path/filepath"
	"runtime"
)

// DefaultDataDir returns the platform default BotOptimizer data directory.
func DefaultDataDir() string {
	if d := os.Getenv("BOTOPTIMIZER_DATA_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "botoptimizer")
		}
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "botoptimizer")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "botoptimizer"
	}
	return filepath.Join(home, ".local", "share", "botoptimizer")
}
