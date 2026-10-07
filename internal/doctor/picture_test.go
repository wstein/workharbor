package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const readPicture = "dscl . -read /Users/workharbor Picture"

func pictureDeps(t *testing.T, r Runner) Deps {
	t.Helper()
	d := hostDeps(r)
	d.PictureFile = filepath.Join(t.TempDir(), "logo.png")
	return d
}

func TestLoginPictureStates(t *testing.T) {
	d := pictureDeps(t, scripted{})
	file := d.PictureFile
	run := func(r Runner) (Status, string) {
		d.Runner = r
		return status(steps(t, d)["login-picture"])
	}
	// the attribute is not set: the macOS form is "Picture:" and nothing
	if st, msg := run(scripted{readPicture: "Picture:\n"}); st != Fail || !strings.Contains(msg, "unset") {
		t.Errorf("unset: %s %q", st, msg)
	}
	// set, but the file is missing
	if st, _ := run(scripted{readPicture: "Picture:\n " + file + "\n"}); st != Fail {
		t.Errorf("missing file: %s", st)
	}
	// set, but the file is something else (the account could not write there; root's file changed)
	if err := os.WriteFile(file, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, _ := run(scripted{readPicture: "Picture:\n " + file + "\n"}); st != Fail {
		t.Errorf("other bytes: %s", st)
	}
	// another picture path
	if st, _ := run(scripted{readPicture: "Picture:\n /Library/User Pictures/Animals/Penguin.heic\n"}); st != Fail {
		t.Errorf("other path: %s", st)
	}
	if err := os.WriteFile(file, loginPicture, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"Picture:\n " + file + "\n", "Picture: " + file + "\n"} {
		if st, msg := run(scripted{readPicture: out}); st != OK {
			t.Errorf("set %q: %s %q", out, st, msg)
		}
	}
	// dscl fails, or this is not a Mac
	if st, _ := run(scripted{readPicture: "ERR:exit status 56"}); st != NotVerified {
		t.Errorf("no user: %s", st)
	}
	if st, _ := run(scripted{}); st != NotVerified {
		t.Errorf("dscl unanswered: %s", st)
	}
	d.GOOS = "linux"
	if st, _ := run(scripted{}); st != NotVerified {
		t.Errorf("not a Mac: %s", st)
	}
}

// A fake dscl and a fake install: the fix runs against a state, sets the picture
// once, and the second check passes without another change.
func TestLoginPictureSetsOnceThenIdempotent(t *testing.T) {
	d := pictureDeps(t, nil)
	var picture string
	fake := funcRunner(func(argv ...string) (string, bool) {
		if strings.Join(argv, " ") == readPicture {
			return "Picture:\n " + picture + "\n", true
		}
		return "", false
	})
	d.Runner = fake
	c := steps(t, d)["login-picture"]
	if st, _ := status(c); st != Fail {
		t.Fatalf("before: %s", st)
	}
	fix := c.Fix
	if err := fix.Do(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(loginPictureTemp())
	if err != nil || string(b) != string(loginPicture) {
		t.Fatalf("temp file: %v", err)
	}
	if fi, _ := os.Stat(loginPictureTemp()); fi.Mode().Perm() != 0o600 {
		t.Errorf("temp mode %v", fi.Mode().Perm())
	}
	// run the commands the way the host would, against the fakes
	for _, cmd := range fix.Cmds {
		if !cmd.Sudo || cmd.SecretPrompt != "" {
			t.Errorf("%v: want plain sudo, no secret", cmd)
		}
		switch cmd.Argv[0] {
		case "install":
			if cmd.Argv[1] == "-d" {
				continue
			}
			if got := cmd.Argv[len(cmd.Argv)-1]; got != d.PictureFile {
				t.Errorf("install target %q", got)
			}
			if err := os.WriteFile(d.PictureFile, b, 0o600); err != nil { //nolint:gosec // a test temp path
				t.Fatal(err)
			}
		case "dscl":
			want := []string{"dscl", ".", "-create", "/Users/workharbor", "Picture", d.PictureFile}
			if strings.Join(cmd.Argv, "\x00") != strings.Join(want, "\x00") {
				t.Errorf("dscl argv %q", cmd.Argv)
			}
			picture = cmd.Argv[len(cmd.Argv)-1]
		default:
			t.Errorf("unexpected command %v", cmd.Argv)
		}
	}
	if st, msg := status(c); st != OK {
		t.Fatalf("after: %s %q", st, msg)
	}
	// idempotent: nothing differs, so a second check wants no change
	if st, _ := status(steps(t, d)["login-picture"]); st != OK {
		t.Errorf("second: %s", st)
	}
	for _, cmd := range fix.Cmds {
		for _, a := range cmd.Argv {
			if strings.Contains(strings.ToLower(a), "password") {
				t.Errorf("a password word in %v", cmd.Argv)
			}
		}
	}
}

func TestLoginPictureEmbeddedIsSquarePNG(t *testing.T) {
	if len(loginPicture) < 8 || string(loginPicture[1:4]) != "PNG" {
		t.Fatal("the embedded login picture is not a PNG")
	}
	w := int(loginPicture[16])<<24 | int(loginPicture[17])<<16 | int(loginPicture[18])<<8 | int(loginPicture[19])
	h := int(loginPicture[20])<<24 | int(loginPicture[21])<<16 | int(loginPicture[22])<<8 | int(loginPicture[23])
	if w != h || w == 0 {
		t.Errorf("picture is %dx%d, want square", w, h)
	}
}

type funcRunner func(argv ...string) (string, bool)

func (f funcRunner) Output(_ context.Context, argv ...string) ([]byte, error) {
	if out, ok := f(argv...); ok {
		return []byte(out), nil
	}
	return nil, os.ErrNotExist
}
