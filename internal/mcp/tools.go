package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/radiancelux/botvault/internal/vault"
)

var errUnknownTool = errors.New("unknown tool")

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
	ctx := r.Context()
	switch name {
	case "list_projects":
		return s.V.ListProjects(ctx, bot)
	case "list":
		return s.V.List(ctx, bot, a.Project, vault.ListFilter{Kind: a.Kind, Tag: a.Tag, Status: a.Status})
	case "get":
		return s.V.Get(ctx, bot, a.Project, a.Kind, a.Key, a.Version)
	case "put":
		return s.V.Put(ctx, bot, vault.PutInput{Project: a.Project, Kind: a.Kind, Key: a.Key,
			Title: a.Title, Body: a.Body, Status: a.Status, Tags: a.Tags, Links: a.Links, IfVersion: a.IfVersion})
	case "append":
		return s.V.Append(ctx, bot, a.Project, a.Kind, a.Key, a.Text)
	case "delete":
		return s.V.Delete(ctx, bot, a.Project, a.Kind, a.Key, a.IfVersion)
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
	addr     = map[string]any{"project": str, "kind": kindProp, "key": str}
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
	{"name": "list_projects", "description": "List projects you can read.", "inputSchema": obj(map[string]any{})},
	{"name": "list", "description": "List record summaries (no body) in a project, newest first. Archived records are hidden unless status=archived.",
		"inputSchema": obj(map[string]any{"project": str, "kind": kindProp, "tag": str, "status": str}, "project")},
	{"name": "get", "description": "Get a record; version>0 fetches a prior version from history.",
		"inputSchema": obj(with(addr, map[string]any{"version": integer}), "project", "kind", "key")},
	{"name": "put", "description": "Create or replace a record. Pass if_version (0 = must not exist, N = must currently be N) to fail with CONFLICT on a stale write. Author is taken from your token.",
		"inputSchema": obj(with(addr, map[string]any{"title": str, "body": str, "status": str, "tags": strs, "links": strs, "if_version": integer}), "project", "kind", "key", "title")},
	{"name": "append", "description": "Append text to a record's body without a version check; stamped with your bot id and time.",
		"inputSchema": obj(with(addr, map[string]any{"text": str}), "project", "kind", "key", "text")},
	{"name": "delete", "description": "Soft delete: marks the record archived (new version, history kept). Project owners only.",
		"inputSchema": obj(with(addr, map[string]any{"if_version": integer}), "project", "kind", "key")},
}
