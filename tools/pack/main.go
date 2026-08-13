// Command pack builds the universal Windows TailscaleMe launcher.
//
// Subcommands:
//
//	shas <386> <amd64> <arm64>     print -X ldflags with each installer's sha256
//	append -out X -launcher L      append the gzipped-tar payload of the three
//	  -386 A -amd64 B -arm64 C     installers to launcher L and verify the image
//
// The launcher itself is compiled by build.sh/build.bat (with the flags from
// "shas") because building it from inside this tool conflicts with the
// GOTOOLCHAIN toolchain-switching environment. This tool only handles bytes.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"tailscale-me/internal/payload"
	"tailscale-me/internal/shasum"
	"tailscale-me/internal/wintarget"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: pack <shas|append> ...")
	}
	switch os.Args[1] {
	case "shas":
		cmdShas(os.Args[2:])
	case "append":
		cmdAppend(os.Args[2:])
	default:
		fatal("unknown subcommand " + os.Args[1] + " (want shas or append)")
	}
}

// cmdShas prints, in the canonical 386/amd64/arm64 order, one -X flag per
// installer so build scripts can paste them into the launcher's -ldflags.
func cmdShas(args []string) {
	if len(args) != len(wintarget.ArchOrder) {
		fatal("usage: pack shas <386> <amd64> <arm64>")
	}
	var flags []string
	for i, arch := range wintarget.ArchOrder {
		data, err := os.ReadFile(args[i])
		if err != nil {
			fatal(fmt.Sprintf("%s: %v", arch, err))
		}
		flags = append(flags, fmt.Sprintf("-X tailscale-me/launcher/windows.sha%s=%s",
			arch, shasum.Hex(data)))
	}
	fmt.Println(strings.Join(flags, " "))
}

// cmdAppend reads the launcher and the three installers, appends the payload
// and verifies the result round-trips through the same code the launcher runs.
func cmdAppend(args []string) {
	fs := flag.NewFlagSet("append", flag.ExitOnError)
	out := fs.String("out", "", "path to write the universal exe")
	launcher := fs.String("launcher", "", "path to the built launcher exe")
	file386 := fs.String("386", "", "windows/386 installer exe")
	fileAmd64 := fs.String("amd64", "", "windows/amd64 installer exe")
	fileArm64 := fs.String("arm64", "", "windows/arm64 installer exe")
	fs.Parse(args)
	files := map[string]string{
		"386":   *file386,
		"amd64": *fileAmd64,
		"arm64": *fileArm64,
	}

	if *out == "" || *launcher == "" {
		fatal("append requires -out and -launcher")
	}
	launcherData, err := os.ReadFile(*launcher)
	if err != nil {
		fatal(err.Error())
	}

	members := make([]payload.Member, 0, len(wintarget.ArchOrder))
	shas := map[string]string{}
	for _, name := range wintarget.ArchOrder {
		data, err := os.ReadFile(files[name])
		if err != nil {
			fatal(fmt.Sprintf("-%s: %v", name, err))
		}
		members = append(members, payload.Member{Name: wintarget.MemberName(name), Data: data})
		shas[name] = shasum.Hex(data)
	}

	image, err := payload.Append(launcherData, members)
	if err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*out, image, 0755); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(image))

	if err := verify(*out, shas); err != nil {
		fatal(err.Error())
	}
	fmt.Println("OK: universal exe verified (each member round-trips and matches its pinned sha).")
}

// verify re-reads the final image and confirms every member extracts cleanly
// and hashes to the pinned value by the same code path the launcher uses.
func verify(imagePath string, shas map[string]string) error {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return err
	}
	start, err := payload.Start(data)
	if err != nil {
		return err
	}
	for _, name := range wintarget.ArchOrder {
		member := wintarget.MemberName(name)
		got, err := payload.Extract(data[start:], member)
		if err != nil {
			return fmt.Errorf("%s: %w", member, err)
		}
		if h := shasum.Hex(got); h != shas[name] {
			return fmt.Errorf("%s: hashed to %s, want %s", member, h, shas[name])
		}
	}
	return nil
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "pack:", msg)
	os.Exit(1)
}
