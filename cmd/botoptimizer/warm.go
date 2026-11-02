package main

import (
	"context"
	"path"

	"github.com/southline-tech/botoptimizer/internal/acl"
	"github.com/southline-tech/botoptimizer/internal/store"
)

func warmStorage(ctx context.Context, b store.Backend, aclFn func() *acl.ACL) {
	a := aclFn()
	if a == nil {
		return
	}
	for _, p := range a.ProjectNames() {
		dir := path.Join("projects", p)
		_, _ = b.List(ctx, dir)
		_, _ = b.Read(ctx, path.Join(dir, "_index.json"))
	}
}
