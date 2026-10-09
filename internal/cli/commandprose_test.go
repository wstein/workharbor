package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A command shown to the person goes in a command block of its own, never
// inside a prose line, where it wraps and cannot be copied whole (issue
// #416). A command-looking token is a command word followed by a flag, a
// placeholder or a subcommand, or any command word in backticks.
var commandInProse = regexp.MustCompile("`(?:whr|brew|tailscale|sudo|make|curl|git|open|diskutil|mdutil|fdesetup|xcode-select|container)\\b|" +
	`\b(?:whr|brew|tailscale|sudo|make|curl|git|open|diskutil|mdutil|fdesetup|xcode-select|container) +(?:-{1,2}[a-z]|<|(?:setup|doctor|serve|status|install|github|ls|show|ssh|passkey|approve|reject|tools|funnel|enable|system)\b)`)

// proseAllowed are the prose lines that name a command on purpose. Each
// entry is a substring of the line and says why it is not a command to run.
var proseAllowed = []string{
	"Host steps (whr setup,",    // a section title naming the phase, not a step to copy
	"User steps (whr setup, as", // the same, for the user phase
	"next: whr setup --from",    // golden.go fixture value of the Outcome.Next field, shown as a one-line trail
	"Next: ",                    // the preflight line of the dry-run fixture names the next step in prose
}

func proseProblems(where, text string, backtickOnly bool) []string {
	var out []string
	lines := strings.Split(text, "\n")
	cmdLines := commandBlockLines(lines)
	for i, l := range lines {
		if cmdLines[i] {
			continue
		}
		m := commandInProse.FindString(l)
		if m == "" || backtickOnly && !strings.HasPrefix(m, "`") {
			continue
		}
		allowed := false
		for _, a := range proseAllowed {
			allowed = allowed || strings.Contains(l, a)
		}
		if !allowed {
			out = append(out, where+": "+m+" in prose: "+strings.TrimSpace(l))
		}
	}
	return out
}

func TestGoldenOutputsHoldNoCommandInProse(t *testing.T) {
	ansiRe := regexp.MustCompile("\x1b\\[[0-9;]*m")
	for _, g := range []string{"testdata/doctor_*.golden", "../setup/testdata/setup_*.golden", "../render/testdata/*.golden"} {
		files, _ := filepath.Glob(g)
		if len(files) == 0 {
			t.Fatalf("no golden files for %s", g)
		}
		for _, f := range files {
			if strings.HasSuffix(f, ".json.golden") || strings.HasSuffix(f, "doctor_json.golden") {
				continue
			}
			b, err := os.ReadFile(f) //nolint:gosec // a golden file found by the glob above
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range proseProblems(f, ansiRe.ReplaceAllString(string(b), ""), false) {
				t.Error(p)
			}
		}
	}
}

// The messages are string literals of the packages that build the output. A
// literal is often a command itself (Fix.Try, Unreachable.Tools) or a name
// ("whr serve"), so only the backticked form, the one a message uses to show a
// command inside a sentence, is flagged here; the goldens cover the rest.
func TestMessageLiteralsHoldNoCommandInProse(t *testing.T) {
	fset := token.NewFileSet()
	// the setup and doctor commands of this package, and the packages behind them
	scanned := []string{"setup.go", "doctor.go", "github.go"}
	for _, dir := range []string{"../doctor", "../setup"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		scanned = append(scanned, files...)
	}
	{
		files := scanned
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			tree, err := parser.ParseFile(fset, f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(tree, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || strings.HasPrefix(lit.Value, "`") {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				for _, p := range proseProblems(fset.Position(lit.Pos()).String(), s, true) {
					t.Error(p)
				}
				return true
			})
		}
	}
}

func TestTheProseScanFindsACommandInProse(t *testing.T) {
	if len(proseProblems("x", "run `whr doctor` again", false)) != 1 || len(proseProblems("x", "then sudo -v asks", false)) != 1 {
		t.Error("a command in prose is not found")
	}
	if len(proseProblems("x", "x:\n\n    whr doctor --user x\n\n", false)) != 0 || len(proseProblems("x", "brew did not answer", false)) != 0 {
		t.Error("a command block or a bare tool name is flagged")
	}
}
