package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/southline-tech/botoptimizer/internal/vault"
)

// Service runs optimizer usage operations on a vault.
type Service struct {
	V   *vault.Vault
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// LogResponse is the compact usage_log result.
type LogResponse struct {
	OK      bool   `json:"ok"`
	Key     string `json:"key"`
	Version int    `json:"version"`
}

func (s *Service) Log(ctx context.Context, authBot, project string, in LogInput) (*LogResponse, error) {
	if err := vault.ValidName("bot", in.Bot); err != nil {
		return nil, err
	}
	key, err := LogKey(in.Bot)
	if err != nil {
		return nil, err
	}
	line, err := in.format(s.now())
	if err != nil {
		return nil, err
	}
	rec, err := s.V.Append(ctx, authBot, project, key, line, "note")
	if err != nil {
		return nil, err
	}
	return &LogResponse{OK: true, Key: key, Version: rec.Version}, nil
}

type ShareDoc struct {
	Date string     `json:"date"`
	Bots []ShareBot `json:"bots"`
}

type ShareBot struct {
	Slug     string  `json:"slug"`
	SharePct float64 `json:"share_pct"`
}

func (s *Service) ImportShare(ctx context.Context, authBot, project, date string, doc ShareDoc, merge bool) (map[string]any, error) {
	if doc.Date == "" {
		doc.Date = date
	}
	if doc.Date != date {
		return nil, fmt.Errorf("%w: date mismatch", ErrInvalid)
	}
	if _, err := parseDateOnly(doc.Date); err != nil {
		return nil, err
	}
	for _, b := range doc.Bots {
		if err := vault.ValidName("bot", b.Slug); err != nil {
			return nil, err
		}
		if b.SharePct < 0 || b.SharePct > 100 {
			return nil, fmt.Errorf("%w: share_pct out of range", ErrInvalid)
		}
	}
	key, err := ShareKey(date)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(doc)
	if merge {
		prev, err := s.V.Get(ctx, authBot, project, key, 0)
		if err == nil {
			var old ShareDoc
			if json.Unmarshal([]byte(prev.Body), &old) == nil {
				doc.Bots = mergeShareBots(old.Bots, doc.Bots)
				body, _ = json.Marshal(doc)
			}
		} else if err != vault.ErrNotFound {
			return nil, err
		}
	}
	title := "usage share " + date
	bodyStr := string(body)
	prev, gerr := s.V.Get(ctx, authBot, project, key, 0)
	if gerr == vault.ErrNotFound {
		rec, err := s.V.Put(ctx, authBot, vault.PutInput{
			Project: project, Kind: "note", Key: key, Title: title, Body: bodyStr,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "key": key, "version": rec.Version, "bots": len(doc.Bots)}, nil
	}
	if gerr != nil {
		return nil, gerr
	}
	ver := prev.Version
	rec, err := s.V.Put(ctx, authBot, vault.PutInput{
		Project: project, Key: key, Title: title, Body: bodyStr, IfVersion: &ver,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "key": key, "version": rec.Version, "bots": len(doc.Bots)}, nil
}

func mergeShareBots(old, neu []ShareBot) []ShareBot {
	m := map[string]ShareBot{}
	for _, b := range old {
		m[b.Slug] = b
	}
	for _, b := range neu {
		m[b.Slug] = b
	}
	out := make([]ShareBot, 0, len(m))
	for _, b := range m {
		out = append(out, b)
	}
	return out
}

func (s *Service) loadEvents(ctx context.Context, authBot, project, botFilter string, since time.Time) ([]Event, error) {
	entries, err := s.V.List(ctx, authBot, project, vault.ListFilter{})
	if err != nil {
		return nil, err
	}
	var all []Event
	for _, e := range entries {
		if !prefixLogUsage(e.Key) {
			continue
		}
		bot := botFromLogKey(e.Key)
		if botFilter != "" && bot != botFilter {
			continue
		}
		rec, err := s.V.Get(ctx, authBot, project, e.Key, 0)
		if err != nil {
			continue
		}
		for _, ev := range parseEventsFromBody(bot, rec.Body) {
			if !since.IsZero() && ev.At.Before(since) {
				continue
			}
			all = append(all, ev)
		}
	}
	return all, nil
}

func parseDateOnly(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalid)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
}
