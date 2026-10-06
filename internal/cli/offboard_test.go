package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/offboard"
	"github.com/wstein/workharbor/internal/render"
)

const offboardDelete = "/usr/bin/sudo /usr/sbin/sysadminctl -deleteUser workharbor"

// offboardHost executes nothing: it records, and flips to the "after" answers
// when the delete argv is run.
type offboardHost struct {
	deleted bool
	answer  string
	ran     []string
	asked   []string
}

func (h *offboardHost) Output(_ context.Context, argv ...string) ([]byte, error) {
	k := strings.Join(argv, " ")
	switch {
	case k == "/usr/sbin/dseditgroup -o checkmember -m werner admin":
		return []byte("yes werner is a member of admin"), nil
	case k == "/usr/sbin/dseditgroup -o checkmember -m workharbor admin":
		return []byte("no workharbor is NOT a member of admin"), nil
	case strings.HasPrefix(k, "/usr/bin/dscl . -read /Users/workharbor RecordName"):
		return []byte("RecordName: workharbor\nUniqueID: 502\nPrimaryGroupID: 20\nNFSHomeDirectory: /Users/workharbor\nRealName:\n WorkHarbor\n"), nil
	case k == "/usr/bin/dscl . -read /Users/workharbor":
		return nil, errors.New("exit status 56: eDSRecordNotFound")
	case k == "/usr/bin/id -Gn workharbor":
		return []byte("staff"), nil
	case k == "/usr/bin/id workharbor":
		return nil, errors.New("exit status 1: id: workharbor: no such user")
	case k == "/usr/bin/stat -f %Su /dev/console":
		return []byte("werner\n"), nil
	case strings.HasPrefix(k, "/usr/bin/dscl . -read /Groups/"):
		return []byte("No such key: GroupMembership\n"), nil
	}
	return nil, errors.New("exit status 1")
}

func (h *offboardHost) Run(_ context.Context, c doctor.Cmd) error {
	k := strings.Join(c.Full(), " ")
	h.ran = append(h.ran, k)
	h.deleted = h.deleted || k == offboardDelete
	return nil
}
func (h *offboardHost) Open(context.Context, string) error { return nil }
func (h *offboardHost) Line(q string) (string, error) {
	h.asked = append(h.asked, q)
	return h.answer, nil
}
func (h *offboardHost) Secret(string) (string, error) { return "", errors.New("no secret") }
func (h *offboardHost) Confirm(q string) (bool, error) {
	h.asked = append(h.asked, q)
	return true, nil
}
func (h *offboardHost) Show(string) {}

type offboardRig struct {
	t    *testing.T
	host *offboardHost
	rig  *setupRig
	env  Env
}

func newOffboardRig(t *testing.T) *offboardRig {
	t.Helper()
	s := newSetupRig(t)
	h := &offboardHost{answer: "workharbor"}
	s.env.Host = h
	r := &offboardRig{t: t, host: h, rig: s}
	r.env = Env{Stdin: strings.NewReader(""), Getenv: func(string) string { return "" }, Setup: s.env}
	r.env.Offboard = OffboardEnv{
		Prefix: filepath.Dir(filepath.Dir(s.exe)),
		Stat: func(string) (offboard.HomeInfo, error) {
			if h.deleted {
				return offboard.HomeInfo{}, fs.ErrNotExist
			}
			return offboard.HomeInfo{Dir: true, UID: 502, UIDKnown: true}, nil
		},
		ReadDir: func(string) ([]string, error) { return nil, fs.ErrNotExist },
	}
	return r
}

func (r *offboardRig) run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	env := r.env
	env.Stdout, env.Stderr = &out, &errOut
	code := Execute(context.Background(), env, append([]string{"offboard", "host"}, args...))
	return code, out.String(), errOut.String()
}

func (r *offboardRig) nothingRan(label string) {
	r.t.Helper()
	if len(r.host.ran) != 0 || len(r.host.asked) != 0 {
		r.t.Errorf("%s: ran %v asked %v", label, r.host.ran, r.host.asked)
	}
}

func TestOffboardDryRunIsTheDefault(t *testing.T) {
	r := newOffboardRig(t)
	code, out, errOut := r.run()
	if code != exitcode.OK || !strings.Contains(out, "command\t"+offboardDelete+"\n") || !strings.Contains(errOut, "dry run: nothing was changed") {
		t.Errorf("exit %d\n%s\n%s", code, out, errOut)
	}
	r.nothingRan("dry run")
	r.rig.env.IsTerminal = func() bool { return false }
	r.env.Setup = r.rig.env
	if code, _, _ := r.run(); code != exitcode.OK {
		t.Errorf("a dry run needs no terminal: exit %d", code)
	}
	r.nothingRan("dry run without a terminal")
}

func TestOffboardRefusesAnswersAndUnattendedEverywhere(t *testing.T) {
	for _, args := range [][]string{
		{"--answers", "a.json"},
		{"--answers="},
		{"--delete", "--answers="},
		{"--unattended"},
		{"--delete", "--answers", "a.json"},
		{"--delete", "--unattended"},
	} {
		r := newOffboardRig(t)
		code, _, errOut := r.run(args...)
		if code != exitcode.Usage || !strings.Contains(errOut, "--answers and --unattended are refused") {
			t.Errorf("%v: exit %d %s", args, code, errOut)
		}
		r.nothingRan(strings.Join(args, " "))
	}
}

func TestOffboardInvocationGuards(t *testing.T) {
	r := newOffboardRig(t)
	if code, _, errOut := r.run("--json"); code != exitcode.Usage || !strings.Contains(errOut, "not supported yet") {
		t.Errorf("--json: %d %s", code, errOut)
	}
	r = newOffboardRig(t)
	r.rig.env.IsTerminal = func() bool { return false }
	r.env.Setup = r.rig.env
	if code, _, errOut := r.run("--delete"); code != exitcode.Usage || !strings.Contains(errOut, "needs a terminal") {
		t.Errorf("piped --delete: %d %s", code, errOut)
	}
	r.nothingRan("piped")
	r = newOffboardRig(t)
	r.rig.env.User, r.rig.env.UID = "root", 0
	r.env.Setup = r.rig.env
	if code, _, _ := r.run("--delete"); code != exitcode.Usage {
		t.Errorf("root: %d", code)
	}
	r.nothingRan("root")
	r = newOffboardRig(t)
	r.rig.env.Executable = func() (string, error) { return "/tmp/elsewhere/whr", nil }
	r.env.Setup = r.rig.env
	code, _, errOut := r.run("--delete")
	if code != exitcode.Usage {
		t.Errorf("not installed: %d %s", code, errOut)
	}
	r.nothingRan("not installed")
	if code, _, errOut := r.run(); code != exitcode.OK || !strings.Contains(errOut, "note (dry run)") {
		t.Errorf("dry run, not installed: %d %s", code, errOut)
	}
}

func TestOffboardDeleteAndTheTypedWord(t *testing.T) {
	r := newOffboardRig(t)
	r.host.answer = "yes"
	code, _, errOut := r.run("--delete")
	if code != exitcode.Usage || !strings.Contains(errOut, "not confirmed: nothing was removed") || len(r.host.ran) != 0 {
		t.Errorf("wrong word: %d %s %v", code, errOut, r.host.ran)
	}
	r = newOffboardRig(t)
	code, out, errOut := r.run("--delete")
	if code != exitcode.OK || strings.Join(r.host.ran, "|") != "/usr/bin/sudo -v|"+offboardDelete {
		t.Fatalf("exit %d ran %v\n%s\n%s", code, r.host.ran, out, errOut)
	}
	if !strings.Contains(out, "ok\tdscl\t") || strings.Count(errOut, "offboard.delete-user") != 2 {
		t.Errorf("%s\n%s", out, errOut)
	}
}

func TestOffboardHelpPointsAtTheManual(t *testing.T) {
	r := newOffboardRig(t)
	var out bytes.Buffer
	env := r.env
	env.Stdout, env.Stderr = &out, &out
	Execute(context.Background(), env, []string{"offboard", "host", "--help"})
	for _, w := range []string{"Remove the workharbor account", "unverified", "provisional"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("help lacks %q", w)
		}
	}
}

// AskWord is the terminal's own: render.AskWord over what the person "types".
func (h *offboardHost) AskWord(q, word string) (render.Answer, error) {
	h.asked = append(h.asked, q)
	return render.AskWord(bufio.NewReader(strings.NewReader(h.answer+"\n")), render.Writer{W: io.Discard}, q, word)
}

func TestOffboardRefusesWhereThereIsNoMac(t *testing.T) {
	r := newOffboardRig(t)
	r.rig.env.GOOS = "linux"
	r.env.Setup = r.rig.env
	if code, _, errOut := r.run(); code != exitcode.Usage || !strings.Contains(errOut, "only on a Mac") {
		t.Errorf("exit %d %s", code, errOut)
	}
	if len(r.host.ran) != 0 {
		t.Error("ran something")
	}
}
