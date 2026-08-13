// Tar.gz extraction for the Linux installer, rejecting path-traversal entries.

package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractTgz unpacks a verified .tgz into destDir, rejecting path-traversal
// entries (e.g. "../evil") and silently skipping non-regular entries. Used by
// the Linux installer so a compromised download cannot write outside the
// extraction directory.
func extractTgz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	base := filepath.Clean(destDir)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Join(base, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(name), base) {
			return fmt.Errorf("archive entry escapes target directory: %s", hdr.Name)
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(name, 0755); err != nil {
				return err
			}
			continue
		}
		if hdr.Typeflag == tar.TypeReg {
			if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
}
