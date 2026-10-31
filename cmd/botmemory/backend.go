package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/southline-tech/botmemory/internal/drive"
	"github.com/southline-tech/botmemory/internal/store"
	"github.com/southline-tech/botmemory/internal/store/local"
)

// resolveBackend picks the storage backend from flags/env (local default).
func resolveBackend(ctx context.Context, storageFlag, dataDirFlag string) (store.Backend, string, error) {
	name := strings.ToLower(strings.TrimSpace(storageFlag))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(env("BACKEND")))
	}
	if name == "gdrive" {
		name = "drive"
	}
	if name == "local" || name == "" {
		if name == "" && env("DRIVE_ROOT_ID") != "" {
			log.Print("botmemory: BOTMEMORY_DRIVE_ROOT_ID is set but BOTMEMORY_BACKEND is empty; using drive backend (set BOTMEMORY_BACKEND=local for local-only)")
			name = "drive"
		}
	}
	switch name {
	case "", "local":
		root := dataDirFlag
		if root == "" {
			root = env("DATA_DIR")
		}
		if root == "" {
			root = store.DefaultDataDir()
		}
		b, err := local.New(root)
		if err != nil {
			return nil, "", err
		}
		return b, root, nil
	case "memory":
		log.Print("WARNING: memory backend; data is lost on exit")
		return store.NewMemory(), "memory", nil
	case "drive":
		rootID := env("DRIVE_ROOT_ID")
		if rootID == "" {
			return nil, "", errors.New("BOTMEMORY_DRIVE_ROOT_ID is required for the drive backend")
		}
		hc, err := driveClient(ctx)
		if err != nil {
			return nil, "", err
		}
		return drive.New(hc, rootID), rootID, nil
	default:
		return nil, "", fmt.Errorf("BOTMEMORY_BACKEND %q must be local, drive, or memory", name)
	}
}

func seedLocalACL(ctx context.Context, b store.Backend) error {
	_, err := b.Read(ctx, "_acl.json")
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return b.WriteIf(ctx, "_acl.json", []byte("{\"bots\":{}}\n"), "")
}

func aclBytes(ctx context.Context, b store.Backend) ([]byte, error) {
	if f := env("ACL_FILE"); f != "" {
		return os.ReadFile(f)
	}
	return b.Read(ctx, "_acl.json")
}

// backend opens the configured vault backend (for acl subcommands).
func backend(ctx context.Context) (store.Backend, error) {
	b, _, err := resolveBackend(ctx, "", "")
	return b, err
}
