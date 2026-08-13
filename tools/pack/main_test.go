package main

import (
	"os"
	"path/filepath"

	"tailscale-me/internal/payload"
	"tailscale-me/internal/shasum"
	"tailscale-me/internal/wintarget"
	"testing"
)

// TestVerifyRoundTrip writes a small launcher plus the three installers,
// appends the payload and confirms verify() accepts the result.
func TestVerifyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	launcher := writeTemp(t, filepath.Join(dir, "launcher.exe"), []byte("MZ-launcher"))
	installers := map[string]string{
		"386":   filepath.Join(dir, "386.exe"),
		"amd64": filepath.Join(dir, "amd64.exe"),
		"arm64": filepath.Join(dir, "arm64.exe"),
	}
	for name, path := range installers {
		writeTemp(t, path, []byte("installer-"+name))
	}

	launcherData, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	var members []payload.Member
	shas := map[string]string{}
	for name, path := range installers {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, payload.Member{Name: wintarget.MemberName(name), Data: data})
		shas[name] = shasum.Hex(data)
	}
	image, err := payload.Append(launcherData, members)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "TailscaleMe-windows.exe")
	if err := os.WriteFile(out, image, 0755); err != nil {
		t.Fatal(err)
	}

	if err := verify(out, shas); err != nil {
		t.Fatalf("verify rejected a valid image: %v", err)
	}

	// A member that no longer matches its pinned sha must fail verify.
	badShas := map[string]string{"386": "00", "amd64": "11", "arm64": "22"}
	if err := verify(out, badShas); err == nil {
		t.Error("verify accepted a digest mismatch")
	}
}

func writeTemp(t *testing.T, path string, content []byte) string {
	t.Helper()
	if err := os.WriteFile(path, content, 0755); err != nil {
		t.Fatal(err)
	}
	return path
}
