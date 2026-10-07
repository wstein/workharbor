// Package offboard removes the workharbor macOS account that `whr setup host`
// created (issue #346, provisional). It inspects read-only, checks the guards as
// a pure function over the facts, shows a plan, and only on request and after
// the typed word runs `sudo sysadminctl -deleteUser workharbor`, then verifies.
//
// Every macOS command and every output format here is unverified on macOS 26.
package offboard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup"
)

// Word is what the person types to confirm; the comparison is exact.
const Word = doctor.WhrUser

// Unverified ends the output of a run.
const Unverified = "macOS commands are unverified on macOS 26 (#346)"

const (
	deletedUsers = "/Users/Deleted Users"
	sysadminctlN = "also removes its Public share point and kills its processes (observed once, unverified)"
)

// The commands run by absolute path, so a PATH of the session cannot swap them.
const (
	sudoBin        = "/usr/bin/sudo"
	sysadminctlBin = "/usr/sbin/sysadminctl"
)

var readTools = map[string]string{
	"dscl": "/usr/bin/dscl", "id": "/usr/bin/id", "stat": "/usr/bin/stat", "dseditgroup": "/usr/sbin/dseditgroup",
}

// absRunner runs the read-only tools by absolute path.
type absRunner struct{ doctor.Runner }

func (r absRunner) Output(ctx context.Context, argv ...string) ([]byte, error) {
	if abs, ok := readTools[argv[0]]; ok {
		argv = append([]string{abs}, argv[1:]...)
	}
	return r.Runner.Output(ctx, argv...)
}

// DeleteCmd is the one command that changes the machine. sudo is part of the
// argv, not Cmd.Sudo, so that it is run by absolute path.
func DeleteCmd() doctor.Cmd {
	return doctor.Cmd{Argv: []string{sudoBin, sysadminctlBin, "-deleteUser", doctor.WhrUser}}
}

func sudoCheckCmd() doctor.Cmd { return doctor.Cmd{Argv: []string{sudoBin, "-v"}} }

// HomeInfo is what Lstat says about the home folder.
type HomeInfo struct {
	Symlink, Dir bool
	UID          int
	UIDKnown     bool
}

// StatFunc is os.Lstat as far as this package needs it.
type StatFunc func(path string) (HomeInfo, error)

// Deps is what the package touches: the read-only runner, Lstat and ReadDir.
type Deps struct {
	Runner  doctor.Runner
	Stat    StatFunc
	ReadDir func(path string) ([]string, error)
}

// Invocation is what the command line and the running process say.
type Invocation struct {
	RunUser, SudoUser             string
	RunUID                        int
	Delete, AllowAdmin            bool
	Answers, Unattended, Terminal bool
}

// Facts is everything the guards decide on.
type Facts struct {
	Invocation
	Account string

	RunnerAdmin, RunnerAdminKnown bool

	Found     bool
	Legacy    bool   // the account is missing but legacy `whr` exists
	Ambiguous string // dscl said neither "not found" nor a parseable record

	RecordNames []string
	UID         int
	UIDOK       bool
	HomeDir     string

	Admin, AdminKnown bool
	Groups            []string
	ConsoleOwner      string
	ConsoleKnown      bool

	Home      HomeInfo
	HomeState string // "present", "missing" or an error text
	Notes     []string
}

// Refusal is one guard that says no, with the exit code it carries.
type Refusal struct {
	Guard string
	Code  int
	Msg   string
}

// InvocationGuards are G1 to G3: they look at the command line and the process
// only, so they run before anything is inspected.
func InvocationGuards(in Invocation) []Refusal {
	var out []Refusal
	if in.RunUID == 0 {
		out = append(out, Refusal{"G1", exitcode.Usage, "never run this as root: it runs its one privileged command through sudo, after showing it"})
	}
	if in.Delete && !in.Terminal {
		out = append(out, Refusal{"G2", exitcode.Usage, "--delete asks you to type a word, so it needs a terminal on standard input"})
	}
	if in.Answers || in.Unattended {
		out = append(out, Refusal{"G3", exitcode.Usage, "whr offboard host asks every answer at the terminal; --answers and --unattended are refused"})
	}
	return out
}

// Guards are G4 to G11 over the inspected facts. Nothing runs when any refuses.
func Guards(f Facts) []Refusal {
	var out []Refusal
	if !f.RunnerAdminKnown || !f.RunnerAdmin {
		out = append(out, Refusal{"G4", exitcode.Usage, fmt.Sprintf("%s is not known to be an administrator: run this as the administrator", f.RunUser)})
	}
	if !f.Found {
		msg := "there is no account " + f.Account
		if f.Legacy {
			msg += ", but the legacy account " + doctor.LegacyUser + " exists: this command never removes it (see the manual's remove section)"
		}
		return append(out, Refusal{"G5", exitcode.NotFound, msg})
	}
	if f.Ambiguous != "" {
		return append(out, Refusal{"G6", exitcode.Error, "not verified: nothing was changed: " + f.Ambiguous})
	}
	if len(f.RecordNames) != 1 || f.RecordNames[0] != doctor.WhrUser {
		out = append(out, Refusal{"G7", exitcode.Conflict, fmt.Sprintf("the account's record names are %q, not exactly the one name %q (an alias or a second name is refused)", strings.Join(f.RecordNames, " "), doctor.WhrUser)})
	}
	if !f.UIDOK || f.UID < 500 {
		out = append(out, Refusal{"G8", exitcode.Conflict, "the account's user ID is below 500 or unreadable: a system account is never removed"})
	}
	switch {
	case !f.ConsoleKnown:
		out = append(out, Refusal{"G9", exitcode.Conflict, "the owner of the console could not be read, so a login of this account cannot be ruled out"})
	case f.UIDOK && f.UID == f.RunUID:
		out = append(out, Refusal{"G9", exitcode.Conflict, "this is the account you run as"})
	case f.RunUser == doctor.WhrUser:
		out = append(out, Refusal{"G9", exitcode.Conflict, "this is the account you run as"})
	case f.SudoUser == doctor.WhrUser:
		out = append(out, Refusal{"G9", exitcode.Conflict, "sudo was started by this account"})
	case f.ConsoleOwner == doctor.WhrUser:
		out = append(out, Refusal{"G9", exitcode.Conflict, "this account is logged in at the console"})
	}
	if (f.Admin || !f.AdminKnown) && !f.AllowAdmin {
		why := "is an administrator"
		if !f.AdminKnown {
			why = "may be an administrator (dseditgroup did not say)"
		}
		out = append(out, Refusal{"G10", exitcode.Conflict, "the account " + why + ": add --allow-admin to remove it"})
	}
	if msg := homeRefusal(f); msg != "" {
		out = append(out, Refusal{"G11", exitcode.Conflict, msg})
	}
	return out
}

func homeRefusal(f Facts) string {
	rec := doctor.WhrUser
	if len(f.RecordNames) > 0 {
		rec = f.RecordNames[0]
	}
	switch {
	case f.HomeDir != "/Users/"+rec:
		return fmt.Sprintf("the home folder is %q, not /Users/%s", f.HomeDir, rec)
	case f.HomeState == "missing":
		return ""
	case f.HomeState != "present":
		return "the home folder could not be inspected: " + f.HomeState
	case f.Home.Symlink:
		return "the home folder is a symbolic link"
	case !f.Home.Dir:
		return "the home folder is not a directory"
	case !f.Home.UIDKnown || !f.UIDOK || f.Home.UID != f.UID:
		return "the home folder is owned by another user ID"
	}
	return ""
}

var dsclKeys = []string{"RecordName", "UniqueID", "PrimaryGroupID", "NFSHomeDirectory", "RealName"}

func rd(d Deps) doctor.Runner { return absRunner{d.Runner} }

func parseDSCL(s string) map[string][]string {
	m := map[string][]string{}
	key := ""
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if key != "" {
				m[key] = append(m[key], strings.Fields(line)...)
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			key = ""
			continue
		}
		key = k
		m[key] = append(m[key], strings.Fields(v)...)
	}
	return m
}

var safeGroup = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Inspect reads the machine, never with sudo and never changing anything.
func Inspect(ctx context.Context, d Deps, in Invocation) Facts {
	f := Facts{Invocation: in, Account: doctor.WhrUser}
	f.RunnerAdmin, f.RunnerAdminKnown, _ = doctor.Membership(ctx, rd(d), in.RunUser)

	argv := append([]string{"dscl", ".", "-read", "/Users/" + f.Account}, dsclKeys...)
	b, err := rd(d).Output(ctx, argv...)
	if err != nil {
		if doctor.DSCLNotFound(err) {
			_, lerr := rd(d).Output(ctx, "dscl", ".", "-read", "/Users/"+doctor.LegacyUser, "UniqueID")
			f.Legacy = lerr == nil
			return f
		}
		f.Found, f.Ambiguous = true, "dscl did not say whether the account exists: "+oneLine(err.Error())
		return f
	}
	f.Found = true
	m := parseDSCL(string(b))
	for _, k := range []string{"RecordName", "UniqueID", "NFSHomeDirectory"} {
		if len(m[k]) == 0 {
			f.Ambiguous = "dscl's answer has no " + k
			return f
		}
	}
	f.RecordNames = m["RecordName"]
	f.UID, err = strconv.Atoi(m["UniqueID"][0])
	f.UIDOK = err == nil && len(m["UniqueID"]) == 1
	f.HomeDir = strings.Join(m["NFSHomeDirectory"], " ")

	f.Admin, f.AdminKnown, _ = doctor.Membership(ctx, rd(d), f.Account)
	if g, err := rd(d).Output(ctx, "id", "-Gn", f.Account); err == nil {
		f.Groups = strings.Fields(string(g))
	} else {
		f.Notes = append(f.Notes, "the groups could not be read ("+oneLine(err.Error())+"): only admin, access_ssh and access_screensharing are verified afterwards")
	}
	if c, err := rd(d).Output(ctx, "stat", "-f", "%Su", "/dev/console"); err == nil {
		f.ConsoleOwner = strings.TrimSpace(string(c))
		f.ConsoleKnown = f.ConsoleOwner != ""
	}
	switch hi, err := d.Stat(f.HomeDir); {
	case err == nil:
		f.Home, f.HomeState = hi, "present"
	case errors.Is(err, fs.ErrNotExist):
		f.HomeState = "missing"
		f.Notes = append(f.Notes, "the home folder does not exist: nothing of it is removed")
	default:
		f.HomeState = oneLine(err.Error())
	}
	return f
}

// Plan prints exactly what would be removed.
func Plan(o Out, f Facts) {
	admin := "no"
	switch {
	case !f.AdminKnown:
		admin = "unknown"
	case f.Admin:
		admin = "yes"
	}
	o.Data("account", f.Account)
	o.Data("uid", strconv.Itoa(f.UID))
	o.Data("home", f.HomeDir)
	o.Data("groups", strings.Join(f.Groups, " "))
	o.Data("admin", admin)
	o.Data("command", setup.QuoteArgv(DeleteCmd().Full()))
	o.Note("note: sysadminctl %s", sysadminctlN)
	for _, n := range f.Notes {
		o.Note("note: %s", n)
	}
}

// Log records the change before and after execution. Record, when present,
// replaces the plain log and must succeed before any command runs.
type Log struct {
	Record func(result string, exit int, ran [][]string) error
	W      io.Writer
	Now    func() time.Time
	Whr    string
}

func (l Log) line(result string, exit int, ran [][]string) error {
	if l.Record != nil {
		return l.Record(result, exit, ran)
	}
	sum := sha256.Sum256([]byte(strings.Join(DeleteCmd().Full(), "\x00")))
	fmt.Fprintf(l.W, "log: %s offboard.delete-user argv-sha256=%s source=interactive result=%s exit=%d whr=%s\n",
		l.Now().UTC().Format(time.RFC3339), hex.EncodeToString(sum[:]), result, exit, l.Whr)
	return nil
}

// Execute is the real run: the typed word, a second inspection, then sudo -v
// once, then the delete, then the read-only verification. It returns the exit
// code. The typed word protects against a mistake, not against an adversary: a
// program in an administrator's session could feed it through a pty, and sudo's
// authentication is the real barrier.
func Execute(ctx context.Context, h setup.Host, d Deps, f Facts, lg Log, o Out) int {
	o.Action("This deletes the macOS account " + f.Account + " and its home folder " + f.HomeDir + ". It cannot be undone.")
	// A quit (q) is "not confirmed", exit 2, like any answer that is not the word:
	// nothing was changed, and a script cannot tell a quit from a refusal.
	a, err := askWord(h, "Delete the account "+f.Account+"?")
	if err != nil || a != render.Yes {
		o.Note("whr: not confirmed: nothing was removed")
		return exitcode.Usage
	}
	// The plan was shown some time ago: look again, and refuse if anything
	// changed or a guard now says no.
	f2 := Inspect(ctx, d, f.Invocation)
	if rs := Guards(f2); len(rs) > 0 {
		for _, r := range rs {
			o.Refusal(r)
		}
		o.Note("whr: the account changed since the plan: nothing was changed")
		return exitcode.Conflict
	}
	if !reflect.DeepEqual(f, f2) {
		o.Refusal(Refusal{"recheck", exitcode.Conflict, "the account differs from the plan you were shown"})
		o.Note("whr: the account changed since the plan: nothing was changed")
		return exitcode.Conflict
	}
	c := DeleteCmd()
	if err := lg.line("started", 0, nil); err != nil {
		o.Note("whr: cannot record the offboard protocol: %s; nothing was removed", oneLine(err.Error()))
		return exitcode.Error
	}
	ran := [][]string{sudoCheckCmd().Full()}
	o.Command(sudoCheckCmd())
	o.Note("  (once, so the command asks for your password only once; no background refresh)")
	runErr := h.Run(ctx, sudoCheckCmd())
	if runErr != nil {
		runErr = fmt.Errorf("sudo did not accept the password: %w", runErr)
	} else {
		o.Command(c)
		ran = append(ran, c.Full())
		if err := h.Run(ctx, c); err != nil {
			runErr = fmt.Errorf("%s failed: %w", setup.QuoteArgv(c.Full()), err)
		}
	}
	code := exitcode.OK
	if runErr != nil {
		code = exitcode.Error
		o.Note("whr: %s", oneLine(runErr.Error()))
	}
	if !Verify(ctx, d, f, o) {
		code = exitcode.Error
	}
	result := "ok"
	if code != exitcode.OK {
		result = "failed"
	}
	if err := lg.line(result, code, ran); err != nil {
		o.Note("whr: cannot record the offboard result: %s", oneLine(err.Error()))
		code = exitcode.Error
	}
	o.Note("%s", Unverified)
	return code
}

// WordAsker is a Host that can ask for a typed word (the terminal does, through
// render.AskWord: only the exact word is yes, Enter and anything else is no,
// q quits). A Host without it never confirms.
type WordAsker interface {
	AskWord(question, word string) (render.Answer, error)
}

func askWord(h setup.Host, question string) (render.Answer, error) {
	if w, ok := h.(WordAsker); ok {
		return w.AskWord(question, Word)
	}
	return render.No, errors.New("this host cannot ask for a typed word")
}

// Status of one verification line.
const (
	OK          = "ok"
	Fail        = "fail"
	NotVerified = "not_verified"
)

// Verify reads the machine again and reports each line as ok, fail or
// not_verified. It returns whether every line is ok.
func Verify(ctx context.Context, d Deps, f Facts, o Out) bool {
	all := true
	say := func(status, check, detail string) {
		if status != OK {
			all = false
		}
		o.Data(status, check, detail)
		o.Report(map[string]render.Level{OK: render.LevelOK, Fail: render.LevelFail, NotVerified: render.LevelNotVerified}[status], check+": "+detail)
	}
	name := f.Account

	if _, err := rd(d).Output(ctx, "dscl", ".", "-read", "/Users/"+name); err == nil {
		say(Fail, "dscl", "the record still exists")
	} else if doctor.DSCLNotFound(err) {
		say(OK, "dscl", "the record is gone")
	} else {
		say(NotVerified, "dscl", "dscl did not say: "+oneLine(err.Error()))
	}

	if _, err := rd(d).Output(ctx, "id", name); err == nil {
		say(Fail, "id", "the user still resolves")
	} else if strings.Contains(strings.ToLower(err.Error()), "no such user") {
		say(OK, "id", "no such user")
	} else {
		say(NotVerified, "id", "id did not say: "+oneLine(err.Error()))
	}

	home := "/Users/" + name
	if f.HomeDir != "" {
		home = f.HomeDir
	}
	if _, err := d.Stat(home); err == nil {
		say(Fail, "home", home+" still exists")
	} else if errors.Is(err, fs.ErrNotExist) {
		say(OK, "home", home+" is gone")
	} else {
		say(NotVerified, "home", "the home folder could not be inspected: "+oneLine(err.Error()))
	}

	if names, err := d.ReadDir(deletedUsers); err == nil {
		var left []string
		for _, n := range names {
			if strings.Contains(strings.ToLower(n), name) {
				left = append(left, n)
			}
		}
		if len(left) > 0 {
			say(Fail, "deleted-users", "leftovers in "+deletedUsers+": "+strings.Join(left, ", "))
		} else {
			say(OK, "deleted-users", "nothing of it in "+deletedUsers)
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		say(OK, "deleted-users", deletedUsers+" does not exist")
	} else {
		say(NotVerified, "deleted-users", "could not list "+deletedUsers+": "+oneLine(err.Error()))
	}

	seen := map[string]bool{}
	var groups []string
	for _, g := range append(append([]string(nil), f.Groups...), "admin", "com.apple.access_ssh", "com.apple.access_screensharing") {
		if !seen[g] && safeGroup.MatchString(g) {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	for _, g := range groups {
		b, err := rd(d).Output(ctx, "dscl", ".", "-read", "/Groups/"+g, "GroupMembership")
		text := strings.TrimSpace(string(b))
		switch rest, isList := strings.CutPrefix(text, "GroupMembership:"); {
		case isList && contains(strings.Fields(rest), name):
			say(Fail, "group:"+g, name+" is still a member")
			o.Note("  fix (shown, never run): sudo dseditgroup -o edit -d %s -t user %s", name, g)
		case isList:
			say(OK, "group:"+g, "not a member")
		case strings.Contains(text, "No such key") || (err != nil && (strings.Contains(err.Error(), "No such key") || doctor.DSCLNotFound(err))):
			say(OK, "group:"+g, "no members listed")
		default:
			detail := "dscl's answer is not one this check knows"
			if err != nil {
				detail = "dscl did not say: " + oneLine(err.Error())
			}
			say(NotVerified, "group:"+g, detail)
		}
	}
	return all
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
