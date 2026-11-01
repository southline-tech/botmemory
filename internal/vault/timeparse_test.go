package vault

import (
	"errors"
	"testing"
	"time"
)

func TestParseSinceRFC3339(t *testing.T) {
	want := time.Date(2026, 11, 1, 12, 30, 0, 0, time.UTC)
	got, err := ParseSince("2026-11-01T12:30:00Z")
	if err != nil || !got.Equal(want) {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestParseSinceDateUTC(t *testing.T) {
	want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got, err := ParseSince("2026-11-01")
	if err != nil || !got.Equal(want) {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestParseSinceInvalid(t *testing.T) {
	_, err := ParseSince("not-a-date")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}
