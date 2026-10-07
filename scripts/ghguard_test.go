package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/ghguard"
)

// Spellings from the refused tokenizer design that contain a literal graphql
// (or a dynamic piece next to graph/ql): every one must be refused.
var ghGraphQLRefused = []string{
	"gh api graphql",
	"gh api graphql -f query=x",
	"gh api -X POST graphql",
	"gh api --method POST graphql -f query=x",
	"gh api -f query=x graphql",
	"gh api -H 'X: y' graphql",
	"gh api --hostname h graphql",
	"gh api /graphql",
	"gh api //graphql/",
	"gh api GraphQL",
	"gh api graphql?x=1",
	"gh api https://api.github.com/graphql",
	"gh api https://ghe.example/api/graphql",
	"gh -R x/y api graphql",
	"gh --repo x/y api graphql",
	"gh --repo=x/y api graphql",
	"gh api -X POST -f query=x https://api.github.com/graphql",
	`gh api "graphql"`,
	`gh api 'graph''ql'`,
	`gh api gr"aph"ql`,
	`gh api \graphql`,
	`gh api "/graph"ql`,
	"gh api $EP",
	"gh api ${EP}",
	"/opt/homebrew/bin/gh api graphql",
	"FOO=1 gh api graphql",
	"env FOO=1 gh api graphql",
	"env -u X gh api graphql",
	"command gh api graphql",
	"exec gh api graphql",
	"nohup gh api graphql",
	"time gh api graphql",
	"sudo -n gh api graphql",
	"timeout 5 gh api graphql",
	"echo x; gh api graphql",
	"true && gh api graphql",
	"false || gh api graphql",
	"echo x | gh api graphql",
	"echo x\ngh api graphql",
	"(gh api graphql)",
	"echo $(gh api graphql)",
	"echo `gh api graphql`",
	"echo graphql | xargs gh api",
	"echo graphql | xargs -n1 gh api",
	"echo graphql | xargs -I{} gh api {}",
	"echo x | xargs -I{} gh api graphql",
	"echo x | xargs -0 -P 4 gh api graphql",
	`find . -exec gh api graphql {} \;`,
	"bash -c 'gh api graphql'",
	`sh -c "gh api /graphql"`,
	"bash -lc 'gh api graphql'",
	"zsh -c 'echo x; gh api graphql'",
	"bash -c \"bash -c 'gh api graphql'\"",
	"echo x | xargs bash -c 'gh api graphql'",
	"xargs -n1 sh -c 'gh api graphql' < /dev/null",
	"eval 'gh api graphql'",
	"eval gh api graphql",
	"if true; then gh api graphql; fi",
	"gh api $(echo graphql)",
	"gh api `echo graphql`",
	"gh api \"`echo graphql`\"",
	"gh api \"$(echo graphql)\"",
	"gh api gr$(echo a)phql",
	"gh api gr${E}ql",
	"2>/dev/null gh api graphql",
	">/dev/null gh api graphql",
	"2>&1 gh api graphql",
	"> /dev/null gh api graphql",
	"echo graphql | xargs -I % gh api %",
	"echo graphql | xargs -J % gh api %",
	"echo graphql | xargs -I@ gh api @",
	"echo graphql | xargs --replace=% gh api %",
	"echo graphql | xargs -i gh api {}",
	"bash -o pipefail -c 'gh api graphql'",
	"bash -O extglob -c 'gh api graphql'",
	"bash -c -- 'gh api graphql'",
	"bash -e -c 'gh api graphql'",
	"env -S 'gh api graphql'",
	"env -Sgh\\ api\\ graphql",
	"env --split-string='gh api graphql'",
	"gh api {-fq=1,graphql}",
	"gh api gr$'a'phql",
	"gh api gr$\"a\"phql",
	"sudo -u gh gh api graphql",
	"coproc gh api graphql",
	"export A=1 gh api graphql",
	"caffeinate gh api graphql",
	"watch gh api graphql",
	"gh api ./graphql",
	"gh api repos/../graphql",
	"gh api %67raphql",
	"((gh api graphql) )",
	"((cd . && gh api graphql) && true)",
	"((echo x) | gh api graphql)",
	"((\ngh api graphql\n) )",
	"echo $((gh api graphql) )",
	"x=$((gh api graphql) ); echo $x",
	"echo $((echo x) | gh api graphql)",
	"$((bash -c 'gh api graphql') )",
	"((gh api graphql))",
	"((",
	"echo $((1+",
	"((cd . ; gh api graphql))",
	"bash <<< 'gh api graphql'",
	"cat <<< 'gh api graphql' | bash",
	"sh <<< \"$x\"",
	"sh <<<'gh api graphql'",
	"grep x <<< \"$v\"\ngh api graphql",
	"cat <<< hi\ngh api graphql",
	"cat <<<EOF\ngh api graphql\nEOF",
	"echo \"$(cat <<< hi\n)\"; gh api graphql",
	"echo \"$(cat <<'EOF'\nx\nEOF)\"\ngh api graphql",
	"x=$(cat <<'EOF'\nx\nEOF)\ngh api graphql",
	"cat <<'EOF'\nx\ngh api graphql",
	"cat <<EOF",
	"cat <<$'EOF'\nx\nEOF\ngh api graphql",
	"echo \"$(grep x <file\n)\"; gh api graphql",
	"echo $((1<<EOF))\ngh api graphql\nEOF",
	"(( x = 1 << 2 ))\ngh api graphql",
	"{ cat <<'EOF'\ngh api graphql\nEOF\n} | bash",
	"( cat <<'EOF'\ngh api graphql\nEOF\n) | bash",
	"cat <<'EOF' |\ngh api graphql\nEOF\nbash",
	"{\ncat <<'EOF'\ngh api graphql\nEOF\n} | bash",
	"((\ngh api graphql",
	"cat <<'EOF' | $SHELL\ngh api graphql\nEOF",
	"source /dev/stdin <<'EOF'\ngh api graphql\nEOF",
	". /dev/stdin <<'EOF'\ngh api graphql\nEOF",
	"gh 2>/dev/null api graphql",
	"gh >/dev/null api graphql",
	"gh {fd}>/dev/null api graphql",
	"gh -R o/r 2>&1 api graphql",
	"echo api graphql | xargs gh",
	"xargs -n1 gh",
	"GH api graphql",
	"Gh api graphql",
	"/OPT/homebrew/bin/GH api graphql",
	"$SUDO gh api graphql",
	"$(command -v env) gh api graphql",
	"trap -- 'gh api graphql' EXIT",
	"timeout 1d gh api graphql",
	"arch -arm64 gh api graphql",
	"csh -c 'gh api graphql'",
	"tcsh -c 'gh api graphql'",
	"noglob gh api graphql",
	"nocorrect gh api graphql",
	"repeat 2 gh api graphql",
	"=gh api graphql",
	"{gh,api} graphql",
	"gh api graph{q..q}l",
	">| /tmp/f gh api graphql",
	"{fd}>/dev/null gh api graphql",
	"echo \"$(echo ')'; gh api graphql)\"",
	"echo \"$(echo \\\")\"; gh api graphql)\"",
	"cat <<EOF\n$(gh api graphql)\nEOF",
	"cat <<EOF\n`gh api graphql`\nEOF",
	"cat <<'EOF'\nx\nEOF\ngh api graphql",
	"bash <<'EOF'\ngh api graphql\nEOF",
	"bash <<EOF\ngh api graphql\nEOF",
	"cat <<'EOF' | bash\ngh api graphql\nEOF",
	"git commit -m \"$(cat <<'EOF'\nx\nEOF\n)\"; gh api graphql",
	"gh api $(printf '%s' graphql)",
	"echo \"$(echo \"a$(echo \")\")b\")\"; gh api graphql",
	"echo \"$(echo \"a\\\"b\")\"; gh api graphql",
	"echo \"$(echo \"x)y\")\"; gh api graphql",
	"echo \"$(echo \"$(echo \")\")\")\"; gh api graphql",
	"cat <<-'EOF'\n\tx\n\tEOF\ngh api graphql",
	"echo \"$(cat <<'EOF'\nit's\nEOF\n)\"; gh api graphql",
	"echo \"$(cat <<'EOF'\n)\nEOF\n)\"; gh api graphql",
	"echo \"$(cat <<'EOF'\n\"\nEOF\n)\"; gh api graphql",
	"echo \"$(gh api graphql)\"",
	"X=\"$(gh api graphql)\"",
	"echo \"`gh api graphql`\"",
	"echo \"a $(echo $(gh api graphql)) b\"",
	">& /dev/null gh api graphql",
	"&> /tmp/x gh api graphql",
	"if false; then :; elif gh api graphql; then :; fi",
	"trap 'gh api graphql' EXIT",
	"find /tmp -exec gh api graphql \\;",
	"find /tmp -name x -execdir gh api graphql {} +",
	"watch -n1 'gh api graphql'",
	"fish -c 'gh api graphql'",
	"G=gh; $G api graphql",
	"$(command -v gh) api graphql",
	"`which gh` api graphql",
	"\"$G\" api graphql",
	"gh api graph?l",
	"gh api grap[h]ql",
	"gh api gr*",
	"gh api *",
	"gh api %2e%2e/graphql",
	"gh api %2fgraphql",
	"echo $(gh api graphql",
	"gh api $(echo graphql",
	"echo `gh api graphql",
	"echo $(gh api graphql) | cat",
	"gh api repos/..",
	"echo graphql | xargs --replace=@ gh api @",
	"echo graphql | xargs -I @ gh api @",
	"echo graphql | xargs -i gh api {}/y",
}

// Documented gaps: refused by the old tokenizer, but with no literal graphql
// text, so the conservative rule does not see them (#314). Pinned so a change
// in behaviour is noticed.
var ghGraphQLGaps = []string{
	"gh api $EP",
	"gh api ${EP}",
	"((",
	"echo $((1+",
	"sh <<< \"$x\"",
	"cat <<EOF",
	"xargs -n1 gh",
	"gh api gr*",
	"gh api *",
	"gh api repos/..",
}

// Refused by the conservative rule only because of the data-context scanner.
var ghGraphQLRefusedExtra = []string{
	"gh api graphql",
	"GH API GraphQL",
	"gh api %67raphql",
	"gh api %2567raphql",
	"gh api Graph%51L",
	"gh api 'graph'\"ql\"",
	"gh api gr\\aphql",
	"gh api graph`echo`ql",
	"g\"h\" api graphql",
	"=gh api graphql",
	"X=gh; $X api graphql",
	"$G api gr$Xql",
	"gh api graph{q,Q}l",
	"gh api graph?l",
	"gh api grap[h]ql",
	"gh api gr$(echo a)phql",
	"gh api 'graph'+'ql'",
	"gh issue list; gh api graphql",
	"gh issue list --search graphql\ngh api graphql",
	"gh issue list --search graphql | gh api graphql",
	"gh issue list --search $(gh api graphql)",
	"git commit -m 'x' && gh api graphql",
	"git commit -m 'x'; gh api graphql",
	"echo \"git commit -m 'x\"; gh api graphql; echo \"'\"",
	"echo '\ngit commit -m 'x'; gh api graphql",
	"git commit -m \"gh api graphql\"",
	"cat <<'EOF' | bash\ngh api graphql\nEOF",
	"bash <<'EOF'\ngh api graphql\nEOF",
	"cat <<'EOF'\nx\nEOF\nbash\ngh api graphql",
	"{\ncat <<'EOF'\ngh api graphql\nEOF\n} | bash",
	"cat <<'EOF'\ngh api graphql",
	"gh pr create --body 'x' && bash <<'EOF'\ngh api graphql\nEOF",
	"curl https://api.github.com/graphql",
	"/opt/homebrew/bin/gh graphql",
	"x issue list graphql gh",
	"git commit -m 'x'\necho 'a\ngh api graphql",
	"git commit -m 'x'\necho \"a\ngh api graphql",
	"echo gh api graphql",
	"echo " + strings.Repeat("a", 40<<10),
}

// Allowed spellings: REST, scripts, data contexts, non-api gh subcommands.
var ghGraphQLAllowed = []string{
	"",
	"gh api repos/wstein/workharbor/rulesets",
	"gh api repos/wstein/workharbor/code-scanning/alerts",
	"gh api --paginate repos/x/y/pulls --jq '.[].title'",
	"gh api repos/x/y/pulls --jq '.[] | select(.n < 3)'",
	"gh api -H 'Accept: application/json' /repos/x/y",
	"gh api https://api.github.com/repos/x/y",
	"gh api user",
	"gh api orgs/x/repos",
	"gh api rate_limit",
	"gh api 'search/issues?q=repo%3Ao%2Fr'",
	"gh api repos/o/r/git/ref/heads/feat%2Fx",
	"gh api repos/$REPO/pulls",
	"gh api 'repos/{owner}/{repo}/pulls'",
	"gh api repos/x/y/issues --jq .[].number",
	"echo $(gh api repos/x/y --jq .n)",
	"gh pr view 1",
	"gh run list",
	"gh issue list --search graphql",
	"gh pr list --search 'graphql in:title'",
	"gh search issues graphql",
	"/opt/homebrew/bin/gh issue list --search graphql",
	"scripts/board-snapshot.sh",
	"scripts/board-snapshot.sh --refresh",
	"scripts/board-snapshot.sh card 314",
	"scripts/board-snapshot.sh queue",
	"bash scripts/board-snapshot.sh queue",
	"scripts/wait-for-ci.sh 123",
	"scripts/main-status.sh",
	"git status --short",
	"git commit -m 'refuse gh api graphql'",
	"git commit -am 'refuse gh api graphql' --message 'and graphql'",
	"gh pr create --title 'no gh api graphql' --body 'see gh api graphql'",
	"gh issue comment 5 --body 'gh api graphql is refused'",
	"git commit -m \"$(cat <<'EOF'\nrefuse gh api graphql\n`gh api graphql`\n$(gh api graphql)\nEOF\n)\"",
	"git commit -F - <<'EOF'\ngh api graphql\nEOF",
	"cat > f <<'EOF'\ngh api graphql\n`gh api graphql`\nEOF",
	"cat <<-'EOF'\n\tgh api graphql\n\tEOF",
	"cat <<'EOF' | tee f\nx\nEOF",
	"gh pr create --title t --body \"$(cat <<'EOF'\nuse gh api graphql never\nEOF\n)\" && gh api repos/x/y",
	"cat <<'EOF'\nit's don't\nEOF\ngh api repos/x/y",
	"echo hi # no graphql here",
	"grep -r graphql docs",
}

// Known over-blocks: refused although harmless. Workaround: a single-quoted
// message, -F file, or a heredoc with a single-quoted delimiter.
var ghGraphQLOverBlocks = []string{
	"echo gh api graphql",
	"grep -r 'gh api graphql' docs",
	"echo hi # gh api graphql",
	"gh api repos/x/graphql",
	"gh api repos/x/y/issues -f title=graphql",
	"gh api repos/x/y; echo graphql",
	"cat > f <<EOF\ngh api graphql\nEOF",
	"cat > f <<\"EOF\"\ngh api graphql\nEOF",
	"git commit -m \"mentions gh api graphql\"",
	"curl https://api.github.com/user # graphql",
}

func TestGhGuardTable(t *testing.T) {
	for _, c := range append(append([]string{}, ghGraphQLRefused...), ghGraphQLRefusedExtra...) {
		if _, gap := indexOf(ghGraphQLGaps, c); gap {
			continue
		}
		if ghguard.Check(c) == "" {
			t.Errorf("not refused: %q", c)
		}
	}
	for _, c := range ghGraphQLAllowed {
		if r := ghguard.Check(c); r != "" {
			t.Errorf("wrongly refused: %q: %s", c, r)
		}
	}
	for _, c := range ghGraphQLOverBlocks {
		if ghguard.Check(c) == "" {
			t.Errorf("documented over-block is now allowed (update the list): %q", c)
		}
	}
	for _, c := range ghGraphQLGaps {
		if ghguard.Check(c) != "" {
			t.Errorf("documented gap is now refused (update the list): %q", c)
		}
	}
}

// TestGhGuardLows pins conservative exception boundaries from #325. These
// checks exercise command text only, not live client hook invocation.
func TestGhGuardLows(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		refused       bool
	}{
		{"L1 adjacent message word", "git commit -m 'gh api graph'ql", true},
		{"L1 adjacent body word", "gh pr create --body 'gh api graph'ql", true},
		{"L2 quoted heredoc accepted over-block", "cat <<\"EOF\"\ngh api graphql\nEOF", true},
		{"L3 ash vetoes exception", "cat <<'EOF'\ngh api graphql\nEOF\nash", true},
		{"L3 busybox vetoes exception", "cat <<'EOF'\ngh api graphql\nEOF\nbusybox", true},
		{"L4 endpoint continuation", "gh api graph\\\nql", true},
		{"L4 command continuation", "g\\\nh api graphql", true},
		{"L4 encoded continuation", "gh api graph%5c%0aql", true},
		{"L4 CRLF continuation", "gh api graph\\\r\nql", true},
		{"L5 unmatched quote", "gh issue list --search 'graphql", true},
		{"L5 unmatched double quote", `gh issue list --search "graphql`, true},
		{"L5 double quoted read", `gh issue list --search "graphql"`, false},
		{"L5 quoted read", "gh pr list --search 'graphql in:title'", false},
		{"L5 rate limit accepted over-block", "gh api rate_limit --jq .resources.graphql", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ghguard.Check(tc.command) != ""; got != tc.refused {
				t.Fatalf("refused = %v, want %v for %q", got, tc.refused, tc.command)
			}
		})
	}
}

func indexOf(l []string, s string) (int, bool) {
	for i, x := range l {
		if x == s {
			return i, true
		}
	}
	return 0, false
}

func TestGhGuardBounds(t *testing.T) {
	start := time.Now()
	big := strings.Repeat("git commit -m 'x' <<'A'\nb\nA\n", 1300)
	_ = ghguard.Check(big[:32<<10])
	_ = ghguard.Check(strings.Repeat("%25", 10<<10) + "gh")
	_ = ghguard.Check(strings.Repeat("<<'A'\n", 5000))
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("bounded inputs took %v", d)
	}
	if ghguard.Check("echo "+strings.Repeat("a", 40<<10)) == "" {
		t.Error("oversized command must be refused")
	}
	md := "git commit -m 'x'\ngit commit -F - <<'EOF'\n" + strings.Repeat("`code` gh api graphql ", 300) + "\nEOF"
	if r := ghguard.Check(md); r != "" {
		t.Errorf("a Markdown body must be allowed: %s", r)
	}
}

// TestGhGuardHookBinary runs the real command with the hook JSON, checking the deny decision on stdout. It is not proof that Claude Code invokes the hook at runtime.
func TestGhGuardHookBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ghguard")
	if out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "../cmd/ghguard").CombinedOutput(); err != nil { //nolint:gosec // fixed arguments
		t.Fatalf("build: %v: %s", err, out)
	}
	run := func(in string) (int, string) {
		cmd := exec.CommandContext(t.Context(), bin) //nolint:gosec // binary built above in a temp dir
		cmd.Stdin = strings.NewReader(in)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("hook must exit 0 (go run maps exit 2 to 1): %v: %s", err, out)
		}
		if strings.Contains(string(out), `"permissionDecision":"deny"`) {
			return 2, string(out)
		}
		return 0, string(out)
	}
	hook := func(tool, cmd string) string {
		b, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": map[string]any{"command": cmd}})
		return string(b)
	}
	for _, tc := range []struct {
		name, in string
		want     int
	}{
		{"graphql", hook("Bash", "gh -R x api /graphql"), 2},
		{"continued endpoint", hook("Bash", "gh api graph\\\nql"), 2},
		{"unmatched quote", hook("Bash", "gh issue list --search 'graphql"), 2},
		{"rest", hook("Bash", "gh api repos/x/y"), 0},
		{"other tool", hook("Read", "gh api graphql"), 0},
		{"monitor", hook("Monitor", "gh api graphql"), 2},
		{"powershell", hook("PowerShell", "gh api graphql"), 2},
		{"no command", `{"tool_name":"Bash","tool_input":{}}`, 0},
		{"malformed", "{", 2},
		{"empty", "", 2},
	} {
		if got, out := run(tc.in); got != tc.want {
			t.Errorf("%s: exit %d, want %d: %s", tc.name, got, tc.want, out)
		}
	}
}

func TestClaudeSettingsRegistersGhGuard(t *testing.T) {
	data, err := os.ReadFile("../.claude/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	for _, e := range s.Hooks.PreToolUse {
		for _, h := range e.Hooks {
			if strings.Contains(e.Matcher, "Bash") && h.Type == "command" && strings.Contains(h.Command, "cmd/ghguard") {
				return
			}
		}
	}
	t.Error("settings.json must register the cmd/ghguard PreToolUse hook on Bash")
}

func FuzzGhGuardCheck(f *testing.F) {
	for _, c := range append(append(append([]string{}, ghGraphQLRefused...), ghGraphQLAllowed...), ghGraphQLRefusedExtra...) {
		f.Add(c)
	}
	f.Fuzz(func(t *testing.T, c string) {
		start := time.Now()
		_ = ghguard.Check(c)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("slow input (%v)", d)
		}
		// Without quotes or heredocs nothing can hide a trailing plain call.
		if !strings.ContainsAny(c, "'<") && len(c) < 30<<10 && ghguard.Check(c+"\ngh api graphql") == "" {
			t.Errorf("not refused after %q", c)
		}
	})
}
