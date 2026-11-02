// Package mcp exposes the vault as an MCP server over Streamable HTTP
// (single JSON response per POST). Each request must carry
// "Authorization: Bearer <bot token>"; the token resolves to the bot id that
// is stamped on every write. Bots never see Drive credentials.
package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/southline-tech/botoptimizer/internal/acl"
	"github.com/southline-tech/botoptimizer/internal/vault"
)

const protocolVersion = "2025-06-18"

type Server struct {
	V       *vault.Vault
	ACL     func() *acl.ACL
	Version string
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	// DNS-rebinding guard (MCP Streamable HTTP): browsers send Origin; only
	// loopback origins are accepted. Non-browser clients send none.
	if o := r.Header.Get("Origin"); o != "" && !loopbackOrigin(o) {
		http.Error(w, "forbidden origin", http.StatusForbidden)
		return
	}
	tok, ok := bearer(r.Header.Get("Authorization"))
	bot, err := s.ACL().Authenticate(tok)
	if !ok || err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var raw json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&raw); err != nil {
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
		return
	}
	if t := strings.TrimSpace(string(raw)); strings.HasPrefix(t, "[") {
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32600, "batch requests are not supported"}})
		return
	}
	var req rpcReq
	if err := json.Unmarshal(raw, &req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcErr{-32600, "invalid request: jsonrpc must be \"2.0\" with a method"}})
		return
	}
	if len(req.ID) == 0 { // notification
		w.WriteHeader(http.StatusAccepted)
		return
	}
	res, rerr := s.dispatch(r, bot, req)
	writeJSON(w, rpcResp{JSONRPC: "2.0", ID: req.ID, Result: res, Error: rerr})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) dispatch(r *http.Request, bot string, req rpcReq) (any, *rpcErr) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "botoptimizer", "version": s.serverVersion()},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcErr{-32602, "invalid params"}
		}
		out, err := s.call(r, bot, p.Name, p.Arguments)
		if errors.Is(err, errUnknownTool) {
			return nil, &rpcErr{-32602, err.Error()}
		}
		return toolResult(out, err), nil
	}
	return nil, &rpcErr{-32601, "method not found: " + req.Method}
}

func (s *Server) serverVersion() string {
	if s.Version != "" {
		return s.Version
	}
	return "dev"
}

func compactJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// toolResult renders tool errors in-band (isError) with a stable code prefix
// so bots can branch on CONFLICT / FORBIDDEN / NOT_FOUND / INVALID.
func toolResult(out any, err error) map[string]any {
	if err != nil {
		return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": formatToolErr(err)}}}
	}
	b, _ := compactJSON(out)
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(b)}},
		"structuredContent": map[string]any{"result": out},
	}
}

// bearer extracts a token from "Bearer <token>" (scheme case-insensitive).
func bearer(h string) (string, bool) {
	scheme, tok, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(tok) == "" {
		return "", false
	}
	return strings.TrimSpace(tok), true
}

func loopbackOrigin(o string) bool {
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
