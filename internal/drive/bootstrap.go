package drive

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/southline-tech/botmemory/internal/store"
)

// RootFolderName is the app folder created in My Drive by Bootstrap.
const RootFolderName = "botMemory"

// TestRootFolderName is the throwaway folder for bootstrap --test-folder.
// It must never be used as the production vault.
const TestRootFolderName = "botMemory-test"

// LegacyRootFolderName is the pre-rename folder name ("bot" + "Vault").
// Bootstrap adopts an existing legacy folder so upgrades keep the same Drive
// root id; set BOTMEMORY_DRIVE_ROOT_ID to that id (or rename the folder in
// Drive to RootFolderName — the id does not change).
const LegacyRootFolderName = "bot" + "Vault"

// Bootstrap finds or creates the botMemory root folder in My Drive and seeds
// an empty deny-all _acl.json if none exists. Under the drive.file scope the
// app only sees files it created, so an existing folder found here is ours;
// re-running is idempotent. Preference order: an existing "botMemory" folder,
// else an existing legacy folder named botVault (adopted, not renamed), else
// create "botMemory". Returns the root folder id (set it as
// BOTMEMORY_DRIVE_ROOT_ID).
//
// When testFolder is true, only the throwaway TestRootFolderName folder is
// used or created; production RootFolderName / LegacyRootFolderName are never
// touched.
func Bootstrap(ctx context.Context, hc *http.Client, base string, testFolder bool) (rootID string, seeded bool, err error) {
	top := New(hc, "root")
	if base != "" {
		top.Base = base
	}
	if testFolder {
		log.Printf("botmemory: bootstrap test-folder mode — using throwaway folder %q only; do not use as production vault", TestRootFolderName)
		id, err := top.folderID(ctx, TestRootFolderName, false)
		if errors.Is(err, store.ErrNotFound) {
			id, err = top.folderID(ctx, TestRootFolderName, true)
		}
		if err != nil {
			return "", false, err
		}
		return seedACL(ctx, hc, id, top.Base)
	}
	id, err := top.folderID(ctx, RootFolderName, false)
	if errors.Is(err, store.ErrNotFound) {
		id, err = top.folderID(ctx, LegacyRootFolderName, false)
		if err == nil {
			log.Printf("botmemory: using legacy Drive folder %q (id unchanged); rename it to %q in Drive when convenient", LegacyRootFolderName, RootFolderName)
		} else if errors.Is(err, store.ErrNotFound) {
			id, err = top.folderID(ctx, RootFolderName, true)
		}
	}
	if err != nil {
		return "", false, err
	}
	return seedACL(ctx, hc, id, top.Base)
}

func seedACL(ctx context.Context, hc *http.Client, id, base string) (rootID string, seeded bool, err error) {
	vb := New(hc, id)
	vb.Base = base
	err = vb.WriteIf(ctx, "_acl.json", []byte("{\"bots\":{}}\n"), "")
	if errors.Is(err, store.ErrPrecondition) {
		return id, false, nil
	}
	return id, err == nil, err
}
