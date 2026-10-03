package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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
	Ruleset(ctx context.Context, repo string, rulesetID int64) (github.RulesetInfo, error)
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
		if !preset.ToDefaultBranch() && branch == def {
			note(Fail, r.Name+" ("+string(preset)+"): the integration branch "+branch+" is the default branch, which the supervisor never moves; name another")
			continue
		}
		st, msg := evaluate(ctx, src, r.Name, preset, branch)
		if preset == policy.Prototype {
			// the one preset that fast-forwards: the default branch must accept no
			// direct update from the App (#217), read from the forge now (def)
			dst, dmsg := evaluateDefault(ctx, src, r.Name, def, cfg.GitHub.AppID)
			msg += "; default branch " + def + ": " + dmsg
			st = worse(st, dst)
		}
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

func worse(a, b Status) Status {
	if a == Fail || b == Fail {
		return Fail
	}
	if a == NotVerified || b == NotVerified {
		return NotVerified
	}
	return OK
}

// evaluateDefault checks that the default branch accepts no direct update from the
// App: an active ruleset with an update or pull_request rule applies to it, that
// ruleset targets ~DEFAULT_BRANCH (so it follows a change of default), and the App
// is not among its bypass actors. Anything it cannot read is not verified, never ok.
func evaluateDefault(ctx context.Context, src RuleSource, repo, def string, appID int64) (Status, string) {
	rules, err := src.BranchRules(ctx, repo, def)
	switch {
	case errors.Is(err, github.ErrRulesUnreadable):
		return NotVerified, "the rules cannot be read with this App, so the ruleset is not verified"
	case err != nil:
		return NotVerified, "GitHub could not be asked: " + oneLine(err.Error())
	}
	var ids []int64
	seen := map[int64]bool{}
	for _, rule := range rules {
		if (rule.Type == "update" || rule.Type == "pull_request") && !seen[rule.RulesetID] {
			seen[rule.RulesetID] = true
			ids = append(ids, rule.RulesetID)
		}
	}
	if len(ids) == 0 {
		return Fail, "no ruleset restricts updates or requires a pull request, so the App could fast-forward it"
	}
	var failed, unknown []string
	targets, unknownTarget := false, false
	for _, id := range ids {
		info, err := src.Ruleset(ctx, repo, id)
		if err != nil {
			unknown = append(unknown, fmt.Sprintf("ruleset %d could not be read: %s", id, oneLine(err.Error())))
			unknownTarget = true
			continue
		}
		switch {
		case !info.BypassKnown:
			unknown = append(unknown, fmt.Sprintf("whether the App bypasses ruleset %d (the bypass list is not shown to this App)", id))
		default:
			for _, a := range info.Bypass {
				if a.ActorType == "Integration" && a.ActorID == appID {
					failed = append(failed, fmt.Sprintf("the App is a bypass actor of ruleset %d", id))
				}
			}
		}
		switch {
		case !info.TargetKnown:
			unknownTarget = true
		case slices.Contains(info.RefInclude, "~DEFAULT_BRANCH") || slices.Contains(info.RefInclude, "~ALL"):
			targets = true
		}
	}
	if !targets {
		if unknownTarget {
			unknown = append(unknown, "whether the ruleset targets ~DEFAULT_BRANCH (its conditions are not shown to this App)")
		} else {
			failed = append(failed, "no such ruleset targets ~DEFAULT_BRANCH, so it would not follow a change of default")
		}
	}
	if len(failed) > 0 {
		return Fail, strings.Join(failed, ", ")
	}
	if len(unknown) > 0 {
		return NotVerified, "a ruleset restricts updates; not verified: " + strings.Join(unknown, ", ")
	}
	return OK, "a ruleset on ~DEFAULT_BRANCH restricts updates or requires a pull request, and the App does not bypass it"
}

func reviews(r github.BranchRule) int {
	var p struct {
		N int `json:"required_approving_review_count"`
	}
	_ = json.Unmarshal(r.Parameters, &p)
	return p.N
}
