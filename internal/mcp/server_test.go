package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/southline-tech/botmemory/internal/drive"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/vault"
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
	if out["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "botmemory" {
		t.Fatal(out)
	}
	_, out = rpc(t, s, "pm-tok", "tools/list", nil)
	names := []string{}
	for _, tl := range out["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "list_projects,stats,brief,list,changed,get,put,append,search,history,reindex,delete" {
		t.Fatal(names)
	}
	_, out = rpc(t, s, "pm-tok", "nope", nil)
	if out["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatal(out)
	}
}

func TestToolFlowStampsAuthorFromToken(t *testing.T) {
	s := newSrv(t)
	k := map[string]any{"project": "vc", "key": "rad-280"}
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
	if e, txt := call(t, s, "impl-tok", "put", with(map[string]any{"kind": "handoff", "title": "H", "body": "b"})); e || !strings.Contains(txt, `"updated_by":"impl"`) {
		t.Fatal(txt)
	}
	// clients cannot spoof the author: unknown fields are rejected
	if e, txt := call(t, s, "impl-tok", "put", with(map[string]any{"title": "H", "updated_by": "pm"})); !e || !strings.HasPrefix(txt, "INVALID") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "put", with(map[string]any{"title": "H2"})); !e || !strings.HasPrefix(txt, "CONFLICT") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "append", with(map[string]any{"text": "note"})); e || (!strings.Contains(txt, `"version":2`) && !strings.Contains(txt, `"version": 2`)) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "impl-tok", "delete", k); !e || !strings.HasPrefix(txt, "FORBIDDEN") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "delete", k); e || (!strings.Contains(txt, `"status":"archived"`) && !strings.Contains(txt, `"status": "archived"`)) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "get", with(map[string]any{"version": 1})); e || (!strings.Contains(txt, `"version":1`) && !strings.Contains(txt, `"version": 1`)) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "list_projects", nil); e || !strings.Contains(txt, "vc") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "list", map[string]any{"project": "vc", "status": "archived"}); e || !strings.Contains(txt, "rad-280") {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "impl-tok", "list", map[string]any{"project": "elsewhere"}); !e || !strings.HasPrefix(txt, "FORBIDDEN") {
		t.Fatal(txt)
	}
	_, out := rpc(t, s, "pm-tok", "tools/call", map[string]any{"name": "bogus"})
	if out["error"] == nil {
		t.Fatal("unknown tool accepted")
	}
}

func raw(t *testing.T, s *httptest.Server, hdr map[string]string, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", s.URL, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
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

const okBody = `{"jsonrpc":"2.0","id":1,"method":"ping"}`

func TestBearerSchemeRequired(t *testing.T) {
	s := newSrv(t)
	for h, want := range map[string]int{"pm-tok": 401, "Token pm-tok": 401, "Bearer ": 401, "Bearer pm-tok": 200, "bearer pm-tok": 200} {
		if code, _ := raw(t, s, map[string]string{"Authorization": h}, okBody); code != want {
			t.Errorf("%q: %d want %d", h, code, want)
		}
	}
}

func TestJSONRPCConformance(t *testing.T) {
	s := newSrv(t)
	auth := map[string]string{"Authorization": "Bearer pm-tok"}
	for body, want := range map[string]float64{
		`[` + okBody + `]`:                         -32600,
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`: -32600,
		`{"id":1,"method":"ping"}`:                 -32600,
		`{not json`:                                -32700,
	} {
		_, out := raw(t, s, auth, body)
		e, _ := out["error"].(map[string]any)
		if e == nil || e["code"].(float64) != want {
			t.Errorf("%s: %v want %v", body, out, want)
		}
	}
	if code, _ := raw(t, s, auth, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); code != 202 {
		t.Errorf("notification: %d", code)
	}
}

func TestOriginCheck(t *testing.T) {
	s := newSrv(t)
	for o, want := range map[string]int{"https://evil.example": 403, "http://localhost:3000": 200, "http://127.0.0.1": 200, "": 200} {
		h := map[string]string{"Authorization": "Bearer pm-tok"}
		if o != "" {
			h["Origin"] = o
		}
		if code, _ := raw(t, s, h, okBody); code != want {
			t.Errorf("origin %q: %d want %d", o, code, want)
		}
	}
}

func TestM2Tools(t *testing.T) {
	s := newSrv(t)
	if isErr, txt := call(t, s, "pm-tok", "put", map[string]any{"project": "vc", "key": "plan", "title": "Rollout plan", "body": "ship botmemory"}); isErr {
		t.Fatal(txt)
	}
	call(t, s, "impl-tok", "append", map[string]any{"project": "vc", "key": "plan", "text": "phase two"})
	if isErr, txt := call(t, s, "impl-tok", "search", map[string]any{"project": "*", "query": "rollout botmemory"}); isErr || !strings.Contains(txt, `"key": "plan"`) && !strings.Contains(txt, `"key":"plan"`) {
		t.Fatalf("search: %s", txt)
	}
	isErr, txt := call(t, s, "impl-tok", "history", map[string]any{"project": "vc", "key": "plan"})
	var h []map[string]any
	if isErr || json.Unmarshal([]byte(txt), &h) != nil || len(h) != 2 || h[1]["updated_by"] != "pm" {
		t.Fatalf("history: %s", txt)
	}
	if isErr, txt := call(t, s, "impl-tok", "get", map[string]any{"project": "vc", "key": "plan", "version": 1}); isErr || strings.Contains(txt, "phase two") {
		t.Fatalf("get v1: %s", txt)
	}
	if isErr, txt := call(t, s, "impl-tok", "reindex", map[string]any{"project": "vc"}); !isErr || !strings.Contains(txt, "FORBIDDEN") {
		t.Fatalf("non-owner reindex: %s", txt)
	}
	if isErr, txt := call(t, s, "pm-tok", "reindex", map[string]any{"project": "vc"}); isErr || (!strings.Contains(txt, `"entries":1`) && !strings.Contains(txt, `"entries": 1`)) {
		t.Fatalf("reindex: %s", txt)
	}
}

// deadlineSpy records the deadline of the ctx each backend call receives.
type deadlineSpy struct {
	*store.Memory
	left time.Duration
}

func (d *deadlineSpy) ReadRev(ctx context.Context, p string) ([]byte, string, error) {
	if dl, ok := ctx.Deadline(); ok {
		d.left = time.Until(dl)
	} else {
		d.left = -1
	}
	return d.Memory.ReadRev(ctx, p)
}

// R2-4: every tool call runs under a CallTimeout (50s) deadline.
func TestToolCallDeadline(t *testing.T) {
	if CallTimeout <= 0 || CallTimeout >= 60*time.Second {
		t.Fatalf("CallTimeout %v must be below the 60s WriteTimeout", CallTimeout)
	}
	a, _ := acl.Parse([]byte(fmt.Sprintf(`{"bots":{"pm":{"token_sha256":%q,"roles":{"vc":"owner"}}}}`, acl.HashToken("pm-tok"))))
	f := func() *acl.ACL { return a }
	spy := &deadlineSpy{Memory: store.NewMemory()}
	s := httptest.NewServer(&Server{V: vault.New(spy, f), ACL: f})
	t.Cleanup(s.Close)
	call(t, s, "pm-tok", "get", map[string]any{"project": "vc", "key": "nope"})
	if spy.left <= 0 || spy.left > CallTimeout {
		t.Fatalf("backend ctx deadline left=%v, want (0, %v]", spy.left, CallTimeout)
	}
}

// R2-4/PM round 3: a hung Drive cannot hold a tool call past CallTimeout.
// The fake Drive never answers; the call must fail at the deadline.
func TestToolCallDeadlineSlowDrive(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); slow.Close() })
	old := CallTimeout
	CallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { CallTimeout = old })

	a, _ := acl.Parse([]byte(fmt.Sprintf(`{"bots":{"pm":{"token_sha256":%q,"roles":{"vc":"owner"}}}}`, acl.HashToken("pm-tok"))))
	f := func() *acl.ACL { return a }
	d := drive.New(slow.Client(), "root")
	d.Base = slow.URL
	s := httptest.NewServer(&Server{V: vault.New(d, f), ACL: f})
	t.Cleanup(s.Close)

	start := time.Now()
	isErr, txt := call(t, s, "pm-tok", "put", map[string]any{"project": "vc", "key": "k", "title": "t"})
	el := time.Since(start)
	if !isErr {
		t.Fatalf("put against hung Drive succeeded: %s", txt)
	}
	if el < CallTimeout || el > 5*time.Second {
		t.Fatalf("call took %v, want about CallTimeout (%v)", el, CallTimeout)
	}
	t.Logf("hung Drive: tool call failed after %v: %s", el.Round(time.Millisecond), txt)
}

func TestSinceDateFormsViaMCP(t *testing.T) {
	s := newSrv(t)
	put := map[string]any{"project": "vc", "key": "since-mcp", "kind": "note", "title": "T", "body": "hello world"}
	if e, txt := call(t, s, "pm-tok", "put", put); e {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "changed", map[string]any{"project": "vc", "since": "2026-11-01"}); e {
		t.Fatal(txt)
	} else if !strings.Contains(txt, `"changed"`) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "get", map[string]any{
		"project": "vc", "key": "since-mcp", "since": "2026-11-01T00:00:00Z",
	}); e || (!strings.Contains(txt, `"mode":"since"`) && !strings.Contains(txt, `"mode": "since"`)) {
		t.Fatal(txt)
	}
	if e, txt := call(t, s, "pm-tok", "get", map[string]any{
		"project": "vc", "key": "since-mcp", "max_bytes": 4,
	}); e || (!strings.Contains(txt, `"truncated":true`) && !strings.Contains(txt, `"truncated": true`)) {
		t.Fatal(txt)
	}
}
