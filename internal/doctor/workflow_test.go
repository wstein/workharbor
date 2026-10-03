package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge/github"
)

// rules is a RuleSource over fixed answers.
type rules struct {
	byBranch   map[string][]github.BranchRule
	bypass     map[int64]int // missing: not shown to the App
	unreadable bool
	def        string
	askedFor   []string
	info       map[int64]github.RulesetInfo // missing: nothing shown to the App
	infoErr    error
	branchErr  map[string]error
}

func (r *rules) Ruleset(_ context.Context, _ string, id int64) (github.RulesetInfo, error) {
	return r.info[id], r.infoErr
}

func (r *rules) BranchRules(_ context.Context, repo, branch string) ([]github.BranchRule, error) {
	r.askedFor = append(r.askedFor, repo+":"+branch)
	if err := r.branchErr[branch]; err != nil {
		return nil, err
	}
	if r.unreadable {
		return nil, github.ErrRulesUnreadable
	}
	return r.byBranch[branch], nil
}

func (r *rules) BypassActors(_ context.Context, _ string, id int64) (int, bool, error) {
	n, ok := r.bypass[id]
	return n, ok, nil
}
func (r *rules) DefaultBranchName(context.Context, string) (string, error) { return r.def, nil }

const appID = 77

// onDefault is a ruleset on ~DEFAULT_BRANCH with n bypass actors that are not the App.
func onDefault(n int) github.RulesetInfo {
	info := github.RulesetInfo{BypassKnown: true, TargetKnown: true, RefInclude: []string{"~DEFAULT_BRANCH"}}
	for i := 0; i < n; i++ {
		info.Bypass = append(info.Bypass, github.BypassActor{ActorID: 1000 + int64(i), ActorType: "User"})
	}
	return info
}

func rule(t string, id int64, params string) github.BranchRule {
	return github.BranchRule{Type: t, RulesetID: id, Parameters: []byte(params)}
}

func repo(workflow, branch string) *config.Config {
	return &config.Config{Repositories: []config.Repository{{Name: "wstein/workharbor", Workflow: workflow, IntegrationBranch: branch}}}
}

func TestTheForgeWorkflowChecksTheRulesetOfTheBranchesThePresetWritesTo(t *testing.T) {
	published := []github.BranchRule{
		rule("pull_request", 1, `{"required_approving_review_count":1}`), rule("required_status_checks", 1, `{}`), rule("required_signatures", 1, ``),
	}
	protectedMain := []github.BranchRule{rule("update", 4, ``)}
	cases := []struct {
		name   string
		cfg    *config.Config
		src    *rules
		want   Status
		detail string
		asked  string
	}{
		{"published, all there, no bypass", repo("published", ""), &rules{def: "main", byBranch: map[string][]github.BranchRule{"main": published}, bypass: map[int64]int{1: 0}}, OK, "as the workflow expects", "main"},
		{"published, a bypass actor", repo("published", ""), &rules{def: "main", byBranch: map[string][]github.BranchRule{"main": published}, bypass: map[int64]int{1: 1}}, Fail, "1 bypass actor", "main"},
		{"published, the bypass list is not shown", repo("published", ""), &rules{def: "main", byBranch: map[string][]github.BranchRule{"main": published}}, NotVerified, "bypass list is not shown", "main"},
		{"published, no review and no signatures", repo("published", ""), &rules{def: "main", byBranch: map[string][]github.BranchRule{"main": {rule("pull_request", 1, `{"required_approving_review_count":0}`), rule("required_status_checks", 1, ``)}}, bypass: map[int64]int{1: 0}}, Fail, "signed commits are not required", "main"},
		{"published goes to the default branch", repo("published", ""), &rules{def: "trunk", byBranch: map[string][]github.BranchRule{"trunk": published}, bypass: map[int64]int{1: 0}}, OK, "", "trunk"},
		{"integration, protected", repo("integration", ""), &rules{def: "main", byBranch: map[string][]github.BranchRule{"develop": {rule("pull_request", 2, ``), rule("non_fast_forward", 2, ``)}}}, OK, "", "develop"},
		{"integration, no PR rule", repo("integration", "next"), &rules{def: "main", byBranch: map[string][]github.BranchRule{"next": {rule("non_fast_forward", 2, ``)}}}, Fail, "a pull request is not required", "next"},
		{"prototype, force push blocked", repo("prototype", "dev"), &rules{def: "main", byBranch: map[string][]github.BranchRule{"dev": {rule("non_fast_forward", 3, ``)}, "main": protectedMain}, info: map[int64]github.RulesetInfo{4: onDefault(0)}}, NotVerified, "only the App and you may write", "dev"},
		{"prototype, force push allowed", repo("prototype", "dev"), &rules{def: "main", byBranch: map[string][]github.BranchRule{"dev": {}, "main": protectedMain}, info: map[int64]github.RulesetInfo{4: onDefault(0)}}, Fail, "force push is not blocked", "dev"},
		{"the rules cannot be read", repo("published", ""), &rules{def: "main", unreadable: true}, NotVerified, "not verified", "main"},
	}
	for _, c := range cases {
		st, detail := workflowCheck(context.Background(), c.cfg, c.src)
		if st != c.want || !strings.Contains(detail, c.detail) {
			t.Errorf("%s: %s %q, want %s with %q", c.name, st, detail, c.want, c.detail)
		}
		if len(c.src.askedFor) == 0 || c.src.askedFor[0] != "wstein/workharbor:"+c.asked {
			t.Errorf("%s: asked for %v, want the branch %s", c.name, c.src.askedFor, c.asked)
		}
	}
	// the supervisor never moves the default branch: naming it as the integration
	// branch fails, without even reading its rules
	for _, preset := range []string{"prototype", "integration"} {
		src := &rules{def: "main"}
		if st, detail := workflowCheck(context.Background(), repo(preset, "main"), src); st != Fail || !strings.Contains(detail, "is the default branch") || len(src.askedFor) != 0 {
			t.Errorf("%s on the default branch: %s %q, asked %v", preset, st, detail, src.askedFor)
		}
	}
	// one failing repository makes the whole check fail; an unread one never passes
	two := &config.Config{Repositories: []config.Repository{{Name: "a/ok", Workflow: "integration"}, {Name: "a/bad", Workflow: "integration"}}}
	src := &rules{def: "main", byBranch: map[string][]github.BranchRule{"develop": {rule("pull_request", 1, ``), rule("non_fast_forward", 1, ``)}}}
	if st, _ := workflowCheck(context.Background(), two, src); st != OK {
		t.Errorf("two good repositories: %s", st)
	}
	src.byBranch = map[string][]github.BranchRule{}
	if st, _ := workflowCheck(context.Background(), two, src); st != Fail {
		t.Errorf("two unprotected repositories: %s", st)
	}
}

func TestPrototypeAlsoExpectsTheDefaultBranchToAcceptNoUpdateFromTheApp(t *testing.T) {
	dev := []github.BranchRule{rule("non_fast_forward", 3, ``)}
	with := func(mod func(*github.RulesetInfo)) map[int64]github.RulesetInfo {
		i := onDefault(1) // the human's own bypass
		if mod != nil {
			mod(&i)
		}
		return map[int64]github.RulesetInfo{4: i}
	}
	cases := []struct {
		name string
		src  *rules
		want Status
		text string
	}{
		{"update rule, targets ~DEFAULT_BRANCH, only the human bypasses", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(nil)}, NotVerified, "only the App and you may write"},
		{"pull_request rule counts too", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("pull_request", 4, ``)}}, info: with(nil)}, NotVerified, "only the App and you may write"},
		{"no update or pull_request rule", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("non_fast_forward", 4, ``)}}, info: with(nil)}, Fail, "could fast-forward it"},
		{"no rule at all", &rules{info: with(nil)}, Fail, "could fast-forward it"},
		{"the App is a bypass actor", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(func(i *github.RulesetInfo) {
			i.Bypass = append(i.Bypass, github.BypassActor{ActorID: appID, ActorType: "Integration"})
		})}, Fail, "the App is a bypass actor of ruleset 4"},
		{"another App is a bypass actor", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(func(i *github.RulesetInfo) {
			i.Bypass = append(i.Bypass, github.BypassActor{ActorID: 5, ActorType: "Integration"})
		})}, NotVerified, "only the App and you may write"},
		{"an actor with the App's number but another type", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(func(i *github.RulesetInfo) {
			i.Bypass = append(i.Bypass, github.BypassActor{ActorID: appID, ActorType: "Team"})
		})}, NotVerified, "only the App and you may write"},
		{"the bypass list is not shown", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(func(i *github.RulesetInfo) { i.BypassKnown, i.Bypass = false, nil })}, NotVerified, "whether the App bypasses ruleset 4"},
		{"the ruleset names the branch, not ~DEFAULT_BRANCH", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(func(i *github.RulesetInfo) { i.RefInclude = []string{"refs/heads/main"} })}, Fail, "targets ~DEFAULT_BRANCH"},
		{"the conditions are not shown", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, info: with(func(i *github.RulesetInfo) { i.TargetKnown, i.RefInclude = false, nil })}, NotVerified, "~DEFAULT_BRANCH (its conditions are not shown"},
		{"the ruleset cannot be read at all", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}}, NotVerified, "not shown to this App"},
		{"the ruleset read errors", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``)}}, infoErr: errors.New("boom")}, NotVerified, "ruleset 4 could not be read: boom"},
		{"the default branch rules are refused", &rules{branchErr: map[string]error{"main": github.ErrRulesUnreadable}}, NotVerified, "default branch main: the rules cannot be read"},
		{"the default branch rules error", &rules{branchErr: map[string]error{"main": errors.New("down")}}, NotVerified, "GitHub could not be asked: down"},
		{"a failure beats an unknown", &rules{byBranch: map[string][]github.BranchRule{"main": {rule("update", 4, ``), rule("pull_request", 8, ``)}}, info: with(func(i *github.RulesetInfo) {
			i.Bypass = append(i.Bypass, github.BypassActor{ActorID: appID, ActorType: "Integration"})
		})}, Fail, "bypass actor of ruleset 4"},
	}
	for _, c := range cases {
		c.src.def = "main"
		c.src.byBranch = merge(c.src.byBranch, map[string][]github.BranchRule{"dev": dev})
		cfg := repo("prototype", "dev")
		cfg.GitHub.AppID = appID
		st, detail := workflowCheck(context.Background(), cfg, c.src)
		if st != c.want || !strings.Contains(detail, c.text) {
			t.Errorf("%s: %s %q, want %s with %q", c.name, st, detail, c.want, c.text)
		}
		if got := strings.Join(c.src.askedFor, ","); got != "wstein/workharbor:dev,wstein/workharbor:main" {
			t.Errorf("%s: asked %s", c.name, got)
		}
	}
	// the other presets never read the default branch's ruleset for this
	src := &rules{def: "main", byBranch: map[string][]github.BranchRule{"develop": {rule("pull_request", 1, ``), rule("non_fast_forward", 1, ``)}}}
	if st, _ := workflowCheck(context.Background(), repo("integration", ""), src); st != OK || len(src.askedFor) != 1 {
		t.Errorf("integration: %s, asked %v", st, src.askedFor)
	}
}

func merge(a, b map[string][]github.BranchRule) map[string][]github.BranchRule {
	out := map[string][]github.BranchRule{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
