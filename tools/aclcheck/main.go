// Verify a tailnet's ACL advertises the rules TailscaleMe relies on for remote
// SSH, before binaries are built. Pure stdlib tool so build.sh/build.bat can
// call it without jq or curl plumbing.
//
// Checks two client-facing requirements against the fetched ACL:
//   - an ACL "ssh" rule whose dst covers the managed tag (Tailscale SSH on
//     Linux/macOS), and
//   - a "grants" (or classic "acls") rule letting the operator reach the
//     managed tag, so Windows OpenSSH on port 22 is reachable over the tailnet.
//
// The stock all-allow ACL needs neither, so that case passes without tests.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	apiBase    = "https://api.tailscale.com/api/v2"
	exitPass   = 0
	exitWarn   = 0 // warn only: the build must not be silently blocked on guesses
	exitStrict = 1
)

type rule struct {
	Action string   `json:"action"`
	Src    []string `json:"src"`
	Dst    []string `json:"dst"`
}

type aclDoc struct {
	ACLs   []rule `json:"acls"`
	SSH    []rule `json:"ssh"`
	Grants []rule `json:"grants"`
}

type tailnetClient struct {
	base, token, tailnet string
	hc                   *http.Client
}

func (c *tailnetClient) get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned HTTP %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return b, nil
}

// resolveTailnet returns the tailnet name for the token when not supplied.
func (c *tailnetClient) resolveTailnet() (string, error) {
	if c.tailnet != "" {
		return c.tailnet, nil
	}
	b, err := c.get("/whoami")
	if err != nil {
		return "", err
	}
	var w struct {
		Tailnet struct {
			Name string `json:"Name"`
		} `json:"Tailnet"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return "", fmt.Errorf("could not parse /whoami: %w", err)
	}
	if w.Tailnet.Name == "" {
		return "", fmt.Errorf("/whoami did not report a tailnet name")
	}
	return w.Tailnet.Name, nil
}

// covers reports whether any dst entry matches tag or wildcards over it.
func covers(dst []string, tag string) bool {
	for _, d := range dst {
		if d == "*" || d == "*:*" || d == tag || strings.HasPrefix(d, tag+":") {
			return true
		}
	}
	return false
}

// defaultAllAllow heuristically recognizes the stock Tailscale ACL, which needs
// no grants/ssh rules for SSH to work.
func defaultAllAllow(a aclDoc) bool {
	if len(a.ACLs) == 0 {
		return len(a.Grants) == 0 && len(a.SSH) == 0
	}
	if len(a.ACLs) != 1 {
		return false
	}
	one := a.ACLs[0]
	return one.Action == "accept" && contains(one.Src, "*") && contains(one.Dst, "*:*")
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// verifyACL evaluates the parsed ACL against the two SSH requirements. It
// prints a human report and returns the exit code (strict failures -> 1).
func verifyACL(data []byte, tag string, strict bool) int {
	var a aclDoc
	if err := json.Unmarshal(data, &a); err != nil {
		fmt.Printf("aclcheck: WARNING: could not parse the ACL JSON: %v\n", err)
		return exitWarn
	}
	if defaultAllAllow(a) {
		fmt.Println("aclcheck: PASS - default all-allow ACL; every node can reach every other, nothing to configure.")
		return exitPass
	}

	code := exitPass
	fmt.Println("aclcheck: custom ACL detected - checking the SSH requirements.")

	sshHit := false
	for _, r := range a.SSH {
		if r.Action == "accept" && covers(r.Dst, tag) {
			sshHit = true
			break
		}
	}
	if !sshHit {
		code = report(code, "Tailscale SSH (Linux/macOS)", "no 'ssh' rule covers "+tag,
			"add the ssh block from ACL_Configuration.json (src=your email, dst=tag:managed, users=\u0022root\u0022/autogroup:nonroot).",
			strict)
	} else {
		fmt.Printf("aclcheck: OK - an 'ssh' rule covers %s (Tailscale SSH on Linux/macOS).\n", tag)
	}

	winHit := false
	for _, r := range a.Grants {
		if covers(r.Dst, tag) {
			winHit = true
			break
		}
	}
	if !winHit {
		for _, r := range a.ACLs {
			if r.Action == "accept" && covers(r.Dst, tag) {
				winHit = true
				break
			}
		}
	}
	if !winHit {
		code = report(code, "Windows OpenSSH (port 22)", "no grant lets the operator reach "+tag,
			"add the grants block from ACL_Configuration.json (src=your email, dst=tag:managed, ip=\u0022*\u0022).",
			strict)
	} else {
		fmt.Printf("aclcheck: OK - the operator can reach %s (Windows OpenSSH on port 22).\n", tag)
	}
	return code
}

func report(code int, name, problem, fix string, strict bool) int {
	if strict {
		fmt.Printf("aclcheck: FAIL - %s: %s. Fix before building: %s\n", name, problem, fix)
		return exitStrict
	}
	fmt.Printf("aclcheck: WARNING - %s: %s. %s (use --strict to fail the build)\n", name, problem, fix)
	return code
}

func main() {
	token := flag.String("token", envOr("TS_API_TOKEN", ""), "Tailscale API token (PAT or OAuth client; also TS_API_TOKEN)")
	tailnet := flag.String("tailnet", envOr("TS_TAILNET", ""), "tailnet name (defaults to the token's tailnet via /whoami)")
	tag := flag.String("tag", envOr("TS_TAG", "tag:managed"), "managed tag to require in the ACL rules")
	strict := flag.Bool("strict", false, "exit non-zero when a required rule is missing")
	base := flag.String("base-url", apiBase, "API base URL (advanced)")
	flag.Parse()
	if *tag == "" {
		*tag = "tag:managed"
	}

	if *token == "" {
		fmt.Println("aclcheck: skipped (no API token; set --token or TS_API_TOKEN to verify the ACL for the SSH rules).")
		return
	}

	c := &tailnetClient{base: *base, token: *token, tailnet: *tailnet,
		hc: &http.Client{Timeout: 30 * time.Second}}
	name, err := c.resolveTailnet()
	if err != nil {
		fmt.Printf("aclcheck: WARNING: could not resolve the tailnet (%v) - skipping the ACL check.\n", err)
		return
	}
	b, err := c.get("/tailnet/" + name + "/acl")
	if err != nil {
		fmt.Printf("aclcheck: WARNING: could not fetch the ACL for %s (%v) - skipping the ACL check.\n", name, err)
		return
	}
	fmt.Printf("aclcheck: checking the ACL of tailnet %s ...\n", name)
	os.Exit(verifyACL(b, *tag, *strict))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
