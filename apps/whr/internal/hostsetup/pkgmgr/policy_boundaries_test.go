// SPDX-License-Identifier: EUPL-1.2
package pkgmgr

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/catalogue"
)

func TestLookupBindsDestinationAndIndependentHostApproval(t *testing.T) {
	for _, tc := range []struct {
		name, raw, host            string
		approved, transportFailure bool
	}{
		{"approved", "https://fixture.invalid/version", "fixture.invalid", true, false},
		{"transport-failure", "https://fixture.invalid/version", "fixture.invalid", true, true},
		{"declared-unapproved-host", "https://other.invalid/version", "fixture.invalid", false, false},
		{"declared-http", "http://fixture.invalid/version", "fixture.invalid", false, false},
		{"declared-userinfo", "https://user:secret@fixture.invalid/version", "fixture.invalid", false, false},
		{"declared-query", "https://fixture.invalid/version?host=private", "fixture.invalid", false, false},
		{"declared-fragment", "https://fixture.invalid/version#private", "fixture.invalid", false, false},
		{"declared-port", "https://fixture.invalid:443/version", "fixture.invalid:443", false, false},
		{"declared-opaque", "https:fixture.invalid/version", "fixture.invalid", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, _, _, _, _, _ := fixture(t, item("jq"))
			n := &fakeNetwork{}
			if tc.transportFailure {
				n.err = errors.New("private-response-canary")
			}
			e.Network = n
			i := item("jq")
			// Even forbidden destinations are declared, so declaration cannot mask host/syntax checks.
			i.Network = catalogue.Network{LookupURLs: []string{tc.raw}, Hosts: []string{tc.host}}
			verified, err := e.Lookup(context.Background(), i, tc.raw)
			if !tc.approved {
				if err == nil || verified || len(n.requests) != 0 {
					t.Fatal("declared forbidden destination reached network", verified, err, n.requests)
				}
				return
			}
			want := LookupRequest{URL: tc.raw, Method: "GET", UserAgent: LookupUserAgent, Timeout: 5 * time.Second, BodyLimit: 65536}
			if err != nil || verified == tc.transportFailure || !reflect.DeepEqual(n.requests, []LookupRequest{want}) {
				t.Fatal("approved destination/policy or transport result changed", verified, err, n.requests)
			}
		})
	}
}

func TestPrivilegedPreviewRequiresDeclarationAndTerminalPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, executable  string
		declared, allowed bool
	}{
		{"approved", "/usr/bin/fixture", true, true},
		{"undeclared-exact-preview", "/usr/bin/fixture", false, false},
		{"declared-arm-brew-exact-preview", "/opt/homebrew/bin/brew", true, false},
		{"declared-intel-brew-exact-preview", "/usr/local/bin/brew", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := item("jq")
			argv := []string{tc.executable, "--readonly"}
			if tc.declared {
				i.Privileged = []catalogue.Privileged{{Argv: argv}}
			}
			shown := append([]string{"/usr/bin/sudo", "--"}, argv...)
			p := Policy{Mode: "fix", Yes: true, Terminal: true, Confirmed: map[string]bool{"jq": true}, Shown: map[string][]string{"jq": shown}}
			cmd, err := PrivilegedPreview(i, argv, p)
			if !tc.allowed {
				if err == nil || !reflect.DeepEqual(cmd, Command{}) {
					t.Fatal("forbidden exact-preview privilege accepted", cmd, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(cmd, Command{Argv: shown, Stdin: "terminal", OutputLimit: 65536}) {
				t.Fatal("terminal preview policy changed", cmd, err)
			}
		})
	}
}

func TestBrewEnvironmentIsExactAllowlist(t *testing.T) {
	e, r, exec, _, _, _, _ := fixture(t, item("jq"))
	r.Environment = map[string]string{
		"HOME": "/fixture/home", "LANG": "C.UTF-8", "LC_ALL": "C", "LC_TIME": "C", "TERM": "xterm", "TMPDIR": "/fixture/tmp",
		"HTTP_PROXY": "http://proxy.invalid:8080", "HTTPS_PROXY": "http://proxy.invalid:8080", "NO_PROXY": "fixture.invalid",
		"PASSWORD": "password-canary", "TOKEN": "token-canary", "DYLD_INSERT_LIBRARIES": "injection-canary", "SSH_AUTH_SOCK": "/private/socket",
		"GIT_CONFIG_COUNT": "1", "UNRELATED_KEY": "private-canary", "PATH": "/untrusted/bin", "HOMEBREW_NO_ANALYTICS": "0",
	}
	out, err := e.Apply(context.Background(), r)
	want := map[string]string{
		"HOME": "/fixture/home", "LANG": "C.UTF-8", "LC_ALL": "C", "LC_TIME": "C", "TERM": "xterm", "TMPDIR": "/fixture/tmp",
		"HTTP_PROXY": "http://proxy.invalid:8080", "HTTPS_PROXY": "http://proxy.invalid:8080", "NO_PROXY": "fixture.invalid",
		"PATH": "/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin", "HOMEBREW_NO_AUTO_UPDATE": "1", "HOMEBREW_NO_ANALYTICS": "1", "HOMEBREW_NO_ENV_HINTS": "1",
	}
	if err != nil || len(out.Steps) != 1 || out.Steps[0].Status != "applied" || len(exec.calls) != 1 || !reflect.DeepEqual(exec.calls[0].Env, want) {
		t.Fatal("environment allowlist changed", out, err, exec.calls)
	}
}

func TestBrewRejectsMissingRelativeAndNoncanonicalHOME(t *testing.T) {
	for _, home := range []string{"", "relative/home", "/fixture/../home", "/fixture//home", "/fixture/home/"} {
		t.Run(home, func(t *testing.T) {
			e, r, exec, _, _, _, _ := fixture(t, item("jq"))
			r.Environment = map[string]string{}
			if home != "" {
				r.Environment["HOME"] = home
			}
			out, err := e.Apply(context.Background(), r)
			if err != nil || len(out.Steps) != 1 || out.Steps[0].Status != "blocked" || out.Steps[0].Reason != "invalid_package_source" || len(exec.calls) != 0 {
				t.Fatal("invalid HOME reached process", home, out, err, exec.calls)
			}
		})
	}
}
