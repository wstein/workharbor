package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fwGlobal = "/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate"

func publicDeps(t *testing.T, cfg string, r scripted) Deps {
	t.Helper()
	d := hostDeps(r)
	d.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	if cfg != "" {
		if err := os.WriteFile(d.ConfigPath, []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func TestThePublicURLCheckSaysWhatItVerifiesAndWhatItDoesNot(t *testing.T) {
	on := scripted{fwGlobal: "Firewall is enabled. (State = 1)"}
	cases := []struct {
		name, cfg string
		r         scripted
		want      Status
		sub       string
	}{
		{"no file", "", on, NotVerified, "needs the configuration"},
		{"no name", `{"listen":"127.0.0.1:8787"}`, on, NotVerified, "--local"},
		{"open listener", `{"listen":"0.0.0.0:8787","public_url":"https://w.example"}`, on, Fail, "not a loopback"},
		{"bare host", `{"public_url":"w.example"}`, on, Fail, "public_url"},
		{"http", `{"public_url":"http://w.example"}`, on, Fail, "http:// is refused"},
		{"good, firewall on", `{"public_url":"https://w.example"}`, on, OK, "does not filter loopback"},
		{"good, firewall unreadable", `{"public_url":"https://w.example"}`, scripted{}, OK, "could not be read"},
		{"good, firewall off", `{"public_url":"https://w.example"}`, scripted{fwGlobal: "Firewall is disabled. (State = 0)"}, OK, "firewall is off"},
	}
	for _, tc := range cases {
		got, detail := status(steps(t, publicDeps(t, tc.cfg, tc.r))["public-url"])
		if got != tc.want || !strings.Contains(detail, tc.sub) {
			t.Errorf("%s: %s %q, want %s containing %q", tc.name, got, detail, tc.want, tc.sub)
		}
		if tc.want == OK && !strings.Contains(detail, "tailscale serve --bg 8787") {
			t.Errorf("%s: no remedy named: %q", tc.name, detail)
		}
		if tc.want == OK && !strings.Contains(detail, "not checked") {
			t.Errorf("%s: an ok that does not say the forwarder is unchecked: %q", tc.name, detail)
		}
	}
}

func TestThePublicURLFixNormalisesShowsAndWritesAfterAY(t *testing.T) {
	d := publicDeps(t, `{"listen":"127.0.0.1:8787","other":1}`, scripted{})
	fix := steps(t, d)["public-url"].Fix
	ctx := context.Background()

	for name, a := range map[string]*answers{
		"unusable": {confirm: true, lines: []string{"https://w.example/path"}},
		"declined": {lines: []string{"W.Example"}},
	} {
		if err := fix.Do(ctx, a); err == nil {
			t.Errorf("%s: no error", name)
		}
		if b, _ := os.ReadFile(d.ConfigPath); strings.Contains(string(b), "public_url") {
			t.Errorf("%s: wrote %s", name, b)
		}
	}
	a := &answers{confirm: true, lines: []string{"W.Example/"}}
	if err := fix.Do(ctx, a); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(d.ConfigPath)
	if !strings.Contains(string(b), `"public_url": "https://w.example"`) || !strings.Contains(string(b), `"other": 1`) {
		t.Errorf("config = %s", b)
	}
	if !strings.Contains(strings.Join(a.shown, "\n"), "public_url will be https://w.example") {
		t.Errorf("the normalised value was not shown: %q", a.shown)
	}
	if got, _ := status(steps(t, d)["public-url"]); got != OK {
		t.Errorf("after the fix: %s", got)
	}

	missing := publicDeps(t, "", scripted{})
	if err := steps(t, missing)["public-url"].Fix.Do(ctx, &answers{confirm: true, lines: []string{"w.example"}}); err == nil || !strings.Contains(err.Error(), "config-base") {
		t.Errorf("without a file: %v", err)
	}
}

func TestTheGitHubAppStepUsesTheConfiguredNameOrAsksAndNormalises(t *testing.T) {
	ctx := context.Background()
	d := publicDeps(t, `{"public_url":"https://w.example"}`, scripted{})
	d.Whr = "/x/whr"
	cmds, err := steps(t, d)["github-app"].Fix.Build(ctx, &answers{})
	if err != nil || strings.Contains(strings.Join(cmds[0].Argv, " "), "--public-url") {
		t.Errorf("configured: %v %v", cmds, err)
	}

	d = publicDeps(t, `{}`, scripted{})
	d.Whr = "/x/whr"
	a := &answers{lines: []string{" W.Example "}}
	cmds, err = steps(t, d)["github-app"].Fix.Build(ctx, a)
	if err != nil || cmds[0].Argv[len(cmds[0].Argv)-1] != "https://w.example" {
		t.Errorf("asked: %v %v", cmds, err)
	}
	if !strings.Contains(strings.Join(a.shown, "\n"), "https://w.example") {
		t.Errorf("the normalised name was not shown: %q", a.shown)
	}
	if _, err := steps(t, d)["github-app"].Fix.Build(ctx, &answers{lines: []string{"http://w.example"}}); err == nil {
		t.Error("http accepted")
	}
}
