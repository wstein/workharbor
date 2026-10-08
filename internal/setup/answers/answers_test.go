package answers

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/version"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	fix1 = "1111111111111111111111111111111111111111111111111111111111111111"
	fix2 = "2222222222222222222222222222222222222222222222222222222222222222"
)

func fakeIdentity(t *testing.T, id string, err error) {
	t.Helper()
	old := identity
	identity = func() (string, error) { return id, err }
	t.Cleanup(func() { identity = old })
}

func sampleChecks() []doctor.Check {
	do := func(context.Context, doctor.Prompter) error { return nil }
	return []doctor.Check{
		{Name: "api-token", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Do: do, Desc: "a"}},
		{Name: "config-dir", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Do: do, Desc: "b"}},
		{Name: "power", Phase: doctor.PhaseHost, Fix: &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"pmset"}, Sudo: true}}}},
	}
}

func sample() File {
	c := sampleChecks()
	return File{Account: "workharbor", Answers: []Entry{
		{Step: "config-dir", Fix: FixDigest(c[1]), Answer: Skip},
		{Step: "api-token", Fix: FixDigest(c[0]), Answer: Run},
	}}
}

const goodJSON = `{"schema":"workharbor.setup-answers","v":1,"whr":"v0.1.0@abc1234","phase":"user","account":"workharbor","answers":[{"step":"api-token","fix":"` + fix1 + `","answer":"run"}]}`

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", "v1", name)
	if *update {
		if err := os.WriteFile(p, got, 0o600); err != nil { //nolint:gosec // golden path
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) //nolint:gosec // golden path
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs:\n got %q\nwant %q", name, got, want)
	}
}

func TestSaveGoldenAndRoundTrip(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := Save(path, sample(), sampleChecks()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path) //nolint:gosec // a test path
	golden(t, "user.json", got)
	f, warns, err := Load(path, os.Getuid())
	if err != nil || len(warns) != 0 {
		t.Fatalf("Load: %v %v", err, warns)
	}
	if f.Answers[0].Step != "api-token" || f.Whr != "v0.1.0@abc1234" {
		t.Fatalf("unexpected %+v", f)
	}
	c := sampleChecks()
	if a, ok := f.Lookup(c[0]); !ok || a != Run {
		t.Fatal("lookup failed")
	}
	c[0].Fix.Desc = "changed"
	if _, ok := f.Lookup(c[0]); ok {
		t.Fatal("a changed command must not match")
	}
	again, _ := Encode(f)
	if !bytes.Equal(again, got) {
		t.Fatal("round trip is not byte-identical")
	}
}

// No t.Parallel: the umask is process-wide.
func TestSavedFileIs0600UnderUmask0(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "a.json")
	if err := Save(path, sample(), sampleChecks()); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
}

func TestDirtyBuildNeitherSavesNorHasIdentity(t *testing.T) {
	fakeIdentity(t, "", ErrNoBuildIdentity)
	if err := Save(filepath.Join(t.TempDir(), "a.json"), sample(), sampleChecks()); !errors.Is(err, ErrNoBuildIdentity) {
		t.Fatalf("got %v", err)
	}
	if _, err := Identity(); !errors.Is(err, ErrNoBuildIdentity) {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeRefusals(t *testing.T) {
	entry := `{"step":"api-token","fix":"` + fix1 + `","answer":"run"}`
	doc := func(mid string) string {
		return `{"schema":"workharbor.setup-answers","v":1,"whr":"v0.1.0@abc1234","phase":"user","account":"workharbor",` + mid + `}`
	}
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"unknown top-level field", strings.Replace(goodJSON, `"v":1,`, `"v":1,"extra":"x",`, 1), ErrUnknownField},
		{"value field top-level", strings.Replace(goodJSON, `"v":1,`, `"v":1,"value":"s3cret",`, 1), ErrUnknownField},
		{"password per entry", doc(`"answers":[{"step":"api-token","fix":"` + fix1 + `","answer":"run","password":"x"}]`), ErrUnknownField},
		{"value per entry", doc(`"answers":[{"step":"api-token","fix":"` + fix1 + `","answer":"run","value":"x"}]`), ErrUnknownField},
		{"duplicate top key", strings.Replace(goodJSON, `"v":1,`, `"v":1,"v":1,`, 1), ErrDuplicateKey},
		{"duplicate entry key", doc(`"answers":[{"step":"a","step":"b","fix":"` + fix1 + `","answer":"run"}]`), ErrDuplicateKey},
		{"duplicate step", doc(`"answers":[` + entry + `,` + entry + `]`), ErrDuplicateStep},
		{"null", strings.Replace(goodJSON, `"account":"workharbor"`, `"account":null`, 1), ErrNull},
		{"null entry", doc(`"answers":[null]`), ErrNull},
		{"float", strings.Replace(goodJSON, `"v":1`, `"v":1.0`, 1), ErrFloat},
		{"exponent", strings.Replace(goodJSON, `"v":1`, `"v":1e0`, 1), ErrFloat},
		{"trailing object", goodJSON + `{}`, ErrTrailing},
		{"trailing garbage", goodJSON + ` x`, ErrTrailing},
		{"v 2", strings.Replace(goodJSON, `"v":1`, `"v":2`, 1), ErrUnknownVersion},
		{"v missing", strings.Replace(goodJSON, `"v":1,`, ``, 1), ErrUnknownVersion},
		{"phase host", strings.Replace(goodJSON, `"phase":"user"`, `"phase":"host"`, 1), ErrBadPhase},
		{"bad hex", strings.Replace(goodJSON, fix1, strings.Repeat("g", 64), 1), ErrBadAnswer},
		{"uppercase hex", strings.Replace(goodJSON, fix1, strings.Repeat("A", 64), 1), ErrBadAnswer},
		{"short hex", strings.Replace(goodJSON, fix1, "abc", 1), ErrBadAnswer},
		{"unknown answer", strings.Replace(goodJSON, `"run"`, `"yes"`, 1), ErrBadAnswer},
		{"bad step", strings.Replace(goodJSON, `"api-token"`, `"Api_Token"`, 1), ErrBadAnswer},
		{"bad schema", strings.Replace(goodJSON, `setup-answers`, `other`, 1), ErrBadSchema},
		{"not an object", `[]`, ErrSyntax},
		{"not json", `{`, ErrSyntax},
		{"answers missing", `{"schema":"workharbor.setup-answers","v":1,"whr":"v0.1.0@abc1234","phase":"user","account":"workharbor"}`, ErrBadAnswer},
		{"invalid UTF-8", strings.Replace(goodJSON, `"workharbor","answers"`, "\"work\xffharbor\",\"answers\"", 1), ErrSyntax},
		{"ANSWER key", strings.Replace(goodJSON, `"answer":"run"`, `"answer":"skip","ANSWER":"run"`, 1), ErrUnknownField},
		{"Answers key", strings.Replace(goodJSON, `"answers":[`, `"answers":[],"Answers":[`, 1), ErrUnknownField},
		{"SCHEMA key", strings.Replace(goodJSON, `"schema"`, `"SCHEMA"`, 1), ErrUnknownField},
		{"long s key", strings.Replace(goodJSON, `"step"`, "\"\u017ftep\"", 1), ErrUnknownField},
		{"kelvin key", strings.Replace(goodJSON, `"whr":`, "\"\\u212a\":\"x\",\"whr\":", 1), ErrUnknownField},
		{"nested object", strings.Replace(goodJSON, `"account":"workharbor"`, `"account":{}`, 1), ErrSyntax},
		{"answers not array", strings.Replace(goodJSON, `"answers":[`, `"answers":{"x":[`, 1), ErrSyntax},
		{"over 64 KiB", goodJSON + strings.Repeat(" ", MaxBytes), ErrTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode([]byte(c.in)); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
	if _, err := Decode([]byte(goodJSON)); err != nil {
		t.Fatalf("good file refused: %v", err)
	}
}

func TestTooManyAnswers(t *testing.T) {
	var es []string
	for i := 0; i < 65; i++ {
		es = append(es, fmt.Sprintf(`{"step":"s%d","fix":"%s","answer":"run"}`, i, fix1))
	}
	in := strings.Replace(goodJSON, `[{"step":"api-token","fix":"`+fix1+`","answer":"run"}]`, "["+strings.Join(es, ",")+"]", 1)
	if _, err := Decode([]byte(in)); !errors.Is(err, ErrTooMany) {
		t.Fatalf("got %v", err)
	}
}

func writeFile(t *testing.T, dir string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, "a.json")
	if err := os.WriteFile(p, []byte(goodJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFileRefusals(t *testing.T) {
	dir := t.TempDir()
	uid := os.Getuid()

	if _, _, err := Load(writeFile(t, dir, 0o600), uid); err != nil {
		t.Fatalf("0600: %v", err)
	}
	if _, w, err := Load(writeFile(t, dir, 0o644), uid); err != nil || len(w) != 1 {
		t.Fatalf("0644 should warn once: %v %v", err, w)
	}
	for _, m := range []os.FileMode{0o620, 0o602, 0o660, 0o666} {
		if _, _, err := Load(writeFile(t, dir, m), uid); !errors.Is(err, ErrWritableByOthers) {
			t.Fatalf("%o: %v", m, err)
		}
	}
	target := writeFile(t, dir, 0o600)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(link, uid); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("symlink: %v", err)
	}
	if _, _, err := Load(dir, uid); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("directory: %v", err)
	}
	if _, _, err := Load(target, uid+1); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("other owner (expected uid): %v", err)
	}
	old := fstat
	fstat = func(f *os.File) (statInfo, error) {
		si, err := old(f)
		si.UID = uid + 4242
		return si, err
	}
	defer func() { fstat = old }()
	if _, _, err := Load(target, uid); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("other owner (injected stat): %v", err)
	}
}

func TestPathInsideGitTreeIsRefused(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	p := writeFile(t, sub, 0o600)
	if _, _, err := Load(p, os.Getuid()); !errors.Is(err, ErrInGitTree) {
		t.Fatalf("Load: %v", err)
	}
	if err := Save(filepath.Join(sub, "new.json"), sample(), sampleChecks()); !errors.Is(err, ErrInGitTree) {
		t.Fatalf("Save: %v", err)
	}
	// a link from outside a repository into it counts too
	out := t.TempDir()
	if err := os.Symlink(sub, filepath.Join(out, "l")); err != nil {
		t.Fatal(err)
	}
	if err := Save(filepath.Join(out, "l", "x.json"), sample(), sampleChecks()); !errors.Is(err, ErrInGitTree) {
		t.Fatalf("Save through link: %v", err)
	}
}

func TestSaveParentAndTargetRules(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	root := t.TempDir()
	if err := Save(filepath.Join(root, "missing", "a.json"), sample(), sampleChecks()); !errors.Is(err, ErrNoDirectory) {
		t.Fatalf("missing parent: %v", err)
	}
	cfg := filepath.Join(root, ".config", "whr", "a.json")
	if err := Save(cfg, sample(), sampleChecks()); err != nil {
		t.Fatalf("~/.config/whr is created: %v", err)
	}
	if fi, _ := os.Stat(filepath.Dir(cfg)); fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", fi.Mode().Perm())
	}
	target := writeFile(t, root, 0o600)
	link := filepath.Join(root, "l.json")
	_ = os.Symlink(target, link)
	if err := Save(link, sample(), sampleChecks()); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("symlink target: %v", err)
	}
	if err := Save(filepath.Join(root, ".config"), sample(), sampleChecks()); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("directory target: %v", err)
	}
	if err := Save(target, sample(), sampleChecks()); err != nil {
		t.Fatal(err)
	}
	es, _ := os.ReadDir(root)
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("left %s", e.Name())
		}
	}
}

func TestSaveRefusesInvalid(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	f := sample()
	f.Answers[0].Fix = "zz"
	if err := Save(filepath.Join(t.TempDir(), "a.json"), f, sampleChecks()); !errors.Is(err, ErrBadAnswer) {
		t.Fatalf("got %v", err)
	}
}

func TestNoFieldCouldHoldASecret(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(File{}), reflect.TypeOf(Entry{})} {
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			for _, bad := range []string{"value", "password", "secret", "token", "key"} {
				if tag == bad {
					t.Fatalf("field %s could hold a secret", typ.Field(i).Name)
				}
			}
		}
	}
}

func TestLookupNeverAnswersHostOrSudoSteps(t *testing.T) {
	c := sampleChecks()
	f := File{Answers: []Entry{{Step: "power", Fix: FixDigest(c[2]), Answer: Run}, {Step: "api-token", Fix: FixDigest(c[0]), Answer: Run}}}
	if _, ok := f.Lookup(c[2]); ok {
		t.Fatal("a host sudo step got an answer")
	}
	if a, ok := f.Lookup(c[0]); !ok || a != Run {
		t.Fatal("an eligible step got none")
	}
}

func TestSaveRefusesIneligibleOrStaleEntries(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	c := sampleChecks()
	p := filepath.Join(t.TempDir(), "a.json")
	host := File{Account: "workharbor", Answers: []Entry{{Step: "power", Fix: FixDigest(c[2]), Answer: Run}}}
	if err := Save(p, host, c); !errors.Is(err, ErrIneligible) {
		t.Fatalf("host step: %v", err)
	}
	stale := sample()
	stale.Answers[0].Fix = fix1
	if err := Save(p, stale, c); !errors.Is(err, ErrIneligible) {
		t.Fatalf("stale digest: %v", err)
	}
	unknown := File{Account: "workharbor", Answers: []Entry{{Step: "nope", Fix: fix1, Answer: Run}}}
	if err := Save(p, unknown, c); !errors.Is(err, ErrIneligible) {
		t.Fatalf("unknown step: %v", err)
	}
}

func TestBuildsWithoutAKnownCommitHaveNoIdentity(t *testing.T) {
	for _, info := range []version.Info{
		{Version: "v1.0.0", Commit: "abc1234", Dirty: true},
		{Version: "v1.0.0", Commit: "unknown"},
		{Version: "v1.0.0", Commit: ""},
		{Version: "v0.0.0-0-gunknown", Commit: "abc1234"},
	} {
		if _, err := identityOf(info); !errors.Is(err, ErrNoBuildIdentity) {
			t.Errorf("%+v: %v", info, err)
		}
	}
	if id, err := identityOf(version.Info{Version: "v1.0.0", Commit: "abc1234"}); err != nil || id != "v1.0.0@abc1234" {
		t.Fatalf("%q %v", id, err)
	}
}

// No t.Parallel: the umask is process-wide. A umask that removes the owner's
// write bit would leave the temporary file 0400 had Save not forced the mode.
func TestSavedFileIs0600UnderARestrictiveUmask(t *testing.T) {
	fakeIdentity(t, "v0.1.0@abc1234", nil)
	path := filepath.Join(t.TempDir(), "a.json")
	old := syscall.Umask(0o277)
	defer syscall.Umask(old)
	if err := Save(path, sample(), sampleChecks()); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
}

func TestAFixThatSaysIrreversibleIsNeverAnswered(t *testing.T) {
	do := func(context.Context, doctor.Prompter) error { return nil }
	c := doctor.Check{Name: "forget-it", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Do: do, Desc: "x", Irreversible: true}}
	if ok, _ := Eligible(c); ok {
		t.Fatal("an irreversible fix is eligible")
	}
	f := File{Answers: []Entry{{Step: "forget-it", Fix: FixDigest(c), Answer: Run}}}
	if _, ok := f.Lookup(c); ok {
		t.Fatal("an irreversible fix got an answer")
	}
	c.Fix.Irreversible = false
	if ok, _ := Eligible(c); !ok {
		t.Fatal("the same fix without the mark is eligible")
	}
}

func TestLoadRawReturnsTheBytesThatWereDecoded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	if err := os.WriteFile(p, []byte(goodJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	f, data, _, err := LoadRaw(p, os.Getuid())
	if err != nil || string(data) != goodJSON || len(f.Answers) != 1 {
		t.Fatalf("%v %q %+v", err, data, f)
	}
}

func TestSaveAsBindsTheGivenIdentity(t *testing.T) {
	fakeIdentity(t, "", ErrNoBuildIdentity) // Save would refuse; SaveAs takes the identity it is given
	path := filepath.Join(t.TempDir(), "a.json")
	if err := SaveAs("v9.9.9@feed123", path, sample(), sampleChecks()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path) //nolint:gosec // a test path
	if !strings.Contains(string(data), `"whr": "v9.9.9@feed123"`) {
		t.Fatalf("%s", data)
	}
}

func TestAccountNamesFollowMacOSShortNames(t *testing.T) {
	for name, ok := range map[string]bool{
		"workharbor": true, "Werner": true, "aBc": true, "9abc": true, "john.doe": true, "A_b-c9": true, "_svc": true,
		"-rf": false, "": false, "bad name": false, "a/b": false, "a:b": false,
		strings.Repeat("a", 32): true, strings.Repeat("a", 33): false, "..": false,
	} {
		err := File{V: Version, Schema: Schema, Phase: PhaseUser, Account: name, Whr: "v0.1.0@abc1234"}.Validate()
		if bad := errors.Is(err, ErrBadAccount); bad == ok {
			t.Errorf("account %q: accepted=%v, error %v", name, !bad, err)
		}
	}
}

func TestCheckSavePathRefusesWhatSaveWouldNotWriteOrWouldOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := CheckSavePath(filepath.Join(root, "new.json")); err != nil {
		t.Fatalf("a new file in an existing directory: %v", err)
	}
	if err := CheckSavePath(filepath.Join(root, "missing", "a.json")); !errors.Is(err, ErrNoDirectory) {
		t.Fatalf("missing parent: %v", err)
	}
	if err := CheckSavePath(filepath.Join(root, ".config", "whr", "a.json")); err != nil {
		t.Fatalf("~/.config/whr is created by Save: %v", err)
	}
	if err := CheckSavePath(root); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("a directory: %v", err)
	}
	target := writeFile(t, root, 0o600)
	if err := CheckSavePath(target); !errors.Is(err, ErrExists) {
		t.Fatalf("an existing file is never overwritten silently: %v", err)
	}
	link := filepath.Join(root, "l.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckSavePath(link); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("a link: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CheckSavePath(filepath.Join(root, "n.json")); !errors.Is(err, ErrInGitTree) {
		t.Fatalf("a git tree: %v", err)
	}
}

// A saved answer binds to the command the preview shows (Show), so a changed
// shim or prefix asks again.
func TestDigestCoversTheShownCommand(t *testing.T) {
	mk := func(arg string) doctor.Check {
		return doctor.Check{Name: "tool-store", Phase: doctor.PhaseUser, Fix: &doctor.Fix{
			Show:  func() []doctor.Cmd { return []doctor.Cmd{{Argv: []string{"whr", "tools", "build", "-shim", arg}}} },
			Build: func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) { return nil, nil },
		}}
	}
	a, b, again := FixDigest(mk("/a/shim")), FixDigest(mk("/b/shim")), FixDigest(mk("/a/shim"))
	if a == b {
		t.Error("the digest ignores the shown command")
	}
	if a != again {
		t.Error("the digest is not stable")
	}
}
