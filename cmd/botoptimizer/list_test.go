package main

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/southline-tech/botoptimizer/internal/acl"
	"github.com/southline-tech/botoptimizer/internal/mcp"
	"github.com/southline-tech/botoptimizer/internal/store"
	"github.com/southline-tech/botoptimizer/internal/vault"
)

func TestListIfEtagDefaultProject(t *testing.T) {
	doc := `{"bots":{"b":{"token_sha256":"` + acl.HashToken("tok") + `","roles":{"p":"write"},"default_project":"p"}}}`
	a, err := acl.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	f := func() *acl.ACL { return a }
	srv := httptest.NewServer(&mcp.Server{V: vault.New(store.NewMemory(), f), ACL: f})
	t.Cleanup(srv.Close)
	t.Setenv("BOTOPTIMIZER_TOKEN", "tok")
	t.Setenv("BOTOPTIMIZER_URL", srv.URL)
	t.Setenv("BOTOPTIMIZER_PROJECT", "p")
	cliFlags, _, err := parseBotFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := &mcpClient{url: srv.URL, token: "tok", hc: srv.Client()}
	ctx := context.Background()
	if _, err := c.callTool(ctx, "put", withProject(cliFlags, map[string]any{
		"key": "a", "kind": "note", "title": "A", "body": "b",
	})); err != nil {
		t.Fatal(err)
	}
	briefRes, err := c.callTool(ctx, "brief", withProject(cliFlags, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	etag, _ := briefRes.(map[string]any)["etag"].(string)
	if etag == "" {
		t.Fatalf("brief: %+v", briefRes)
	}
	out, err := dispatchBot(ctx, c, cliFlags, "list", []string{"--if-etag", etag})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["unchanged"] != true {
		t.Fatalf("%+v", out)
	}
}
