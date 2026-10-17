// SPDX-License-Identifier: EUPL-1.2

package workspacecfg_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func TestValidateGitInvocation(t *testing.T) {
	t.Setenv("GIT_TRACE", "1")
	t.Setenv("GIT_DIR", "/elsewhere")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.fsmonitor=x'")
	t.Setenv("GIT_EXEC_PATH", "/elsewhere")
	t.Setenv("GIT_INDEX_FILE", "/elsewhere")
	dir := validDir(t)
	minimalGitDir(t, dir)
	write(t, dir, ".git/index", "")
	write(t, dir, ".gitmodules", gitmodulesApp)
	f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		switch sub {
		case "ls-files":
			return ok("100644 " + sha + " 0\t.gitmodules\x00160000 " + sha + " 0\tprojects/app\x00")
		case "ls-tree":
			return ok("100644 blob " + sha + "\t.gitmodules\x00160000 commit " + sha + "\tprojects/app\x00")
		case "cat-file":
			return ok(gitmodulesApp)
		}
		return healthyHandler(sub, c)
	}}
	r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/usr/bin/git-fixed")})
	if !r.Valid || !has(r, "submodule.pinned") {
		t.Fatalf("%+v", r.Findings)
	}
	want := []string{"--version", "rev-parse", "ls-files", "ls-tree", "cat-file"}
	if got := f.subs(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("calls %v, want %v", got, want)
	}
	for _, c := range f.calls {
		if c.Path != "/usr/bin/git-fixed" || c.Dir != "/" {
			t.Errorf("path %q dir %q", c.Path, c.Dir)
		}
		if subcommand(c.Args) == "--version" {
			continue
		}
		joined := " " + strings.Join(c.Args, " ") + " "
		for _, need := range []string{
			" --git-dir=" + dir + "/.git ", " --work-tree=" + dir + " ", " -c core.fsmonitor=false ", " -c core.hooksPath=/dev/null ",
			" -c protocol.allow=never ", " --no-pager ", " --no-optional-locks ", " --no-replace-objects ",
		} {
			if !strings.Contains(joined, need) {
				t.Errorf("%s: argument %q missing in %v", subcommand(c.Args), strings.TrimSpace(need), c.Args)
			}
		}
		for _, p := range []string{"file", "git", "ssh", "http", "https", "ext"} {
			if !strings.Contains(joined, " -c protocol."+p+".allow=never ") {
				t.Errorf("protocol %s not denied: %v", p, c.Args)
			}
		}
		if strings.Count(joined, " -c ") != 9 {
			t.Errorf("unexpected -c count: %v", c.Args)
		}
	}
	env := map[string]string{}
	for _, c := range f.calls {
		for _, kv := range c.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0",
		"GIT_NO_LAZY_FETCH": "1", "GIT_PAGER": "cat", "HOME": "/nonexistent",
	} {
		if env[k] != v {
			t.Errorf("%s=%q, want %q", k, env[k], v)
		}
	}
	if env["GIT_ALLOW_PROTOCOL"] == "" || strings.ContainsAny(env["GIT_ALLOW_PROTOCOL"], ":") {
		t.Errorf("GIT_ALLOW_PROTOCOL=%q", env["GIT_ALLOW_PROTOCOL"])
	}
	for _, p := range []string{"file", "git", "ssh", "http", "https", "ext"} {
		if env["GIT_ALLOW_PROTOCOL"] == p {
			t.Errorf("GIT_ALLOW_PROTOCOL matches %s", p)
		}
	}
	for k := range env {
		switch k {
		case "PATH", "HOME", "LC_ALL", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_OPTIONAL_LOCKS", "GIT_TERMINAL_PROMPT",
			"GIT_NO_LAZY_FETCH", "GIT_PAGER", "GIT_ALLOW_PROTOCOL":
		default:
			t.Errorf("unexpected environment variable %s", k)
		}
	}
}

func TestValidateGitSubcommandsAreTheSevenVectors(t *testing.T) {
	// Unborn HEAD with an index .gitmodules uses the staged blob vector.
	dir := validDir(t)
	minimalGitDir(t, dir)
	f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		switch sub {
		case "rev-parse":
			return workspacecfg.GitOut{Exit: 1}, nil
		case "symbolic-ref":
			return ok("refs/heads/main\n")
		case "ls-files":
			return ok("100644 " + sha + " 0\t.gitmodules\x00")
		case "cat-file":
			return ok(gitmodulesApp)
		}
		return healthyHandler(sub, c)
	}}
	r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
	if !has(r, "git.head_missing") {
		t.Fatalf("%+v", r.Findings)
	}
	if got := strings.Join(f.subs(), ","); got != "--version,rev-parse,symbolic-ref,ls-files,cat-file" {
		t.Errorf("calls %s", got)
	}
	last := f.calls[len(f.calls)-1].Args
	if last[len(last)-3] != "cat-file" || last[len(last)-2] != "blob" || last[len(last)-1] != ":.gitmodules" {
		t.Errorf("last call %v", last)
	}
}

func TestValidateOldGit(t *testing.T) {
	for _, tc := range []struct {
		version string
		bad     bool
	}{
		{"git version 2.39.4\n", true}, {"git version 2.38.9\n", true}, {"git version 1.9.0\n", true},
		{"git version 2.39.5\n", false}, {"git version 2.39.6\n", false}, {"git version 2.40.0\n", false},
		{"git version 2.50.1 (Apple Git-155)\n", false}, {"git version 3.0.0\n", false},
		{"git version 2.39\n", true}, {"garbage\n", true}, {"", true}, {"git version x.y.z\n", true},
	} {
		dir := validDir(t)
		minimalGitDir(t, dir)
		f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "--version" {
				return ok(tc.version)
			}
			return healthyHandler(sub, c)
		}}
		r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
		if got := has(r, "git.unavailable"); got != tc.bad {
			t.Errorf("%q: unavailable=%v: %+v", tc.version, got, r.Findings)
		}
		if tc.bad && (r.Valid || len(f.calls) != 1 || find(r, "git.unavailable").Severity != workspacecfg.SevError) {
			t.Errorf("%q: valid=%v calls=%d", tc.version, r.Valid, len(f.calls))
		}
	}
}

func TestValidateGitUnavailableIsError(t *testing.T) {
	for name, look := range map[string]func() (string, error){
		"absent":   func() (string, error) { return "", exec.ErrNotFound },
		"dot":      func() (string, error) { return "", exec.ErrDot },
		"mac shim": func() (string, error) { return "", workspacecfg.ErrGitShim },
	} {
		dir := validDir(t)
		minimalGitDir(t, dir)
		f := &fakeGit{handler: healthyHandler}
		r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: look})
		if r.Valid || errorCodes(r) != "git.unavailable" || len(f.calls) != 0 {
			t.Errorf("%s: %+v calls=%d", name, r.Findings, len(f.calls))
		}
	}
	// Without a .git nothing about Git is an error.
	r := validate(t, validDir(t), workspacecfg.Options{LookGit: func() (string, error) { return "", exec.ErrNotFound }})
	if !r.Valid || has(r, "git.unavailable") {
		t.Errorf("%+v", r.Findings)
	}
}

func TestGitShimDetection(t *testing.T) {
	exists := func(set ...string) func(string) bool {
		return func(p string) bool {
			for _, s := range set {
				if s == p {
					return true
				}
			}
			return false
		}
	}
	for _, tc := range []struct {
		goos, path string
		present    []string
		shim       bool
	}{
		{"darwin", "/usr/bin/git", nil, true},
		{"darwin", "/usr/bin/git", []string{"/Library/Developer/CommandLineTools/usr/bin/git"}, false},
		{"darwin", "/usr/bin/git", []string{"/Applications/Xcode.app/Contents/Developer/usr/bin/git"}, false},
		{"darwin", "/opt/homebrew/bin/git", nil, false},
		{"linux", "/usr/bin/git", nil, false},
	} {
		if got := workspacecfg.IsMacOSInstallerShim(tc.goos, tc.path, exists(tc.present...)); got != tc.shim {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

func TestValidateGitUnsafePath(t *testing.T) {
	cases := map[string]func(t *testing.T, d string){
		".git symlink": func(t *testing.T, d string) {
			symlink(t, d, ".git", d+"/real-git")
			minimalGitDirAt(t, d, "real-git")
		},
		".git gitfile": func(t *testing.T, d string) { remove(t, d, ".git"); write(t, d, ".git", "gitdir: /elsewhere\n") },
		".git fifo":    func(t *testing.T, d string) { fifo(t, d, ".git") },
		"HEAD fifo":    func(t *testing.T, d string) { fifo(t, d, ".git/HEAD") },
		"HEAD socket":  func(t *testing.T, d string) { socket(t, d, ".git/HEAD") },
		"HEAD directory": func(t *testing.T, d string) {
			remove(t, d, ".git/HEAD")
			write(t, d, ".git/HEAD/x", "")
		},
		"HEAD symlink":    func(t *testing.T, d string) { symlink(t, d, ".git/HEAD", "/etc/hostname") },
		"HEAD missing":    func(t *testing.T, d string) { remove(t, d, ".git/HEAD") },
		"index fifo":      func(t *testing.T, d string) { fifo(t, d, ".git/index") },
		"index socket":    func(t *testing.T, d string) { socket(t, d, ".git/index") },
		"index directory": func(t *testing.T, d string) { write(t, d, ".git/index/x", "") },
		"index symlink":   func(t *testing.T, d string) { symlink(t, d, ".git/index", "/etc/hostname") },
		"config fifo":     func(t *testing.T, d string) { fifo(t, d, ".git/config") },
		"config socket":   func(t *testing.T, d string) { socket(t, d, ".git/config") },
		"config directory": func(t *testing.T, d string) {
			remove(t, d, ".git/config")
			write(t, d, ".git/config/x", "")
		},
		"config symlink": func(t *testing.T, d string) { symlink(t, d, ".git/config", "/etc/hostname") },
		"config missing": func(t *testing.T, d string) { remove(t, d, ".git/config") },
		"commondir":      func(t *testing.T, d string) { write(t, d, ".git/commondir", "../elsewhere\n") },
		"alternates":     func(t *testing.T, d string) { write(t, d, ".git/objects/info/alternates", "/elsewhere/objects\n") },
		"objects symlink": func(t *testing.T, d string) {
			remove(t, d, ".git/objects")
			symlink(t, d, ".git/objects", "/etc")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := validDir(t)
			minimalGitDir(t, dir)
			setup(t, dir)
			f := &fakeGit{handler: healthyHandler}
			r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
			if r.Valid || errorCodes(r) != "git.unsafe_path" {
				t.Errorf("%+v", r.Findings)
			}
			if len(f.calls) != 0 {
				t.Errorf("Git ran (%v) although .git is unsafe", f.subs())
			}
			for _, fd := range r.Findings {
				if strings.Contains(fd.Message+fd.Subject, "elsewhere") || strings.Contains(fd.Message, "/etc/hostname") {
					t.Errorf("link target disclosed: %+v", fd)
				}
			}
		})
	}
}

// minimalGitDirAt builds the pre-check files below another directory name.
func minimalGitDirAt(t *testing.T, dir, name string) {
	t.Helper()
	write(t, dir, name+"/HEAD", "ref: refs/heads/main\n")
	write(t, dir, name+"/config", "")
	write(t, dir, name+"/objects/info/.keep", "")
}

// TestValidateGitFailureIsError: a failing, timed-out or unparsable Git is an
// error, never a warning.
func TestValidateGitFailureIsError(t *testing.T) {
	t.Run("injected failures", injectedGitFailures)
	t.Run("blocked git", blockedGit)
}

func injectedGitFailures(t *testing.T) {
	for name, handler := range map[string]func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error){
		"rev-parse exit 128": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			return failing(sub, c, "rev-parse")
		},
		"rev-parse exit 1 noisy": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) { return noisyUnborn(sub, c) },
		"ls-files exit 128": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			return failing(sub, c, "ls-files")
		},
		"ls-tree exit 128": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			return failing(sub, c, "ls-tree")
		},
		"ls-files unparsable": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			return garbage(sub, c, "ls-files")
		},
		"ls-tree unparsable": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			return garbage(sub, c, "ls-tree")
		},
		"rev-parse unparsable": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			return garbage(sub, c, "rev-parse")
		},
		"exec error":           func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) { return execFails(sub, c) },
		"detached unborn":      func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) { return detached(sub, c) },
		"symbolic-ref fails":   func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) { return symFails(sub, c) },
		"unmerged index entry": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) { return unmerged(sub, c) },
		"cat-file exit 128":    func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) { return catFails(sub, c) },
	} {
		dir := validDir(t)
		minimalGitDir(t, dir)
		write(t, dir, "projects/.gitkeep", "")
		f := &fakeGit{handler: handler}
		r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
		if r.Valid || errorCodes(r) != "git.listing_failed" {
			t.Errorf("%s: valid=%v %+v", name, r.Valid, r.Findings)
		}
		if find(r, "git.listing_failed").Severity != workspacecfg.SevError {
			t.Errorf("%s: not an error", name)
		}
	}
}

func failing(sub string, c workspacecfg.GitCmd, which string) (workspacecfg.GitOut, error) {
	if sub == which {
		return workspacecfg.GitOut{Exit: 128, Stderr: []byte("fatal: CANARY secret\n")}, nil
	}
	return healthyHandler(sub, c)
}

func noisyUnborn(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	if sub == "rev-parse" {
		return workspacecfg.GitOut{Exit: 1, Stderr: []byte("warning: something\n")}, nil
	}
	return healthyHandler(sub, c)
}

func garbage(sub string, c workspacecfg.GitCmd, which string) (workspacecfg.GitOut, error) {
	if sub == which {
		return ok("not the expected format\x00")
	}
	return healthyHandler(sub, c)
}

func execFails(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	if sub == "ls-files" {
		return workspacecfg.GitOut{}, errors.New("exec: failed")
	}
	return healthyHandler(sub, c)
}

func detached(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	switch sub {
	case "rev-parse", "symbolic-ref":
		return workspacecfg.GitOut{Exit: 1}, nil
	}
	return healthyHandler(sub, c)
}

func symFails(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	switch sub {
	case "rev-parse":
		return workspacecfg.GitOut{Exit: 1}, nil
	case "symbolic-ref":
		return workspacecfg.GitOut{Exit: 128}, nil
	}
	return healthyHandler(sub, c)
}

func unmerged(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	if sub == "ls-files" {
		return ok("100644 " + sha + " 2\tfile\x00")
	}
	return healthyHandler(sub, c)
}

func catFails(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
	switch sub {
	case "ls-files", "ls-tree":
		return ok("100644 " + sha + " 0\t.gitmodules\x00")
	case "cat-file":
		return workspacecfg.GitOut{Exit: 128}, nil
	}
	return healthyHandler(sub, c)
}

func TestValidateGitFailureDoesNotEchoGitOutput(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		return failing(sub, c, "ls-files")
	}}
	for _, fd := range validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")}).Findings {
		if strings.Contains(fd.Message+fd.Subject+fd.Remediation, "CANARY") {
			t.Errorf("Git stderr echoed: %+v", fd)
		}
	}
}

func TestValidateGitLimits(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	for name, h := range map[string]func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error){
		"output cap": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "ls-files" {
				return workspacecfg.GitOut{Stdout: []byte("x"), Truncated: true}, nil
			}
			return healthyHandler(sub, c)
		},
		"entry cap": func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "ls-tree" {
				return ok(strings.Repeat("100644 blob "+sha+"\tf\x00", workspacecfg.MaxListingEntries+1))
			}
			return healthyHandler(sub, c)
		},
	} {
		r := validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: h}, LookGit: fixedGit("/git")})
		if r.Valid || errorCodes(r) != "git.limit_exceeded" {
			t.Errorf("%s: %+v", name, r.Findings)
		}
	}
	// The command asks Git for no more than the cap allows.
	f := &fakeGit{handler: healthyHandler}
	validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
	for _, c := range f.calls {
		if subcommand(c.Args) != "--version" && (c.MaxOutput <= 0 || c.MaxOutput > 8<<20) {
			t.Errorf("%s: MaxOutput %d", subcommand(c.Args), c.MaxOutput)
		}
	}
}

func TestValidateGitmodulesBlobTooLargeFromGit(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		switch sub {
		case "ls-files", "ls-tree":
			return ok("100644 " + sha + " 0\t.gitmodules\x00")
		case "cat-file":
			return workspacecfg.GitOut{Stdout: make([]byte, 65537), Truncated: true}, nil
		}
		return healthyHandler(sub, c)
	}}
	// ls-tree output has another shape; use the right one for it.
	g := f.handler
	f.handler = func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		if sub == "ls-tree" {
			return ok("100644 blob " + sha + "\t.gitmodules\x00")
		}
		return g(sub, c)
	}
	r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
	if errorCodes(r) != "gitmodules.too_large" {
		t.Errorf("%+v", r.Findings)
	}
}

// fakeClock advances only when told to.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestValidateTimeBudget(t *testing.T) {
	t.Run("during Git work", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(5000, 0)}
		dir := validDir(t)
		minimalGitDir(t, dir)
		f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "rev-parse" {
				clock.now = clock.now.Add(6 * time.Second) // the call used up the budget
				return workspacecfg.GitOut{}, context.DeadlineExceeded
			}
			return healthyHandler(sub, c)
		}}
		r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git"), Clock: clock.Now})
		if r.Valid || errorCodes(r) != "git.listing_failed" || has(r, "findings.time_budget_exhausted") {
			t.Errorf("%+v", r.Findings)
		}
		if got := strings.Join(f.subs(), ","); got != "--version,rev-parse" {
			t.Errorf("later calls ran: %s", got)
		}
	})
	t.Run("budget used up between Git calls", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(5000, 0)}
		dir := validDir(t)
		minimalGitDir(t, dir)
		f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			if sub == "rev-parse" {
				clock.now = clock.now.Add(6 * time.Second)
			}
			return healthyHandler(sub, c)
		}}
		r := validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git"), Clock: clock.Now})
		if errorCodes(r) != "git.listing_failed" || len(f.calls) != 2 {
			t.Errorf("%+v calls=%v", r.Findings, f.subs())
		}
	})
	t.Run("each call gets the remainder", func(t *testing.T) {
		clock := &fakeClock{now: time.Unix(5000, 0)}
		dir := validDir(t)
		minimalGitDir(t, dir)
		var deadlines []time.Duration
		f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
			clock.now = clock.now.Add(time.Second)
			deadlines = append(deadlines, c.Timeout)
			return healthyHandler(sub, c)
		}}
		validate(t, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git"), Clock: clock.Now})
		for i := 1; i < len(deadlines); i++ {
			if deadlines[i] >= deadlines[i-1] || deadlines[i] <= 0 || deadlines[0] > workspacecfg.DefaultBudget {
				t.Errorf("timeouts %v", deadlines)
			}
		}
	})
}

func TestValidateInterruptedDuringGit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := validDir(t)
	minimalGitDir(t, dir)
	f := &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		if sub == "rev-parse" {
			cancel()
			return workspacecfg.GitOut{}, context.Canceled
		}
		return healthyHandler(sub, c)
	}}
	_, err := workspacecfg.Validate(ctx, dir, workspacecfg.Options{Git: f, LookGit: fixedGit("/git")})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("%v", err)
	}
}

func TestValidateDeclaredProjectPinnedByGitlink(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	write(t, dir, "workharbor.json", `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[],"project":{"url":"https://f.example/o/app.git","path":"projects/app"}}`)
	pinned := func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		switch sub {
		case "ls-files":
			return ok("100644 " + sha + " 0\t.gitmodules\x00160000 " + sha + " 0\tprojects/app\x00")
		case "ls-tree":
			return ok("100644 blob " + sha + "\t.gitmodules\x00160000 commit " + sha + "\tprojects/app\x00")
		case "cat-file":
			return ok(gitmodulesApp)
		}
		return healthyHandler(sub, c)
	}
	r := validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: pinned}, LookGit: fixedGit("/git")})
	if !r.Valid || has(r, "project.declared_not_pinned") {
		t.Errorf("pinned: %+v", r.Findings)
	}
	r = validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: healthyHandler}, LookGit: fixedGit("/git")})
	if !has(r, "project.declared_not_pinned") {
		t.Errorf("not pinned: %+v", r.Findings)
	}
	// A failing Git leaves its own errors; no extra declaration warning.
	r = validate(t, dir, workspacecfg.Options{Git: &fakeGit{handler: func(sub string, c workspacecfg.GitCmd) (workspacecfg.GitOut, error) {
		return failing(sub, c, "ls-files")
	}}, LookGit: fixedGit("/git")})
	if has(r, "project.declared_not_pinned") {
		t.Errorf("failed: %+v", r.Findings)
	}
}

func TestGitFreeProfileRunsNoGit(t *testing.T) {
	dir := validDir(t)
	minimalGitDir(t, dir)
	f := &fakeGit{handler: healthyHandler}
	r := validate(t, dir, workspacecfg.Options{GitFree: true, Git: f, LookGit: fixedGit("/git")})
	if len(f.calls) != 0 || len(r.Findings) != 0 {
		t.Errorf("calls %v findings %+v", f.subs(), r.Findings)
	}
}
