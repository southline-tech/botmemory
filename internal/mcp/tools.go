package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/southline-tech/botvault/internal/vault"
)

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
	Query     string   `json:"query"`
	Limit     int      `json:"limit"`
}

func (s *Server) call(r *http.Request, bot, name string, raw json.RawMessage) (any, error) {
	var a args
	if len(raw) > 0 {
		dec := json.NewDecoder(bytesReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&a); err != nil {
			return nil, fmt.Errorf("%w: %v", vault.ErrInvalid, err)
		}
	}
	// Every tool call gets a deadline inside the HTTP server's 60s
	// WriteTimeout; Drive requests and retry sleeps all honour ctx.
	ctx, cancel := context.WithTimeout(r.Context(), CallTimeout)
	defer cancel()
	switch name {
	case "list_projects":
		return s.V.ListProjects(ctx, bot)
	case "list":
		return s.V.List(ctx, bot, a.Project, vault.ListFilter{Kind: a.Kind, Tag: a.Tag, Status: a.Status})
	case "get":
		return s.V.Get(ctx, bot, a.Project, a.Key, a.Version)
	case "put":
		return s.V.Put(ctx, bot, vault.PutInput{Project: a.Project, Kind: a.Kind, Key: a.Key,
			Title: a.Title, Body: a.Body, Status: a.Status, Tags: a.Tags, Links: a.Links, IfVersion: a.IfVersion})
	case "append":
		return s.V.Append(ctx, bot, a.Project, a.Key, a.Text)
	case "search":
		return s.V.Search(ctx, bot, a.Project, a.Query, vault.ListFilter{Kind: a.Kind, Tag: a.Tag, Status: a.Status}, a.Limit)
	case "history":
		return s.V.History(ctx, bot, a.Project, a.Key)
	case "reindex":
		n, skipped, err := s.V.Reindex(ctx, bot, a.Project)
		if skipped == nil {
			skipped = []string{}
		}
		return map[string]any{"entries": n, "skipped": skipped}, err
	case "delete":
		return s.V.Delete(ctx, bot, a.Project, a.Key, a.IfVersion)
	}
	return nil, fmt.Errorf("%w: %s", errUnknownTool, name)
}

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

var (
	str      = map[string]any{"type": "string"}
	strs     = map[string]any{"type": "array", "items": str}
	integer  = map[string]any{"type": "integer", "minimum": 0}
	kindProp = map[string]any{"type": "string", "enum": vault.Kinds}
	addr     = map[string]any{"project": str, "key": str}
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
	{"name": "list_projects", "description": "List projects your token is explicitly granted (deny by default).", "inputSchema": obj(map[string]any{})},
	{"name": "list", "description": "List record summaries (no body) in a project, newest first. Archived records are hidden unless status=archived.",
		"inputSchema": obj(map[string]any{"project": str, "kind": kindProp, "tag": str, "status": str}, "project")},
	{"name": "get", "description": "Get a record; version>0 fetches a prior version from history.",
		"inputSchema": obj(with(addr, map[string]any{"version": integer}), "project", "key")},
	{"name": "put", "description": "Create or update a record (keys are unique per project; kind is optional metadata). Without if_version (or 0) it only creates; updating requires if_version = current version, else CONFLICT. Author is taken from your token.",
		"inputSchema": obj(with(addr, map[string]any{"kind": kindProp, "title": str, "body": str, "status": str, "tags": strs, "links": strs, "if_version": integer}), "project", "key", "title")},
	{"name": "append", "description": "Append text to a record's body without a version check; stamped with your bot id and time.",
		"inputSchema": obj(with(addr, map[string]any{"text": str}), "project", "key", "text")},
	{"name": "search", "description": "Search the index (key, kind, title, tags, first 500 chars of body); all terms must match. project=\"*\" searches every project you can read. Archived hidden unless status=archived.",
		"inputSchema": obj(map[string]any{"project": str, "query": str, "kind": kindProp, "tag": str, "status": str, "limit": integer}, "project", "query")},
	{"name": "history", "description": "List every version of a key (newest first) with author and time; fetch one with get(version=N).",
		"inputSchema": obj(addr, "project", "key")},
	{"name": "reindex", "description": "Rebuild a project's search index from its records. Project owners only. Returns entries and any skipped (unparseable) record files.",
		"inputSchema": obj(map[string]any{"project": str}, "project")},
	{"name": "delete", "description": "Soft delete: marks the record archived (new version, history kept). Project owners only.",
		"inputSchema": obj(with(addr, map[string]any{"if_version": integer}), "project", "key")},
}
