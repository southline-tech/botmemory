// Package mcp exposes the vault as an MCP server over Streamable HTTP
// (single JSON response per POST). Each request must carry
// "Authorization: Bearer <bot token>"; the token resolves to the bot id that
// is stamped on every write. Bots never see Drive credentials.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/vault"
)

const protocolVersion = "2025-06-18"

type Server struct {
	V   *vault.Vault
	ACL func() *acl.ACL
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
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	bot, err := s.ACL().Authenticate(tok)
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req rpcReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
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
			"serverInfo":      map[string]any{"name": "botvault", "version": "0.1.0-m1"},
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

// toolResult renders tool errors in-band (isError) with a stable code prefix
// so bots can branch on CONFLICT / FORBIDDEN / NOT_FOUND / INVALID.
func toolResult(out any, err error) map[string]any {
	if err != nil {
		code := "INTERNAL"
		switch {
		case errors.Is(err, vault.ErrConflict):
			code = "CONFLICT"
		case errors.Is(err, vault.ErrForbidden):
			code = "FORBIDDEN"
		case errors.Is(err, vault.ErrNotFound):
			code = "NOT_FOUND"
		case errors.Is(err, vault.ErrInvalid):
			code = "INVALID"
		}
		return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("%s: %v", code, err)}}}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return map[string]any{
		"content":           []any{map[string]any{"type": "text", "text": string(b)}},
		"structuredContent": map[string]any{"result": out},
	}
}
