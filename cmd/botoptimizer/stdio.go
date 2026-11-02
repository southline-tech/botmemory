package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/southline-tech/botoptimizer/internal/mcp"
)

func runStdio(s *mcp.Server) error {
	tok := strings.TrimSpace(os.Getenv("BOTOPTIMIZER_TOKEN"))
	if tok == "" {
		if f := strings.TrimSpace(os.Getenv("BOTOPTIMIZER_TOKEN_FILE")); f != "" {
			b, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			tok = strings.TrimSpace(string(b))
		}
	}
	if tok == "" {
		return fmt.Errorf("BOTOPTIMIZER_TOKEN or BOTOPTIMIZER_TOKEN_FILE is required for stdio")
	}
	bot, err := s.ACL().Authenticate(tok)
	if err != nil {
		return fmt.Errorf("token rejected: %w", err)
	}
	log.SetOutput(os.Stderr)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "parse error"}})
			continue
		}
		var resp map[string]any
		switch req.Method {
		case "initialize":
			ver := s.Version
			if ver == "" {
				ver = Version
			}
			resp = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "botoptimizer", "version": ver},
			}}
		case "ping":
			resp = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}}
		case "tools/list":
			resp = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"tools": mcp.ToolDefs()}}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			resp = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": s.CallTool(context.Background(), bot, p.Name, p.Arguments)}
		default:
			resp = map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32601, "message": "method not found"}}
		}
		_ = json.NewEncoder(os.Stdout).Encode(resp)
	}
	return sc.Err()
}
