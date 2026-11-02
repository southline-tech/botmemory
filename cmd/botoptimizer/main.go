// Command botoptimizer runs the botMemory MCP server.
//
//	botoptimizer serve          # serve MCP over HTTP (see .env.example)
//	botoptimizer bootstrap      # create the botMemory root folder in Drive, print its id
//	botoptimizer mint-token BOT # print a new token + the hash to put in _acl.json
//	botoptimizer acl grant|revoke|list  # edit _acl.json (see aclcmd.go)
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

	"github.com/southline-tech/botoptimizer/internal/acl"
	"github.com/southline-tech/botoptimizer/internal/drive"
	"github.com/southline-tech/botoptimizer/internal/instance"
	"github.com/southline-tech/botoptimizer/internal/mcp"
	"github.com/southline-tech/botoptimizer/internal/store"
	"github.com/southline-tech/botoptimizer/internal/vault"
)

const driveScope = "https://www.googleapis.com/auth/drive.file"

func main() {
	if maybePrintHelp(os.Args) {
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("botoptimizer %s\n", Version)
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: botoptimizer serve | stdio | migrate | projects|list|get|… | bootstrap | mint-token | acl …")
		os.Exit(2)
	}
	cmd := os.Args[1]
	if isBotCommand(cmd) {
		if err := runBotCommand(cmd, os.Args[2:]); err != nil {
			if errors.Is(err, errBotUsage) {
				os.Exit(2)
			}
		}
		return
	}
	switch cmd {
	case "usage":
		if err := runUsageCommand(os.Args[2:]); err != nil {
			if errors.Is(err, errBotUsage) {
				os.Exit(2)
			}
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "watch":
		if err := runWatchCommand(os.Args[2:]); err != nil {
			if errors.Is(err, errBotUsage) {
				os.Exit(2)
			}
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "import-usage":
		if err := runImportUsageCommand(os.Args[2:]); err != nil {
			if errors.Is(err, errBotUsage) {
				os.Exit(2)
			}
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "mint-token":
		if len(os.Args) != 3 {
			log.Fatal("usage: botoptimizer mint-token BOT_ID")
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
		if err := bootstrap(context.Background(), bootstrapTestFolder(os.Args[2:])); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Printf("botoptimizer %s\n", Version)
	case "ping":
		if err := runBotPing(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "migrate":
		if err := migrateMain(os.Args[2:]); err != nil {
			if errors.Is(err, errUsage) {
				os.Exit(2)
			}
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	case "serve":
		storage, dataDir, warmEvery := "", "", time.Duration(0)
		for i := 2; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--storage":
				i++
				if i < len(os.Args) {
					storage = os.Args[i]
				}
			case "--data-dir":
				i++
				if i < len(os.Args) {
					dataDir = os.Args[i]
				}
			case "--warm-interval":
				i++
				if i < len(os.Args) {
					d, err := time.ParseDuration(os.Args[i])
					if err != nil {
						log.Fatal(err)
					}
					warmEvery = d
				}
			}
		}
		if warmEvery == 0 {
			if s := strings.TrimSpace(env("WARM_INTERVAL")); s != "" {
				d, err := time.ParseDuration(s)
				if err != nil {
					log.Fatalf("BOTOPTIMIZER_WARM_INTERVAL: %v", err)
				}
				warmEvery = d
			}
		}
		if err := runServer(false, storage, dataDir, warmEvery); err != nil {
			log.Fatal(err)
		}
	case "stdio":
		if err := runServer(true, "", "", 0); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown command %q", cmd)
	}
}

func runServer(stdio bool, storageFlag, dataDirFlag string, warmEvery time.Duration) error {
	ctx := context.Background()
	b, rootKey, err := resolveBackend(ctx, storageFlag, dataDirFlag)
	if err != nil {
		return err
	}
	if env("ACL_FILE") == "" && env("DRIVE_ROOT_ID") == "" && strings.ToLower(env("BACKEND")) != "drive" {
		if err := seedLocalACL(ctx, b); err != nil && !errors.Is(err, store.ErrPrecondition) {
			return fmt.Errorf("seed ACL: %w", err)
		}
	}
	loader := &acl.Loader{MaxFailures: 5, Fetch: func() ([]byte, error) {
		return aclBytes(ctx, b)
	}}
	if err := loader.Load(); err != nil {
		return fmt.Errorf("load ACL: %w", err)
	}
	go loader.Run(60*time.Second, nil)
	aclFn := loader.Current
	v := vault.New(b, aclFn)
	warmStorage(ctx, b, aclFn)
	if warmEvery > 0 {
		go func() {
			t := time.NewTicker(warmEvery)
			defer t.Stop()
			for range t.C {
				warmStorage(context.Background(), b, aclFn)
			}
		}()
	}
	srv := &mcp.Server{V: v, ACL: aclFn, Version: Version}
	if stdio {
		return runStdio(srv)
	}
	addr := env("ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	addr = normalizeAddr(addr)
	if err := checkAddr(addr); err != nil {
		return err
	}
	lp := lockPath(rootKey)
	log.Printf("botoptimizer single-instance lock: %s", lp)
	lk, err := instance.Acquire(lp)
	if err != nil {
		return err
	}
	defer lk.Release()
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv)
	log.Printf("botoptimizer listening on http://%s/mcp", addr)
	hs := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	return hs.ListenAndServe()
}

// bootstrap creates (or finds) the app's root folder under drive.file and
// prints its id. If BOTOPTIMIZER_DRIVE_ROOT_ID is already set it does nothing.
func bootstrap(ctx context.Context, testFolder bool) error {
	if id := env("DRIVE_ROOT_ID"); id != "" {
		fmt.Printf("BOTOPTIMIZER_DRIVE_ROOT_ID already set (%s); nothing to do\n", id)
		return nil
	}
	hc, err := driveClient(ctx)
	if err != nil {
		return err
	}
	id, seeded, err := drive.Bootstrap(ctx, hc, "", testFolder)
	if err != nil {
		return err
	}
	fmt.Printf("BOTOPTIMIZER_DRIVE_ROOT_ID=%s\n", id)
	if testFolder {
		fmt.Println("test-folder bootstrap: save this id for smoke/tests only; set BOTOPTIMIZER_SMOKE_TEST_ROOT=1 when using scripts/smoke_drive.py against it")
	}
	if seeded {
		fmt.Println("seeded empty _acl.json (deny all); add bots with `botoptimizer mint-token`")
	}
	return nil
}

// bootstrapTestFolder is true when --test-folder is passed (wins) or
// BOTOPTIMIZER_TEST_FOLDER is a truthy value (1/true/yes, case-insensitive).
func bootstrapTestFolder(args []string) bool {
	for _, a := range args {
		if a == "--test-folder" {
			return true
		}
	}
	v := strings.ToLower(strings.TrimSpace(env("TEST_FOLDER")))
	return v == "1" || v == "true" || v == "yes"
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
			return nil, fmt.Errorf("no Drive credentials (set BOTOPTIMIZER_OAUTH_* or ADC): %w", err)
		}
		ts = creds.TokenSource
	}
	return oauth2.NewClient(ctx, ts), nil
}

// checkAddr refuses any bind address that is not loopback (spec: local only).
func checkAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("BOTOPTIMIZER_ADDR %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("BOTOPTIMIZER_ADDR %q: refusing non-loopback bind (use 127.0.0.1)", addr)
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
// TMPDIR/XDG_RUNTIME_DIR. BOTOPTIMIZER_LOCK_FILE overrides.
// The guard holds for one host only; it cannot see a server on another host.
func lockPath(rootID string) string {
	if p := env("LOCK_FILE"); p != "" {
		return p
	}
	return "/var/tmp/botoptimizer-" + rootHash(rootID) + ".lock"
}

func rootHash(rootID string) string {
	if rootID == "" {
		rootID = "memory"
	}
	sum := sha256.Sum256([]byte(rootID))
	return hex.EncodeToString(sum[:])[:12]
}
