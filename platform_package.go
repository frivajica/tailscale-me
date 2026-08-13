//go:build windows || linux

// Shared package-based install flow for the OSes that fetch an official
// Tailscale artifact (MSI on Windows, .tgz on Linux) and verify its checksum.

package main

import "fmt"

// installerArtifact decides which package to fetch. Legacy Windows uses the
// pinned v1.44.3 MSI; everything else resolves the newest stable version from
// the package index and verifies against its versioned .sha256 endpoint (the
// "latest" aliases do not expose one). This keeps the tool auto-updated yet
// tamper-proof on every platform.
func installerArtifact(legacy bool) (url, wantSHA, localName string, err error) {
	if legacy {
		return legacyMSIURL, legacyMSISHA256, "tailscale-setup.msi", nil
	}
	version, err := fetchLatestVersion()
	if err != nil {
		return "", "", "", err
	}
	base, err := packageBase()
	if err != nil {
		return "", "", "", err
	}
	url = "https://pkgs.tailscale.com/stable/" + fmt.Sprintf(base, version)
	wantSHA, err = fetchSHA256(url + ".sha256")
	if err != nil {
		return "", "", "", err
	}
	return url, wantSHA, packageLocalName(), nil
}
