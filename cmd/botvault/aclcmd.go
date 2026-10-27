package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/radiancelux/botvault/internal/acladmin"
	"github.com/radiancelux/botvault/internal/instance"
	"github.com/radiancelux/botvault/internal/store"
)

const aclUsage = `usage:
  botvault acl grant <bot> <project> <role> --token-out FILE [--rotate]
  botvault acl revoke <bot> [--force]
  botvault acl list
roles: read | write | owner. A new bot (or --rotate) gets a fresh token written
ONLY to FILE (mode 0600, must not exist); only its hash goes into _acl.json.
revoke disables the bot's token for EVERY project; it refuses to remove the
last active owner of any project unless --force.`

// runACL implements `botvault acl ...` against b. It never prints a token
// or a hash.
func runACL(ctx context.Context, b store.Backend, args []string, out io.Writer) error {
	var pos []string
	tokenOut, rotate, force := "", false, false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--rotate":
			rotate = true
		case a == "--force":
			force = true
		case a == "--token-out":
			if i+1 >= len(args) {
				return errors.New("--token-out needs a file path")
			}
			i++
			tokenOut = args[i]
		case strings.HasPrefix(a, "--token-out="):
			tokenOut = strings.TrimPrefix(a, "--token-out=")
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q\n%s", a, aclUsage)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		return errors.New(aclUsage)
	}
	ad := acladmin.New(b)
	switch pos[0] {
	case "grant":
		if force {
			return errors.New("--force applies only to revoke")
		}
		if len(pos) != 4 {
			return errors.New(aclUsage)
		}
		r, err := ad.Grant(ctx, pos[1], pos[2], pos[3], acladmin.GrantOpts{TokenOut: tokenOut, Rotate: rotate})
		if err != nil {
			return err
		}
		if r.Minted {
			fmt.Fprintf(out, "granted %s %s on %s; new token written to %s (mode 0600). Hand it over out of band; never paste it in chat.\n", pos[1], pos[3], pos[2], tokenOut)
		} else {
			fmt.Fprintf(out, "granted %s %s on %s (existing token unchanged)\n", pos[1], pos[3], pos[2])
		}
		fmt.Fprintln(out, "the server picks this up at its next ACL reload (within 60s)")
	case "revoke":
		if rotate || tokenOut != "" {
			return errors.New("revoke takes only --force")
		}
		if len(pos) != 2 {
			return errors.New(aclUsage)
		}
		if err := ad.Revoke(ctx, pos[1], force); err != nil {
			return err
		}
		fmt.Fprintf(out, "revoked %s for all projects; its token stops working at the next ACL reload (within 60s)\n", pos[1])
	case "list":
		if len(pos) != 1 {
			return errors.New(aclUsage)
		}
		es, err := ad.List(ctx)
		if err != nil {
			return err
		}
		if len(es) == 0 {
			fmt.Fprintln(out, "(no bots)")
		}
		for _, e := range es {
			ps := make([]string, 0, len(e.Roles))
			for p, r := range e.Roles {
				ps = append(ps, p+"="+r)
			}
			sort.Strings(ps)
			st := "active"
			if e.Revoked {
				st = "REVOKED"
			}
			fmt.Fprintf(out, "%-28s %-8s %s\n", e.Bot, st, strings.Join(ps, " "))
		}
	default:
		return errors.New(aclUsage)
	}
	return nil
}

// aclLockPath serialises `botvault acl` runs against one vault on this host
// (BOTVAULT_ACL_LOCK_FILE overrides). It is separate from the server's
// instance lock: the server only ever reads _acl.json, so it never needs it.
// Like the instance lock it covers ONE HOST only; two hosts editing the same
// ACL rely on the Drive version check plus the re-read verification.
func aclLockPath(rootID string) string {
	if p := os.Getenv("BOTVAULT_ACL_LOCK_FILE"); p != "" {
		return p
	}
	return "/var/tmp/botvault-acl-" + rootHash(rootID) + ".lock"
}

// runACLLocked holds the exclusive (non-blocking, O_NOFOLLOW) ACL lock for
// the whole command; a concurrent run is refused, not queued.
func runACLLocked(ctx context.Context, b store.Backend, rootID string, args []string, out io.Writer) error {
	lp := aclLockPath(rootID)
	l, err := instance.Acquire(lp)
	if err != nil {
		if errors.Is(err, instance.ErrRunning) {
			return fmt.Errorf("another `botvault acl` run holds %s; wait for it and retry", lp)
		}
		return err
	}
	defer l.Release()
	return runACL(ctx, b, args, out)
}

func aclMain(args []string) error {
	if os.Getenv("BOTVAULT_ACL_FILE") != "" {
		return errors.New("BOTVAULT_ACL_FILE is set: `botvault acl` edits _acl.json in the vault backend, not a local file; unset it or edit the local file by hand")
	}
	ctx := context.Background()
	b, err := backend(ctx)
	if err != nil {
		return err
	}
	root := os.Getenv("BOTVAULT_DRIVE_ROOT_ID")
	if os.Getenv("BOTVAULT_BACKEND") == "memory" {
		root = ""
	}
	return runACLLocked(ctx, b, root, args, os.Stdout)
}
