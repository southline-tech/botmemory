package usage

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/vault"
)

var uctx = context.Background()

func intPtr(n int) *int {
	v := n
	return &v
}

func usageVault(t *testing.T) (*Service, string) {
	t.Helper()
	a, err := acl.Parse([]byte(`{"bots":{"pm":{"token_sha256":"` + acl.HashToken("pm-tok") + `","roles":{"vc":"owner"}},"ro":{"token_sha256":"` + acl.HashToken("ro-tok") + `","roles":{"vc":"read"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	v := vault.New(store.NewMemory(), func() *acl.ACL { return a })
	return &Service{V: v, Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }}, "vc"
}

func TestCSVRecKeys(t *testing.T) {
	data := "timestamp,bot,tokens_in,tokens_out\n2026-11-01T10:00:00Z,grok,3,2\n"
	cr := csv.NewReader(strings.NewReader(data))
	h, err := cr.Read()
	if err != nil {
		t.Fatal(err)
	}
	row, err := cr.Read()
	if err != nil {
		t.Fatal(err)
	}
	m := detectCSVMap(h)
	rec := map[string]string{}
	for i, hi := range h {
		rec[hi] = row[i]
	}
	if rec[m.Time] == "" {
		t.Fatalf("m=%+v h=%v rec=%v", m, h, rec)
	}
}

func TestParseCSVTimeRFC3339(t *testing.T) {
	if _, err := parseCSVTime("2026-11-01T10:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
}

func TestUsageLogTokensAndProxy(t *testing.T) {
	s, p := usageVault(t)
	_, err := s.Log(uctx, "pm", p, LogInput{Bot: "alpha", Type: "tokens", In: intPtr(10), Out: intPtr(5)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Log(uctx, "pm", p, LogInput{Bot: "alpha", Type: "wake", N: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Log(uctx, "pm", p, LogInput{Bot: "alpha", Type: "bogus"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestUsageLogTokensRequireInOut(t *testing.T) {
	s, p := usageVault(t)
	v := s.V

	t.Run("service omitted", func(t *testing.T) {
		_, err := s.Log(uctx, "pm", p, LogInput{Bot: "a", Type: "tokens"})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "tokens require in and out") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("service explicit zero", func(t *testing.T) {
		if _, err := s.Log(uctx, "pm", p, LogInput{Bot: "a", Type: "tokens", In: intPtr(0), Out: intPtr(0)}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("service negative", func(t *testing.T) {
		_, err := s.Log(uctx, "pm", p, LogInput{Bot: "a", Type: "tokens", In: intPtr(-1), Out: intPtr(1)})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("got %v", err)
		}
	})

	resolve := func(proj string) (string, error) {
		if proj == "" {
			return p, nil
		}
		return proj, nil
	}
	t.Run("mcp omitted", func(t *testing.T) {
		raw := json.RawMessage(`{"bot":"a","type":"tokens","project":"vc"}`)
		_, err := CallTool(uctx, v, "pm", "usage_log", raw, resolve)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "tokens require in and out") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("mcp explicit zero", func(t *testing.T) {
		raw := json.RawMessage(`{"bot":"a","type":"tokens","in":0,"out":0,"project":"vc"}`)
		if _, err := CallTool(uctx, v, "pm", "usage_log", raw, resolve); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("cli map omitted", func(t *testing.T) {
		err := ValidateUsageLogMap(map[string]any{"type": "tokens", "bot": "a"})
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "tokens require in and out") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("cli map explicit zero", func(t *testing.T) {
		if err := ValidateUsageLogMap(map[string]any{"type": "tokens", "bot": "a", "in": 0, "out": 0}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUsageLogLineCap(t *testing.T) {
	big := strings.Repeat("x", 64)
	_, err := LogInput{Bot: "a", Type: "wake", N: 1, Tool: big, PR: big, Key: big, Ref: big}.format(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("expected cap", err)
	}
}

func TestUsageLogAppendCreate(t *testing.T) {
	s, p := usageVault(t)
	r, err := s.Log(uctx, "pm", p, LogInput{Bot: "newbot", Type: "msg", N: 1})
	if err != nil || !r.OK || r.Version < 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestShareImportValidInvalid(t *testing.T) {
	s, p := usageVault(t)
	doc := ShareDoc{Date: "2026-11-01", Bots: []ShareBot{{Slug: "grok", SharePct: 12.5}}}
	_, err := s.ImportShare(uctx, "pm", p, "2026-11-01", doc, false)
	if err != nil {
		t.Fatal(err)
	}
	bad := ShareDoc{Date: "2026-11-01", Bots: []ShareBot{{Slug: "grok", SharePct: 150}}}
	_, err = s.ImportShare(uctx, "pm", p, "2026-11-01", bad, false)
	if !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestShareImportMerge(t *testing.T) {
	s, p := usageVault(t)
	d1 := ShareDoc{Date: "2026-11-02", Bots: []ShareBot{{Slug: "a", SharePct: 1}}}
	d2 := ShareDoc{Date: "2026-11-02", Bots: []ShareBot{{Slug: "b", SharePct: 2}}}
	if _, err := s.ImportShare(uctx, "pm", p, "2026-11-02", d1, false); err != nil {
		t.Fatal(err)
	}
	out, err := s.ImportShare(uctx, "pm", p, "2026-11-02", d2, true)
	if err != nil || out["bots"].(int) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestWatchTokenAndProxy(t *testing.T) {
	s, p := usageVault(t)
	for i := 0; i < 5; i++ {
		if _, err := s.Log(uctx, "pm", p, LogInput{Bot: "hot", Type: "tokens", In: intPtr(200000), Out: intPtr(200000)}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.WatchCheck(uctx, "pm", p, "hot", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	alerts := out["alerts"].([]WatchAlert)
	if len(alerts) == 0 {
		t.Fatal("expected token alert")
	}
	if alerts[0].ID != "tok-day" {
		t.Fatalf("%+v", alerts)
	}
	for i := 0; i < 40; i++ {
		if _, err := s.Log(uctx, "pm", p, LogInput{Bot: "hot", Type: "recheck", N: 1}); err != nil {
			t.Fatal(err)
		}
	}
	out, err = s.WatchCheck(uctx, "pm", p, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	foundProxy := false
	for _, a := range out["alerts"].([]WatchAlert) {
		if a.ID == "proxy-recheck" {
			foundProxy = true
		}
	}
	if !foundProxy {
		t.Fatal("proxy alert", out)
	}
}

func TestWatchClean(t *testing.T) {
	s, p := usageVault(t)
	out, err := s.WatchCheck(uctx, "pm", p, "", s.now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if out["count"].(int) != 0 {
		t.Fatal(out)
	}
}

func TestChartTokensHTML(t *testing.T) {
	s, p := usageVault(t)
	_, _ = s.Log(uctx, "pm", p, LogInput{Bot: "b", Type: "tokens", In: intPtr(100), Out: intPtr(50)})
	html, meta, err := s.ChartHTML(uctx, "pm", p, "7d")
	if err != nil || meta["mode"] != "tokens" || !strings.Contains(html, "<svg") {
		t.Fatalf("%v %q", meta, html[:80])
	}
}

func TestChartShareFallback(t *testing.T) {
	s, p := usageVault(t)
	doc := ShareDoc{Date: "2026-11-01", Bots: []ShareBot{{Slug: "x", SharePct: 5}}}
	if _, err := s.ImportShare(uctx, "pm", p, "2026-11-01", doc, false); err != nil {
		t.Fatal(err)
	}
	html, meta, err := s.ChartHTML(uctx, "pm", p, "7d")
	if err != nil || meta["mode"] != "share" || !strings.Contains(html, "disclaimer") {
		t.Fatalf("%v", meta)
	}
	_ = html
}

func TestChartRangeBounds(t *testing.T) {
	if _, err := ParseRange("23h"); !errors.Is(err, ErrRangeInvalid) {
		t.Fatal(err)
	}
	if _, err := ParseRange("91d"); !errors.Is(err, ErrRangeInvalid) {
		t.Fatal(err)
	}
	if _, err := ParseRange("24h"); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRange("90d"); err != nil {
		t.Fatal(err)
	}
}

func TestCSVImportDedupe(t *testing.T) {
	s, p := usageVault(t)
	csv := "timestamp,bot,tokens_in,tokens_out\n2026-11-01 10:00:00,grok,10,5\n2026-11-01 10:00:00,grok,10,5\n"
	r1, err := s.ImportCSV(uctx, "pm", p, "2026-11-01", "UTC", strings.NewReader(csv), CSVMap{})
	if err != nil || r1.Imported != 1 || r1.Skipped != 1 {
		t.Fatalf("%+v %v", r1, err)
	}
	r2, err := s.ImportCSV(uctx, "pm", p, "2026-11-01", "UTC", strings.NewReader(csv), CSVMap{})
	if err != nil || r2.Imported != 0 || r2.Skipped != 2 {
		t.Fatalf("%+v %v", r2, err)
	}
}

func TestCSVZonelessRequiresTZ(t *testing.T) {
	s, p := usageVault(t)
	csv := "timestamp,bot,tokens_in,tokens_out\n2026-11-01 10:00:00,grok,1,1\n"
	_, err := s.ImportCSV(uctx, "pm", p, "2026-11-01", "", strings.NewReader(csv), CSVMap{})
	if !errors.Is(err, ErrCSVZone) {
		t.Fatal(err)
	}
}

func TestCSVZonedTimestamp(t *testing.T) {
	s, p := usageVault(t)
	csv := "timestamp,bot,tokens_in,tokens_out\n2026-11-01T10:00:00Z,grok,3,2\n"
	r, err := s.ImportCSV(uctx, "pm", p, "2026-11-01", "", strings.NewReader(csv), CSVMap{})
	if err != nil || r.Imported != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestHintsFixtures(t *testing.T) {
	s, p := usageVault(t)
	cases := []LogInput{
		{Bot: "b", Type: "recheck", N: 1},
		{Bot: "b", Type: "image", Bytes: 600000},
		{Bot: "b", Type: "msg", N: 1},
		{Bot: "b", Type: "read", Key: "plan"},
		{Bot: "b", Type: "retry", N: 1},
		{Bot: "b", Type: "tokens", In: intPtr(100), Out: intPtr(100)},
	}
	for i := 0; i < 25; i++ {
		for _, c := range cases {
			if _, err := s.Log(uctx, "pm", p, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Log(uctx, "pm", p, LogInput{Bot: "b", Type: "tokens", In: intPtr(5000), Out: intPtr(5000)}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.Hints(uctx, "pm", p, "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, h := range out["hints"].([]Hint) {
		ids[h.ID] = true
	}
	for _, want := range []string{"recheck_loop", "screenshot_bloat", "status_chatter", "dup_read", "retry_loop", "token_spike"} {
		if !ids[want] {
			t.Fatalf("missing %s in %+v", want, ids)
		}
	}
}

func TestParseSinceFormsChanged(t *testing.T) {
	s, p := usageVault(t)
	since, err := vault.ParseSince("2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.Log(uctx, "pm", p, LogInput{Bot: "b", Type: "msg", N: 1})
	out, err := s.WatchCheck(uctx, "pm", p, "", since)
	if err != nil {
		t.Fatal(err)
	}
	if out["count"].(int) < 0 {
		t.Fatal(out)
	}
}
