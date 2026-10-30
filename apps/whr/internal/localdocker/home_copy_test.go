// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

const homeTestImage = "docker.io/library/busybox@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const homeTestOp = "00112233445566ff"

// homeFake is a strict fake Docker CLI. It records every argv (NUL separated,
// one file per call) and answers only the vectors this feature admits.
type homeFake struct {
	t      *testing.T
	dir    string
	client Client
}

func newHomeFake(t *testing.T, pin string, volumes ...string) *homeFake {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schema_version":2,"devcontainer_path":".devcontainer/devcontainer.json","projects_path":"projects","bundles":[]`
	if pin != "" {
		manifest += `,"helper_images":{"base":"` + pin + `"}`
	}
	manifest += "}\n"
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("source/workharbor.json", manifest)
	id := strings.Repeat("a", 64)
	record := fmt.Sprintf(`{"Id":%q,"Name":"/ws1","Image":"img","State":"running","Labels":{"devcontainer.local_folder":%q},"Mounts":[{"Type":"volume","Name":"ws1-home","Destination":"/home/captain","RW":true},{"Type":"bind","Source":"/host","Destination":"/workspaces/ws1","RW":true}]}`, id, source)
	write("listing", fmt.Sprintf("{\"ID\":%q,\"Status\":\"Up\"}\n", id))
	write("inspect", record+"\n")
	write("volumes", strings.Join(append([]string{"ws1-home"}, volumes...), "\n")+"\n")
	write("df", `[{"Name":"ws1-home","Size":"1.5GB"},{"Name":"other","Size":"1kB"}]`+"\n")
	write("verify", homeVerifyOutput(3, 3))
	write("count", "0")
	script := fmt.Sprintf(`D=%q
n=$(cat "$D/count"); n=$((n+1)); printf '%%s' "$n" > "$D/count"
printf '%%s\0' "$@" > "$D/call.$n"
test "$1" = '--host=unix:///var/run/docker.sock' || exit 98
case "$2 $3" in
 'container ls') cat "$D/listing";;
 'container inspect') cat "$D/inspect";;
 'volume ls') cat "$D/volumes";;
 'system df') test -f "$D/df" || exit 1; cat "$D/df";;
 'image pull') test -f "$D/pull-fails" && exit 1; printf '%%s\n' "$6";;
 'volume create') printf '%%s\n' "${11}" >> "$D/volumes"; printf '%%s\n' "$7" > "$D/created-label"; printf '%%s\n' "${11}";;
 'volume inspect') l=$(cat "$D/created-label"); printf '{"workharbor.home-copy.operation":"%%s"}\n' "${l#workharbor.home-copy.operation=}";;
 'run --rm') if [ "${36}" = whr-home-verify ]; then cat "$D/verify"; else test -f "$D/copy-fails" && exit 2; :; fi;;
 *) exit 94;;
esac`, dir)
	return &homeFake{t: t, dir: dir, client: Client{Executable: fakeDocker(t, script)}}
}

func homeVerifyOutput(sourceFiles, targetFiles int) string {
	var b strings.Builder
	for _, side := range []struct {
		root  string
		files int
	}{{"/from", sourceFiles}, {"/to", targetFiles}} {
		fmt.Fprintf(&b, "%s f %d\n%s d       4\n%s l 1\n%s .claude/projects 2\n%s .codex/sessions 0\n", side.root, side.files, side.root, side.root, side.root, side.root)
	}
	return b.String()
}

func (f *homeFake) set(name, content string) {
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *homeFake) calls() [][]string {
	f.t.Helper()
	raw, _ := os.ReadFile(filepath.Join(f.dir, "count"))
	n, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	out := [][]string{}
	for i := 1; i <= n; i++ {
		data, err := os.ReadFile(filepath.Join(f.dir, "call."+strconv.Itoa(i)))
		if err != nil {
			f.t.Fatal(err)
		}
		parts := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
		out = append(out, parts[1:])
	}
	return out
}

func (f *homeFake) mutations() []string {
	var out []string
	for _, c := range f.calls() {
		key := strings.Join(c[:2], " ")
		switch key {
		case "container ls", "container inspect", "volume ls", "system df", "volume inspect":
			continue
		}
		out = append(out, key)
	}
	return out
}

func prepareHome(t *testing.T, f *homeFake, o HomeCopyOptions) *HomeCopy {
	t.Helper()
	h, err := f.client.PrepareHomeCopy(context.Background(), socketPorts(), o)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	h.newOp = func() (string, error) { return homeTestOp, nil }
	return h
}

func TestHomeCopyOptionsAreValidatedBeforeIO(t *testing.T) {
	for name, o := range map[string]HomeCopyOptions{
		"mode":              {Mode: "restore", Target: "ws1"},
		"selector":          {Mode: "clone"},
		"bad to":            {Mode: "clone", Target: "ws1", To: "-x"},
		"long to":           {Mode: "clone", Target: "ws1", To: strings.Repeat("a", 129)},
		"wildcard":          {Mode: "clone", Target: "ws1", Excludes: []string{".cache/*"}},
		"dotdot":            {Mode: "clone", Target: "ws1", Excludes: []string{"a/../b"}},
		"empty":             {Mode: "clone", Target: "ws1", Excludes: []string{"./"}},
		"trailing slash":    {Mode: "clone", Target: "ws1", Excludes: []string{"a/"}},
		"space":             {Mode: "clone", Target: "ws1", Excludes: []string{"a b"}},
		"newline":           {Mode: "clone", Target: "ws1", Excludes: []string{"a\nb"}},
		"backup exclude":    {Mode: "backup", Target: "ws1", Excludes: []string{"x"}},
		"backup credential": {Mode: "backup", Target: "ws1", IncludeCredentials: true},
		"too many":          {Mode: "clone", Target: "ws1", Excludes: strings.Split(strings.Repeat("x,", 33)[:65], ",")},
	} {
		f := newHomeFake(t, homeTestImage)
		_, err := f.client.PrepareHomeCopy(context.Background(), socketPorts(), o)
		if exitcode.FromError(err) != exitcode.Usage || len(f.calls()) != 0 {
			t.Errorf("%s: %v calls=%d", name, err, len(f.calls()))
		}
	}
}

func TestHomeClonePreviewDefaultsAndZeroEffects(t *testing.T) {
	f := newHomeFake(t, homeTestImage, "whr-home-clone-ws1-1")
	h := prepareHome(t, f, HomeCopyOptions{Mode: "clone", Target: "ws1", Excludes: []string{"/projects/big", "./.cache", "tmp"}})
	p := h.Preview()
	if p.Status != "preview" || !p.DryRun || p.Mode != "clone" || p.Source.ContainerID != strings.Repeat("a", 64) || p.Source.ContainerName != "ws1" || p.Source.Role != "primary" || p.Source.Volume != "ws1-home" {
		t.Fatalf("preview %+v", p)
	}
	if p.TargetVolume != "whr-home-clone-ws1-2" || p.HelperImage != homeTestImage || p.EstimatedSize == nil || *p.EstimatedSize != "1.5GB" || p.CredentialsIncluded {
		t.Fatalf("preview %+v", p)
	}
	want := append([]string{"./.cache", "./projects/big", "./tmp"}, credentialExcludes...)
	sort.Strings(want)
	if !reflect.DeepEqual(p.Excludes, want) {
		t.Fatalf("excludes %v want %v", p.Excludes, want)
	}
	if !strings.HasPrefix(p.Fingerprint, "sha256:") || len(p.Fingerprint) != 71 {
		t.Fatalf("fingerprint %q", p.Fingerprint)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Fatalf("preview mutated: %v", m)
	}
	again := prepareHome(t, f, HomeCopyOptions{Mode: "clone", Target: "ws1", Excludes: []string{"tmp", "projects/big"}})
	if again.Preview().Fingerprint != p.Fingerprint {
		t.Fatal("fingerprint is not deterministic for the same plan")
	}
	other := prepareHome(t, f, HomeCopyOptions{Mode: "clone", Target: "ws1", IncludeCredentials: true})
	if other.Preview().Fingerprint == p.Fingerprint || !other.Preview().CredentialsIncluded || strings.Contains(strings.Join(other.Preview().Excludes, ","), ".ssh") {
		t.Fatalf("credential opt-in %+v", other.Preview())
	}
}

func TestHomeBackupExcludesOnlyCacheAndKeepsCredentials(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	p := prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: strings.Repeat("a", 64), To: "my-backup"}).Preview()
	if !reflect.DeepEqual(p.Excludes, []string{"./.cache"}) || !p.CredentialsIncluded || p.TargetVolume != "my-backup" {
		t.Fatalf("backup %+v", p)
	}
}

// Without a usable pin the workspace's own local image runs the copy: no config, no pull.
func TestHomeCopyWithoutPinUsesWorkspaceImageWithoutPull(t *testing.T) {
	for _, pin := range []string{"", "docker.io/library/busybox:latest"} {
		f := newHomeFake(t, pin)
		h := prepareHome(t, f, HomeCopyOptions{Mode: "clone", Target: "ws1"})
		preview := h.Preview()
		if preview.HelperImage != "img" || len(f.mutations()) != 0 {
			t.Fatalf("%q: preview image %q %v", pin, preview.HelperImage, f.mutations())
		}
		result, err := h.Apply(context.Background(), preview.Fingerprint)
		if err != nil || result.Status != "copied" {
			t.Fatalf("%q: apply %+v %v", pin, result, err)
		}
		if got := strings.Join(f.mutations(), ","); got != "volume create,run --rm,run --rm" {
			t.Fatalf("%q: mutations %s", pin, got)
		}
		for _, c := range f.calls() {
			if c[0] == "run" && c[31] != "img" {
				t.Fatalf("%q: run image %q", pin, c[31])
			}
		}
	}
}

func TestHomeCopyTargetProtections(t *testing.T) {
	for _, to := range []string{"ws1-home", "existing", "anchorage-home-x", "whr-stage-home-1", "bundledock-go-1"} {
		f := newHomeFake(t, homeTestImage, "existing")
		_, err := f.client.PrepareHomeCopy(context.Background(), socketPorts(), HomeCopyOptions{Mode: "backup", Target: "ws1", To: to})
		if exitcode.FromError(err) != exitcode.Conflict || len(f.mutations()) != 0 {
			t.Errorf("%s: %v", to, err)
		}
	}
	f := newHomeFake(t, homeTestImage)
	if _, err := f.client.PrepareHomeCopy(context.Background(), socketPorts(), HomeCopyOptions{Mode: "backup", Target: "missing"}); exitcode.FromError(err) != exitcode.NotFound {
		t.Fatalf("missing selector: %v", err)
	}
	f.set("inspect", strings.Replace(mustRead(t, filepath.Join(f.dir, "inspect")), `"/home/captain"`, `"/data"`, 1))
	if _, err := f.client.PrepareHomeCopy(context.Background(), socketPorts(), HomeCopyOptions{Mode: "backup", Target: "ws1"}); exitcode.FromError(err) != exitcode.Conflict {
		t.Fatalf("no home volume: %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestHomeCloneApplyRunsFixedArgvAndVerifies(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	h := prepareHome(t, f, HomeCopyOptions{Mode: "clone", Target: "ws1", Excludes: []string{"projects/big"}})
	preview := h.Preview()
	result, err := h.Apply(context.Background(), preview.Fingerprint)
	if err != nil || result.Status != "copied" || result.DryRun {
		t.Fatalf("apply %+v %v", result, err)
	}
	if v := result.Verification; v == nil || v.Status != "match" || v.Source.RegularFiles != 3 || v.Target.Directories != 4 || v.Source.ClaudeSessionFiles != 2 || v.Target.CodexSessionFiles != 0 || v.Target.SymbolicLinks != 1 {
		t.Fatalf("verification %+v", result.Verification)
	}
	if got := strings.Join(f.mutations(), ","); got != "image pull,volume create,run --rm,run --rm" {
		t.Fatalf("mutations %s", got)
	}
	var runs [][]string
	for _, c := range f.calls() {
		if c[0] == "run" {
			runs = append(runs, c)
		}
	}
	prefix := []string{"run", "--rm", "--pull", "never", "--network", "none", "--read-only", "--security-opt", "no-new-privileges", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "DAC_OVERRIDE", "--cap-add", "FOWNER", "--cap-add", "FSETID", "--user", "0:0", "--name"}
	excl := []string{"--exclude=./.cache", "--exclude=./.claude.json", "--exclude=./.claude/.credentials.json", "--exclude=./.codex/auth.json", "--exclude=./.config/gh", "--exclude=./.docker/config.json", "--exclude=./.git-credentials", "--exclude=./.gnupg", "--exclude=./.netrc", "--exclude=./.ssh", "--exclude=./projects/big"}
	copyWant := append(append([]string{}, prefix...), "whr-home-copy-"+homeTestOp, "--label", "workharbor.home-copy.operation="+homeTestOp, "-v", "ws1-home:/from:ro", "-v", "whr-home-clone-ws1-1:/to", "--entrypoint", "/bin/sh", homeTestImage, "-c",
		`set -eu -o pipefail; cd /from; tar -cpf - --numeric-owner "$@" . | tar -xpf - --numeric-owner -C /to`, "whr-home-copy")
	copyWant = append(copyWant, excl...)
	if !reflect.DeepEqual(runs[0], copyWant) {
		t.Fatalf("copy argv\n got %q\nwant %q", runs[0], copyWant)
	}
	verify := runs[1]
	if verify[22] != "whr-home-verify-"+homeTestOp || verify[26] != "ws1-home:/from:ro" || verify[28] != "whr-home-clone-ws1-1:/to:ro" || verify[34] != "whr-home-verify" {
		t.Fatalf("verify argv %q", verify)
	}
	if got := strings.Join(verify[35:], ","); got != ".cache,.claude.json,.claude/.credentials.json,.codex/auth.json,.config/gh,.docker/config.json,.git-credentials,.gnupg,.netrc,.ssh,projects/big" {
		t.Fatalf("verify excludes %s", got)
	}
	for _, run := range runs {
		if strings.Contains(run[33], "projects/big") || strings.Contains(run[33], ".cache") {
			t.Fatal("exclude interpolated into the shell template")
		}
	}
	for _, c := range f.calls() {
		if c[0] == "volume" && c[1] == "create" {
			want := []string{"volume", "create", "--label", "workharbor.home-copy=clone", "--label", "workharbor.home-copy.operation=" + homeTestOp, "--label", "workharbor.home-copy.source=ws1-home", "--", "whr-home-clone-ws1-1"}
			if !reflect.DeepEqual(c, want) {
				t.Fatalf("create argv %q", c)
			}
		}
		if c[0] == "image" && !reflect.DeepEqual(c, []string{"image", "pull", "--quiet", "--", homeTestImage}) {
			t.Fatalf("pull argv %q", c)
		}
	}
	if _, err := h.Apply(context.Background(), preview.Fingerprint); exitcode.FromError(err) != exitcode.Conflict {
		t.Fatalf("transaction reused: %v", err)
	}
}

func TestHomeCopyTrustMismatchAndChangedPlanRefuseBeforeEffects(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	h := prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	if _, err := h.Apply(context.Background(), "sha256:"+strings.Repeat("0", 64)); exitcode.FromError(err) != exitcode.Conflict || len(f.mutations()) != 0 {
		t.Fatalf("trust mismatch: %v %v", err, f.mutations())
	}
	h = prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	fp := h.Preview().Fingerprint
	f.set("volumes", "ws1-home\nwhr-home-backup-ws1-1\n")
	if _, err := h.Apply(context.Background(), fp); exitcode.FromError(err) != exitcode.Conflict || len(f.mutations()) != 0 {
		t.Fatalf("changed plan: %v %v", err, f.mutations())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h = prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	if _, err := h.Apply(ctx, h.Preview().Fingerprint); exitcode.FromError(err) != exitcode.Interrupted || len(f.mutations()) != 0 {
		t.Fatalf("cancel before effects: %v", err)
	}
}

func TestHomeCopyPullFailureCreatesNothing(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	f.set("pull-fails", "")
	h := prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	r, err := h.Apply(context.Background(), h.Preview().Fingerprint)
	if exitcode.FromError(err) != exitcode.Conflict || strings.Join(f.mutations(), ",") != "image pull" || len(r.Residuals) != 0 {
		t.Fatalf("%+v %v %v", r, err, f.mutations())
	}
}

func TestHomeCopyFailureKeepsOnlyReportedTarget(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	f.set("copy-fails", "")
	h := prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	r, err := h.Apply(context.Background(), h.Preview().Fingerprint)
	if exitcode.FromError(err) != exitcode.PartialFailure || r.Status != "failed" || !reflect.DeepEqual(r.Residuals, []string{"volume:whr-home-backup-ws1-1"}) {
		t.Fatalf("%+v %v", r, err)
	}
	for _, c := range f.calls() {
		if strings.Join(c, " ") != "" && (c[0] == "rm" || (c[0] == "volume" && c[1] == "rm") || (c[0] == "container" && c[1] == "rm")) {
			t.Fatalf("automatic deletion %q", c)
		}
	}
	var e *exitcode.Error
	if !errorsAs(err, &e) || e.Result == nil {
		t.Fatal("failure carries no result")
	}
}

func TestHomeCopyVerificationMismatchIsPartial(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	f.set("verify", homeVerifyOutput(3, 2))
	h := prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	r, err := h.Apply(context.Background(), h.Preview().Fingerprint)
	if exitcode.FromError(err) != exitcode.PartialFailure || r.Verification == nil || r.Verification.Status != "differs" || len(r.Residuals) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	f = newHomeFake(t, homeTestImage)
	f.set("verify", "garbage\n")
	h = prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	if _, err := h.Apply(context.Background(), h.Preview().Fingerprint); exitcode.FromError(err) != exitcode.PartialFailure {
		t.Fatalf("unparsable verification: %v", err)
	}
}

func TestHomeCopyCreateRaceIsNotAdopted(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	h := prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"})
	fp := h.Preview().Fingerprint
	// The fake reports a foreign operation label: someone else owns the volume.
	h.newOp = func() (string, error) { return "ffffffffffffffff", nil }
	script := mustRead(t, f.client.Executable)
	script = strings.Replace(script, `printf '%s\n' "$7" > "$D/created-label"`, `printf '%s\n' workharbor.home-copy.operation=0000000000000000 > "$D/created-label"`, 1)
	if err := os.WriteFile(f.client.Executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := h.Apply(context.Background(), fp)
	if exitcode.FromError(err) != exitcode.Conflict || len(r.Residuals) != 0 || strings.Contains(strings.Join(f.mutations(), ","), "run") {
		t.Fatalf("%+v %v %v", r, err, f.mutations())
	}
}

func TestHomeCopyArgvAdmission(t *testing.T) {
	good := homeRunArgv(false, "src", "dst", homeTestOp, homeTestImage, []string{"--exclude=./.cache"})
	if !admitHomeMutation(good) || !admitHomeMutation(homeRunArgv(true, "src", "dst", homeTestOp, homeTestImage, []string{".cache"})) {
		t.Fatal("canonical vectors refused")
	}
	if !admitHomeMutation(homeRunArgv(false, "src", "dst", homeTestOp, "vsc-anchorage-abc:latest", []string{"--exclude=./.cache"})) {
		t.Fatal("local workspace image refused")
	}
	if admitHomeMutation(homeRunArgv(false, "src", "dst", homeTestOp, "--privileged", []string{"--exclude=./.cache"})) {
		t.Fatal("option-looking image admitted")
	}
	mutate := func(i int, v string) []string {
		c := append([]string(nil), good...)
		c[i] = v
		return c
	}
	for name, args := range map[string][]string{
		"rw source":        mutate(26, "src:/from"),
		"network":          mutate(5, "host"),
		"option image":     mutate(31, "--privileged"),
		"template":         mutate(33, "rm -rf /from"),
		"exclude grammar":  append(append([]string(nil), good...), "--exclude=./a b"),
		"exclude option":   append(append([]string(nil), good...), "--to-command=sh"),
		"protected target": homeRunArgv(false, "src", "anchorage-home-x", homeTestOp, homeTestImage, nil),
		"same volume":      homeRunArgv(false, "src", "src", homeTestOp, homeTestImage, nil),
		"bad op":           homeRunArgv(false, "src", "dst", "xyz", homeTestImage, nil),
		"privileged":       append([]string{"run", "--privileged"}, good[1:]...),
		"volume rm":        {"volume", "rm", "--", "dst"},
		"pull tag":         {"image", "pull", "--quiet", "--", "busybox:latest"},
	} {
		if admitHomeMutation(args) {
			t.Errorf("%s admitted", name)
		}
	}
	if _, err := (Client{Executable: fakeDocker(t, "exit 0")}).homeMutate(context.Background(), "unix:///s", "volume", "rm", "--", "dst"); err == nil {
		t.Fatal("unadmitted mutation reached the executable")
	}
}

func errorsAs(err error, target **exitcode.Error) bool {
	e, ok := err.(*exitcode.Error)
	if ok {
		*target = e
	}
	return ok
}

func TestHomeCopyResultJSONShape(t *testing.T) {
	f := newHomeFake(t, homeTestImage)
	raw, err := json.Marshal(prepareHome(t, f, HomeCopyOptions{Mode: "backup", Target: "ws1"}).Preview())
	if err != nil || !bytes.Contains(raw, []byte(`"verification":null`)) || !bytes.Contains(raw, []byte(`"residuals":[]`)) || !bytes.Contains(raw, []byte(`"actions":[]`)) {
		t.Fatalf("%s %v", raw, err)
	}
}
