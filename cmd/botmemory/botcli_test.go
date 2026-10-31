package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/mcp"
	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/vault"
)

func TestBotCLIProjects(t *testing.T) {
	doc := `{"bots":{"b":{"token_sha256":"` + acl.HashToken("tok") + `","roles":{"p":"read"},"default_project":"p"}}}`
	a, err := acl.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	f := func() *acl.ACL { return a }
	srv := httptest.NewServer(&mcp.Server{V: vault.New(store.NewMemory(), f), ACL: f})
	t.Cleanup(srv.Close)
	t.Setenv("BOTMEMORY_TOKEN", "tok")
	t.Setenv("BOTMEMORY_URL", srv.URL)
	c := &mcpClient{url: srv.URL, token: "tok", hc: srv.Client()}
	out, err := c.callTool(context.Background(), "list_projects", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"name"`) {
		t.Fatalf("%s", b)
	}
}
