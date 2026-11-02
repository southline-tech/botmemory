package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/southline-tech/botoptimizer/internal/usage"
)

func TestParseCSVMapFlag(t *testing.T) {
	m, err := parseCSVMapFlag("time=When,bot=Agent,tokens_in=Pin,tokens_out=Pout")
	if err != nil {
		t.Fatal(err)
	}
	if m.Time != "When" || m.Bot != "Agent" || m.Tokens != "Pin" || m.TOut != "Pout" {
		t.Fatalf("%+v", m)
	}
}

func TestUsageLogTokensRequireInOutCLI(t *testing.T) {
	err := usage.ValidateUsageLogMap(map[string]any{"type": "tokens", "bot": "a"})
	if !errors.Is(err, usage.ErrInvalid) || !strings.Contains(err.Error(), "tokens require in and out") {
		t.Fatalf("got %v", err)
	}
	if err := usage.ValidateUsageLogMap(map[string]any{"type": "tokens", "bot": "a", "in": 0, "out": 0}); err != nil {
		t.Fatalf("explicit zero: %v", err)
	}
}
