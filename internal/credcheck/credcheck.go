// Package credcheck is the one place that decides whether a name/value pair
// destined for an agent API-key variable is a subscription credential (D40,
// issue #348). whr never reads, stores or relays a subscription login, so the
// pair is refused before it is written or passed on.
//
// What is and is not verified:
//   - Names: the patterns below come from names the repo already treats as
//     subscription credentials (CLAUDE_CODE_OAUTH_TOKEN, spike #2/#82) and from
//     the names issue #348 lists (ANTHROPIC_AUTH_TOKEN). Generic fragments
//     (oauth, session, auth_token, access_token, refresh_token, setup_token)
//     are a conservative rule, not a vendor fact.
//   - Values: the prefix of a `claude setup-token` or OAuth token is
//     UNVERIFIED (spike #82 shows only "sk-ant-..." elided), so NO prefix is
//     treated as a subscription shape: a real API key also starts "sk-ant-".
//     The only value shape refused is the credentials-file JSON documented in
//     spike agent-signin (accessToken / refreshToken keys). Value-shape
//     detection by token prefix needs a measurement that records no token.
//   - Codex and Antigravity: names only, through the generic fragments; no
//     vendor-specific value shape is known.
//
// Nothing here returns, logs or formats the value.
package credcheck

import (
	"errors"
	"regexp"
	"strings"
)

var subscriptionName = regexp.MustCompile(`(?i)oauth|session|auth_token|access_token|refresh_token|setup_token|login_token`)

var subscriptionJSONKey = regexp.MustCompile(`(?i)"?(access|refresh)token"?\s*[:=]`)

// ErrSubscriptionName and ErrSubscriptionValue are returned by Check.
var (
	ErrSubscriptionName  = errors.New("a subscription or login credential name")
	ErrSubscriptionValue = errors.New("a value shaped like a subscription login, not an API key")
)

// Name reports whether the variable name is a known subscription credential.
func Name(name string) bool { return subscriptionName.MatchString(name) }

// Check refuses a subscription credential name or value. The value never
// appears in the error.
func Check(name, value string) error {
	if Name(name) {
		return ErrSubscriptionName
	}
	v := strings.TrimSpace(value)
	if strings.HasPrefix(v, "{") || subscriptionJSONKey.MatchString(v) {
		return ErrSubscriptionValue
	}
	return nil
}

// Advice is the English text shown with a refusal.
const Advice = "API key, not a setup-token or login token: create an API key in the vendor's console (for Anthropic, the Console's API keys page) and use that; sign in inside the environment for a subscription"
