package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/southline-tech/botoptimizer/internal/drive"
	"github.com/southline-tech/botoptimizer/internal/store"
	"github.com/southline-tech/botoptimizer/internal/store/local"
)

const migrateUsage = `usage:
  botoptimizer migrate --from local --to drive [--dry-run] [--force] [--delete-source]
Copies vault files from local disk to Google Drive. Drive→local is not supported.
Refuses a nonempty Drive vault unless --force. Source is kept unless --delete-source.`

func migrateMain(args []string) error {
	from, to := "", ""
	dryRun, force, deleteSrc := false, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from":
			i++
			if i >= len(args) {
				return usageErr(migrateUsage)
			}
			from = args[i]
		case "--to":
			i++
			if i >= len(args) {
				return usageErr(migrateUsage)
			}
			to = args[i]
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		case "--delete-source":
			deleteSrc = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, migrateUsage)
			return nil
		default:
			return fmt.Errorf("unknown flag %q\n%s", args[i], migrateUsage)
		}
	}
	if from != "local" || to != "drive" {
		return usageErr(migrateUsage)
	}
	ctx := context.Background()
	srcRoot := env("DATA_DIR")
	if srcRoot == "" {
		srcRoot = store.DefaultDataDir()
	}
	src, err := local.New(srcRoot)
	if err != nil {
		return err
	}
	paths, err := collectStorePaths(ctx, src, "")
	if err != nil {
		return err
	}
	if dryRun {
		var n int
		for _, p := range paths {
			data, err := src.Read(ctx, p)
			if err != nil {
				return fmt.Errorf("read %s: %w", p, err)
			}
			fmt.Fprintf(os.Stdout, "would copy %s (%d bytes)\n", p, len(data))
			n++
		}
		rootID := env("DRIVE_ROOT_ID")
		if rootID != "" {
			fmt.Fprintf(os.Stdout, "dry-run: %d files (destination BOTOPTIMIZER_DRIVE_ROOT_ID=%s)\n", n, rootID)
		} else {
			fmt.Fprintf(os.Stdout, "dry-run: %d files (set BOTOPTIMIZER_DRIVE_ROOT_ID before real migrate)\n", n)
		}
		return nil
	}
	rootID := env("DRIVE_ROOT_ID")
	if rootID == "" {
		return fmt.Errorf("INVALID: BOTOPTIMIZER_DRIVE_ROOT_ID required for migrate (not dry-run)")
	}
	hc, err := driveClient(ctx)
	if err != nil {
		return err
	}
	dst := drive.New(hc, rootID)
	if err := destNonempty(ctx, dst); err != nil {
		if !force {
			return err
		}
	}
	var copied int
	for _, p := range paths {
		data, err := src.Read(ctx, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		if _, err := dst.Read(ctx, p); err == nil && !force {
			return fmt.Errorf("CONFLICT: %s exists on drive; use --force", p)
		}
		if err := dst.Write(ctx, p, data); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
		copied++
	}
	fmt.Fprintf(os.Stdout, "migrated %d files local→drive\n", copied)
	if deleteSrc {
		for _, p := range paths {
			_ = src.Delete(ctx, p)
		}
		fmt.Fprintln(os.Stdout, "deleted source files (local tree may retain empty dirs)")
	}
	return nil
}

func usageErr(msg string) error {
	fmt.Fprintln(os.Stderr, msg)
	return errUsage
}

var errUsage = errors.New("usage")

func destNonempty(ctx context.Context, dst store.Backend) error {
	if _, err := dst.Read(ctx, "_acl.json"); err == nil {
		return fmt.Errorf("CONFLICT: destination already has _acl.json; use --force")
	}
	names, _ := dst.List(ctx, "projects")
	for _, n := range names {
		if strings.HasSuffix(n, "/") {
			return fmt.Errorf("CONFLICT: destination projects/ is not empty; use --force")
		}
	}
	return nil
}

func collectStorePaths(ctx context.Context, b store.Backend, dir string) ([]string, error) {
	names, err := b.List(ctx, dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		full := n
		if dir != "" {
			full = path.Join(dir, n)
		}
		if strings.HasSuffix(n, "/") {
			sub, err := collectStorePaths(ctx, b, strings.TrimSuffix(full, "/"))
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		out = append(out, full)
	}
	return out, nil
}
