package offboard

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup"
)

const (
	deleteArgv = "/usr/bin/sudo /usr/sbin/sysadminctl -deleteUser workharbor"
	sudoV      = "/usr/bin/sudo -v"
)

// fakeHost never executes anything: it records every call and answers from
// tables, switching to the "after" answers once the delete argv was run.
type fakeHost struct {
	before, after map[string]string
	errs          map[string]error // errors of both states, by argv
	afterErrs     map[string]error
	deleted       bool
	failDelete    bool
	answer        string
	answerErr     error

	reads, ran, lines, confirms []string
	log                         *bytes.Buffer
	ranBeforeLog                bool
	afterHome                   bool
	failSudo                    bool
	onAsk                       func()
	leftovers                   []string
}

func (h *fakeHost) Output(_ context.Context, argv ...string) ([]byte, error) {
	k := strings.Join(argv, " ")
	h.reads = append(h.reads, k)
	outs, errs := h.before, h.errs
	if h.deleted {
		outs, errs = h.after, h.afterErrs
	}
	if e, ok := errs[k]; ok {
		return nil, e
	}
	if o, ok := outs[k]; ok {
		return []byte(o), nil
	}
	return nil, errors.New("exit status 1")
}

func (h *fakeHost) Run(_ context.Context, c doctor.Cmd) error {
	k := strings.Join(c.Full(), " ")
	h.ran = append(h.ran, k)
	if k == sudoV && h.failSudo {
		return errors.New("exit status 1")
	}
	if k == deleteArgv {
		h.ranBeforeLog = strings.Contains(h.log.String(), "result=started")
		h.deleted = true
		if h.failDelete {
			return errors.New("exit status 70")
		}
	}
	return nil
}
func (h *fakeHost) Open(context.Context, string) error { return nil }
func (h *fakeHost) Line(q string) (string, error) {
	h.lines = append(h.lines, q)
	return h.answer, h.answerErr
}
func (h *fakeHost) Secret(string) (string, error) { return "", errors.New("no secret") }
func (h *fakeHost) Confirm(q string) (bool, error) {
	h.confirms = append(h.confirms, q)
	return true, nil
}
func (h *fakeHost) Show(string) {}

const record = "RecordName: workharbor\nUniqueID: 502\nPrimaryGroupID: 20\nNFSHomeDirectory: /Users/workharbor\nRealName:\n WorkHarbor\n"

var dsclRead = "/usr/bin/dscl . -read /Users/workharbor RecordName UniqueID PrimaryGroupID NFSHomeDirectory RealName"

func newHost() *fakeHost {
	notFound := errors.New("exit status 56: <dscl_cmd> DS Error: -14136 (eDSRecordNotFound)")
	return &fakeHost{
		log: &bytes.Buffer{}, answer: "workharbor",
		before: map[string]string{
			"/usr/sbin/dseditgroup -o checkmember -m werner admin": "yes werner is a member of admin",
			dsclRead: record,
			"/usr/sbin/dseditgroup -o checkmember -m workharbor admin": "no workharbor is NOT a member of admin",
			"/usr/bin/id -Gn workharbor":                               "staff com.apple.access_ssh",
			"/usr/bin/stat -f %Su /dev/console":                        "werner\n",
		},
		after: map[string]string{
			"/usr/bin/dscl . -read /Groups/staff GroupMembership":                          "GroupMembership: root werner\n",
			"/usr/bin/dscl . -read /Groups/admin GroupMembership":                          "GroupMembership: root werner\n",
			"/usr/bin/dscl . -read /Groups/com.apple.access_ssh GroupMembership":           "No such key: GroupMembership\n",
			"/usr/bin/dscl . -read /Groups/com.apple.access_screensharing GroupMembership": "No such key: GroupMembership\n",
		},
		errs: map[string]error{},
		afterErrs: map[string]error{
			"/usr/bin/dscl . -read /Users/workharbor": notFound,
			"/usr/bin/id workharbor":                  errors.New("exit status 1: id: workharbor: no such user"),
		},
	}
}

func (h *fakeHost) deps() Deps {
	return Deps{
		Runner: h,
		Stat: func(string) (HomeInfo, error) {
			if h.deleted && !h.afterHome {
				return HomeInfo{}, fs.ErrNotExist
			}
			return HomeInfo{Dir: true, UID: 502, UIDKnown: true}, nil
		},
		ReadDir: func(string) ([]string, error) {
			if h.deleted && h.leftovers == nil {
				return nil, fs.ErrNotExist
			}
			return h.leftovers, nil
		},
	}
}

func inv() Invocation {
	return Invocation{RunUser: "werner", RunUID: 501, Delete: true, Terminal: true}
}

type outcome struct {
	refusals []Refusal
	code     int
	stdout   string
	stderr   string
}

// attempt is the command's flow: invocation guards, inspect, guards, then the
// real run when nothing refuses.
func attempt(h *fakeHost, in Invocation) outcome {
	var so, se bytes.Buffer
	o := Out{Out: &so, Err: &se}
	h.log = &se
	lg := Log{W: &se, Now: func() time.Time { return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC) }, Whr: "v0.0.0-test"}
	res := outcome{}
	if r := InvocationGuards(in); len(r) > 0 {
		res.refusals, res.code = r, r[0].Code
	} else {
		f := Inspect(context.Background(), h.deps(), in)
		if r := Guards(f); len(r) > 0 {
			res.refusals, res.code = r, r[0].Code
		} else if in.Delete {
			res.code = Execute(context.Background(), h, h.deps(), f, lg, o)
		}
	}
	res.stdout, res.stderr = so.String(), se.String()
	return res
}

func TestGuards(t *testing.T) {
	enoent := func(h *fakeHost) { h.errs[dsclRead] = errors.New("exit status 56: eDSRecordNotFound") }
	rec := func(old, repl string) func(*fakeHost) {
		return func(h *fakeHost) { h.before[dsclRead] = strings.Replace(record, old, repl, 1) }
	}
	tests := []struct {
		name  string
		guard string
		code  int
		mod   func(*fakeHost, *Invocation)
	}{
		{"G1 root", "G1", 2, func(_ *fakeHost, in *Invocation) { in.RunUID = 0; in.RunUser = "root" }},
		{"G2 no terminal", "G2", 2, func(_ *fakeHost, in *Invocation) { in.Terminal = false }},
		{"G3 answers alone", "G3", 2, func(_ *fakeHost, in *Invocation) { in.Delete, in.Answers = false, true }},
		{"G3 unattended with delete", "G3", 2, func(_ *fakeHost, in *Invocation) { in.Unattended = true }},
		{"G3 answers in the dry run", "G3", 2, func(_ *fakeHost, in *Invocation) { in.Delete, in.Answers = false, true }},
		{"G4 not admin", "G4", 2, func(h *fakeHost, _ *Invocation) {
			h.before["/usr/sbin/dseditgroup -o checkmember -m werner admin"] = "no werner is NOT a member of admin"
		}},
		{"G4 unknown", "G4", 2, func(h *fakeHost, _ *Invocation) {
			delete(h.before, "/usr/sbin/dseditgroup -o checkmember -m werner admin")
		}},
		{"G5 missing", "G5", 3, func(h *fakeHost, _ *Invocation) { enoent(h) }},
		{"G5 missing with legacy", "G5", 3, func(h *fakeHost, _ *Invocation) {
			enoent(h)
			h.before["/usr/bin/dscl . -read /Users/whr UniqueID"] = "UniqueID: 501"
		}},
		{"G6 dscl error", "G6", 1, func(h *fakeHost, _ *Invocation) { h.errs[dsclRead] = errors.New("exit status 1: permission denied") }},
		{"G6 no keys", "G6", 1, func(h *fakeHost, _ *Invocation) { h.before[dsclRead] = "garbage\n" }},
		{"G7 case variant", "G7", 5, func(h *fakeHost, _ *Invocation) { rec("RecordName: workharbor", "RecordName: Workharbor")(h) }},
		{"G7 alias second", "G7", 5, func(h *fakeHost, _ *Invocation) { rec("RecordName: workharbor", "RecordName: workharbor wh")(h) }},
		{"G8 several values", "G8", 5, func(h *fakeHost, _ *Invocation) { rec("UniqueID: 502", "UniqueID: 502 503")(h) }},
		{"G9 console unreadable", "G9", 5, func(h *fakeHost, _ *Invocation) { delete(h.before, "/usr/bin/stat -f %Su /dev/console") }},
		{"G7 alias primary", "G7", 5, func(h *fakeHost, _ *Invocation) { rec("RecordName: workharbor", "RecordName: wh workharbor")(h) }},
		{"G8 499", "G8", 5, func(h *fakeHost, _ *Invocation) { rec("502", "499")(h) }},
		{"G8 unparsable", "G8", 5, func(h *fakeHost, _ *Invocation) { rec("UniqueID: 502", "UniqueID: abc")(h) }},
		{"G9 same uid", "G9", 5, func(_ *fakeHost, in *Invocation) { in.RunUID = 502 }},
		{"G9 run user by name", "G9", 5, func(h *fakeHost, in *Invocation) {
			in.RunUser = "workharbor"
			h.before["/usr/sbin/dseditgroup -o checkmember -m workharbor admin"] = "yes workharbor is a member of admin"
		}},
		{"G9 sudo user", "G9", 5, func(_ *fakeHost, in *Invocation) { in.SudoUser = "workharbor" }},
		{"G9 console owner", "G9", 5, func(h *fakeHost, _ *Invocation) { h.before["/usr/bin/stat -f %Su /dev/console"] = "workharbor\n" }},
		{"G10 admin", "G10", 5, func(h *fakeHost, _ *Invocation) {
			h.before["/usr/sbin/dseditgroup -o checkmember -m workharbor admin"] = "yes workharbor is a member of admin"
		}},
		{"G10 unknown", "G10", 5, func(h *fakeHost, _ *Invocation) {
			delete(h.before, "/usr/sbin/dseditgroup -o checkmember -m workharbor admin")
		}},
		{"G11 other home", "G11", 5, func(h *fakeHost, _ *Invocation) { rec("/Users/workharbor", "/Users/other")(h) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost()
			in := inv()
			tc.mod(h, &in)
			r := attempt(h, in)
			found := false
			for _, x := range r.refusals {
				found = found || x.Guard == tc.guard && x.Code == tc.code
			}
			if !found {
				t.Errorf("refusals %+v, want %s with %d", r.refusals, tc.guard, tc.code)
			}
			if len(h.ran) != 0 || len(h.lines) != 0 || len(h.confirms) != 0 || h.deleted {
				t.Errorf("a refused run ran %v asked %v", h.ran, h.lines)
			}
		})
	}
}

func TestGuardsOfTheHome(t *testing.T) {
	for name, hi := range map[string]HomeInfo{
		"symlink":       {Symlink: true, Dir: true, UID: 502, UIDKnown: true},
		"not a dir":     {UID: 502, UIDKnown: true},
		"other owner":   {Dir: true, UID: 0, UIDKnown: true},
		"owner unknown": {Dir: true},
	} {
		h := newHost()
		d := h.deps()
		d.Stat = func(string) (HomeInfo, error) { return hi, nil }
		f := Inspect(context.Background(), d, inv())
		rs := Guards(f)
		if len(rs) != 1 || rs[0].Guard != "G11" || rs[0].Code != exitcode.Conflict {
			t.Errorf("%s: %+v", name, rs)
		}
	}
	h := newHost()
	d := h.deps()
	d.Stat = func(string) (HomeInfo, error) { return HomeInfo{}, fs.ErrNotExist }
	f := Inspect(context.Background(), d, inv())
	if rs := Guards(f); len(rs) != 0 || len(f.Notes) == 0 {
		t.Errorf("a missing home is allowed with a note: %+v %v", rs, f.Notes)
	}
	d.Stat = func(string) (HomeInfo, error) { return HomeInfo{}, os.ErrPermission }
	if rs := Guards(Inspect(context.Background(), d, inv())); len(rs) != 1 || rs[0].Guard != "G11" {
		t.Errorf("an unreadable home is refused: %+v", rs)
	}
}

func TestTheUserIDBoundary(t *testing.T) {
	h := newHost()
	h.before[dsclRead] = strings.Replace(record, "502", "500", 1)
	if rs := Guards(Inspect(context.Background(), h.deps(), inv())); len(rs) != 1 || rs[0].Guard != "G11" {
		t.Errorf("500 is accepted by G8 (only the home owner 502 differs): %+v", rs)
	}
	h = newHost()
	h.before[dsclRead] = strings.Replace(record, "502", "500", 1)
	d := h.deps()
	d.Stat = func(string) (HomeInfo, error) { return HomeInfo{Dir: true, UID: 500, UIDKnown: true}, nil }
	if rs := Guards(Inspect(context.Background(), d, inv())); len(rs) != 0 {
		t.Errorf("500 is accepted: %+v", rs)
	}
}

func TestTheTypedWord(t *testing.T) {
	for _, ans := range []string{"", "y", "yes", "Workharbor", "whr", "q", "quit", "workharbor now"} {
		h := newHost()
		h.answer = ans
		r := attempt(h, inv())
		if r.code != exitcode.Usage || !strings.Contains(r.stderr, "not confirmed: nothing was removed") || len(h.ran) != 0 || h.deleted {
			t.Errorf("%q: exit %d ran %v", ans, r.code, h.ran)
		}
		if strings.Contains(r.stderr, "log:") {
			t.Errorf("%q: logged a change that did not happen", ans)
		}
	}
	h := newHost()
	h.answerErr = errors.New("no answer: standard input ended")
	if r := attempt(h, inv()); r.code != exitcode.Usage || len(h.ran) != 0 {
		t.Errorf("EOF: exit %d ran %v", r.code, h.ran)
	}
}

func TestTheHappyPath(t *testing.T) {
	h := newHost()
	r := attempt(h, inv())
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if got := strings.Join(h.ran, "|"); got != sudoV+"|"+deleteArgv {
		t.Errorf("ran %q", got)
	}
	if !h.ranBeforeLog {
		t.Error("the log line did not come before the change")
	}
	if len(h.lines) != 1 || h.lines[0] != "Delete the account workharbor?" || len(h.confirms) != 0 {
		t.Errorf("asked %v %v", h.lines, h.confirms)
	}
	if !strings.Contains(r.stderr, "ACTION  This deletes the macOS account workharbor and its home folder /Users/workharbor. It cannot be undone.") {
		t.Errorf("no ACTION text: %s", r.stderr)
	}
	for _, l := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		if !strings.HasPrefix(l, "ok\t") {
			t.Errorf("not ok: %q", l)
		}
	}
	started := strings.Index(r.stderr, "result=started")
	ok := strings.Index(r.stderr, "result=ok exit=0")
	if started < 0 || ok < started || strings.Count(r.stderr, "log: 2026-10-06T12:00:00Z offboard.delete-user argv-sha256=") != 2 || !strings.Contains(r.stderr, "source=interactive") || !strings.Contains(r.stderr, "whr=v0.0.0-test") {
		t.Errorf("log lines: %s", r.stderr)
	}
	if !strings.HasSuffix(strings.TrimSpace(r.stderr), Unverified) {
		t.Errorf("does not end with the unverified line: %s", r.stderr)
	}
	if strings.LastIndex(r.stderr, "group:com.apple.access_screensharing") > ok {
		t.Errorf("the result line came before the verification: %s", r.stderr)
	}
}

func TestTheLogHoldsNoPathAndNoAnswer(t *testing.T) {
	h := newHost()
	h.answer = "  workharbor \n"
	r := attempt(h, inv())
	for _, l := range strings.Split(r.stderr, "\n") {
		if strings.HasPrefix(l, "log:") && (strings.Contains(l, "/Users") || strings.Contains(l, "werner") || strings.Contains(l, "workharbor")) {
			t.Errorf("the log line leaks: %q", l)
		}
	}
	if r.code != 0 {
		t.Errorf("surrounding space is trimmed: exit %d", r.code)
	}
}

func TestADryRunRunsNothing(t *testing.T) {
	h := newHost()
	in := inv()
	in.Delete, in.Terminal = false, false
	f := Inspect(context.Background(), h.deps(), in)
	if rs := Guards(f); len(rs) != 0 {
		t.Fatalf("%+v", rs)
	}
	var so, se bytes.Buffer
	Plan(Out{Out: &so, Err: &se}, f)
	if len(h.ran)+len(h.lines)+len(h.confirms) != 0 {
		t.Errorf("a dry run ran %v asked %v", h.ran, h.lines)
	}
	for _, want := range []string{"account\tworkharbor", "uid\t502", "home\t/Users/workharbor", "groups\tstaff com.apple.access_ssh", "admin\tno", "command\t" + deleteArgv} {
		if !strings.Contains(so.String(), want+"\n") {
			t.Errorf("plan lacks %q:\n%s", want, so.String())
		}
	}
	if !strings.Contains(se.String(), "sysadminctl also removes its Public share point and kills its processes (observed once, unverified)") {
		t.Errorf("no fixed note: %s", se.String())
	}
}

func TestAFailedDeleteStillVerifies(t *testing.T) {
	h := newHost()
	h.failDelete = true
	r := attempt(h, inv())
	if r.code != exitcode.Error || !strings.Contains(r.stderr, "result=failed exit=1") || !strings.Contains(r.stdout, "ok\tdscl") {
		t.Errorf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}

func TestEachRemnantIsNamed(t *testing.T) {
	tests := []struct {
		name, line string
		mod        func(*fakeHost)
	}{
		{"dscl", "fail\tdscl\t", func(h *fakeHost) {
			delete(h.afterErrs, "/usr/bin/dscl . -read /Users/workharbor")
			h.after["/usr/bin/dscl . -read /Users/workharbor"] = record
		}},
		{"id", "fail\tid\t", func(h *fakeHost) {
			delete(h.afterErrs, "/usr/bin/id workharbor")
			h.after["/usr/bin/id workharbor"] = "uid=502"
		}},
		{"home", "fail\thome\t", func(h *fakeHost) { h.afterHome = true }},
		{"deleted users", "fail\tdeleted-users\t", func(h *fakeHost) { h.leftovers = []string{"workharbor-old", "other"} }},
		{"group", "fail\tgroup:com.apple.access_ssh\t", func(h *fakeHost) {
			h.after["/usr/bin/dscl . -read /Groups/com.apple.access_ssh GroupMembership"] = "GroupMembership: root workharbor\n"
		}},
		{"dscl not verified", "not_verified\tdscl\t", func(h *fakeHost) {
			h.afterErrs["/usr/bin/dscl . -read /Users/workharbor"] = errors.New("exit status 1: no permission")
		}},
		{"id not verified", "not_verified\tid\t", func(h *fakeHost) { h.afterErrs["/usr/bin/id workharbor"] = errors.New("exit status 1: boom") }},
		{"group not verified", "not_verified\tgroup:admin\t", func(h *fakeHost) { delete(h.after, "/usr/bin/dscl . -read /Groups/admin GroupMembership") }},
	}
	for _, tc := range tests {
		h := newHost()
		tc.mod(h)
		r := attempt(h, inv())
		if r.code != exitcode.Error || !strings.Contains(r.stdout, tc.line) {
			t.Errorf("%s: exit %d\n%s", tc.name, r.code, r.stdout)
		}
		if tc.name == "group" && !strings.Contains(r.stderr, "sudo dseditgroup -o edit -d workharbor -t user com.apple.access_ssh") {
			t.Errorf("no fix shown: %s", r.stderr)
		}
		for _, ran := range h.ran {
			if strings.Contains(ran, "dseditgroup") {
				t.Errorf("a fix was run: %s", ran)
			}
		}
	}
}

// AskWord is the terminal's own: render.AskWord over what the person "types".
func (h *fakeHost) AskWord(q, word string) (render.Answer, error) {
	h.lines = append(h.lines, q)
	if h.onAsk != nil {
		h.onAsk()
	}
	in := h.answer + "\n"
	if h.answerErr != nil {
		in = ""
	}
	return render.AskWord(bufio.NewReader(strings.NewReader(in)), render.Writer{W: h.log}, q, word)
}

// A host that cannot ask for a typed word never confirms.
func TestAHostWithoutATypedWordNeverConfirms(t *testing.T) {
	h := newHost()
	f := Inspect(context.Background(), h.deps(), inv())
	var so, se bytes.Buffer
	code := Execute(context.Background(), struct{ setup.Host }{h}, h.deps(), f, Log{W: &se, Now: time.Now}, Out{Out: &so, Err: &se})
	if code != exitcode.Usage || len(h.ran) != 0 || strings.Contains(se.String(), "log:") {
		t.Errorf("exit %d ran %v\n%s", code, h.ran, se.String())
	}
}

func TestTheAccountIsInspectedAgainAfterTheWord(t *testing.T) {
	h := newHost()
	h.onAsk = func() { h.before["/usr/bin/stat -f %Su /dev/console"] = "workharbor\n" }
	r := attempt(h, inv())
	if r.code != exitcode.Conflict || len(h.ran) != 0 || strings.Contains(r.stderr, "log:") || !strings.Contains(r.stderr, "changed since the plan") {
		t.Errorf("exit %d ran %v\n%s", r.code, h.ran, r.stderr)
	}
	// a change no guard refuses is still a change from the plan
	h = newHost()
	h.onAsk = func() { h.before["/usr/bin/id -Gn workharbor"] = "staff com.apple.access_ssh admin2" }
	r = attempt(h, inv())
	if r.code != exitcode.Conflict || len(h.ran) != 0 {
		t.Errorf("a changed group list: exit %d ran %v", r.code, h.ran)
	}
}

func TestTheDeleteIsNotRunWhenSudoRefuses(t *testing.T) {
	h := newHost()
	h.failSudo = true
	r := attempt(h, inv())
	if r.code != exitcode.Error || strings.Join(h.ran, "|") != sudoV || h.deleted {
		t.Errorf("exit %d ran %v", r.code, h.ran)
	}
	if !strings.Contains(r.stderr, "sudo did not accept") || !strings.Contains(r.stderr, "result=failed exit=1") {
		t.Errorf("%s", r.stderr)
	}
}

func TestTheFinalLogLineMatchesTheExitCode(t *testing.T) {
	h := newHost()
	h.after["/usr/bin/dscl . -read /Groups/admin GroupMembership"] = "GroupMembership: root workharbor\n"
	r := attempt(h, inv())
	if r.code != exitcode.Error || !strings.Contains(r.stderr, "result=failed exit=1") || strings.Contains(r.stderr, "result=ok") {
		t.Errorf("a remnant after a clean delete: exit %d\n%s", r.code, r.stderr)
	}
}

func TestOnlySafeGroupNamesAreVerified(t *testing.T) {
	h := newHost()
	h.before["/usr/bin/id -Gn workharbor"] = "staff bad;name $(x)"
	attempt(h, inv())
	for _, k := range h.reads {
		if strings.Contains(k, "bad;name") || strings.Contains(k, "$(x)") {
			t.Errorf("an unsafe group name reached a command: %q", k)
		}
	}
}

func TestTheReadToolsRunByAbsolutePath(t *testing.T) {
	h := newHost()
	attempt(h, inv())
	for _, k := range h.reads {
		if !strings.HasPrefix(k, "/usr/") {
			t.Errorf("run by name: %q", k)
		}
	}
}

func TestRecorderFailureBeforeAndAfterExecution(t *testing.T) {
	for _, failAt := range []string{"started", "ok"} {
		t.Run(failAt, func(t *testing.T) {
			h := newHost()
			d := h.deps()
			f := Inspect(context.Background(), d, inv())
			var out bytes.Buffer
			lg := Log{Record: func(result string, _ int, ran [][]string) error {
				if result == "ok" && len(ran) != 2 {
					t.Fatalf("attempted commands: %v", ran)
				}
				if result == failAt {
					return errors.New("disk full")
				}
				return nil
			}}
			code := Execute(context.Background(), h, d, f, lg, Out{Out: &out, Err: &out})
			if code != exitcode.Error || !strings.Contains(out.String(), "disk full") {
				t.Fatalf("%d %s", code, out.String())
			}
			if failAt == "started" && len(h.ran) != 0 {
				t.Fatalf("ran after recording failed: %v", h.ran)
			}
			if failAt == "ok" && !h.deleted {
				t.Fatal("expected deletion before final record failure")
			}
		})
	}
}

func TestThePlanShowsTheHomeSize(t *testing.T) {
	plan := func(h *fakeHost) (string, string) {
		f := Inspect(context.Background(), h.deps(), inv())
		var so, se bytes.Buffer
		Plan(Out{Out: &so, Err: &se}, f)
		return so.String(), se.String()
	}
	h := newHost()
	h.before["/usr/bin/du -skx /Users/workharbor"] = "2048\t/Users/workharbor\n"
	so, _ := plan(h)
	if !strings.Contains(so, "home-size\t2048 KiB\n") {
		t.Errorf("no size:\n%s", so)
	}
	// An unreadable size is said, never guessed.
	so, se := plan(newHost())
	if !strings.Contains(so, "home-size\tunknown\n") || !strings.Contains(se, "home size could not be read") {
		t.Errorf("unknown size not said:\n%s\n%s", so, se)
	}
	// A garbled answer is unknown too.
	h = newHost()
	h.before["/usr/bin/du -skx /Users/workharbor"] = "lots\n"
	if so, _ := plan(h); !strings.Contains(so, "home-size\tunknown\n") {
		t.Errorf("garbled size taken:\n%s", so)
	}
	// A missing home has nothing to measure and is not measured.
	h = newHost()
	h.afterHome, h.deleted = false, true
	f := Inspect(context.Background(), h.deps(), inv())
	for _, r := range h.reads {
		if strings.Contains(r, "du ") {
			t.Errorf("measured a missing home: %s", r)
		}
	}
	_ = f
}

func TestTheSizeIsNotPartOfTheRecheck(t *testing.T) {
	h := newHost()
	h.before["/usr/bin/du -skx /Users/workharbor"] = "2048\t/Users/workharbor\n"
	f := Inspect(context.Background(), h.deps(), inv())
	h.before["/usr/bin/du -skx /Users/workharbor"] = "4096\t/Users/workharbor\n"
	h.answer = "workharbor"
	var so, se bytes.Buffer
	h.log = &se
	lg := Log{W: &se, Now: time.Now, Whr: "t"}
	if c := Execute(context.Background(), h, h.deps(), f, lg, Out{Out: &so, Err: &se}); c != exitcode.OK {
		t.Errorf("a growing home blocked the delete: %d\n%s", c, se.String())
	}
}

func TestOnlyOneExactDULineIsASize(t *testing.T) {
	for out, ok := range map[string]bool{
		"2048\t/Users/workharbor\n":                    true,
		"0\t/Users/workharbor\n":                       true,
		"+7\t/Users/workharbor\n":                      false,
		"-5\t/Users/workharbor\n":                      false,
		"7\t/Users/workharbor\n8\t/Users/workharbor\n": false,
		"7\t/Users/other\n":                            false,
		"\t/Users/workharbor\n":                        false,
		"99999999999999999999\t/Users/workharbor\n":    false,
	} {
		if _, got := parseDU(out, "/Users/workharbor"); got != ok {
			t.Errorf("%q: %v, want %v", out, got, ok)
		}
	}
}

func TestTheSizeNoteAloneIsIgnoredByTheRecheck(t *testing.T) {
	a := []string{"x", "the home size could not be read: y"}
	if got := withoutSizeNote(a); len(got) != 1 || got[0] != "x" {
		t.Errorf("%v", got)
	}
	h := newHost()
	h.before["/usr/bin/du -skx /Users/workharbor"] = "1\t/Users/workharbor\n"
	f := Inspect(context.Background(), h.deps(), inv())
	f.Notes = append(f.Notes, "another note")
	var so, se bytes.Buffer
	h.log = &se
	lg := Log{W: &se, Now: time.Now, Whr: "t"}
	if c := Execute(context.Background(), h, h.deps(), f, lg, Out{Out: &so, Err: &se}); c != exitcode.Conflict {
		t.Errorf("other note drift did not block: %d", c)
	}
}

func TestASizeThatBecomesUnreadableDoesNotBlock(t *testing.T) {
	h := newHost()
	h.before["/usr/bin/du -skx /Users/workharbor"] = "1\t/Users/workharbor\n"
	f := Inspect(context.Background(), h.deps(), inv())
	delete(h.before, "/usr/bin/du -skx /Users/workharbor")
	var so, se bytes.Buffer
	h.log = &se
	lg := Log{W: &se, Now: time.Now, Whr: "t"}
	if c := Execute(context.Background(), h, h.deps(), f, lg, Out{Out: &so, Err: &se}); c != exitcode.OK {
		t.Errorf("blocked: %d\n%s", c, se.String())
	}
}
