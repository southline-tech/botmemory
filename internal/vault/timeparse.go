package vault

import (
	"fmt"
	"strings"
	"time"
)

// ParseSince accepts RFC3339 timestamps or YYYY-MM-DD (UTC midnight).
func ParseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: since required", ErrInvalid)
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, fmt.Errorf("%w: since must be RFC3339 or YYYY-MM-DD", ErrInvalid)
}
