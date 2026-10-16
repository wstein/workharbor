// SPDX-License-Identifier: EUPL-1.2
package workspaceshell

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const bin = "/opt/bundledock/toolbox/bin"
const share = "/opt/bundledock/toolbox/share"

func toolbox() *ToolboxDescription {
	return &ToolboxDescription{MountPath: "/opt/bundledock/toolbox", ArtifactDigest: "sha256:" + strings.Repeat("a", 64), SourceRevision: strings.Repeat("b", 40), Platform: "linux/amd64", ReadOnly: true, Entries: map[string]EntryKind{
		"bin":                   Directory,
		"share/shell/init.bash": RegularFile,
		"share/shell/init.zsh":  RegularFile,
		"share/fish/vendor_conf.d/bdock-shell.fish":    RegularFile,
		"share/nushell/vendor/autoload/bdock-shell.nu": RegularFile,
	}}
}

func TestPlanning(t *testing.T) {
	cases := []struct {
		name      string
		env       map[string]string
		absent    bool
		path, xdg string
		rc        bool
	}{
		{"attached defaults", map[string]string{"PATH": "/usr/bin:/bin"}, false, "/usr/bin:/bin:" + bin, share + ":/usr/local/share:/usr/share", true},
		{"priority duplicates", map[string]string{"PATH": bin + ":/image/bin:" + bin + ":/bin", "XDG_DATA_DIRS": "/image/share:" + share + ":" + share + ":/usr/share"}, false, "/image/bin:/bin:" + bin, share + ":/image/share:/usr/share", true},
		{"absent", map[string]string{"PATH": "/usr/bin:" + bin, "XDG_DATA_DIRS": share + ":/custom"}, true, "/usr/bin", "/custom", false},
		{"absent share only", map[string]string{"PATH": "/bin", "XDG_DATA_DIRS": share}, true, "/bin", "/usr/local/share:/usr/share", false},
		{"complete optout", map[string]string{"PATH": "/bin", "XDG_DATA_DIRS": share, "BDOCK_SHORTCUTS": "0"}, false, "/bin:" + bin, "/usr/local/share:/usr/share", false},
		{"empty xdg", map[string]string{"PATH": "/bin", "XDG_DATA_DIRS": ""}, false, "/bin:" + bin, share + ":/usr/local/share:/usr/share", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.env["BDOCK_STARSHIP"] = "1"
			tc.env["BDOCK_ZSH_EXPAND"] = "0"
			tc.env["UNCHANGED"] = "line\nvalue"
			before := copyMap(tc.env)
			d := toolbox()
			if tc.absent {
				d = nil
			}
			got, err := Prepare(tc.env, d)
			if err != nil {
				t.Fatal(err)
			}
			if got.Environment["PATH"] != tc.path || got.Environment["XDG_DATA_DIRS"] != tc.xdg {
				t.Fatalf("unexpected search paths: %#v", got.Environment)
			}
			if got.Environment["BDOCK_STARSHIP"] != "0" || got.Environment["BDOCK_ZSH_EXPAND"] != "0" || got.Environment["UNCHANGED"] != "line\nvalue" {
				t.Fatalf("policy/env lost: %#v", got.Environment)
			}
			if (got.BashRC != "") != tc.rc || (got.ZshRC != "") != tc.rc {
				t.Fatalf("wrong fragments: %#v", got)
			}
			if !reflect.DeepEqual(tc.env, before) {
				t.Fatal("input mutated")
			}
			again, err := Prepare(got.Environment, d)
			if err != nil || !reflect.DeepEqual(got, again) {
				t.Fatalf("not idempotent: %#v, %v", again, err)
			}
			got.Environment["UNCHANGED"] = "changed"
			if tc.env["UNCHANGED"] != "line\nvalue" {
				t.Fatal("aliased input")
			}
		})
	}
}

func TestDescriptionRefusals(t *testing.T) {
	cases := map[string]func(*ToolboxDescription){
		"writable":         func(d *ToolboxDescription) { d.ReadOnly = false },
		"mount":            func(d *ToolboxDescription) { d.MountPath = "/tmp/toolbox" },
		"mutable artifact": func(d *ToolboxDescription) { d.ArtifactDigest = "toolbox:latest" },
		"uppercase digest": func(d *ToolboxDescription) { d.ArtifactDigest = "sha256:" + strings.Repeat("A", 64) },
		"source alias":     func(d *ToolboxDescription) { d.SourceRevision = "main" },
		"platform":         func(d *ToolboxDescription) { d.Platform = "darwin/arm64" },
		"missing bash":     func(d *ToolboxDescription) { delete(d.Entries, "share/shell/init.bash") },
		"missing zsh":      func(d *ToolboxDescription) { delete(d.Entries, "share/shell/init.zsh") },
		"missing fish":     func(d *ToolboxDescription) { delete(d.Entries, "share/fish/vendor_conf.d/bdock-shell.fish") },
		"missing nu":       func(d *ToolboxDescription) { delete(d.Entries, "share/nushell/vendor/autoload/bdock-shell.nu") },
		"symlink":          func(d *ToolboxDescription) { d.Entries["share/shell/init.bash"] = EntryKind("symlink") },
		"bin is file":      func(d *ToolboxDescription) { d.Entries["bin"] = RegularFile },
		"missing bin":      func(d *ToolboxDescription) { delete(d.Entries, "bin") },
		"nil entries":      func(d *ToolboxDescription) { d.Entries = nil },
		"short digest":     func(d *ToolboxDescription) { d.ArtifactDigest = "sha256:" + strings.Repeat("a", 63) },
		"long digest":      func(d *ToolboxDescription) { d.ArtifactDigest = "sha256:" + strings.Repeat("a", 65) },
		"short revision":   func(d *ToolboxDescription) { d.SourceRevision = strings.Repeat("b", 39) },
		"long revision":    func(d *ToolboxDescription) { d.SourceRevision = strings.Repeat("b", 41) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := toolbox()
			mutate(d)
			before := copyEntries(d.Entries)
			env := map[string]string{"PATH": "/bin", "BDOCK_STARSHIP": "1"}
			p, err := Prepare(env, d)
			var e *Error
			if !errors.As(err, &e) || e.Code != "unsupported_attachment" {
				t.Fatalf("got %v", err)
			}
			if !reflect.DeepEqual(p, Plan{}) || !reflect.DeepEqual(copyEntries(d.Entries), before) || env["BDOCK_STARSHIP"] != "1" {
				t.Fatal("partial output or mutation")
			}
		})
	}
	for _, platform := range []string{"linux/amd64", "linux/arm64"} {
		d := toolbox()
		d.Platform = platform
		d.SourceRevision = strings.Repeat("c", 64)
		if _, err := Prepare(map[string]string{"PATH": "/bin"}, d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnvironmentRefusals(t *testing.T) {
	cases := []map[string]string{nil, {}, {"PATH": ""}, {"PATH": bin}, {"PATH": "/bin:"}, {"PATH": ":/bin"}, {"PATH": "/bin::/usr/bin"}, {"PATH": "relative:/bin"}, {"PATH": "/bin\n"}, {"PATH": "/bin", "XDG_DATA_DIRS": "/share:"}, {"PATH": "/bin", "XDG_DATA_DIRS": "relative"}, {"PATH": "/bin", "XDG_DATA_DIRS": "/share\t"}, {"PATH": "/bin", "BAD=KEY": "v"}, {"PATH": "/bin", "": "v"}, {"PATH": "/bin", "V": "a\x00b"}}
	for i, env := range cases {
		for _, d := range []*ToolboxDescription{nil, toolbox()} {
			p, err := Prepare(env, d)
			var e *Error
			if !errors.As(err, &e) || e.Code != "invalid_environment" || !reflect.DeepEqual(p, Plan{}) {
				t.Fatalf("case %d got %#v, %v", i, p, err)
			}
		}
	}
}

func TestFragments(t *testing.T) {
	p, err := Prepare(map[string]string{"PATH": "/bin"}, toolbox())
	if err != nil {
		t.Fatal(err)
	}
	wantBash := "case $- in *i*) if [ -r /opt/bundledock/toolbox/share/shell/init.bash ]; then . /opt/bundledock/toolbox/share/shell/init.bash; fi ;; esac"
	wantZsh := "if [[ -o interactive ]] && [[ -r /opt/bundledock/toolbox/share/shell/init.zsh ]]; then source /opt/bundledock/toolbox/share/shell/init.zsh; fi"
	if p.BashRC != wantBash || p.ZshRC != wantZsh {
		t.Fatalf("unexpected fragments %#v", p)
	}
}

// This exercises authored harmless content at a substituted temporary path.
// It is surrogate evidence, not fixed /opt image/system-rc or vendor acceptance.
func TestBashSnippetSurrogate(t *testing.T) {
	p, err := Prepare(map[string]string{"PATH": "/bin:/usr/bin", "BDOCK_STARSHIP": "1"}, toolbox())
	if err != nil {
		t.Fatal(err)
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("required acceptance test needs bash on PATH: %v", err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "init.bash")
	marker := filepath.Join(dir, "loaded")
	prompt := filepath.Join(dir, "prompt")
	if err := os.WriteFile(script, []byte("printf loaded > \"$LOAD_MARKER\"\nif [ \"$BDOCK_STARSHIP\" != 0 ]; then printf unsafe > \"$PROMPT_MARKER\"; fi\n"), 0600); err != nil {
		t.Fatal(err)
	}
	snippet := strings.ReplaceAll(p.BashRC, "/opt/bundledock/toolbox/share/shell/init.bash", script)
	for _, interactive := range []bool{false, true} {
		args := []string{"--noprofile", "--norc", "-c", snippet}
		if interactive {
			args[2] = "-ic"
		}
		cmd := exec.Command(bash, args...)
		cmd.Env = []string{"PATH=" + p.Environment["PATH"], "BDOCK_STARSHIP=" + p.Environment["BDOCK_STARSHIP"], "LOAD_MARKER=" + marker, "PROMPT_MARKER=" + prompt}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bash %t: %v %s", interactive, err, out)
		}
		_, err := os.Stat(marker)
		if interactive != (err == nil) {
			t.Fatalf("wrong source behavior interactive=%t: %v", interactive, err)
		}
		if _, err := os.Stat(prompt); !os.IsNotExist(err) {
			t.Fatalf("prompt fence failed: %v", err)
		}
	}
}
func copyMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func copyEntries(in map[string]EntryKind) map[string]EntryKind {
	out := map[string]EntryKind{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
