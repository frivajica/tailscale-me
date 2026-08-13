package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tag = "tag:managed"

func TestVerifyACLDefaultAllAllow(t *testing.T) {
	body := []byte(`{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`)
	if code := verifyACL(body, tag, true); code != exitPass {
		t.Fatalf("default ACL = %d, want %d", code, exitPass)
	}
}

func TestVerifyACLEmpty(t *testing.T) {
	if code := verifyACL([]byte(`{}`), tag, true); code != exitPass {
		t.Fatalf("empty ACL = %d, want %d (no grants/ssh/acls => all-allow)", code, exitPass)
	}
}

func TestVerifyACLCustomComplete(t *testing.T) {
	body := []byte(`{
  "acls":[{"action":"accept","src":["*"],"dst":["*:*"]}],
  "ssh":[{"action":"accept","src":["you@example.com"],"dst":["tag:managed"],"users":["root"]}],
  "grants":[{"src":["you@example.com"],"dst":["tag:managed"],"ip":["*"]}]
}`)
	code := verifyACL(body, tag, true)
	if code != exitPass {
		t.Fatalf("custom+complete ACL = %d, want %d\n%s", code, exitPass, "check messages above")
	}
}

func TestVerifyACLCustomMissingBothStrict(t *testing.T) {
	body := []byte(`{"acls":[{"action":"accept","src":["tag:others"],"dst":["tag:others"]}]}`)
	code := verifyACL(body, tag, true)
	if code != exitStrict {
		t.Fatalf("strict missing-rule ACL = %d, want %d", code, exitStrict)
	}
}

func TestVerifyACLCustomMissingBothWarnOnly(t *testing.T) {
	body := []byte(`{"acls":[{"action":"accept","src":["tag:others"],"dst":["tag:others"]}]}`)
	if code := verifyACL(body, tag, false); code != exitPass {
		t.Fatalf("non-strict missing-rule ACL = %d, want %d (warn, do not block)", code, exitPass)
	}
}

func TestCovers(t *testing.T) {
	for d, want := range map[string]bool{
		"tag:managed":    true,
		"tag:managed:22": true,
		"*":              true,
		"*:*":            true,
		"tag:other":      false,
	} {
		if got := covers([]string{d}, tag); got != want {
			t.Errorf("covers([%q], %q) = %v, want %v", d, tag, got, want)
		}
	}
}

func TestMainRemote(t *testing.T) {
	// whoami + acl served by a test server; token resolves the tailnet name.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whoami":
			w.Write([]byte(`{"Tailnet":{"Name":"example.com"}}`))
		case "/tailnet/example.com/acl":
			w.Write([]byte(`{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}],"ssh":[{"action":"accept","src":["me@x.dev"],"dst":["tag:managed"],"users":["root"]}],"grants":[{"src":["me@x.dev"],"dst":["tag:managed"],"ip":["*"]}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &tailnetClient{base: srv.URL, token: "tskey-abc", hc: srv.Client()}
	name, err := c.resolveTailnet()
	if err != nil || name != "example.com" {
		t.Fatalf("resolveTailnet = %q, %v", name, err)
	}
	b, err := c.get("/tailnet/" + name + "/acl")
	if err != nil {
		t.Fatalf("get acl: %v", err)
	}
	if !strings.Contains(string(b), "grants") {
		t.Fatalf("unexpected acl body: %s", b)
	}
}
