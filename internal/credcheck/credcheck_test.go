package credcheck

import (
	"errors"
	"strings"
	"testing"
)

// Synthetic values only; none is a real token.
func TestNamesAreRefused(t *testing.T) {
	for _, n := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "SOME_SESSION_TOKEN", "X_ACCESS_TOKEN", "X_REFRESH_TOKEN", "CLAUDE_SETUP_TOKEN", "X_OAUTH"} {
		if err := Check(n, "synthetic-value-0123456789"); !errors.Is(err, ErrSubscriptionName) {
			t.Errorf("%s: %v", n, err)
		}
	}
}

func TestCredentialsFileShapeIsRefused(t *testing.T) {
	for _, v := range []string{`{"claudeAiOauth":{"accessToken":"synthetic"}}`, `accessToken: synthetic`, `refreshToken=synthetic`, `"accessToken" : "x"`} {
		if err := Check("ANTHROPIC_API_KEY", v); !errors.Is(err, ErrSubscriptionValue) {
			t.Errorf("%q: %v", v, err)
		}
	}
}

func TestAnAPIKeyPasses(t *testing.T) {
	for _, n := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"} {
		if err := Check(n, "sk-ant-api03-"+strings.Repeat("x", 30)); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
}

func TestTheValueNeverAppearsInTheError(t *testing.T) {
	err := Check("ANTHROPIC_API_KEY", `{"accessToken":"SYNTHETIC-SECRET"}`)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("%v", err)
	}
}

func TestSnakeCaseKeysBomAndEdgeShapesAreRefused(t *testing.T) {
	for _, v := range []string{
		`"access_token":"x"`, `access_token=x`, `"refresh_token": "x"`,
		"\uFEFF{\"a\":1}", "\uFEFF  accessToken: x", "  \n{", "{", "{}",
		`prefix "accessToken": "x"`, // the key is found anywhere, not only at the start
	} {
		if err := Check("ANTHROPIC_API_KEY", v); !errors.Is(err, ErrSubscriptionValue) {
			t.Errorf("%q: %v", v, err)
		}
	}
}

func TestLoginTokenNameAndLeadingWhitespaceValue(t *testing.T) {
	if err := Check("CLAUDE_LOGIN_TOKEN", "synthetic-value-0123456789"); !errors.Is(err, ErrSubscriptionName) {
		t.Errorf("login_token: %v", err)
	}
	if err := Check("ANTHROPIC_API_KEY", "  \t sk-ant-api03-"+strings.Repeat("x", 30)); err != nil {
		t.Errorf("leading whitespace on a real key: %v", err)
	}
}

func TestAdviceSaysUseAnAPIKey(t *testing.T) {
	if !strings.HasPrefix(Advice, "Use an API key, not a setup-token or login token") {
		t.Errorf("%q", Advice)
	}
}

func TestASubscriptionNameOutranksAnEmptyValue(t *testing.T) {
	if err := Check("CLAUDE_CODE_OAUTH_TOKEN", ""); !errors.Is(err, ErrSubscriptionName) {
		t.Errorf("got %v", err)
	}
}

func TestAnEmptyOrBlankValueIsRefused(t *testing.T) {
	for _, v := range []string{"", " ", "\t", "\uFEFF", " \r\n "} {
		if err := Check("ANTHROPIC_API_KEY", v); !errors.Is(err, ErrEmptyValue) {
			t.Errorf("%q: %v", v, err)
		}
	}
}
