package doctor

import (
	"context"
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
}

func (r *rules) BranchRules(_ context.Context, repo, branch string) ([]github.BranchRule, error) {
	r.askedFor = append(r.askedFor, repo+":"+branch)
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
		{"prototype, force push blocked", repo("prototype", ""), &rules{def: "main", byBranch: map[string][]github.BranchRule{"main": {rule("non_fast_forward", 3, ``)}}}, NotVerified, "only the App and you may write", "main"},
		{"prototype, force push allowed", repo("prototype", "dev"), &rules{def: "main", byBranch: map[string][]github.BranchRule{"dev": {}}}, Fail, "force push is not blocked", "dev"},
		{"the rules cannot be read", repo("published", ""), &rules{def: "main", unreadable: true}, NotVerified, "not verified", "main"},
	}
	for _, c := range cases {
		st, detail := workflowCheck(context.Background(), c.cfg, c.src)
		if st != c.want || !strings.Contains(detail, c.detail) {
			t.Errorf("%s: %s %q, want %s with %q", c.name, st, detail, c.want, c.detail)
		}
		if len(c.src.askedFor) != 1 || c.src.askedFor[0] != "wstein/workharbor:"+c.asked {
			t.Errorf("%s: asked for %v, want the branch %s", c.name, c.src.askedFor, c.asked)
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
