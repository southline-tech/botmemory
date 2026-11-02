package main

import (
	"strings"
	"testing"
)

func TestHelpAppend(t *testing.T) {
	text, ok := helpTopics["append"]
	if !ok || !strings.Contains(text, "--kind") {
		t.Fatalf("append help: %q", text)
	}
}

func TestHelpUsageLog(t *testing.T) {
	text, ok := helpTopics["usage log"]
	if !ok || !strings.Contains(text, "tokens") {
		t.Fatalf("usage log help: %q", text)
	}
}

func TestMaybePrintHelpRoot(t *testing.T) {
	if !maybePrintHelp([]string{"botoptimizer", "--help"}) {
		t.Fatal("expected root help")
	}
}
