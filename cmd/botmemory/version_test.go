package main

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/mcp"
	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/vault"
)

func TestVersionNoNetwork(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "botmemory")
	root, err := filepath.Abs(filepath.Join(".", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/botmemory")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	out, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "botmemory") {
		t.Fatalf("%q", out)
	}
}

func TestPingCLI(t *testing.T) {
	a, _ := acl.Parse([]byte(`{"bots":{"b":{"token_sha256":"` + acl.HashToken("tok") + `","roles":{"p":"read"}}}}`))
	f := func() *acl.ACL { return a }
	srv := httptest.NewServer(&mcp.Server{V: vault.New(store.NewMemory(), f), ACL: f})
	t.Cleanup(srv.Close)
	t.Setenv("BOTMEMORY_TOKEN", "tok")
	t.Setenv("BOTMEMORY_URL", srv.URL)
	if err := runBotPing(nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrePingDefault(t *testing.T) {
	a, _ := acl.Parse([]byte(`{"bots":{"b":{"token_sha256":"` + acl.HashToken("tok") + `","roles":{"p":"read"}}}}`))
	f := func() *acl.ACL { return a }
	srv := httptest.NewServer(&mcp.Server{V: vault.New(store.NewMemory(), f), ACL: f})
	t.Cleanup(srv.Close)
	os.Unsetenv("BOTMEMORY_PREPING")
	prePingOnce = sync.Once{}
	c := &mcpClient{url: srv.URL, token: "tok", hc: srv.Client()}
	if _, err := c.callTool(context.Background(), "stats", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}
