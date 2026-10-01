package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rig struct {
	dir string
	cfg Config
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{dir: dir}
	for _, d := range []string{"cache", "workspaces", "store", "secrets"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	secret := func(name string) string {
		p := filepath.Join(dir, "secrets", name)
		if err := os.WriteFile(p, []byte("value\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	r.cfg = Config{
		Listen:            "127.0.0.1:8787",
		Repositories:      []Repository{{Name: "wstein/workharbor", CloneDepth: 0}},
		Roots:             Roots{Cache: filepath.Join(dir, "cache"), Workspaces: filepath.Join(dir, "workspaces"), ToolStore: filepath.Join(dir, "store")},
		GitHub:            GitHub{AppID: 12345, KeyFile: secret("app.pem")},
		AgentLoginEnvFile: secret("agent.env"), APITokenFile: secret("api.token"),
	}
	return r
}

func (r *rig) parse(t *testing.T) (*Config, error) {
	t.Helper()
	raw, err := json.Marshal(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(raw)
}

func problems(err error) string {
	var ce *Error
	if errors.As(err, &ce) {
		return strings.Join(ce.Problems, "\n")
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func TestAGoodConfigLoads(t *testing.T) {
	r := newRig(t)
	raw, _ := json.MarshalIndent(r.cfg, "", "  ")
	path := filepath.Join(r.dir, "whr.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8787" || len(c.Repositories) != 1 || c.GitHub.AppID != 12345 {
		t.Errorf("config = %+v", c)
	}
	if _, err := Load(filepath.Join(r.dir, "missing.json")); err == nil || errors.As(err, new(*Error)) {
		t.Errorf("a missing file = %v, want a plain read error", err)
	}
}

// Every problem is reported at once, with the key it is about.
func TestEveryProblemIsReportedWithItsKey(t *testing.T) {
	r := newRig(t)
	r.cfg.Listen = "0.0.0.0:8787"
	r.cfg.Repositories = []Repository{{Name: "no-slash"}, {Name: "Wstein/Workharbor"}, {Name: "wstein/workharbor"}, {Name: "a/b", CloneDepth: -1}}
	r.cfg.Roots.Cache = "relative/cache"
	r.cfg.Roots.ToolStore = filepath.Join(r.dir, "nope")
	r.cfg.GitHub.AppID = 0
	r.cfg.APITokenFile = ""
	_, err := r.parse(t)
	got := problems(err)
	for _, want := range []string{
		"listen: \"0.0.0.0\" is not a loopback address",
		"repositories[0].name: \"no-slash\"",
		"repositories[2].name: \"wstein/workharbor\" is the same repository as repositories[1]",
		"repositories[3].clone_depth: -1",
		"roots.cache: \"relative/cache\" must be an absolute",
		"roots.tool_store:",
		"github.app_id",
		"api_token_file: a file path is needed",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the report lacks %q:\n%s", want, got)
		}
	}
}

func TestOnlyLoopbackListens(t *testing.T) {
	for addr, ok := range map[string]bool{
		"127.0.0.1:8787": true, "[::1]:8787": true, "127.0.0.2:80": true,
		"0.0.0.0:8787": false, "192.168.1.5:8787": false, "localhost:8787": false, "[::]:8787": false,
		"8787": false, "": false, "127.0.0.1:0": false, "127.0.0.1:70000": false, "127.0.0.1:x": false,
	} {
		r := newRig(t)
		r.cfg.Listen = addr
		_, err := r.parse(t)
		if ok != (err == nil) {
			t.Errorf("listen %q: accepted=%v, want %v (%s)", addr, err == nil, ok, problems(err))
		}
	}
}

func TestSecretsAreFilesWithModeZeroSixHundred(t *testing.T) {
	r := newRig(t)
	for name, mod := range map[string]func(){
		"group readable": func() { _ = os.Chmod(r.cfg.APITokenFile, 0o640) }, //nolint:gosec // a test making a secret too open,
		"world readable": func() { _ = os.Chmod(r.cfg.APITokenFile, 0o644) }, //nolint:gosec // a test making a secret too open,
		"too open":       func() { _ = os.Chmod(r.cfg.APITokenFile, 0o700) }, //nolint:gosec // a test making a secret too open,
		"empty": func() {
			_ = os.Chmod(r.cfg.APITokenFile, 0o600)
			_ = os.WriteFile(r.cfg.APITokenFile, nil, 0o600)
		},
		"a symlink": func() {
			link := filepath.Join(r.dir, "secrets", "link")
			_ = os.Symlink(r.cfg.APITokenFile, link)
			r.cfg.APITokenFile = link
		},
		"a directory": func() { r.cfg.APITokenFile = filepath.Join(r.dir, "secrets") },
		"missing":     func() { r.cfg.APITokenFile = filepath.Join(r.dir, "secrets", "nope") },
	} {
		r2 := newRig(t)
		r = r2
		mod()
		if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "api_token_file") {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	// A value written in place of a path is refused too: it is not an absolute path.
	r = newRig(t)
	r.cfg.GitHub.KeyFile = "-----BEGIN PRIVATE KEY-----"
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "github.key_file") {
		t.Errorf("a key pasted into the config = %v", err)
	}
	// The error never prints a file's content.
	if strings.Contains(problems(err2(t)), "value") {
		t.Error("an error leaked a secret's content")
	}
}

func err2(t *testing.T) error {
	r := newRig(t)
	_ = os.Chmod(r.cfg.APITokenFile, 0o644) //nolint:gosec // a test making a secret too open
	_, err := r.parse(t)
	return err
}

func TestTheRootsAreSeparateAndSecretsAreOutOfTheWorkspace(t *testing.T) {
	r := newRig(t)
	r.cfg.Roots.Cache = filepath.Join(r.dir, "workspaces") // the cache is where an agent writes
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "overlap") {
		t.Errorf("the cache in the workspace root = %v", err)
	}
	r = newRig(t)
	nested := filepath.Join(r.dir, "workspaces", "store")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	r.cfg.Roots.ToolStore = nested
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "overlap") {
		t.Errorf("the tool store inside the workspace root = %v", err)
	}
	r = newRig(t)
	inside := filepath.Join(r.dir, "workspaces", "api.token")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.cfg.APITokenFile = inside
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "inside the workspace root") {
		t.Errorf("a secret in the workspace root = %v", err)
	}
	// A link that leads into the workspace root is judged by where it leads.
	r = newRig(t)
	link := filepath.Join(r.dir, "secrets", "ws")
	if err := os.Symlink(filepath.Join(r.dir, "workspaces"), link); err != nil {
		t.Skip(err)
	}
	r.cfg.Roots.Cache = link
	if _, err := r.parse(t); err == nil || !strings.Contains(problems(err), "overlap") {
		t.Errorf("a link to the workspace root = %v", err)
	}
}

func TestTheFileIsDecodedStrictly(t *testing.T) {
	r := newRig(t)
	raw, _ := json.Marshal(r.cfg)
	for name, body := range map[string]string{
		"an unknown key":        strings.Replace(string(raw), `"listen"`, `"listen_addr":"x","listen"`, 1),
		"a token value":         strings.Replace(string(raw), `"api_token_file"`, `"api_token":"ghp_abc","api_token_file"`, 1),
		"two values":            string(raw) + string(raw),
		"not json":              "listen = 127.0.0.1",
		"a string for a number": strings.Replace(string(raw), `"app_id":12345`, `"app_id":"12345"`, 1),
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
