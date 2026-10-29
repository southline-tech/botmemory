// Command botmemory runs the botMemory MCP server.
//
//	botmemory serve          # serve MCP over HTTP (see .env.example)
//	botmemory bootstrap      # create the botMemory root folder in Drive, print its id
//	botmemory mint-token BOT # print a new token + the hash to put in _acl.json
//	botmemory acl grant|revoke|list  # edit _acl.json (see aclcmd.go)
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/southline-tech/botmemory/internal/acl"
	"github.com/southline-tech/botmemory/internal/drive"
	"github.com/southline-tech/botmemory/internal/instance"
	"github.com/southline-tech/botmemory/internal/mcp"
	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/vault"
)

const driveScope = "https://www.googleapis.com/auth/drive.file"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: botmemory serve | bootstrap | mint-token BOT_ID | acl grant|revoke|list")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "mint-token":
		if len(os.Args) != 3 {
			log.Fatal("usage: botmemory mint-token BOT_ID")
		}
		tok, hash, err := acl.NewToken()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("bot:          %s\ntoken:        %s   (give to the bot; never commit)\ntoken_sha256: %s   (put in _acl.json)\n", os.Args[2], tok, hash)
	case "acl":
		if err := aclMain(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "bootstrap":
		if err := bootstrap(context.Background()); err != nil {
			log.Fatal(err)
		}
	case "serve":
		if err := serve(); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

func backend(ctx context.Context) (store.Backend, error) {
	switch env("BACKEND") {
	case "memory":
		log.Print("WARNING: memory backend; data is lost on exit")
		return store.NewMemory(), nil
	case "", "drive":
	default:
		return nil, errors.New("BOTMEMORY_BACKEND must be drive or memory")
	}
	root := env("DRIVE_ROOT_ID")
	if root == "" {
		return nil, errors.New("BOTMEMORY_DRIVE_ROOT_ID is required for the drive backend (run `botmemory bootstrap` to create one)")
	}
	hc, err := driveClient(ctx)
	if err != nil {
		return nil, err
	}
	return drive.New(hc, root), nil
}

// bootstrap creates (or finds) the app's root folder under drive.file and
// prints its id. If BOTMEMORY_DRIVE_ROOT_ID is already set it does nothing.
func bootstrap(ctx context.Context) error {
	if id := env("DRIVE_ROOT_ID"); id != "" {
		fmt.Printf("BOTMEMORY_DRIVE_ROOT_ID already set (%s); nothing to do\n", id)
		return nil
	}
	hc, err := driveClient(ctx)
	if err != nil {
		return err
	}
	id, seeded, err := drive.Bootstrap(ctx, hc, "")
	if err != nil {
		return err
	}
	fmt.Printf("BOTMEMORY_DRIVE_ROOT_ID=%s\n", id)
	if seeded {
		fmt.Println("seeded empty _acl.json (deny all); add bots with `botmemory mint-token`")
	}
	return nil
}

func driveClient(ctx context.Context) (*http.Client, error) {
	var ts oauth2.TokenSource
	if rt := env("OAUTH_REFRESH_TOKEN"); rt != "" {
		cfg := &oauth2.Config{ClientID: env("OAUTH_CLIENT_ID"), ClientSecret: env("OAUTH_CLIENT_SECRET"),
			Endpoint: google.Endpoint, Scopes: []string{driveScope}}
		ts = cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: rt})
	} else {
		creds, err := google.FindDefaultCredentials(ctx, driveScope)
		if err != nil {
			return nil, fmt.Errorf("no Drive credentials (set BOTMEMORY_OAUTH_* or ADC): %w", err)
		}
		ts = creds.TokenSource
	}
	return oauth2.NewClient(ctx, ts), nil
}

// checkAddr refuses any bind address that is not loopback (spec: local only).
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("BOTMEMORY_ADDR %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("BOTMEMORY_ADDR %q: refusing non-loopback bind (use 127.0.0.1)", addr)
	}
	return nil
}

// normalizeAddr binds the literal "localhost" as 127.0.0.1 (never resolved).
func normalizeAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err == nil && strings.EqualFold(host, "localhost") {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

// lockPath is fixed per vault, not per environment: every instance serving
// the same Drive root (or memory) contends on the same file regardless of
// TMPDIR/XDG_RUNTIME_DIR. BOTMEMORY_LOCK_FILE overrides.
// The guard holds for one host only; it cannot see a server on another host.
func lockPath(rootID string) string {
	if p := env("LOCK_FILE"); p != "" {
		return p
	}
	return "/var/tmp/botmemory-" + rootHash(rootID) + ".lock"
}

func rootHash(rootID string) string {
	if rootID == "" {
		rootID = "memory"
	}
	sum := sha256.Sum256([]byte(rootID))
	return hex.EncodeToString(sum[:])[:12]
}

func serve() error {
	ctx := context.Background()
	addr := env("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	addr = normalizeAddr(addr)
	if err := checkAddr(addr); err != nil {
		return err
	}
	root := env("DRIVE_ROOT_ID")
	if env("BACKEND") == "memory" {
		root = ""
	}
	lp := lockPath(root)
	log.Printf("botmemory single-instance lock: %s", lp)
	lk, err := instance.Acquire(lp)
	if err != nil {
		return err
	}
	defer lk.Release()
	b, err := backend(ctx)
	if err != nil {
		return err
	}
	// ACL: _acl.json in the vault root (Drive), or BOTMEMORY_ACL_FILE locally.
	// Reloaded every 60s; fails closed after 5 consecutive failures.
	loader := &acl.Loader{MaxFailures: 5, Fetch: func() ([]byte, error) {
		if f := env("ACL_FILE"); f != "" {
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
	log.Printf("botmemory listening on http://%s/mcp", addr)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	return srv.ListenAndServe()
}
