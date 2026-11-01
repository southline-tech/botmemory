package usage

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxLogLine = 280

var (
	proxyTypes = map[string]struct{}{
		"wake": {}, "bytes_r": {}, "bytes_w": {}, "tool": {}, "subagent": {},
		"recheck": {}, "msg": {}, "image": {}, "retry": {}, "read": {}, "other": {},
	}
	logLineRE = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z) ([a-z_]+)(.*)$`)
)

// Event is one parsed usage log line.
type Event struct {
	At    time.Time
	Type  string
	Bot   string // from key, not line
	In    int
	Out   int
	Cache int
	N     int
	Bytes int
	Model string
	Cost  float64
	Tool  string
	PR    string
	Key   string
	Ref   string
	Raw   string
}

// LogInput is usage_log / CLI input.
type LogInput struct {
	Bot   string
	Type  string
	In    *int
	Out   *int
	Cache int
	Model string
	Cost  float64
	N     int
	Bytes int
	Tool  string
	PR    string
	Key   string
	Ref   string
}

func (in LogInput) format(at time.Time) (string, error) {
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		return "", fmt.Errorf("%w: type required", ErrInvalid)
	}
	if typ == "tokens" {
		if in.In == nil || in.Out == nil {
			return "", fmt.Errorf("%w: tokens require in and out", ErrInvalid)
		}
		if *in.In < 0 || *in.Out < 0 {
			return "", fmt.Errorf("%w: tokens in and out must be non-negative", ErrInvalid)
		}
	} else if _, ok := proxyTypes[typ]; !ok {
		return "", fmt.Errorf("%w: unknown type %q", ErrInvalid, typ)
	}
	parts := []string{at.UTC().Format(time.RFC3339), typ}
	if typ == "tokens" {
		parts = append(parts, fmt.Sprintf("in=%d out=%d", *in.In, *in.Out))
		if in.Cache > 0 {
			parts = append(parts, fmt.Sprintf("cache=%d", in.Cache))
		}
		if in.Model != "" {
			parts = append(parts, "model="+sanitizeKV(in.Model))
		}
		if in.Cost > 0 {
			parts = append(parts, fmt.Sprintf("cost=%g", in.Cost))
		}
	} else {
		if in.N > 0 {
			parts = append(parts, fmt.Sprintf("n=%d", in.N))
		}
		if in.Bytes > 0 {
			parts = append(parts, fmt.Sprintf("bytes=%d", in.Bytes))
		}
		if in.Tool != "" {
			parts = append(parts, "tool="+sanitizeKV(in.Tool))
		}
		if in.PR != "" {
			parts = append(parts, "pr="+sanitizeKV(in.PR))
		}
		if in.Key != "" {
			parts = append(parts, "key="+sanitizeKV(in.Key))
		}
	}
	if in.Ref != "" {
		parts = append(parts, "ref="+sanitizeKV(in.Ref))
	}
	line := strings.Join(parts, " ")
	if len(line) > maxLogLine {
		return "", fmt.Errorf("%w: line exceeds %d chars", ErrInvalid, maxLogLine)
	}
	return line, nil
}

func sanitizeKV(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func parseKV(rest string) map[string]string {
	m := map[string]string{}
	for _, tok := range strings.Fields(strings.TrimSpace(rest)) {
		i := strings.Index(tok, "=")
		if i <= 0 {
			continue
		}
		m[tok[:i]] = tok[i+1:]
	}
	return m
}

func parseEventsFromBody(bot, body string) []Event {
	var out []Event
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "—") {
			continue
		}
		m := logLineRE.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}
		at, err := time.Parse(time.RFC3339, m[1])
		if err != nil {
			continue
		}
		ev := Event{At: at, Type: m[2], Bot: bot, Raw: line}
		kv := parseKV(m[3])
		ev.In = atoi(kv["in"])
		ev.Out = atoi(kv["out"])
		ev.Cache = atoi(kv["cache"])
		ev.N = atoi(kv["n"])
		ev.Bytes = atoi(kv["bytes"])
		ev.Model = kv["model"]
		ev.Tool = kv["tool"]
		ev.PR = kv["pr"]
		ev.Key = kv["key"]
		ev.Ref = kv["ref"]
		if c, ok := kv["cost"]; ok {
			ev.Cost, _ = strconv.ParseFloat(c, 64)
		}
		out = append(out, ev)
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
