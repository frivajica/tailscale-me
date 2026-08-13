package payload

import (
	"bytes"
	"testing"

	"tailscale-me/internal/wintarget"
)

func TestRoundTrip(t *testing.T) {
	exe := []byte("MZ....some-launcher")
	members := []Member{
		{Name: wintarget.MemberName("386"), Data: []byte{0x01}},
		{Name: wintarget.MemberName("amd64"), Data: []byte{0x02}},
		{Name: wintarget.MemberName("arm64"), Data: []byte{0x03}},
	}
	image, err := Append(exe, members)
	if err != nil {
		t.Fatal(err)
	}
	start, err := Start(image)
	if err != nil {
		t.Fatal(err)
	}
	payloadBytes := image[start:]

	got, err := Extract(payloadBytes, wintarget.MemberName("amd64"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0x02}) {
		t.Errorf("extracted %v, want [0x02]", got)
	}

	if _, err := Extract(payloadBytes, "nope.exe"); err == nil {
		t.Error("expected error for missing member")
	}

	// Deterministic output: same input, same image.
	again, err := Append(exe, members)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(image, again) {
		t.Error("Append is not deterministic")
	}
}

func TestStartErrors(t *testing.T) {
	if _, err := Start([]byte("no marker here")); err == nil {
		t.Error("expected error for missing marker")
	}
	if _, err := Start([]byte(Marker)); err == nil {
		t.Error("expected error for marker with nothing after it")
	}
}

func TestCRLFMarker(t *testing.T) {
	// Batch echoes the marker with CRLF; Start must skip it.
	image := append([]byte("exe"), []byte(Marker+"\r\n")...)
	image = append(image, []byte{0xDE, 0xAD}...)
	start, err := Start(image)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(image[start:], []byte{0xDE, 0xAD}) {
		t.Error("CRLF marker not skipped")
	}
}

func TestNoMagicMembers(t *testing.T) {
	// The launcher exe must not accidentally carry a payload member that
	// contains the marker mid-stream when appended.
	image, err := Append([]byte("exe"), []Member{{Name: "a", Data: []byte("b")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Start(image); err != nil {
		t.Fatalf("valid image rejected: %v", err)
	}
}
