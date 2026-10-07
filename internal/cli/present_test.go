package cli

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/exitcode"
	"github.com/wstein/workharbor/internal/render"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

var escape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// uiOpts say how a run looks to the program: which streams are terminals and
// what NO_COLOR says.
type uiOpts struct {
	stdoutTTY, stderrTTY bool
	noColor              string
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
	code := Execute(context.Background(), env, append(args, "--config", "/Users/workharbor/.config/whr/config.json", "--prefix", filepath.Dir(filepath.Dir(r.exe))))
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
	for _, label := range []string{"FAIL", "?", "ACTION", "$"} {
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

func TestADevelopmentWarningIsSetApartByARule(t *testing.T) {
	r := newSetupRig(t)
	_, _, errOut := r.runUI(uiOpts{}, "doctor", "--dev")
	lines := strings.Split(errOut, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "warning: development installation") {
			if i == 0 || strings.Trim(lines[i-1], "-") != "" || strings.Trim(lines[i+1], "-") != "" {
				t.Errorf("the warning is not between rules:\n%s", errOut)
			}
			return
		}
	}
	t.Errorf("no development warning:\n%s", errOut)
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
	if !regexp.MustCompile(`\$ whr setup host .*--only power`).MatchString(errOut) || !strings.Contains(errOut, "stopped at your request") {
		t.Errorf("no resume hint:\n%s", errOut)
	}
	if len(h.ran) != 0 {
		t.Errorf("quitting ran %v", h.ran)
	}
}

func TestSetupDevWarningIsSetApartByARule(t *testing.T) {
	r := newSetupRig(t)
	_, _, errOut := r.runUI(uiOpts{}, "setup", "--dev", "--dry-run")
	lines := strings.Split(errOut, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "warning: development installation") {
			if i == 0 || strings.Trim(lines[i-1], "-") != "" || strings.Trim(lines[i+1], "-") != "" {
				t.Errorf("the warning is not between rules:\n%s", errOut)
			}
			return
		}
	}
	t.Errorf("no development warning:\n%s", errOut)
}
