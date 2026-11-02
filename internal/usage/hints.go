package usage

import (
	"context"
	"fmt"
	"time"
)

type Hint struct {
	ID      string `json:"id"`
	Bot     string `json:"bot,omitempty"`
	Message string `json:"message"`
}

func (s *Service) Hints(ctx context.Context, authBot, project, botFilter string, since time.Time) (map[string]any, error) {
	if since.IsZero() {
		since = s.now().Add(-24 * time.Hour)
	}
	events, err := s.loadEvents(ctx, authBot, project, botFilter, since)
	if err != nil {
		return nil, err
	}
	var hints []Hint
	if h := detectRecheckLoop(events); h != nil {
		hints = append(hints, *h)
	}
	if h := detectScreenshotBloat(events); h != nil {
		hints = append(hints, *h)
	}
	if h := detectStatusChatter(events); h != nil {
		hints = append(hints, *h)
	}
	if h := detectDupRead(events); h != nil {
		hints = append(hints, *h)
	}
	if h := detectRetryLoop(events); h != nil {
		hints = append(hints, *h)
	}
	if h := detectTokenSpike(events); h != nil {
		hints = append(hints, *h)
	}
	return map[string]any{"hints": hints, "count": len(hints)}, nil
}

func detectRecheckLoop(ev []Event) *Hint {
	n := 0
	for _, e := range ev {
		if e.Type == "recheck" {
			n += max(1, e.N)
		}
	}
	if n >= 10 {
		return &Hint{ID: "recheck_loop", Message: fmt.Sprintf("%d rechecks in window", n)}
	}
	return nil
}

func detectScreenshotBloat(ev []Event) *Hint {
	for _, e := range ev {
		if e.Type == "image" && e.Bytes >= 500000 {
			return &Hint{ID: "screenshot_bloat", Bot: e.Bot, Message: "large image proxy bytes"}
		}
	}
	return nil
}

func detectStatusChatter(ev []Event) *Hint {
	n := 0
	for _, e := range ev {
		if e.Type == "msg" {
			n += max(1, e.N)
		}
	}
	if n >= 20 {
		return &Hint{ID: "status_chatter", Message: fmt.Sprintf("%d status messages", n)}
	}
	return nil
}

func detectDupRead(ev []Event) *Hint {
	seen := map[string]int{}
	for _, e := range ev {
		if e.Type != "read" || e.Key == "" {
			continue
		}
		k := e.Bot + "|" + e.Key
		seen[k]++
		if seen[k] >= 5 {
			return &Hint{ID: "dup_read", Bot: e.Bot, Message: "repeated read on " + e.Key}
		}
	}
	return nil
}

func detectRetryLoop(ev []Event) *Hint {
	n := 0
	for _, e := range ev {
		if e.Type == "retry" {
			n += max(1, e.N)
		}
	}
	if n >= 5 {
		return &Hint{ID: "retry_loop", Message: fmt.Sprintf("%d retries", n)}
	}
	return nil
}

func detectTokenSpike(ev []Event) *Hint {
	var vals []int
	for _, e := range ev {
		if e.Type == "tokens" {
			vals = append(vals, e.In+e.Out+e.Cache)
		}
	}
	if len(vals) < 3 {
		return nil
	}
	sum := 0
	for _, v := range vals[:len(vals)-1] {
		sum += v
	}
	avg := sum / (len(vals) - 1)
	last := vals[len(vals)-1]
	if avg > 0 && last > avg*3 {
		return &Hint{ID: "token_spike", Message: fmt.Sprintf("last=%d avg=%d", last, avg)}
	}
	return nil
}
