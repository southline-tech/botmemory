package usage

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/southline-tech/botmemory/internal/vault"
)

type CSVImportResult struct {
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
	Key      string `json:"key"`
	Date     string `json:"date"`
}

type csvStore struct {
	Rows   []csvRow `json:"rows"`
	Dedupe []string `json:"dedupe"`
}

type csvRow struct {
	AtUTC   string `json:"at"`
	Bot     string `json:"bot,omitempty"`
	In      int    `json:"in,omitempty"`
	Out     int    `json:"out,omitempty"`
	Source  string `json:"source"`
	RowHash string `json:"hash"`
}

type CSVMap struct {
	Time   string `json:"time"`
	Bot    string `json:"bot"`
	Tokens string `json:"tokens_in"`
	TOut   string `json:"tokens_out"`
}

func detectCSVMap(header []string) CSVMap {
	lower := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	m := CSVMap{Time: "timestamp", Bot: "bot", Tokens: "tokens_in", TOut: "tokens_out"}
	for i, h := range header {
		hl := lower(h)
		switch {
		case strings.Contains(hl, "time") || hl == "date":
			m.Time = header[i]
		case hl == "bot" || hl == "agent" || hl == "slug":
			m.Bot = header[i]
		case strings.Contains(hl, "input") || hl == "tokens_in" || hl == "in":
			m.Tokens = header[i]
		case strings.Contains(hl, "output") || hl == "tokens_out" || hl == "out":
			m.TOut = header[i]
		}
	}
	return m
}

func (s *Service) ImportCSV(ctx context.Context, authBot, project, date, tz string, r io.Reader, override CSVMap) (*CSVImportResult, error) {
	if _, err := parseDateOnly(date); err != nil {
		return nil, err
	}
	cr := csv.NewReader(r)
	cr.ReuseRecord = false
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("%w: csv header: %v", ErrInvalid, err)
	}
	header = append([]string(nil), header...)
	m := detectCSVMap(header)
	if override.Time != "" {
		m = override
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	_ = idx
	key, _ := CSVKey(date)
	store := csvStore{}
	if prev, err := s.V.Get(ctx, authBot, project, key, 0); err == nil {
		_ = json.Unmarshal([]byte(prev.Body), &store)
	}
	dedupeSet := map[string]struct{}{}
	for _, h := range store.Dedupe {
		dedupeSet[h] = struct{}{}
	}
	imported, skipped := 0, 0
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: csv row: %v", ErrInvalid, err)
		}
		rec := map[string]string{}
		for i, h := range header {
			if i < len(row) {
				rec[h] = row[i]
			}
		}
		tsRaw := strings.TrimSpace(rec[m.Time])
		if tsRaw == "" {
			skipped++
			continue
		}
		at, zerr := parseCSVTime(tsRaw, tz)
		if zerr == ErrCSVZone {
			return nil, fmt.Errorf("%w: zoneless timestamp; pass --tz", ErrCSVZone)
		}
		if zerr != nil {
			skipped++
			continue
		}
		bot := strings.TrimSpace(rec[m.Bot])
		in := atoi(strings.TrimSpace(rec[m.Tokens]))
		out := atoi(strings.TrimSpace(rec[m.TOut]))
		hash := rowHash(tsRaw, bot, in, out)
		if _, ok := dedupeSet[hash]; ok {
			skipped++
			continue
		}
		dedupeSet[hash] = struct{}{}
		store.Dedupe = append(store.Dedupe, hash)
		store.Rows = append(store.Rows, csvRow{
			AtUTC: at.UTC().Format(time.RFC3339), Bot: bot, In: in, Out: out, Source: "csv", RowHash: hash,
		})
		imported++
	}
	body, _ := json.Marshal(store)
	title := "usage csv " + date
	if prev, err := s.V.Get(ctx, authBot, project, key, 0); err == vault.ErrNotFound {
		_, err = s.V.Put(ctx, authBot, vault.PutInput{
			Project: project, Kind: "note", Key: key, Title: title, Body: string(body),
		})
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		ver := prev.Version
		_, err = s.V.Put(ctx, authBot, vault.PutInput{
			Project: project, Key: key, Title: title, Body: string(body), IfVersion: &ver,
		})
		if err != nil {
			return nil, err
		}
	}
	return &CSVImportResult{Imported: imported, Skipped: skipped, Key: key, Date: date}, nil
}

func rowHash(ts, bot string, in, out int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d", ts, bot, in, out)))
	return hex.EncodeToString(sum[:8])
}

func parseCSVTime(raw, defaultTZ string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	if defaultTZ == "" {
		return time.Time{}, ErrCSVZone
	}
	loc, err := time.LoadLocation(defaultTZ)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: bad tz", ErrInvalid)
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, loc)
	if err != nil {
		t, err = time.ParseInLocation("2006-01-02", raw, loc)
	}
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
