// Pure text parsers for OS/tooling output. No process I/O, so every function
// here is unit-testable in isolation.

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// parseOSRelease extracts PRETTY_NAME="…" from /etc/os-release.
func parseOSRelease(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
		}
	}
	return ""
}

var winVerRe = regexp.MustCompile(`(\d+)\.(\d+)\.\d+`)

// parseWindowsVer extracts (major, minor) from `cmd /c ver` output. Windows 11
// still reports itself as 10.0.x, which is fine: the legacy check only cares
// about 6.x.
func parseWindowsVer(text string) (major, minor int, err error) {
	m := winVerRe.FindStringSubmatch(text)
	if len(m) != 3 {
		return 0, 0, fmt.Errorf("could not parse version from %q", text)
	}
	major, err = strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, err
	}
	minor, err = strconv.Atoi(m[2])
	return major, minor, err
}

var goVersionRe = regexp.MustCompile(`go(\d+)\.(\d+)`)

// parseGoVersion extracts (major, minor) from `go version` output. ok is false
// when the string cannot be parsed.
func parseGoVersion(text string) (major, minor int, ok bool) {
	m := goVersionRe.FindStringSubmatch(text)
	if len(m) != 3 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// parseRouteInterface extracts the `interface:` value from `route -n get
// default`.
func parseRouteInterface(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	return ""
}

var svcNameRe = regexp.MustCompile(`^\(\d+\)(.+)$`)

// parseServiceForInterface maps an interface (en0) to its network-service name
// using `networksetup -listnetworkserviceorder` output:
//
//	(1) Wi-Fi
//	(Hardware Port: Wi-Fi, Device: en0)
func parseServiceForInterface(orderText, iface string) string {
	lines := strings.Split(orderText, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		m := svcNameRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[1])
		for _, n := range lines[i+1:] {
			n = strings.TrimSpace(n)
			if n == "" {
				break
			}
			if strings.HasPrefix(n, "(Hardware Port") && strings.Contains(n, "Device: "+iface+")") {
				return name
			}
		}
	}
	return ""
}

// parseDNSServers parses `networksetup -getdnsservers` output. It returns nil
// for the "There aren't any DNS Servers…" case.
func parseDNSServers(text string) []string {
	if strings.Contains(text, "There aren't any DNS Servers") {
		return nil
	}
	var servers []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.EqualFold(line, "DNS Servers") {
			servers = append(servers, line)
		}
	}
	return servers
}
