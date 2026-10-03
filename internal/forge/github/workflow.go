package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// BranchRule is one rule that applies to a branch, from the repository's rulesets.
type BranchRule struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
	RulesetID  int64           `json:"ruleset_id"`
}

// ErrRulesUnreadable is GitHub not letting the App read the rules: the App has no
// permission for them, or the repository has none to show it. It is not a
// finding about the rules themselves, so `whr doctor` reports not verified.
var ErrRulesUnreadable = errors.New("github: the rules of the branch cannot be read with this App")

// BranchRules reads the rules that apply to a branch (GET
// /repos/{repo}/rules/branches/{branch}). A refusal is ErrRulesUnreadable. Which
// permission the App needs for this read is unverified (issue #105).
func (c *Client) BranchRules(ctx context.Context, repo, branch string) ([]BranchRule, error) {
	esc, err := escapeBranch(branch)
	if err != nil {
		return nil, err
	}
	var rules []BranchRule
	err = c.call(ctx, repo, http.MethodGet, "/repos/"+repo+"/rules/branches/"+esc, nil, &rules)
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusForbidden || ae.Status == http.StatusNotFound) {
		return nil, errors.Join(ErrRulesUnreadable, err)
	}
	return rules, err
}

// BypassActors returns how many bypass actors a ruleset has. known is false when
// GitHub does not show the ruleset's bypass list to this App, which is the usual
// case without administration access.
func (c *Client) BypassActors(ctx context.Context, repo string, rulesetID int64) (n int, known bool, err error) {
	var rs struct {
		BypassActors *[]json.RawMessage `json:"bypass_actors"`
	}
	err = c.call(ctx, repo, http.MethodGet, "/repos/"+repo+"/rulesets/"+strconv.FormatInt(rulesetID, 10), nil, &rs)
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusForbidden || ae.Status == http.StatusNotFound) {
		return 0, false, nil
	}
	if err != nil || rs.BypassActors == nil {
		return 0, false, err
	}
	return len(*rs.BypassActors), true, nil
}

// BypassActor is one entry of a ruleset's bypass list. ActorType is GitHub's
// ("Integration" for a GitHub App, "RepositoryRole", "Team", "OrganizationAdmin").
type BypassActor struct {
	ActorID   int64  `json:"actor_id"`
	ActorType string `json:"actor_type"`
}

// RulesetInfo is what GitHub shows an App of one ruleset (GET
// /repos/{repo}/rulesets/{id}). Each part is known only when GitHub showed it:
// without administration access the bypass list is usually left out, and whether
// the conditions are shown to such an App is unverified (issue #217).
type RulesetInfo struct {
	BypassKnown bool
	Bypass      []BypassActor
	TargetKnown bool
	RefInclude  []string // conditions.ref_name.include, e.g. "~DEFAULT_BRANCH"
	RefExclude  []string // conditions.ref_name.exclude
}

// Ruleset reads one ruleset. A refusal (403, 404) is an answer with nothing known,
// not an error; the caller never takes unknown for a pass.
func (c *Client) Ruleset(ctx context.Context, repo string, rulesetID int64) (RulesetInfo, error) {
	var rs struct {
		BypassActors *[]BypassActor `json:"bypass_actors"`
		Conditions   *struct {
			RefName *struct {
				Include []string `json:"include"`
				Exclude []string `json:"exclude"`
			} `json:"ref_name"`
		} `json:"conditions"`
	}
	err := c.call(ctx, repo, http.MethodGet, "/repos/"+repo+"/rulesets/"+strconv.FormatInt(rulesetID, 10), nil, &rs)
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusForbidden || ae.Status == http.StatusNotFound) {
		return RulesetInfo{}, nil
	}
	if err != nil {
		return RulesetInfo{}, err
	}
	var info RulesetInfo
	if rs.BypassActors != nil {
		info.BypassKnown, info.Bypass = true, *rs.BypassActors
	}
	if rs.Conditions != nil && rs.Conditions.RefName != nil {
		info.TargetKnown, info.RefInclude, info.RefExclude = true, rs.Conditions.RefName.Include, rs.Conditions.RefName.Exclude
	}
	return info, nil
}

// DefaultBranchName is the default branch, read from GitHub on every call (no cache, design §6).
func (c *Client) DefaultBranchName(ctx context.Context, repo string) (string, error) {
	return c.defaultBranch(ctx, repo)
}
