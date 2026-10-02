package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wstein/workharbor/internal/config"
	"github.com/wstein/workharbor/internal/forge/github"
	"github.com/wstein/workharbor/internal/policy"
)

// RuleSource is what the workflow check reads of the forge.
type RuleSource interface {
	BranchRules(ctx context.Context, repo, branch string) ([]github.BranchRule, error)
	BypassActors(ctx context.Context, repo string, rulesetID int64) (n int, known bool, err error)
	DefaultBranchName(ctx context.Context, repo string) (string, error)
}

// workflowCheck reads the rulesets of the branches each repository's preset writes
// to and compares them with what D47 expects. A mismatch is a failure; a rule it
// cannot read, or a fact it cannot see (the bypass list, who else may write), is
// not verified, never a pass.
func workflowCheck(ctx context.Context, cfg *config.Config, src RuleSource) (Status, string) {
	worst := OK
	var parts []string
	note := func(st Status, msg string) {
		parts = append(parts, msg)
		if st == Fail || (st == NotVerified && worst == OK) {
			worst = st
		}
	}
	for _, r := range cfg.Repositories {
		preset := r.Preset()
		def, err := src.DefaultBranchName(ctx, r.Name)
		if err != nil {
			note(NotVerified, r.Name+": the default branch could not be read: "+oneLine(err.Error()))
			continue
		}
		branch := r.Target(def)
		st, msg := evaluate(ctx, src, r.Name, preset, branch)
		note(st, r.Name+" ("+string(preset)+", "+branch+"): "+msg)
	}
	return worst, strings.Join(parts, "; ")
}

// evaluate compares the rules of one branch with what a preset expects.
func evaluate(ctx context.Context, src RuleSource, repo string, preset policy.Preset, branch string) (Status, string) {
	rules, err := src.BranchRules(ctx, repo, branch)
	switch {
	case errors.Is(err, github.ErrRulesUnreadable):
		return NotVerified, "the rules cannot be read with this App, so the ruleset is not verified"
	case err != nil:
		return NotVerified, "GitHub could not be asked: " + oneLine(err.Error())
	}
	has := map[string]github.BranchRule{}
	var ids []int64
	seen := map[int64]bool{}
	for _, rule := range rules {
		has[rule.Type] = rule
		if !seen[rule.RulesetID] { // a ruleset counts once, however many rules it has
			seen[rule.RulesetID] = true
			ids = append(ids, rule.RulesetID)
		}
	}
	var missing []string
	need := func(rule, what string) {
		if _, ok := has[rule]; !ok {
			missing = append(missing, what)
		}
	}
	unknown := []string{}
	switch preset {
	case policy.Prototype:
		need("non_fast_forward", "force push is not blocked")
		unknown = append(unknown, "whether only the App and you may write to it")
	case policy.Integration:
		need("pull_request", "a pull request is not required")
		need("non_fast_forward", "force push is not blocked")
	case policy.Published:
		need("pull_request", "a pull request with review is not required")
		if pr, ok := has["pull_request"]; ok && reviews(pr) < 1 {
			missing = append(missing, "the pull request needs no approving review")
		}
		need("required_status_checks", "status checks are not required")
		need("required_signatures", "signed commits are not required")
		bypass, known := 0, len(ids) > 0
		for _, id := range ids {
			n, ok, err := src.BypassActors(ctx, repo, id)
			if err != nil || !ok {
				known = false
				break
			}
			bypass += n
		}
		switch {
		case known && bypass > 0:
			missing = append(missing, fmt.Sprintf("%d bypass actor(s) can skip the ruleset", bypass))
		case !known:
			unknown = append(unknown, "whether the ruleset has a bypass actor (the bypass list is not shown to this App)")
		}
	}
	if len(missing) > 0 {
		return Fail, strings.Join(missing, ", ")
	}
	if len(unknown) > 0 {
		return NotVerified, "the rules are as expected; not verified: " + strings.Join(unknown, ", ")
	}
	return OK, "the ruleset is as the workflow expects"
}

func reviews(r github.BranchRule) int {
	var p struct {
		N int `json:"required_approving_review_count"`
	}
	_ = json.Unmarshal(r.Parameters, &p)
	return p.N
}
