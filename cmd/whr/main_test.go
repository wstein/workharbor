package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/gittest"
)

func TestVersionPrintsDataOnStdoutAndNothingOnStderr(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != exitcode.OK {
		t.Fatalf("exit %d", code)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", errOut.String())
	}
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+\S* \(commit \S+, (clean|dirty)\)\n$`).MatchString(out.String()) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version", "--json"}, &out, &errOut); code != exitcode.OK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var got struct {
		SchemaVersion int    `json:"schema_version"`
		Version       string `json:"version"`
		Commit        string `json:"commit"`
		Dirty         *bool  `json:"dirty"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v: %q", err, out.String())
	}
	if got.SchemaVersion != 1 || got.Version == "" || got.Commit == "" || got.Dirty == nil {
		t.Errorf("json = %+v, want a schema_version, a version, a commit and the dirty flag", got)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Errorf("the data must be one line: %q", out.String())
	}
}

func TestVersionRefusesUnknownOptions(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version", "--yaml"}, &out, &errOut); code != exitcode.Usage {
		t.Errorf("exit %d, want Usage", code)
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "--yaml") {
		t.Errorf("stdout %q, stderr %q: the error is human text on stderr", out.String(), errOut.String())
	}
	if code := run([]string{"--version"}, &out, &errOut); code != exitcode.OK {
		t.Errorf("--version exit %d", code)
	}
}

// versionPattern matches what git describe gives: vX.Y.Z, vX.Y.Z-<pre>, and
// either followed by -<n>-g<sha> after the tag.
var versionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?(-\d+-g[0-9a-f]{7,})?$`)

func TestVersionPattern(t *testing.T) {
	for v, ok := range map[string]bool{
		"v0.1.0":                       true,
		"v0.1.0-alpha.2":               true,
		"v0.1.0-3-g392443d":            true,
		"v0.1.0-alpha.2-1-g392443d":    true,
		"v0.1.0-alpha.2-1-gXYZ":        false,
		"0.1.0":                        false,
		"v0.1":                         false,
		"v0.1.0-alpha.2-1-g392443d-x!": false,
	} {
		if versionPattern.MatchString(v) != ok {
			t.Errorf("%q: match = %v, want %v", v, !ok, ok)
		}
	}
}

// make build stamps the binary: the version is not empty, the commit is the
// checkout's, and the build is trimpath.
func TestMakeBuildStampsTheBinary(t *testing.T) {
	for _, tool := range []string{"make", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not available", tool)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("not built from a git checkout")
	}
	bin := filepath.Join(t.TempDir(), "whr")
	build := exec.CommandContext(t.Context(), "make", "-s", "build", "BIN="+bin) //nolint:gosec // fixed arguments
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("make build: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(t.Context(), bin, "version", "--json").Output() //nolint:gosec // the binary just built
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	head := gittest.Git(t.Context(), "", root, nil, "rev-parse", "--short=7", "HEAD")
	want, err := head.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != strings.TrimSpace(string(want)) {
		t.Errorf("commit = %q, want %q", got.Commit, strings.TrimSpace(string(want)))
	}
	if !versionPattern.MatchString(got.Version) {
		t.Errorf("version = %q, want a tag version or a git describe form", got.Version)
	}
	if got.Date == "" {
		t.Error("the build date was not stamped")
	}
	// -trimpath: the binary does not carry the build directory.
	data, err := os.ReadFile(bin) //nolint:gosec // the binary just built
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(root+"/cmd/whr")) {
		t.Error("the binary contains the build directory: -trimpath was not used")
	}
}

// whr tools build fills the store from the pins, checks the download, adds the
// shim and makes the profile; a mismatch stores nothing and exits non-zero.
func TestToolsBuild(t *testing.T) {
	bin := []byte("#!/bin/sh\necho claude\n")
	sum := sha256.Sum256(bin)
	hash := hex.EncodeToString(sum[:])
	vendor := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rel/9.9.9/manifest.json":
			fmt.Fprintf(w, `{"platforms":{"linux-arm64":{"checksum":%q}}}`, hash)
		case "/rel/9.9.9/linux-arm64/claude":
			_, _ = w.Write(bin)
		default:
			http.NotFound(w, r)
		}
	}))
	defer vendor.Close()
	toolsClient = vendor.Client() // trusts the test server's certificate
	t.Cleanup(func() { toolsClient = nil })
	dir := t.TempDir()
	pins := filepath.Join(dir, "pins.json")
	writePins := func(h string) {
		body := fmt.Sprintf(`{"tools":[{"name":"claude","version":"9.9.9","platform":"linux-arm64","base_url":%q,"sha256":%q}]}`, vendor.URL+"/rel", h)
		if err := os.WriteFile(pins, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	shim := filepath.Join(dir, "whr-shim")
	if err := os.WriteFile(shim, []byte("shim"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "tools")
	t.Cleanup(func() { // the store is read-only
		_ = filepath.WalkDir(store, func(p string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(p, 0o750) //nolint:gosec // a test cleaning up
			}
			return nil
		})
	})

	writePins(hex.EncodeToString(make([]byte, 32))) // a wrong pin
	var out, errOut bytes.Buffer
	if code := run([]string{"tools", "build", "-store", store, "-pins", pins}, &out, &errOut); code == exitcode.OK {
		t.Fatalf("a checksum mismatch exited 0: %s", errOut.String())
	}
	if out.Len() != 0 || !strings.Contains(errOut.String(), "checksum") {
		t.Errorf("stdout %q stderr %q", out.String(), errOut.String())
	}
	if entries, _ := os.ReadDir(filepath.Join(store, "store")); len(entries) != 0 {
		t.Errorf("a mismatch stored %d entries", len(entries))
	}

	writePins(hash)
	out.Reset()
	errOut.Reset()
	if code := run([]string{"tools", "build", "-store", store, "-pins", pins, "-shim", shim}, &out, &errOut); code != exitcode.OK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "store ") || !strings.Contains(lines[0], hash) || !strings.HasPrefix(lines[2], "profile ") {
		t.Errorf("stdout = %q, want two store lines and the profile", out.String())
	}
	profile := filepath.Join(store, "profiles", "claude-9.9.9", "bin")
	for _, tool := range []string{"claude", "whr-shim"} {
		if _, err := os.Stat(filepath.Join(profile, tool)); err != nil {
			t.Errorf("the profile lacks %s: %v", tool, err)
		}
	}
	if code := run([]string{"tools"}, &out, &errOut); code != exitcode.Usage {
		t.Errorf("whr tools alone: exit %d, want Usage", code)
	}
}

// make install builds from the committed tree: it installs whr, the shim and
// the proxy with the version stamp, and refuses a dirty tree and a commit that
// is not on origin/main (D34).
func TestMakeInstallBuildsCommittedCodeAndRefusesADirtyTree(t *testing.T) {
	for _, tool := range []string{"make", "git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not available", tool)
		}
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("not built from a git checkout")
	}
	clone := filepath.Join(t.TempDir(), "src")
	gitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := gittest.Git(t.Context(), "", dir, gittest.Identity, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn(t.TempDir(), "clone", "--quiet", "--local", root, clone)
	// The clone's HEAD is whatever the checkout has, a topic branch included,
	// so origin/main is pinned to it: this commit counts as merged.
	gitIn(clone, "update-ref", "refs/remotes/origin/main", "HEAD")
	prefix := t.TempDir()
	install := func() ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), "make", "-s", "install", "PREFIX="+prefix) //nolint:gosec // fixed arguments
		cmd.Dir = clone
		return cmd.CombinedOutput()
	}

	if out, err := install(); err != nil {
		t.Fatalf("make install on a clean clone: %v\n%s", err, out)
	}
	for _, p := range []string{"bin/whr", "libexec/whr/whr-shim-linux-arm64", "libexec/whr/whr-proxy-linux-arm64"} {
		if info, err := os.Stat(filepath.Join(prefix, p)); err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s was not installed (%v)", p, err)
		}
	}
	out, err := exec.CommandContext(t.Context(), filepath.Join(prefix, "bin", "whr"), "version", "--json").Output() //nolint:gosec // the binary just built
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version string `json:"version"`
		Dirty   bool   `json:"dirty"`
	}
	if err := json.Unmarshal(out, &got); err != nil || got.Dirty || got.Version == "" {
		t.Errorf("the installed whr reports %s (%v): it must be stamped and clean", out, err)
	}

	// A dirty tree is refused, and nothing new is installed.
	if err := os.Remove(filepath.Join(prefix, "bin", "whr")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg, err := install()
	if err == nil || !strings.Contains(string(msg), "dirty tree") {
		t.Fatalf("make install on a dirty tree = %v\n%s", err, msg)
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "whr")); err == nil {
		t.Error("a dirty tree still installed whr")
	}

	// A clean commit that is not on origin/main is refused too.
	gitIn(clone, "-c", "user.name=t", "-c", "user.email=t@example.test", "-c", "commit.gpgsign=false",
		"commit", "--quiet", "--allow-empty", "-m", "test: an unmerged commit")
	if err := os.Remove(filepath.Join(clone, "stray.txt")); err != nil {
		t.Fatal(err)
	}
	msg, err = install()
	if err == nil || !strings.Contains(string(msg), "not on origin/main") {
		t.Fatalf("make install of an unmerged commit = %v\n%s", err, msg)
	}
	if _, err := os.Stat(filepath.Join(prefix, "bin", "whr")); err == nil {
		t.Error("an unmerged commit still installed whr")
	}
}

func TestCommandSignalsRespectTheSignInShellChildScope(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ordinary interrupt", "child interrupt", "child terminate"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, bin, "-test.run=^TestCommandSignalContextHelper$")
			child.Env = []string{"WHR_TEST_SIGNALS=" + mode}
			if out, err := child.CombinedOutput(); err != nil {
				t.Fatalf("signal helper: %v %s", err, out)
			}
		})
	}
}

func TestCommandSignalContextHelper(t *testing.T) {
	mode := os.Getenv("WHR_TEST_SIGNALS")
	if mode == "" {
		return
	}
	ctx, stop, scope := commandSignalContext()
	defer stop()
	if mode == "ordinary interrupt" {
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
	} else {
		restore := scope()
		defer restore()
		sig := syscall.SIGINT
		if mode == "child terminate" {
			sig = syscall.SIGTERM
		}
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		if mode == "child interrupt" {
			select {
			case <-ctx.Done():
				t.Fatal("child interrupt cancelled the hold")
			case <-time.After(100 * time.Millisecond):
			}
			restore()
			if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
				t.Fatal(err)
			}
		}
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("ordinary interrupt or terminate did not cancel command")
	}
}
