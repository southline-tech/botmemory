//go:build windows

package local

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

func fileRev(fi os.FileInfo, path string) (string, error) {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())))
	return hex.EncodeToString(h[:16]), nil
}
