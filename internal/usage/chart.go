package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/southline-tech/botoptimizer/internal/vault"
)

func ParseRange(r string) (time.Duration, error) {
	r = strings.TrimSpace(strings.ToLower(r))
	if r == "" {
		r = "7d"
	}
	var d time.Duration
	switch {
	case strings.HasSuffix(r, "d"):
		n, err := time.ParseDuration(strings.TrimSuffix(r, "d") + "h")
		if err != nil {
			return 0, fmt.Errorf("%w: bad range", ErrRangeInvalid)
		}
		d = n * 24
	case strings.HasSuffix(r, "h"):
		var err error
		d, err = time.ParseDuration(r)
		if err != nil {
			return 0, fmt.Errorf("%w: bad range", ErrRangeInvalid)
		}
	default:
		return 0, fmt.Errorf("%w: use 24h-90d", ErrRangeInvalid)
	}
	min := 24 * time.Hour
	max := 90 * 24 * time.Hour
	if d < min || d > max {
		return 0, fmt.Errorf("%w: range must be 24h to 90d", ErrRangeInvalid)
	}
	return d, nil
}

type bucket struct {
	Start time.Time
	Total int
	Mode  string
}

func (s *Service) ChartHTML(ctx context.Context, authBot, project, rangeStr string) (string, map[string]any, error) {
	dur, err := ParseRange(rangeStr)
	if err != nil {
		return "", nil, err
	}
	now := s.now()
	since := now.Add(-dur)
	events, err := s.loadEvents(ctx, authBot, project, "", since)
	if err != nil {
		return "", nil, err
	}
	hourly := dur <= 72*time.Hour
	buckets := map[int64]*bucket{}
	mode := "tokens"
	hasTokens := false
	for _, ev := range events {
		if ev.Type == "tokens" {
			hasTokens = true
			key := bucketKey(ev.At, hourly)
			b := buckets[key]
			if b == nil {
				b = &bucket{Start: truncateBucket(ev.At, hourly), Mode: "tokens"}
				buckets[key] = b
			}
			b.Total += ev.In + ev.Out + ev.Cache
		}
	}
	if !hasTokens {
		mode = "share"
		entries, _ := s.V.List(ctx, authBot, project, vault.ListFilter{})
		for _, e := range entries {
			if !strings.HasPrefix(e.Key, "usage.share.") {
				continue
			}
			rec, err := s.V.Get(ctx, authBot, project, e.Key, 0)
			if err != nil {
				continue
			}
			var doc ShareDoc
			if json.Unmarshal([]byte(rec.Body), &doc) != nil {
				continue
			}
			for _, b := range doc.Bots {
				key := bucketKey(now, hourly)
				bk := buckets[key]
				if bk == nil {
					bk = &bucket{Start: truncateBucket(now, hourly), Mode: "share"}
					buckets[key] = bk
				}
				bk.Total += int(b.SharePct * 1000)
			}
		}
	}
	var keys []int64
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	maxV := 1
	for _, k := range keys {
		if buckets[k].Total > maxV {
			maxV = buckets[k].Total
		}
	}
	var bars strings.Builder
	w := 800
	h := 200
	pad := 40
	for i, k := range keys {
		b := buckets[k]
		x := pad + i*(w-pad*2)/max(len(keys), 1)
		height := b.Total * (h - pad) / maxV
		if height < 1 {
			height = 1
		}
		fmt.Fprintf(&bars, `<rect x="%d" y="%d" width="20" height="%d" fill="#4a90d9"><title>%s total=%d mode=%s</title></rect>`,
			x, h-height, height, b.Start.Format(time.RFC3339), b.Total, b.Mode)
	}
	disclaimer := ""
	if mode == "share" {
		disclaimer = `<p class="disclaimer">Share/import estimates only — no real token series in range.</p>`
	}
	html := fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>botoptimizer usage</title></head><body>
<h1>Usage chart</h1><p>Range: %s · Mode: %s</p>%s
<svg width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">%s</svg>
</body></html>`, rangeStr, mode, disclaimer, w, h+pad, bars.String())
	meta := map[string]any{"mode": mode, "buckets": len(keys), "range": rangeStr}
	return html, meta, nil
}

func (s *Service) ExplainAt(ctx context.Context, authBot, project, atStr string) (map[string]any, error) {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(atStr))
	if err != nil {
		at, err = vault.ParseSince(atStr)
		if err != nil {
			return nil, fmt.Errorf("%w: at must be RFC3339", ErrInvalid)
		}
	}
	since := at.Add(-time.Hour)
	until := at.Add(time.Hour)
	events, err := s.loadEvents(ctx, authBot, project, "", since)
	if err != nil {
		return nil, err
	}
	var top []map[string]any
	for _, ev := range events {
		if ev.At.Before(since) || ev.At.After(until) {
			continue
		}
		top = append(top, map[string]any{
			"bot": ev.Bot, "type": ev.Type, "at": ev.At.Format(time.RFC3339),
			"in": ev.In, "out": ev.Out,
		})
	}
	return map[string]any{"at": at.Format(time.RFC3339), "events": top, "count": len(top)}, nil
}

func bucketKey(t time.Time, hourly bool) int64 {
	return truncateBucket(t, hourly).Unix()
}

func truncateBucket(t time.Time, hourly bool) time.Time {
	t = t.UTC()
	if hourly {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
