package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/southline-tech/botoptimizer/internal/acl"
	"github.com/southline-tech/botoptimizer/internal/mcp"
	"github.com/southline-tech/botoptimizer/internal/store"
	"github.com/southline-tech/botoptimizer/internal/vault"
)

func TestStdioRoundtrip(t *testing.T) {
	a, err := acl.Parse([]byte(`{"bots":{"b":{"token_sha256":"` + acl.HashToken("stdio-tok") + `","roles":{"p":"write"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	f := func() *acl.ACL { return a }
	s := &mcp.Server{V: vault.New(store.NewMemory(), f), ACL: f, Version: "test"}
	t.Setenv("BOTOPTIMIZER_TOKEN", "stdio-tok")
	ctx := context.Background()
	res := s.CallTool(ctx, "b", "put", json.RawMessage(`{"project":"p","key":"k","title":"t","body":"b"}`))
	if res["isError"] == true {
		t.Fatalf("%v", res)
	}
	get := s.CallTool(ctx, "b", "get", json.RawMessage(`{"project":"p","key":"k"}`))
	if get["isError"] == true {
		t.Fatalf("%v", get)
	}
	_ = bytes.Buffer{}
}
