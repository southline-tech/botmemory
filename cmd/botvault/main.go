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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/radiancelux/botvault/internal/acl"
	"github.com/radiancelux/botvault/internal/drive"
	"github.com/radiancelux/botvault/internal/instance"
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

// checkAddr refuses any bind address that is not loopback (spec: local only).
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("BOTVAULT_ADDR %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("BOTVAULT_ADDR %q: refusing non-loopback bind (use 127.0.0.1)", addr)
	}
	return nil
}

func lockPath() string {
	if p := os.Getenv("BOTVAULT_LOCK_FILE"); p != "" {
		return p
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "botvault.lock")
	}
	return filepath.Join(os.TempDir(), "botvault.lock")
}

func serve() error {
	ctx := context.Background()
	addr := os.Getenv("BOTVAULT_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	if err := checkAddr(addr); err != nil {
		return err
	}
	lk, err := instance.Acquire(lockPath())
	if err != nil {
		return err
	}
	defer lk.Release()
	b, err := backend(ctx)
	if err != nil {
		return err
	}
	// ACL: _acl.json in the vault root (Drive), or BOTVAULT_ACL_FILE locally.
	// Reloaded every 60s; fails closed after 5 consecutive failures.
	loader := &acl.Loader{MaxFailures: 5, Fetch: func() ([]byte, error) {
		if f := os.Getenv("BOTVAULT_ACL_FILE"); f != "" {
			return os.ReadFile(f)
		}
		return b.Read(ctx, "_acl.json")
	}}
	if err := loader.Load(); err != nil {
		return fmt.Errorf("load ACL: %w", err)
	}
	go loader.Run(60*time.Second, nil)
	aclFn := loader.Current
	mux := http.NewServeMux()
	mux.Handle("/mcp", &mcp.Server{V: vault.New(b, aclFn), ACL: aclFn})
	log.Printf("botvault listening on http://%s/mcp", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	return srv.ListenAndServe()
}
