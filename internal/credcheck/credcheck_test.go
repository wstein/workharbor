package credcheck

import (
	"errors"
	"strings"
	"testing"
)

// Synthetic values only; none is a real token.
func TestNamesAreRefused(t *testing.T) {
	for _, n := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "SOME_SESSION_TOKEN", "X_ACCESS_TOKEN", "X_REFRESH_TOKEN", "CLAUDE_SETUP_TOKEN"} {
		if err := Check(n, "synthetic-value-0123456789"); !errors.Is(err, ErrSubscriptionName) {
			t.Errorf("%s: %v", n, err)
		}
	}
}

func TestCredentialsFileShapeIsRefused(t *testing.T) {
	for _, v := range []string{`{"claudeAiOauth":{"accessToken":"synthetic"}}`, `accessToken: synthetic`, `refreshToken=synthetic`} {
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
