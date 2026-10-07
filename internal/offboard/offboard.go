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
	"slices"
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
	"du": "/usr/bin/du", "mount": "/sbin/mount",
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
//
// sysadminctl's own usage text (run it with no arguments; verified on the
// development Mac, macOS 26) says: "-deleteUser <user name> (interactive ||
// -adminUser <administrator user name> -adminPassword <administrator password>)"
// and "Pass '-' instead of password in commands above to request prompt".
// Without these flags it printed a "User password:" prompt that echoed what was
// typed (issue #378). whr therefore passes "-adminPassword -" and reads the
// password itself, without echo, and writes it to sysadminctl's stdin.
// UNVERIFIED (nothing here ran a deletion): that "-" makes sysadminctl read the
// password from its stdin rather than from /dev/tty, and whether it then echoes;
// there is no man page for sysadminctl on this host. The relay drops any prompt
// for a secret and the terminal's echo is switched off while the command runs,
// but a real run must confirm that the account is deleted and nothing is shown.
func DeleteCmd(admin string) doctor.Cmd {
	return doctor.Cmd{
		Argv:         []string{sudoBin, sysadminctlBin, "-deleteUser", doctor.WhrUser, "-adminUser", admin, "-adminPassword", "-"},
		SecretPrompt: "Password of " + admin + " for sysadminctl (not shown)",
	}
}

// PictureCmd removes the login picture that `whr setup host` installed outside
// the home (#382); sysadminctl -deleteUser only removes the account record and
// its home. The path is the constant directory of the setup step, never an
// argument. UNVERIFIED on a real host, like every command here.
func PictureCmd() doctor.Cmd {
	return doctor.Cmd{Argv: []string{sudoBin, "/bin/rm", "-rf", "--", doctor.LoginPictureDir}}
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

	// HomeSizeKiB is what du says about the home folder, on its own volume. It
	// is shown in the plan only: it changes while the plan is read, so the
	// recheck before the delete ignores it.
	HomeSizeKiB   int64
	HomeSizeKnown bool

	// HomeVolumes are the mount points at or under the home folder, read from
	// mount(8). They are only shown, never a refusal, but the recheck compares
	// them: a volume mounted after the plan blocks the delete.
	HomeVolumes      []string
	HomeVolumesKnown bool
	Notes            []string
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
		if hi.Dir && !hi.Symlink {
			f.HomeSizeKiB, f.HomeSizeKnown = homeSize(ctx, d, f.HomeDir)
			if !f.HomeSizeKnown {
				f.Notes = append(f.Notes, "the home size could not be read: the plan cannot say how much is removed")
			}
			f.HomeVolumes, f.HomeVolumesKnown = homeVolumes(ctx, d, f.HomeDir)
			if !f.HomeVolumesKnown {
				f.Notes = append(f.Notes, "the mounted volumes could not be read: the plan cannot say whether one is mounted in the home")
			}
		} else {
			f.Notes = append(f.Notes, "the home folder is not a plain directory: its size and volumes were not read")
		}
	case errors.Is(err, fs.ErrNotExist):
		f.HomeState = "missing"
		f.Notes = append(f.Notes, "the home folder does not exist: nothing of it is removed")
	default:
		f.HomeState = oneLine(err.Error())
	}
	return f
}

// homeSize asks du for the size in KiB; -x stays on the home's own volume.
func homeSize(ctx context.Context, d Deps, home string) (int64, bool) {
	b, err := rd(d).Output(ctx, "du", "-skx", home)
	if err != nil {
		return 0, false
	}
	return parseDU(string(b), home)
}

// parseDU accepts exactly one line, "<digits><TAB><home>".
func parseDU(out, home string) (int64, bool) {
	kib, path, ok := strings.Cut(strings.TrimSuffix(out, "\n"), "\t")
	if !ok || path != home || kib == "" || strings.Trim(kib, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseInt(kib, 10, 64)
	return n, err == nil
}

// homeVolumes lists the mount points at or under home. The answer is unknown
// unless every line parsed and the table has the root mount: an empty or
// garbled table must never read as "none".
func homeVolumes(ctx context.Context, d Deps, home string) ([]string, bool) {
	b, err := rd(d).Output(ctx, "mount")
	if err != nil {
		return nil, false
	}
	return parseMount(string(b), home)
}

// parseMount reads "<device> on <mount point> (<options>)" lines. A device or
// a mount point may itself contain " on /", so a line with several is tried at
// every split: the line counts as a volume of the home when any split puts the
// mount point at or under it (the earliest such split is printed). A line that
// has no split or no option list, or a table without the root mount, makes the
// whole answer unknown. Paths compare case-insensitively (the APFS default)
// and also in the firmlink form /System/Volumes/Data/Users/....
func parseMount(out, home string) ([]string, bool) {
	var vols []string
	root := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		i := strings.LastIndex(line, " (")
		if i < 0 || !strings.HasSuffix(line, ")") {
			return nil, false
		}
		head := line[:i]
		found, hit := false, ""
		for off := 0; ; {
			j := strings.Index(head[off:], " on /")
			if j < 0 {
				break
			}
			found = true
			mp := head[off+j+len(" on "):]
			if mp == "/" {
				root = true
			}
			if hit == "" && atOrUnder(mp, home) {
				hit = mp
			}
			off += j + 1
		}
		if !found {
			return nil, false
		}
		if hit != "" {
			vols = append(vols, hit)
		}
	}
	if !root {
		return nil, false
	}
	return vols, true
}

func atOrUnder(mp, home string) bool {
	low := strings.TrimPrefix(strings.ToLower(mp), "/system/volumes/data")
	lowHome := strings.ToLower(home)
	return low == lowHome || strings.HasPrefix(low, lowHome+"/")
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
	o.Note("preflight: account %s found, home %s. Next: show the plan; "+
		"nothing is changed without --delete.", f.Account, f.HomeDir)
	o.Data("account", f.Account)
	o.Data("uid", strconv.Itoa(f.UID))
	o.Data("home", f.HomeDir)
	size := "unknown"
	if f.HomeSizeKnown {
		size = strconv.FormatInt(f.HomeSizeKiB, 10) + " KiB"
	}
	o.Data("home-size", size)
	switch {
	case !f.HomeVolumesKnown:
		o.Data("home-volumes", "unknown")
	case len(f.HomeVolumes) == 0:
		o.Data("home-volumes", "none")
	}
	for _, v := range f.HomeVolumes {
		o.Data("home-volume", v)
	}
	o.Data("groups", strings.Join(f.Groups, " "))
	o.Data("admin", admin)
	o.Data("command", setup.QuoteArgv(DeleteCmd(f.RunUser).Full()))
	o.Data("command", setup.QuoteArgv(PictureCmd().Full()))
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
	Admin  string // the account sysadminctl authenticates as, set by Execute
}

func (l Log) line(result string, exit int, ran [][]string) error {
	if l.Record != nil {
		return l.Record(result, exit, ran)
	}
	sum := sha256.Sum256([]byte(strings.Join(DeleteCmd(l.Admin).Full(), "\x00")))
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
	o.Action("This deletes the macOS account " + f.Account + " and its home folder " + f.HomeDir + ". It cannot be undone. No backup is made: copy what you need first.")
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
	f.HomeSizeKiB, f2.HomeSizeKiB = 0, 0
	f.HomeSizeKnown, f2.HomeSizeKnown = false, false
	f.Notes, f2.Notes = withoutSizeNote(f.Notes), withoutSizeNote(f2.Notes)
	if v := newVolume(f.HomeVolumes, f2.HomeVolumes); v != "" {
		o.Refusal(Refusal{"recheck", exitcode.Conflict, "a volume is mounted in the home: " + v})
		o.Note("whr: the account changed since the plan: nothing was changed")
		return exitcode.Conflict
	}
	if !reflect.DeepEqual(f, f2) {
		o.Refusal(Refusal{"recheck", exitcode.Conflict, "the account differs from the plan you were shown"})
		o.Note("whr: the account changed since the plan: nothing was changed")
		return exitcode.Conflict
	}
	c := DeleteCmd(f.RunUser)
	lg.Admin = f.RunUser
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
		} else {
			// the login picture of the setup step lives outside the home; a
			// failure here leaves a public logo behind and is only a note
			pc := PictureCmd()
			o.Command(pc)
			ran = append(ran, pc.Full())
			if err := h.Run(ctx, pc); err != nil {
				o.Note("note: the login picture %s was not removed: %s", doctor.LoginPictureDir, oneLine(err.Error()))
			}
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
	o.ui().Final(finalOf(f, code))
	o.Note("%s", Unverified)
	return code
}

// finalOf is the closing summary of a delete run.
func finalOf(f Facts, code int) render.Final {
	fin := render.Final{}
	if code == exitcode.OK {
		fin.Changed = []string{"Deleted the account " + f.Account + " and its home folder."}
		return fin
	}
	// a failure is explained once by the caller's failure summary (cause and
	// next ACTION); a record failure must not claim a partial delete
	return fin
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
			o.Note("  fix (shown, never run):")
			o.ui().Command(fmt.Sprintf("sudo dseditgroup -o edit -d %s -t user %s", name, g))
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

func withoutSizeNote(notes []string) []string {
	var out []string
	for _, n := range notes {
		if !strings.HasPrefix(n, "the home size could not be read") {
			out = append(out, n)
		}
	}
	return out
}

// newVolume is the first mount point of now that the plan did not show.
func newVolume(plan, now []string) string {
	for _, v := range now {
		if !slices.Contains(plan, v) {
			return v
		}
	}
	return ""
}
