package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/southline-tech/botmemory/internal/usage"
	"github.com/southline-tech/botmemory/internal/vault"
	"net/http"
	"strings"
	"time"
)

var usageToolNames = map[string]struct{}{
	"usage_log": {}, "usage_import": {}, "usage_import_csv": {},
	"watch_check": {}, "watch_init": {}, "usage_chart": {}, "usage_hints": {},
}

var errUnknownTool = errors.New("unknown tool")

// CallTimeout bounds one tool call (below the server's 60s WriteTimeout).
var CallTimeout = 50 * time.Second

type args struct {
	Project   string   `json:"project"`
	Kind      string   `json:"kind"`
	Key       string   `json:"key"`
	Version   int      `json:"version"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Status    string   `json:"status"`
	Tag       string   `json:"tag"`
	Tags      []string `json:"tags"`
	Links     []string `json:"links"`
	Text      string   `json:"text"`
	IfVersion *int     `json:"if_version"`
	IfEtag    string   `json:"if_etag"`
	Query     string   `json:"query"`
	Limit     int      `json:"limit"`
	Since     string   `json:"since"`
	Head      int      `json:"head"`
	Tail      int      `json:"tail"`
	MaxBytes  int      `json:"max_bytes"`
	Summary   bool     `json:"summary"`
}

func (s *Server) resolveProject(bot, project string) (string, error) {
	project = strings.TrimSpace(project)
	if project != "" {
		return project, nil
	}
	if dp := s.ACL().DefaultProject(bot); dp != "" {
		return dp, nil
	}
	return "", fmt.Errorf("%w: project required; set ACL default_project or pass project", vault.ErrInvalid)
}

func (s *Server) call(r *http.Request, bot, name string, raw json.RawMessage) (any, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), CallTimeout)
	defer cancel()
	if _, ok := usageToolNames[name]; ok {
		out, err := usage.CallTool(ctx, s.V, bot, name, raw, func(p string) (string, error) {
			return s.resolveProject(bot, p)
		})
		code := ""
		ok := err == nil
		if !ok {
			code = errCode(err)
		}
		RecordCall(bot, name, "", "", time.Since(start).Milliseconds(), ok, code)
		return out, err
	}
	var a args
	if len(raw) > 0 {
		dec := json.NewDecoder(bytesReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&a); err != nil {
			err = fmt.Errorf("%w: %v", vault.ErrInvalid, err)
			RecordCall(bot, name, a.Project, a.Key, time.Since(start).Milliseconds(), false, errCode(err))
			return nil, err
		}
	}
	var (
		out any
		err error
	)
	switch name {
	case "list_projects":
		out, err = s.V.ListProjects(ctx, bot)
	case "stats":
		out = Snapshot()
	case "list":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		out, err = s.V.ListWithEtag(ctx, bot, p, vault.ListFilter{Kind: a.Kind, Tag: a.Tag, Status: a.Status}, a.IfEtag)
	case "brief":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		out, err = s.V.Brief(ctx, bot, p, vault.BriefOpts{Limit: a.Limit, IfEtag: a.IfEtag, Kind: a.Kind, Tag: a.Tag})
	case "changed":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		if strings.TrimSpace(a.Since) == "" {
			err = fmt.Errorf("%w: since required (RFC3339 or YYYY-MM-DD)", vault.ErrInvalid)
			break
		}
		since, perr := vault.ParseSince(a.Since)
		if perr != nil {
			err = perr
			break
		}
		out, err = s.V.Changed(ctx, bot, p, since, vault.ListFilter{Kind: a.Kind, Tag: a.Tag})
	case "get":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		opts := vault.GetOpts{Head: a.Head, Tail: a.Tail, MaxBytes: a.MaxBytes, Summary: a.Summary}
		if strings.TrimSpace(a.Since) != "" {
			since, perr := vault.ParseSince(a.Since)
			if perr != nil {
				err = perr
				break
			}
			opts.Since = since
			opts.HasSince = true
		}
		out, err = s.V.GetWithOpts(ctx, bot, p, a.Key, a.Version, opts)
	case "put":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		out, err = s.V.Put(ctx, bot, vault.PutInput{Project: p, Kind: a.Kind, Key: a.Key,
			Title: a.Title, Body: a.Body, Status: a.Status, Tags: a.Tags, Links: a.Links, IfVersion: a.IfVersion})
	case "append":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		out, err = s.V.Append(ctx, bot, p, a.Key, a.Text)
	case "search":
		p := strings.TrimSpace(a.Project)
		if p == "" {
			err = fmt.Errorf("%w: project required (use \"*\" for all granted)", vault.ErrInvalid)
			break
		}
		out, err = s.V.Search(ctx, bot, p, a.Query, vault.ListFilter{Kind: a.Kind, Tag: a.Tag, Status: a.Status}, a.Limit)
	case "history":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		out, err = s.V.History(ctx, bot, p, a.Key)
	case "reindex":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		n, skipped, e := s.V.Reindex(ctx, bot, p)
		if skipped == nil {
			skipped = []string{}
		}
		out, err = map[string]any{"entries": n, "skipped": skipped}, e
	case "delete":
		p, e := s.resolveProject(bot, a.Project)
		if e != nil {
			err = e
			break
		}
		out, err = s.V.Delete(ctx, bot, p, a.Key, a.IfVersion)
	default:
		err = fmt.Errorf("%w: %s", errUnknownTool, name)
	}
	code := ""
	ok := err == nil
	if !ok {
		code = errCode(err)
	}
	RecordCall(bot, name, a.Project, a.Key, time.Since(start).Milliseconds(), ok, code)
	return out, err
}

// CallTool runs one MCP tool (HTTP or stdio).
func (s *Server) CallTool(ctx context.Context, bot, name string, raw json.RawMessage) map[string]any {
	r, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1/mcp", nil)
	out, err := s.call(r, bot, name, raw)
	return toolResult(out, err)
}

// ToolDefs returns MCP tool metadata.
func ToolDefs() []map[string]any { return toolDefs }

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var (
	str      = map[string]any{"type": "string"}
	strs     = map[string]any{"type": "array", "items": str}
	integer  = map[string]any{"type": "integer", "minimum": 0}
	kindProp = map[string]any{"type": "string", "enum": vault.Kinds}
	proj     = map[string]any{"type": "string", "pattern": vault.NamePattern, "description": "project slug; omit if ACL default_project is set"}
	key      = map[string]any{"type": "string", "pattern": vault.NamePattern, "description": "record key within project"}
	addr     = map[string]any{"project": proj, "key": key}
)

func with(base map[string]any, extra map[string]any) map[string]any {
	m := map[string]any{}
	for k, v := range base {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

var toolDefs = []map[string]any{
	{"name": "list_projects", "description": "List granted projects (name, role, default flag). Includes granted projects with no records yet.", "inputSchema": obj(map[string]any{})},
	{"name": "stats", "description": "Process call counters (no note content). Counts by tool, bot, and error code.", "inputSchema": obj(map[string]any{})},
	{"name": "brief", "description": "Orient fast: index one-liners (~2k tokens). Call before get. Use if_etag to skip unchanged.",
		"inputSchema": obj(map[string]any{"project": proj, "limit": integer, "kind": kindProp, "tag": str, "if_etag": str})},
	{"name": "list", "description": "Call list before get. Returns index summaries; optional if_etag returns unchanged stub only.",
		"inputSchema": obj(map[string]any{"project": proj, "kind": kindProp, "tag": str, "status": str, "if_etag": str})},
	{"name": "changed", "description": "Keys whose index updated_at is after since (RFC3339 or YYYY-MM-DD UTC). Use etag with brief/list if_etag.",
		"inputSchema": obj(map[string]any{"project": proj, "since": str, "kind": kindProp, "tag": str}, "since")},
	{"name": "get", "description": "Get a record. Use brief/list first. Partial: head, tail, since, max_bytes, or summary (no body). head+tail together is INVALID.",
		"inputSchema": obj(with(addr, map[string]any{"version": integer, "head": integer, "tail": integer, "since": str, "max_bytes": integer, "summary": map[string]any{"type": "boolean"}}), "key")},
	{"name": "put", "description": "Create or update. Without if_version only creates; update needs matching if_version. Identical update is a no-op.",
		"inputSchema": obj(with(addr, map[string]any{"kind": kindProp, "title": str, "body": str, "status": str, "tags": strs, "links": strs, "if_version": integer}), "key", "title")},
	{"name": "append", "description": "Append text; creates missing key as kind=note (v1).",
		"inputSchema": obj(with(addr, map[string]any{"text": str}), "key", "text")},
	{"name": "search", "description": "Search summaries; project=\"*\" searches all granted projects.",
		"inputSchema": obj(map[string]any{"project": str, "query": str, "kind": kindProp, "tag": str, "status": str, "limit": integer}, "project", "query")},
	{"name": "history", "description": "List versions for a key (newest first).",
		"inputSchema": obj(addr, "key")},
	{"name": "reindex", "description": "Rebuild search index (owners only).",
		"inputSchema": obj(map[string]any{"project": proj})},
	{"name": "delete", "description": "Soft archive (owners only).",
		"inputSchema": obj(with(addr, map[string]any{"if_version": integer}), "key")},
	{"name": "usage_log", "description": "Append one usage line to log.usage.<bot>. Real tokens primary; never fabricate counts.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"project": proj, "bot": str, "type": str, "in": integer, "out": integer, "cache": integer,
			"model": str, "cost": map[string]any{"type": "number"}, "n": integer, "bytes": integer,
			"tool": str, "pr": str, "key": key, "ref": str,
		}, "required": []string{"bot", "type"}, "additionalProperties": false}},
	{"name": "usage_import", "description": "Write hosted fleet share JSON to usage.share.<date>.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"project": proj, "date": str, "body": str, "merge": map[string]any{"type": "boolean"},
		}, "required": []string{"date", "body"}, "additionalProperties": false}},
	{"name": "usage_import_csv", "description": "Import Grok/dashboard CSV ground truth to usage.csv.<date>.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"project": proj, "date": str, "csv": str, "tz": str,
		}, "required": []string{"date", "csv"}, "additionalProperties": false}},
	{"name": "watch_check", "description": "Evaluate watch.rules; token rules first.",
		"inputSchema": obj(map[string]any{"project": proj, "bot": str, "since": str})},
	{"name": "watch_init", "description": "Create watch.rules with defaults if missing.",
		"inputSchema": obj(map[string]any{"project": proj})},
	{"name": "usage_chart", "description": "Offline HTML+SVG chart (24h-90d). explain with at+explain.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"project": proj, "range": str, "out": str, "at": str, "explain": map[string]any{"type": "boolean"},
		}, "additionalProperties": false}},
	{"name": "usage_hints", "description": "Waste hints (proxy patterns + token_spike).",
		"inputSchema": obj(map[string]any{"project": proj, "bot": str, "since": str})},
}
