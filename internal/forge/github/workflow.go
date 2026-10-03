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

// DefaultBranchName is the default branch, read from GitHub on every call (no cache, design §6).
func (c *Client) DefaultBranchName(ctx context.Context, repo string) (string, error) {
	return c.defaultBranch(ctx, repo)
}
