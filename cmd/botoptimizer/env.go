package main

import "os"

// env returns BOTOPTIMIZER_<suffix> from the environment.
func env(suffix string) string {
	return os.Getenv("BOTOPTIMIZER_" + suffix)
}
