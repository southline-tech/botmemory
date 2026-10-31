//go:build !windows

package local

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"syscall"
)

func fileRev(fi os.FileInfo, path string) (string, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	ino := uint64(0)
	if ok {
		ino = st.Ino
	}
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d", fi.Size(), fi.ModTime().UnixNano(), ino)))
	return hex.EncodeToString(h[:16]), nil
}
