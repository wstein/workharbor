package scripts

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/gittest"
)

func bash(t *testing.T, env []string, script string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "bash", "-c", script) //nolint:gosec // a test script
	hasHome := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			hasHome = true
			break
		}
	}
	if hasHome {
		cmd.Env = env
	} else {
		cmd.Env = gittest.Env(t.TempDir(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// semver_lt decides whether a tag is a downgrade.
func TestSemverLt(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		a, b  string
		older bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.1.0", false},
		{"v0.9.0", "v0.10.0", true},
		{"v0.10.0", "v0.9.0", false},
		{"v1.0.0-rc1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc1", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0-rc2", "v1.0.0-rc10", true},
		{"v0.0.0-3-gabc", "v0.1.0", true},
	} {
		_, err := bash(t, nil, `source install-release.sh; semver_lt `+c.a+` `+c.b)
		if (err == nil) != c.older {
			t.Errorf("semver_lt %s %s = %v, want %v", c.a, c.b, err == nil, c.older)
		}
	}
}

// release makes a folder with the three files of a release, and a prefix with
// a fake installed whr of the given version, and fakes for uname and gh that
// record how gh was called.
type release struct {
	dir, prefix, bin, ghlog string
}

func tarball(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path) //nolint:gosec // a test path
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
}

func newRelease(t *testing.T, version, installed string) release {
	t.Helper()
	r := release{dir: t.TempDir(), prefix: filepath.Join(t.TempDir(), "whr"), bin: t.TempDir()}
	r.ghlog = filepath.Join(r.bin, "gh.log")
	mac, guest := "whr_"+version+"_darwin_arm64.tar.gz", "whr-guest_"+version+"_linux_arm64.tar.gz"
	tarball(t, filepath.Join(r.dir, mac), map[string]string{"whr": "#!/bin/sh\necho " + "v" + version + " abc\n"})
	tarball(t, filepath.Join(r.dir, guest), map[string]string{"whr-shim": "x", "whr-proxy": "y"})
	var sums strings.Builder
	for _, f := range []string{mac, guest} {
		b, _ := os.ReadFile(filepath.Join(r.dir, f)) //nolint:gosec // a test path
		h := sha256.Sum256(b)
		sums.WriteString(hex.EncodeToString(h[:]) + "  " + f + "\n")
	}
	if err := os.WriteFile(filepath.Join(r.dir, "checksums.txt"), []byte(sums.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(r.bin, name), []byte(body), 0o700); err != nil { //nolint:gosec // an executable test fake
			t.Fatal(err)
		}
	}
	write("uname", "#!/bin/sh\ncase \"$1\" in -s) echo Darwin;; -m) echo arm64;; esac\n")
	write("gh", "#!/bin/sh\necho \"$@\" >> '"+r.ghlog+"'\nif [ \"$1\" = api ]; then echo 0123456789abcdef0123456789abcdef01234567; fi\nexit 0\n")
	if installed != "" {
		if err := os.MkdirAll(filepath.Join(r.prefix, "bin"), 0o750); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(r.bin, "installed-whr-ran")
		if err := os.WriteFile(filepath.Join(r.prefix, "bin", "whr"), []byte("#!/bin/sh\ntouch '"+marker+"'\necho "+installed+" abc\n"), 0o700); err != nil { //nolint:gosec // an executable test fake
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(r.prefix, "libexec", "whr"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.prefix, "libexec", "whr", "VERSION"), []byte(installed+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r release) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return bash(t, []string{"PATH=" + r.bin + ":" + os.Getenv("PATH"), "WHR_RELEASE_DIR=" + r.dir}, "./install-release.sh "+strings.Join(args, " "))
}

// The attestation is pinned to this tag, its commit and a hosted runner, and the
// repository whose attestations are trusted is named.
func TestTheAttestationIsPinnedToTheTag(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	out, err := r.run(t, "v0.2.0", r.prefix)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "trusting attestations of wstein/workharbor for v0.2.0") {
		t.Errorf("the trusted repository is not named:\n%s", out)
	}
	log, _ := os.ReadFile(r.ghlog) //nolint:gosec // a test path
	for _, want := range []string{"--source-ref refs/tags/v0.2.0", "--deny-self-hosted-runners", "--source-digest 0123456789abcdef0123456789abcdef01234567", "--signer-workflow wstein/workharbor/.github/workflows/release.yml"} {
		if strings.Count(string(log), want) != 2 { // once for each archive
			t.Errorf("gh attestation verify lacks %q for each archive:\n%s", want, log)
		}
	}
	if _, err := os.Stat(filepath.Join(r.prefix, "bin", "whr")); err != nil {
		t.Errorf("nothing was installed: %v", err)
	}
}

// An older release than the installed one is refused unless the human says so, and an
// installed version that cannot be read counts as a reason to ask.
func TestAnOlderReleaseIsRefusedUnlessAllowed(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	r := newRelease(t, "0.1.0", "v0.2.0")
	if out, err := r.run(t, "v0.1.0", r.prefix); err == nil || !strings.Contains(out, "older than the installed v0.2.0") {
		t.Fatalf("a downgrade was not refused: %v\n%s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(r.prefix, "bin", "whr")); err != nil || !strings.Contains(string(b), "v0.2.0") {
		t.Error("the installed whr was replaced by a refused downgrade")
	}
	before, err := os.Stat(filepath.Join(r.prefix, "bin", "whr"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := r.run(t, "v0.1.0", r.prefix, "--allow-downgrade"); err != nil {
		t.Fatalf("--allow-downgrade: %v\n%s", err, out)
	}
	if after, err := os.Stat(filepath.Join(r.prefix, "bin", "whr")); err != nil || os.SameFile(before, after) {
		t.Errorf("whr was rewritten in place, not replaced by rename: %v", err)
	}
	same := newRelease(t, "0.2.0", "v0.2.0")
	if out, err := same.run(t, "v0.2.0", same.prefix); err != nil {
		t.Errorf("reinstalling the same version: %v\n%s", err, out)
	}
	unreadable := newRelease(t, "0.2.0", "garbage")
	if out, err := unreadable.run(t, "v0.2.0", unreadable.prefix); err == nil || !strings.Contains(out, "cannot read the installed version") {
		t.Errorf("an unreadable installed version: %v\n%s", err, out)
	}
}

// The installed whr is never run before the release is verified: its version comes
// from the file the installer wrote.
func TestTheInstalledBinaryIsNotRunBeforeVerification(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.1.0", "v0.2.0")
	if out, err := r.run(t, "v0.1.0", r.prefix); err == nil {
		t.Fatalf("a downgrade was not refused:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(r.bin, "installed-whr-ran")); err == nil {
		t.Error("the installed whr was executed before verification")
	}
	// An install without the version file cannot be read, so it needs the flag.
	if err := os.Remove(filepath.Join(r.prefix, "libexec", "whr", "VERSION")); err != nil {
		t.Fatal(err)
	}
	if out, err := r.run(t, "v0.2.0", r.prefix); err == nil || !strings.Contains(out, "cannot read the installed version") {
		t.Errorf("a missing version file: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(r.bin, "installed-whr-ran")); err == nil {
		t.Error("the installed whr was executed to find its version")
	}
}

// WHR_RELEASE_REPO must be owner/name and is refused without --trust-release-repo.
func TestTheReleaseRepoOverrideNeedsConfirmation(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	run := func(repo string, args ...string) (string, error) {
		return bash(t, []string{"PATH=" + r.bin + ":" + os.Getenv("PATH"), "WHR_RELEASE_DIR=" + r.dir, "WHR_RELEASE_REPO=" + repo},
			"./install-release.sh "+strings.Join(args, " "))
	}
	if out, err := run("someone/fork", "v0.2.0", r.prefix); err == nil || !strings.Contains(out, "--trust-release-repo") {
		t.Errorf("an unconfirmed override was accepted: %v\n%s", err, out)
	}
	for _, bad := range []string{"a/b/c", "nogash", "a/b?x=1", "a/../b", "../..", "a b/c"} {
		if out, err := run("'"+bad+"'", "v0.2.0", r.prefix, "--trust-release-repo"); err == nil || !strings.Contains(out, "owner/name") {
			t.Errorf("%q was accepted: %v\n%s", bad, err, out)
		}
	}
	if out, err := run("someone/fork", "v0.2.0", r.prefix, "--trust-release-repo"); err != nil || !strings.Contains(out, "trusting attestations of someone/fork") {
		t.Errorf("a confirmed override: %v\n%s", err, out)
	}
}

// A source install does not write a VERSION file, so it removes the one an earlier
// release install left: a stale one would misguide the downgrade check.
func TestSourceInstallRemovesAStaleVersionFile(t *testing.T) {
	t.Parallel()
	out, err := bash(t, nil, `cd .. && make -n -o check-clean -o check-main install PREFIX=/p`)
	if err != nil {
		t.Fatalf("make -n install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "rm -f '/p/libexec/whr/VERSION'") {
		t.Errorf("the install target does not remove the stale VERSION file:\n%s", out)
	}
}

// noGH is a PATH without gh: links to exactly the tools the script runs, plus the
// helpers GNU tar execs by name (gzip) and the interpreter behind shasum (perl).
// A tool missing on the machine fails the test with its name. The fakes in r.bin
// (uname, id) win over the real tools; gh is never linked.
func (r release) noGH(t *testing.T) []string {
	t.Helper()
	farm := t.TempDir()
	for _, name := range []string{"bash", "env", "awk", "sort", "head", "wc", "find", "cp", "rm", "mv", "mkdir", "install", "tar", "gzip", "shasum", "perl", "mktemp", "id", "uname"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("the test needs %s on PATH: %v", name, err)
		}
		if err := os.Symlink(p, filepath.Join(farm, name)); err != nil {
			t.Fatal(err)
		}
	}
	fakes, err := os.ReadDir(r.bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fakes {
		if f.Name() == "gh" || strings.HasSuffix(f.Name(), ".log") || f.Name() == "installed-whr-ran" {
			continue
		}
		link := filepath.Join(farm, f.Name())
		_ = os.Remove(link)
		if err := os.Symlink(filepath.Join(r.bin, f.Name()), link); err != nil {
			t.Fatal(err)
		}
	}
	return []string{"PATH=" + farm, "WHR_RELEASE_DIR=" + r.dir}
}

// Without gh the checksums are checked and the script says that the origin is not.
func TestWithoutGHOnlyTheChecksumsAreChecked(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	env := r.noGH(t)
	out, err := bash(t, env, "./install-release.sh v0.2.0 "+r.prefix)
	if err != nil {
		t.Fatalf("install without gh: %v\n%s", err, out)
	}
	if !strings.Contains(out, "not who built it") || !strings.Contains(out, "only the checksums were verified, not the origin") {
		t.Errorf("the caveat is missing:\n%s", out)
	}
	if v, err := os.ReadFile(filepath.Join(r.prefix, "libexec", "whr", "VERSION")); err != nil || string(v) != "v0.2.0\n" {
		t.Errorf("VERSION = %q, %v", v, err)
	}
	if _, err := os.Stat(filepath.Join(r.prefix, "bin", "whr")); err != nil {
		t.Errorf("nothing was installed: %v", err)
	}
	if _, err := os.Stat(r.ghlog); err == nil {
		t.Error("gh was called")
	}
}

// A changed archive fails the checksum check and installs nothing, with or without gh.
func TestAChecksumMismatchInstallsNothing(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	mac := filepath.Join(r.dir, "whr_0.2.0_darwin_arm64.tar.gz")
	tarball(t, mac, map[string]string{"whr": "#!/bin/sh\necho tampered\n"})
	out, err := bash(t, r.noGH(t), "./install-release.sh v0.2.0 "+r.prefix)
	if err == nil || !strings.Contains(out, "does not match checksums.txt") {
		t.Fatalf("a tampered archive was accepted: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(r.prefix, "bin", "whr")); err == nil {
		t.Error("a tampered archive was installed")
	}
}

// The downgrade guard does not depend on gh.
func TestWithoutGHAnOlderReleaseIsRefused(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.1.0", "v0.2.0")
	out, err := bash(t, r.noGH(t), "./install-release.sh v0.1.0 "+r.prefix)
	if err == nil || !strings.Contains(out, "older than the installed v0.2.0") {
		t.Fatalf("a downgrade was not refused: %v\n%s", err, out)
	}
}

// A prefix the whr user could write is refused.
func TestAWritablePrefixIsRefused(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	if err := os.MkdirAll(r.prefix, 0o755); err != nil { //nolint:gosec // the test needs a group-writable dir below
		t.Fatal(err)
	}
	if err := os.Chmod(r.prefix, 0o775); err != nil { //nolint:gosec // the point of the test
		t.Fatal(err)
	}
	out, err := r.run(t, "v0.2.0", r.prefix)
	if err == nil || !strings.Contains(out, "must not be able to write the prefix") {
		t.Fatalf("a group-writable prefix was accepted: %v\n%s", err, out)
	}
}

// A symlinked prefix is judged by its target.
func TestASymlinkedWritablePrefixIsRefused(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	target := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(target, 0o755); err != nil { //nolint:gosec // widened below
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o777); err != nil { //nolint:gosec // the point of the test
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	out, err := r.run(t, "v0.2.0", link)
	if err == nil || !strings.Contains(out, "must not be able to write the prefix") {
		t.Fatalf("a symlink to a writable dir was accepted: %v\n%s", err, out)
	}
}

// A prefix not owned by the installing user is refused; a fake id stands in for
// another user, as the real setup needs root.
func TestAPrefixOfAnotherOwnerIsRefused(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "0.2.0", "")
	if err := os.MkdirAll(r.prefix, 0o755); err != nil { //nolint:gosec // a test dir
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.bin, "id"), []byte("#!/bin/sh\necho 4242\n"), 0o700); err != nil { //nolint:gosec // an executable test fake
		t.Fatal(err)
	}
	out, err := r.run(t, "v0.2.0", r.prefix)
	if err == nil || !strings.Contains(out, "not owned by uid 4242") {
		t.Fatalf("a prefix of another owner was accepted: %v\n%s", err, out)
	}
}

// A gh that is present but fails never falls back to the checksum-only path.
func TestAFailingGHInstallsNothing(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"attestation": "#!/bin/sh\nif [ \"$1\" = api ]; then echo 0123456789abcdef0123456789abcdef01234567; exit 0; fi\nexit 1\n",
		"api":         "#!/bin/sh\nexit 1\n",
	} {
		r := newRelease(t, "0.2.0", "")
		if err := os.WriteFile(filepath.Join(r.bin, "gh"), []byte(body), 0o700); err != nil { //nolint:gosec // an executable test fake
			t.Fatal(err)
		}
		out, err := r.run(t, "v0.2.0", r.prefix)
		if err == nil {
			t.Errorf("%s: a failing gh passed:\n%s", name, out)
		}
		if _, err := os.Stat(filepath.Join(r.prefix, "bin", "whr")); err == nil {
			t.Errorf("%s: whr was installed", name)
		}
		if strings.Contains(out, "only the checksums were verified") {
			t.Errorf("%s: the checksum-only caveat was printed:\n%s", name, out)
		}
	}
}
