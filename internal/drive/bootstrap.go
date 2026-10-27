package drive

import (
	"context"
	"errors"
	"net/http"

	"github.com/radiancelux/botvault/internal/store"
)

// RootFolderName is the app folder created in My Drive by Bootstrap.
const RootFolderName = "botVault"

// Bootstrap finds or creates the botVault root folder in My Drive and seeds
// an empty deny-all _acl.json if none exists. Under the drive.file scope the
// app only sees files it created, so an existing folder found here is ours;
// re-running is idempotent. Returns the root folder id
// (set it as BOTVAULT_DRIVE_ROOT_ID).
func Bootstrap(ctx context.Context, hc *http.Client, base string) (rootID string, seeded bool, err error) {
	top := New(hc, "root")
	if base != "" {
		top.Base = base
	}
	id, err := top.folderID(ctx, RootFolderName, true)
	if err != nil {
		return "", false, err
	}
	vb := New(hc, id)
	vb.Base = top.Base
	err = vb.WriteIf(ctx, "_acl.json", []byte("{\"bots\":{}}\n"), "")
	if errors.Is(err, store.ErrPrecondition) {
		return id, false, nil
	}
	return id, err == nil, err
}
