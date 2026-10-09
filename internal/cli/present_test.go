package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/doctor"
	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/render"
	"github.com/wstein/workharbor/internal/setup"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

var escape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TERM=dumb turns colour off on a terminal; FORCE_COLOR and --color=always turn
// it on without one; --color takes only auto, always or never.
func TestColourTermForceAndFlagValue(t *testing.T) {
	has := func(o uiOpts, args ...string) bool {
		r := newSetupRig(t)
		_, _, errOut := r.runUI(o, append([]string{"setup", "--dry-run"}, args...)...)
		return strings.Contains(errOut, "\x1b")
	}
	if has(uiOpts{stdoutTTY: true, stderrTTY: true, term: "dumb"}) {
		t.Error("TERM=dumb still drew colour")
	}
	if !has(uiOpts{forceColor: "1"}) {
		t.Error("FORCE_COLOR drew no colour")
	}
	if !has(uiOpts{}, "--color=always") {
		t.Error("--color=always drew no colour")
	}
	r := newSetupRig(t)
	code, _, _ := r.runUI(uiOpts{}, "setup", "--dry-run", "--color=sometimes")
	if code != exitcode.Usage {
		t.Errorf("--color=sometimes: exit %d, want usage %d", code, exitcode.Usage)
	}
}

// uiOpts say how a run looks to the program: which streams are terminals and
// what NO_COLOR says.
type uiOpts struct {
	stdoutTTY, stderrTTY bool
	noColor              string
	term, forceColor     string
	ctx                  context.Context // the run's context; Background when nil
}

// runUI runs a command line like setupRig.run, with terminals decided by o.
func (r *setupRig) runUI(o uiOpts, args ...string) (int, string, string) {
	r.t.Helper()
	var out, errOut bytes.Buffer
	env := Env{
		Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Setup: r.env,
		Getenv: func(k string) string {
			switch k {
			case "HOME":
				return "/Users/workharbor"
			case "NO_COLOR":
				return o.noColor
			case "TERM":
				return o.term
			case "FORCE_COLOR":
				return o.forceColor
			}
			return ""
		},
		IsTTY: func(w io.Writer) bool {
			if w == io.Writer(&out) {
				return o.stdoutTTY
			}
			return o.stderrTTY
		},
	}
	ctx := o.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	code := Execute(ctx, env, append(args, "--config", "/Users/workharbor/.config/whr/config.json", "--prefix", filepath.Dir(filepath.Dir(r.exe))))
	tmp := filepath.Dir(filepath.Dir(filepath.Dir(r.exe)))
	return code, strings.ReplaceAll(out.String(), tmp, "<tmp>"), strings.ReplaceAll(errOut.String(), tmp, "<tmp>")
}

func goldenCLI(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.WriteFile(p, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p) //nolint:gosec // a golden file of this package
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden file; got:\n%s", name, got)
	}
}

// `whr doctor`'s readable report, in the three modes, as goldens. The same run
// is what the tab-separated lines on stdout are made from.
func TestGoldenDoctorReport(t *testing.T) {
	for name, o := range map[string]uiOpts{
		"doctor_tty":     {stdoutTTY: true, stderrTTY: true},
		"doctor_plain":   {},
		"doctor_nocolor": {stdoutTTY: true, stderrTTY: true, noColor: "1"},
	} {
		r := newSetupRig(t)
		_, _, errOut := r.runUI(o, "doctor", "--user", "operator")
		goldenCLI(t, name, errOut)
		if o.stderrTTY && o.noColor == "" {
			if !strings.Contains(errOut, "\x1b[") || !strings.Contains(errOut, "▌ ACTION") {
				t.Errorf("%s: a terminal gets colour and the bar", name)
			}
		} else if strings.Contains(errOut, "\x1b") || strings.ContainsAny(errOut, "▌✓✗─") {
			t.Errorf("%s: plain output has colour or non-ASCII symbols", name)
		}
	}
}

func TestPlainFlagTurnsColourOffOnATerminal(t *testing.T) {
	r := newSetupRig(t)
	_, _, errOut := r.runUI(uiOpts{stdoutTTY: true, stderrTTY: true}, "doctor", "--plain")
	if strings.Contains(errOut, "\x1b") || strings.ContainsAny(errOut, "▌✓✗─") {
		t.Errorf("--plain:\n%s", errOut)
	}
}

func TestHumanReportIsPlainWhenEitherOutputIsPiped(t *testing.T) {
	for _, o := range []uiOpts{{stdoutTTY: true}, {stderrTTY: true}} {
		for _, args := range [][]string{{"doctor"}, {"setup", "--dry-run"}} {
			r := newSetupRig(t)
			_, _, got := r.runUI(o, args...)
			if strings.Contains(got, "\x1b") || strings.ContainsAny(got, "▌✓✗─") {
				t.Errorf("%v with %+v was not plain: %q", args, o, got)
			}
		}
	}
}

// Colour is decoration: with the escape codes removed, a terminal's report says
// the same as the plain one, apart from the drawing characters.
func TestEveryColouredLineHasItsTextLabel(t *testing.T) {
	r := newSetupRig(t)
	_, _, errOut := r.runUI(uiOpts{stdoutTTY: true, stderrTTY: true}, "doctor", "--user", "operator")
	known := map[string]bool{"\x1b[0m": true}
	for _, c := range render.Palette {
		known["\x1b["+c.SGR+"m"] = true
	}
	for _, seq := range escape.FindAllString(errOut, -1) {
		if !known[seq] {
			t.Errorf("colour %q has no label in the palette", seq)
		}
	}
	for _, label := range []string{"FAIL", "?", "ACTION", "command"} {
		if !strings.Contains(escape.ReplaceAllString(errOut, ""), label) {
			t.Errorf("label %q missing", label)
		}
	}
}

func TestOnATerminalStdoutHoldsNoDataLinesAndOtherwiseItDoes(t *testing.T) {
	r := newSetupRig(t)
	_, out, errOut := r.runUI(uiOpts{stdoutTTY: true, stderrTTY: true}, "doctor")
	if out != "" || !strings.Contains(errOut, "power") {
		t.Errorf("terminal: stdout %q", out)
	}
	_, out, _ = r.runUI(uiOpts{}, "doctor")
	if !strings.Contains(out, "fail\tconfig\t") {
		t.Errorf("piped: stdout lacks the data lines: %q", out)
	}
	// --json is never touched by the presentation, terminal or not
	_, tty, _ := r.runUI(uiOpts{stdoutTTY: true, stderrTTY: true}, "doctor", "--json", "--user", "operator")
	_, pipe, _ := r.runUI(uiOpts{}, "doctor", "--json", "--user", "operator")
	if tty != pipe || strings.Contains(tty, "\x1b") {
		t.Errorf("--json differs by terminal")
	}
}

func TestRawToolTextOnlyWithVerbose(t *testing.T) {
	r := newSetupRig(t)
	_, _, errOut := r.runUI(uiOpts{}, "doctor", "--user", "operator")
	if strings.Contains(errOut, "tool output") || strings.Contains(errOut, "exit status 1") {
		t.Errorf("raw tool text without --verbose:\n%s", errOut)
	}
	_, _, errOut = r.runUI(uiOpts{}, "doctor", "--user", "operator", "--verbose")
	if !strings.Contains(errOut, "tool output:") || !strings.Contains(errOut, "exit status 1") {
		t.Errorf("--verbose lacks the raw tool text:\n%s", errOut)
	}
}

// askingHost is a setup host that also answers [Y/n/q].
type askingHost struct {
	setupHost
	answers []render.Answer
	asked2  []string
}

func (a *askingHost) Ask(q string, _ render.Default) (render.Answer, error) {
	a.asked2 = append(a.asked2, q)
	if len(a.answers) == 0 {
		return render.No, nil
	}
	r := a.answers[0]
	a.answers = a.answers[1:]
	return r, nil
}

func TestQuitExitsWithItsOwnCodeAndAResumeHint(t *testing.T) {
	r := newSetupRig(t)
	h := &askingHost{setupHost: setupHost{outputs: map[string]string{}}, answers: []render.Answer{render.Quit}}
	r.env.Host = h
	r.dsclSays("workharbor", io.ErrUnexpectedEOF)
	code, _, errOut := r.runUI(uiOpts{}, "setup", "host", "--only", "power")
	if code != exitcode.Quit || code == exitcode.Error || code == exitcode.OK {
		t.Errorf("exit %d, want %d, a code of its own\n%s", code, exitcode.Quit, errOut)
	}
	if !regexp.MustCompile(`\n +whr setup .*--only power`).MatchString(errOut) || !strings.Contains(errOut, "stopped at your request") {
		t.Errorf("no resume hint:\n%s", errOut)
	}
	if len(h.ran) != 0 {
		t.Errorf("quitting ran %v", h.ran)
	}
}

// runFailsHost fails every command the way os/exec does when it kills one:
// with the signal, never with the context's own error. cancel, when set, is the
// Ctrl-C that came with it.
type runFailsHost struct {
	askingHost
	cancel context.CancelFunc
	wait   bool // block until the context is done
}

func (h *runFailsHost) Run(ctx context.Context, _ doctor.Cmd) error {
	if h.wait { // a deadline ends it
		<-ctx.Done()
	}
	if h.cancel != nil {
		h.cancel()
	}
	return errors.New("signal: killed")
}

func TestInterruptExitsWithItsOwnCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := newSetupRig(t)
	h := &runFailsHost{askingHost: askingHost{setupHost: setupHost{outputs: map[string]string{}}, answers: []render.Answer{render.Yes}}, cancel: cancel}
	r.env.Host = h
	r.dsclSays("workharbor", io.ErrUnexpectedEOF)
	code, _, errOut := r.runUI(uiOpts{ctx: ctx}, "setup", "host", "--only", "power")
	if code != exitcode.Interrupted {
		t.Errorf("exit %d, want %d\n%s", code, exitcode.Interrupted, errOut)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "whr: interrupted") || strings.Contains(last, "context") {
		t.Errorf("final line %q", last)
	}
	if !regexp.MustCompile(`to check it and go on, run\n\n +whr setup .*--only power`).MatchString(errOut) {
		t.Errorf("no resume command:\n%s", errOut)
	}
}

func TestDeadlineExitsWithTheInterruptCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r := newSetupRig(t)
	h := &runFailsHost{askingHost: askingHost{setupHost: setupHost{outputs: map[string]string{}}, answers: []render.Answer{render.Yes}}, wait: true}
	r.env.Host = h
	r.dsclSays("workharbor", io.ErrUnexpectedEOF)
	code, _, errOut := r.runUI(uiOpts{ctx: ctx}, "setup", "host", "--only", "power")
	if code != exitcode.Interrupted || !strings.Contains(errOut, "whr: interrupted") {
		t.Errorf("exit %d\n%s", code, errOut)
	}
}

func TestACommandThatFailsWithALiveContextIsNotAnInterrupt(t *testing.T) {
	r := newSetupRig(t)
	h := &runFailsHost{askingHost: askingHost{setupHost: setupHost{outputs: map[string]string{}}, answers: []render.Answer{render.Yes}}}
	r.env.Host = h
	r.dsclSays("workharbor", io.ErrUnexpectedEOF)
	code, _, errOut := r.runUI(uiOpts{}, "setup", "host", "--only", "power")
	if code == exitcode.Interrupted || strings.Contains(errOut, "whr: interrupted") {
		t.Errorf("exit %d, an ordinary failure is not an interrupt\n%s", code, errOut)
	}
}

func TestInterruptedTextNamesOnlyWhatStarted(t *testing.T) {
	for _, c := range []struct {
		when setup.InterruptedWhen
		want string
		not  string
	}{
		{setup.DuringStep, "the step that was running did not finish", "had not started"},
		{setup.BeforeStep, "the next one had not started", "did not finish"},
		{setup.AfterLastStep, "every step had finished", "did not finish"},
	} {
		var b bytes.Buffer
		printInterrupted(render.Writer{W: &b}, &setup.InterruptedError{Step: "two", When: c.when, Resume: "whr setup --from two"})
		if got := b.String(); !strings.Contains(got, c.want) || strings.Contains(got, c.not) || !strings.Contains(got, "whr setup --from two") {
			t.Errorf("when %d:\n%s", c.when, got)
		}
	}
}

func TestColourFlagsAndEnv(t *testing.T) {
	has := func(args ...string) bool {
		r := newSetupRig(t)
		_, _, errOut := r.runUI(uiOpts{stdoutTTY: true, stderrTTY: true}, args...)
		return strings.Contains(errOut, "\x1b")
	}
	if !has("setup", "--dry-run") {
		t.Fatal("setup on a terminal draws no colour")
	}
	for _, a := range [][]string{{"--no-color"}, {"--color=never"}} {
		if has(append([]string{"setup", "--dry-run"}, a...)...) {
			t.Errorf("%v still printed an escape sequence", a)
		}
	}
}

// Output that is not a terminal wraps at the default 80: no width is read.
func TestTermColsOfAPipeIsZero(t *testing.T) {
	var b bytes.Buffer
	if n := termCols(&b); n != 0 {
		t.Errorf("termCols = %d", n)
	}
}

// No line of a doctor report is wider than 90 runes once colour is stripped.
func TestDoctorGoldensStayWithin90Runes(t *testing.T) {
	esc := regexp.MustCompile("\x1b\\[[0-9;]*m")
	for _, name := range []string{"doctor_tty", "doctor_plain", "doctor_nocolor"} {
		b, err := os.ReadFile(filepath.Join("testdata", name+".golden")) //nolint:gosec // a golden file of this package
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if n := utf8.RuneCountInString(esc.ReplaceAllString(line, "")); n > 90 {
				t.Errorf("%s: %d runes wide: %q", name, n, line)
			}
		}
	}
}
