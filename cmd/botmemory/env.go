package main

import (
	"log"
	"os"
	"sync"
)

// deprecatedEnv tracks which legacy BOTVAULT_* keys have already logged a
// one-line warning, so each key warns at most once per process.
var deprecatedEnv sync.Map

// env returns BOTMEMORY_<suffix>, falling back to BOTVAULT_<suffix>.
// The first time a legacy key is used, a one-line deprecation warning is
// written to the log so existing pilot env keeps working through the rename.
func env(suffix string) string {
	if v := os.Getenv("BOTMEMORY_" + suffix); v != "" {
		return v
	}
	old := "BOTVAULT_" + suffix
	if v := os.Getenv(old); v != "" {
		if _, seen := deprecatedEnv.LoadOrStore(old, true); !seen {
			log.Printf("botmemory: %s is deprecated; use BOTMEMORY_%s", old, suffix)
		}
		return v
	}
	return ""
}
