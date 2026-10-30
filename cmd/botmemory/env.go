package main

import "os"

// env returns BOTMEMORY_<suffix> from the environment.
func env(suffix string) string {
	return os.Getenv("BOTMEMORY_" + suffix)
}
