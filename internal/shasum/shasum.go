// Package shasum provides the sha256 helpers shared by the installer, the
// Windows launcher and the packer so every digest is computed the same way.
package shasum

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// Hex returns the lowercase hex sha256 digest of b.
func Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// FileHex streams path and returns its lowercase hex sha256 digest.
func FileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
