package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/store"
	"github.com/radiancelux/botvault/internal/vault"
)

func newSrv(t *testing.T) *httptest.Server {
	a, err := acl.Parse([]byte(fmt.Sprintf(`{"bots":{"pm":{"token_sha256":%q,"roles":{"vc":"owner"}},"impl":{"token_sha256":%q,"roles":{"vc":"write"}}}}`,
		acl.HashToken("pm-tok"), acl.HashToken("impl-tok"))))
	if err != nil {
		t.Fatal(err)
	}
	f := func() *acl.ACL { return a }
	s := httptest.NewServer(&Server{V: vault.New(store.NewMemory(), f), ACL: f})
	t.Cleanup(s.Close)
	return s
}

func rpc(t *testing.T, s *httptest.Server, tok, method string, params any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest("POST", s.URL, bytes.NewReader(body))
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func call(t *testing.T, s *httptest.Server, tok, tool string, args map[string]any) (bool, string) {
	t.Helper()
	_, out := rpc(t, s, tok, "tools/call", map[string]any{"name": tool, "arguments": args})
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", out)
	}
	txt := res["content"].([]any)[0].(map[string]any)["text"].(string)
	isErr, _ := res["isError"].(bool)
	return isErr, txt
}

func TestAuthRequired(t *testing.T) {
	s := newSrv(t)
	for _, tok := range []string{"", "wrong"} {
		if code, _ := rpc(t, s, tok, "tools/list", nil); code != 401 {
			t.Errorf("token %q: %d", tok, code)
		}
	}
}

func TestInitializeAndList(t *testing.T) {
	s := newSrv(t)
	_, out := rpc(t, s, "pm-tok", "initialize", map[string]any{})
	if out["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "botvault" {
		t.Fatal(out)
	}
	_, out = rpc(t, s, "pm-tok", "tools/list", nil)
	names := []string{}
	for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "list_projects,list,get,put,append,delete" {
		t.Fatal(names)
	}
	_, out = rpc(t, s, "pm-tok", "nope", nil)
	if out["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatal(out)
	}
}

func TestToolFlowStampsAuthorFromToken(t *testing.T) {
	s := newSrv(t)
	k := map[string]any{"project": "vc", "kind": "handoff", "key": "rad-280"}
	with := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for a, b := range k {
			m[a] = b
		}
		for a, b := range extra {
			m[a] = b
		}
		return m
	}
	if e, txt := call(t, s, "impl-tok", "put", with(map[string]any{"title": "H", "body": "b", "if_version": 0})); e || !strings.Contains(txt, `"updated_by": "impl"`) {
		t.Fatal(txt)
	}
	// clients cannot spoof the author: unknown fields are rejected
	if e, txt := call(t, s, "impl-tok", "put", with(map[string]any{"title": "H", "updated_by": "pm"})); !e || !strings.HasPrefix(txt, "INVALID") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "put", with(map[string]any{"title": "H2", "if_version": 0})); !e || !strings.HasPrefix(txt, "CONFLICT") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "append", with(map[string]any{"text": "note"})); e || !strings.Contains(txt, `"version": 2`) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "impl-tok", "delete", k); !e || !strings.HasPrefix(txt, "FORBIDDEN") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "delete", k); e || !strings.Contains(txt, `"status": "archived"`) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "get", with(map[string]any{"version": 1})); e || !strings.Contains(txt, `"version": 1`) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "list_projects", nil); e || !strings.Contains(txt, "vc") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "list", map[string]any{"project": "vc", "status": "archived"}); e || !strings.Contains(txt, "rad-280") {
		t.Fatal(txt)
	}
	_, out := rpc(t, s, "pm-tok", "tools/call", map[string]any{"name": "bogus"})
	if out["error"] == nil {
		t.Fatal("unknown tool accepted")
	}
}
