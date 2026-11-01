package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/southline-tech/botmemory/internal/mcp"
)

var prePingOnce sync.Once

var errBotUsage = errors.New("bot cli usage")

type botFlags struct {
	tokenFile string
	project   string
	url       string
	human     bool
}

func parseBotFlags(args []string) (botFlags, []string, error) {
	var f botFlags
	f.url = strings.TrimSpace(os.Getenv("BOTMEMORY_URL"))
	if f.url == "" {
		f.url = "http://127.0.0.1:8787/mcp"
	}
	f.project = strings.TrimSpace(env("PROJECT"))
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--human":
			f.human = true
		case a == "--token-file":
			i++
			if i >= len(args) {
				return f, nil, errBotUsage
			}
			f.tokenFile = args[i]
		case strings.HasPrefix(a, "--token-file="):
			f.tokenFile = strings.TrimPrefix(a, "--token-file=")
		case a == "--project":
			i++
			if i >= len(args) {
				return f, nil, errBotUsage
			}
			f.project = args[i]
		case strings.HasPrefix(a, "--project="):
			f.project = strings.TrimPrefix(a, "--project=")
		case a == "--url":
			i++
			if i >= len(args) {
				return f, nil, errBotUsage
			}
			f.url = args[i]
		case strings.HasPrefix(a, "--url="):
			f.url = strings.TrimPrefix(a, "--url=")
		default:
			rest = append(rest, a)
		}
	}
	if os.Getenv("BOTMEMORY_HUMAN") == "1" {
		f.human = true
	}
	return f, rest, nil
}

func loadToken(f botFlags) (string, error) {
	if f.tokenFile != "" {
		b, err := os.ReadFile(f.tokenFile)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	if p := strings.TrimSpace(env("TOKEN_FILE")); p != "" {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	if t := strings.TrimSpace(env("TOKEN")); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("INVALID: token required; set BOTMEMORY_TOKEN_FILE or --token-file")
}

type mcpClient struct {
	url   string
	token string
	hc    *http.Client
}

func (c *mcpClient) callTool(ctx context.Context, tool string, args map[string]any) (any, error) {
	c.maybePrePing(ctx)
	id := json.RawMessage(`1`)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("FORBIDDEN: unauthorized; check token")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if wrap.Error != nil {
		return nil, fmt.Errorf("INTERNAL: %s", wrap.Error.Message)
	}
	if wrap.Result == nil {
		return nil, fmt.Errorf("INTERNAL: empty MCP result")
	}
	if wrap.Result["isError"] == true {
		text, _ := wrap.Result["content"].([]any)
		if len(text) > 0 {
			if m, ok := text[0].(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					return nil, errors.New(s)
				}
			}
		}
		return nil, fmt.Errorf("INTERNAL: tool failed")
	}
	if sc, ok := wrap.Result["structuredContent"].(map[string]any); ok {
		if r, ok := sc["result"]; ok {
			return r, nil
		}
	}
	if content, ok := wrap.Result["content"].([]any); ok && len(content) > 0 {
		if m, ok := content[0].(map[string]any); ok {
			if s, ok := m["text"].(string); ok {
				var v any
				if json.Unmarshal([]byte(s), &v) == nil {
					return v, nil
				}
				return s, nil
			}
		}
	}
	return wrap.Result, nil
}

func (c *mcpClient) maybePrePing(ctx context.Context) {
	prePingOnce.Do(func() {
		if strings.TrimSpace(os.Getenv("BOTMEMORY_PREPING")) == "0" {
			return
		}
		_ = c.ping(ctx)
	})
}

func (c *mcpClient) ping(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("FORBIDDEN: unauthorized")
	}
	var wrap struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&wrap)
	if wrap.Error != nil {
		return fmt.Errorf("INTERNAL: %s", wrap.Error.Message)
	}
	return nil
}

func runBotPing(args []string) error {
	f, _, err := parseBotFlags(args)
	if err != nil {
		return errBotUsage
	}
	tok, err := loadToken(f)
	if err != nil {
		return err
	}
	c := &mcpClient{url: f.url, token: tok, hc: &http.Client{Timeout: 10 * time.Second}}
	return c.ping(context.Background())
}

func withProject(f botFlags, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	if f.project != "" {
		args["project"] = f.project
	}
	return args
}

func runBotCommand(cmd string, args []string) error {
	f, rest, err := parseBotFlags(args)
	if err != nil {
		printBotUsage()
		return errBotUsage
	}
	tok, err := loadToken(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error()+"; hint: set BOTMEMORY_TOKEN_FILE")
		os.Exit(6)
	}
	c := &mcpClient{url: f.url, token: tok, hc: &http.Client{Timeout: 55 * time.Second}}
	ctx := context.Background()
	out, err := dispatchBot(ctx, c, f, cmd, rest)
	if err != nil {
		if s := err.Error(); strings.Contains(s, ":") {
			fmt.Fprintln(os.Stderr, s)
			os.Exit(mcp.ExitCodeFromToolText(s))
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	b, err := encodeOut(out, f.human)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Stdout.Write(b)
	if !f.human && len(b) > 0 && b[len(b)-1] != '\n' {
		os.Stdout.Write([]byte("\n"))
	}
	return nil
}

func encodeOut(v any, human bool) ([]byte, error) {
	if human {
		return json.MarshalIndent(v, "", "  ")
	}
	return json.Marshal(v)
}

func dispatchBot(ctx context.Context, c *mcpClient, f botFlags, cmd string, args []string) (any, error) {
	switch cmd {
	case "projects":
		return c.callTool(ctx, "list_projects", map[string]any{})
	case "stats":
		return c.callTool(ctx, "stats", map[string]any{})
	case "list":
		a := withProject(f, map[string]any{})
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			a["project"] = args[0]
			args = args[1:]
		}
		for i := 0; i < len(args); i++ {
			if args[i] == "--if-etag" && i+1 < len(args) {
				a["if_etag"] = args[i+1]
				i++
			}
		}
		return c.callTool(ctx, "list", a)
	case "brief":
		a := withProject(f, map[string]any{})
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			a["project"] = args[0]
			args = args[1:]
		}
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--limit":
				i++
				v, _ := strconv.Atoi(args[i])
				a["limit"] = v
			case "--kind":
				i++
				a["kind"] = args[i]
			case "--tag":
				i++
				a["tag"] = args[i]
			case "--if-etag":
				i++
				a["if_etag"] = args[i]
			}
		}
		return c.callTool(ctx, "brief", a)
	case "changed":
		a := withProject(f, map[string]any{})
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			a["project"] = args[0]
			args = args[1:]
		}
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--since":
				i++
				a["since"] = args[i]
			case "--kind":
				i++
				a["kind"] = args[i]
			case "--tag":
				i++
				a["tag"] = args[i]
			}
		}
		return c.callTool(ctx, "changed", a)
	case "get":
		if len(args) < 1 {
			return nil, errBotUsage
		}
		a := withProject(f, map[string]any{"key": args[0]})
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "--version":
				i++
				v, _ := strconv.Atoi(args[i])
				a["version"] = v
			case "--head":
				i++
				v, _ := strconv.Atoi(args[i])
				a["head"] = v
			case "--tail":
				i++
				v, _ := strconv.Atoi(args[i])
				a["tail"] = v
			case "--since":
				i++
				a["since"] = args[i]
			case "--max-bytes":
				i++
				v, _ := strconv.Atoi(args[i])
				a["max_bytes"] = v
			case "--summary":
				a["summary"] = true
			}
		}
		return c.callTool(ctx, "get", a)
	case "put":
		return botPut(ctx, c, f, args)
	case "append":
		return botAppend(ctx, c, f, args)
	case "search":
		return botSearch(ctx, c, f, args)
	case "history":
		if len(args) < 1 {
			return nil, errBotUsage
		}
		return c.callTool(ctx, "history", withProject(f, map[string]any{"key": args[0]}))
	case "delete":
		return botDelete(ctx, c, f, args)
	default:
		return nil, errBotUsage
	}
}

func botPut(ctx context.Context, c *mcpClient, f botFlags, args []string) (any, error) {
	if len(args) < 1 {
		return nil, errBotUsage
	}
	a := withProject(f, map[string]any{"key": args[0]})
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--title":
			i++
			a["title"] = args[i]
		case "--body":
			i++
			a["body"] = args[i]
		case "--file":
			i++
			b, err := os.ReadFile(args[i])
			if err != nil {
				return nil, err
			}
			a["body"] = string(b)
		case "--kind":
			i++
			a["kind"] = args[i]
		case "--tags":
			i++
			var tags []string
			for _, t := range strings.Split(args[i], ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
			a["tags"] = tags
		case "--if-version":
			i++
			v, _ := strconv.Atoi(args[i])
			a["if_version"] = v
		}
	}
	if _, ok := a["title"]; !ok {
		return nil, fmt.Errorf("INVALID: --title required")
	}
	return c.callTool(ctx, "put", a)
}

func botAppend(ctx context.Context, c *mcpClient, f botFlags, args []string) (any, error) {
	if len(args) < 1 {
		return nil, errBotUsage
	}
	a := withProject(f, map[string]any{"key": args[0]})
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--text":
			i++
			a["text"] = args[i]
		case "--file":
			i++
			b, err := os.ReadFile(args[i])
			if err != nil {
				return nil, err
			}
			a["text"] = string(b)
		}
	}
	if _, ok := a["text"]; !ok {
		return nil, fmt.Errorf("INVALID: --text or --file required")
	}
	return c.callTool(ctx, "append", a)
}

func botSearch(ctx context.Context, c *mcpClient, f botFlags, args []string) (any, error) {
	if len(args) < 1 {
		return nil, errBotUsage
	}
	a := map[string]any{"query": args[0]}
	if f.project != "" {
		a["project"] = f.project
	} else {
		a["project"] = "*"
	}
	for i := 1; i < len(args); i++ {
		if args[i] == "--limit" && i+1 < len(args) {
			v, _ := strconv.Atoi(args[i+1])
			a["limit"] = v
			i++
		}
	}
	return c.callTool(ctx, "search", a)
}

func botDelete(ctx context.Context, c *mcpClient, f botFlags, args []string) (any, error) {
	if len(args) < 1 {
		return nil, errBotUsage
	}
	a := withProject(f, map[string]any{"key": args[0]})
	for i := 1; i < len(args); i++ {
		if args[i] == "--if-version" && i+1 < len(args) {
			v, _ := strconv.Atoi(args[i+1])
			a["if_version"] = v
			i++
		}
	}
	return c.callTool(ctx, "delete", a)
}

func printBotUsage() {
	fmt.Fprintln(os.Stderr, `usage: botmemory [--token-file PATH] [--project NAME] [--human] projects|list|get|put|append|search|history|stats|delete …
Alias: bm=botmemory. Token: --token-file, BOTMEMORY_TOKEN_FILE, or BOTMEMORY_TOKEN.`)
}

func isBotCommand(s string) bool {
	switch s {
	case "projects", "list", "get", "put", "append", "search", "history", "stats", "delete", "brief", "changed":
		return true
	}
	return false
}
