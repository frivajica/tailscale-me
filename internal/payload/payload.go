// Package payload is the single source of truth for the self-extracting
// format shared by the Windows universal launcher (reader) and the packer
// tool (writer). Keeping both sides here guarantees they can never drift
// apart, and lets the round-trip be tested on any CI host.
//
// Format appended to the launcher exe:
//
//	<launcher exe bytes> + "__TSME_PAYLOAD_MARKER__" + "\n" + <gzipped tar>
package payload

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Marker anchors the appended payload. The launcher finds the LAST occurrence
// (the exe itself also contains the literal, see Start) and reads everything
// after it.
const Marker = "__TSME_PAYLOAD_MARKER__"

// Member is one installer to place in the payload archive.
type Member struct {
	Name string
	Data []byte
}

// Append builds the self-extracting image: exe bytes, marker, then a gzipped
// tar of every member. Members are sorted by name so the output is
// deterministic and reproducible across runs.
func Append(exe []byte, members []Member) ([]byte, error) {
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	tw := tar.NewWriter(gz)

	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	for _, m := range members {
		if err := tw.WriteHeader(&tar.Header{Name: m.Name, Mode: 0755, Size: int64(len(m.Data))}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(m.Data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	out := make([]byte, 0, len(exe)+len(Marker)+1+gzBuf.Len())
	out = append(out, exe...)
	out = append(out, []byte(Marker+"\n")...)
	return append(out, gzBuf.Bytes()...), nil
}

// Start returns the byte offset where the gzipped payload begins. It uses the
// last occurrence of the marker because the launcher binary itself contains
// the marker literal (see Marker); the appended one is always last. Leading
// CR/LF after the marker are skipped so batch and shell output agree.
func Start(data []byte) (int, error) {
	i := bytes.LastIndex(data, []byte(Marker))
	if i < 0 {
		return 0, fmt.Errorf("payload marker %q not found", Marker)
	}
	start := i + len(Marker)
	for start < len(data) && (data[start] == '\r' || data[start] == '\n') {
		start++
	}
	if start >= len(data) {
		return 0, fmt.Errorf("payload marker %q has nothing after it", Marker)
	}
	return start, nil
}

// Extract returns the bytes of the named member, rejecting archives whose
// entries escape the archive or that lack the wanted member.
func Extract(payload []byte, name string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("payload is not a valid gzip stream: %w", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("payload has no member %q", name)
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Clean(hdr.Name) != name {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		return data, nil
	}
}

// ExtractFile writes the named member to dest.
func ExtractFile(payload []byte, name, dest string) error {
	data, err := Extract(payload, name)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0755)
}
