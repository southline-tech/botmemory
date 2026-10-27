// Command botvault runs the botVault MCP server.
//
//	botvault serve          # serve MCP over HTTP (see .env.example)
//	botvault mint-token BOT # print a new token + the hash to put in _acl.json
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/drive"
	"github.com/radiancelux/botvault/internal/mcp"
	"github.com/radiancelux/botvault/internal/store"
	"github.com/radiancelux/botvault/internal/vault"
)

const driveScope = "https://www.googleapis.com/auth/drive.file"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: botvault serve | mint-token BOT_ID")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "mint-token":
		if len(os.Args) != 3 {
			log.Fatal("usage: botvault mint-token BOT_ID")
		}
		tok, hash, err := acl.NewToken()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("bot:          %s\ntoken:        %s   (give to the bot; never commit)\ntoken_sha256: %s   (put in _acl.json)\n", os.Args[2], tok, hash)
	case "serve":
		if err := serve(); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func backend(ctx context.Context) (store.Backend, error) {
	switch os.Getenv("BOTVAULT_BACKEND") {
	case "memory":
		log.Print("WARNING: memory backend; data is lost on exit")
		return store.NewMemory(), nil
	case "", "drive":
	default:
		return nil, errors.New("BOTVAULT_BACKEND must be drive or memory")
	}
	root := os.Getenv("BOTVAULT_DRIVE_ROOT_ID")
	if root == "" {
		return nil, errors.New("BOTVAULT_DRIVE_ROOT_ID is required for the drive backend")
	}
	var ts oauth2.TokenSource
	if rt := os.Getenv("BOTVAULT_OAUTH_REFRESH_TOKEN"); rt != "" {
		cfg := &oauth2.Config{ClientID: os.Getenv("BOTVAULT_OAUTH_CLIENT_ID"), ClientSecret: os.Getenv("BOTVAULT_OAUTH_CLIENT_SECRET"),
			Endpoint: google.Endpoint, Scopes: []string{driveScope}}
		ts = cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: rt})
	} else {
		creds, err := google.FindDefaultCredentials(ctx, driveScope)
		if err != nil {
			return nil, fmt.Errorf("no Drive credentials (set BOTVAULT_OAUTH_* or ADC): %w", err)
		}
		ts = creds.TokenSource
	}
	return drive.New(oauth2.NewClient(ctx, ts), root), nil
}

func serve() error {
	ctx := context.Background()
	b, err := backend(ctx)
	if err != nil {
		return err
	}
	// ACL: _acl.json in the vault root (Drive), or BOTVAULT_ACL_FILE locally.
	var cur atomic.Pointer[acl.ACL]
	load := func() error {
		var data []byte
		var err error
		if f := os.Getenv("BOTVAULT_ACL_FILE"); f != "" {
			data, err = os.ReadFile(f)
		} else {
			data, err = b.Read(ctx, "_acl.json")
		}
		if err != nil {
			return fmt.Errorf("load ACL: %w", err)
		}
		a, err := acl.Parse(data)
		if err != nil {
			return err
		}
		cur.Store(a)
		return nil
	}
	if err := load(); err != nil {
		return err
	}
	go func() { // pick up revocations without restart
		for range time.Tick(60 * time.Second) {
			if err := load(); err != nil {
				log.Printf("ACL reload failed (keeping previous): %v", err)
			}
		}
	}()
	aclFn := func() *acl.ACL { return cur.Load() }
	addr := os.Getenv("BOTVAULT_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", &mcp.Server{V: vault.New(b, aclFn), ACL: aclFn})
	log.Printf("botvault listening on http://%s/mcp", addr)
	return (&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe()
}
