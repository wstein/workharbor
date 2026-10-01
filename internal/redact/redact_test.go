package redact

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Fake tokens in each well-known format, assembled so no real secret scanner
// is triggered by this file.
var (
	ghToken    = "ghp_" + strings.Repeat("a1B2c3", 6)      // 36 characters after the prefix
	ghFine     = "github_pat_" + strings.Repeat("x9Y8", 8) //nolint:gosec // a fake token for the test
	glToken    = "glpat-" + strings.Repeat("q7W", 8)
	antToken   = "sk-ant-api03-" + strings.Repeat("Zz09_-", 5)
	oaToken    = "sk-proj-" + strings.Repeat("Ab12", 8)
	awsKey     = "AKIA" + "ABCDEFGH12345678"
	slackToken = "xoxb-" + "1234567890-abcdefghij"
	googleKey  = "AIza" + strings.Repeat("Kk1_", 8) + "xyz"
	jwtToken   = "eyJhbGciOiJIUzI1NiJ9" + "." + "eyJzdWIiOiIxMjM0NTY3ODkwIn0" + "." + "dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

func TestKnownFormatsAreRedacted(t *testing.T) {
	r := New()
	tests := []struct {
		name string
		text string
		leak string // the part that must be gone
		keep string // the context that must stay
	}{
		{"github classic token", "clone with " + ghToken + " now", ghToken, "clone with"},
		{"github fine-grained token", "token " + ghFine, ghFine, "token"},
		{"gitlab token", "GITLAB=" + glToken + ";", glToken, ";"},
		{"anthropic key", "key " + antToken + " end", antToken, "end"},
		{"openai key", "key " + oaToken + " end", oaToken, "end"},
		{"aws key", "id " + awsKey + " ok", awsKey, "ok"},
		{"slack token", slackToken + " posted", slackToken, "posted"},
		{"google key", "k=" + googleKey, googleKey, "k="},
		{"jwt", "jwt " + jwtToken + " done", jwtToken, "done"},
		{"bearer header", "curl -H 'Authorization: Bearer abcdefghijklmnop1234567890' https://x.test", "abcdefghijklmnop1234567890", "https://x.test"},
		{"basic header", "Authorization: Basic dXNlcjpwYXNzd29yZDEyMzQ1Ng==", "dXNlcjpwYXNzd29yZDEyMzQ1Ng", "Authorization"},
		{"password in a url", "git clone https://bot:s3cr3tP4ss@example.test/repo.git", "s3cr3tP4ss", "bot:"},
		{"password assignment", "export DB_PASSWORD=hunter2hunter2", "hunter2hunter2", "export DB_PASSWORD="},
		{"json secret value", `{"api_key":"abcd-1234-efgh","ok":true}`, "abcd-1234-efgh", `"ok":true`},
		{"token in a yaml line", "github_token: ghs_notARealOneButLongEnough", "ghs_notARealOneButLongEnough", "github_token:"},
		{"private key block", "key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\nabcdef\n-----END RSA PRIVATE KEY-----\nafter", "MIIEowIBAAKCAQEA", "after"},
		{"openssh key block", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\n-----END OPENSSH PRIVATE KEY-----", "b3BlbnNzaC1rZXktdjEAAAAA", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := r.String(tc.text)
			if strings.Contains(got, tc.leak) {
				t.Errorf("the secret survived: %q", got)
			}
			if !strings.Contains(got, Mask) {
				t.Errorf("no mask in %q", got)
			}
			if tc.keep != "" && !strings.Contains(got, tc.keep) {
				t.Errorf("the context %q was lost: %q", tc.keep, got)
			}
		})
	}
}

// What must NOT be touched: usage records and commit hashes.
func TestOrdinaryTextIsLeftAlone(t *testing.T) {
	r := New()
	for _, text := range []string{
		`{"input_tokens": 123456, "output_tokens": 7890, "cache_read_tokens": 1000000}`,
		"max_tokens=4096 and tokens: 12345678",
		"commit 8e2f1c4a9b3d5e7f0a1b2c3d4e5f60718293a4b5 on agent/topic",
		"the password policy and a token bucket were discussed",
		"password_required: false",
		"git clone https://github.com/wstein/workharbor.git",
		"Authorization is checked by the forge",
		"see https://docs.example.test/path?token=1234",
		"sk-short",
	} {
		if got := r.String(text); got != text {
			t.Errorf("ordinary text changed:\n got %q\nwant %q", got, text)
		}
	}
}

func TestRegisteredSecretsAreRedactedInEveryForm(t *testing.T) {
	r := New(WithoutDefaults())
	secret := `pa"ss/wor d+tok&en=1234` //nolint:gosec // a fake secret for the test
	if !r.Add(secret) {
		t.Fatal("a long enough secret must be accepted")
	}
	forms := map[string]string{
		"raw":            secret,
		"json escaped":   func() string { b, _ := json.Marshal(secret); return strings.Trim(string(b), `"`) }(),
		"query escaped":  url.QueryEscape(secret),
		"path escaped":   url.PathEscape(secret),
		"base64":         base64.StdEncoding.EncodeToString([]byte(secret)),
		"base64 raw url": base64.RawURLEncoding.EncodeToString([]byte(secret)),
	}
	for name, form := range forms {
		got := r.String("before " + form + " after")
		if strings.Contains(got, form) || got != "before "+Mask+" after" {
			t.Errorf("%s form survived or was mangled: %q", name, got)
		}
	}
}

func TestOnlyLongEnoughSecretsAreRegistered(t *testing.T) {
	r := New(WithoutDefaults())
	if r.Add("short") || r.Add("1234567") {
		t.Error("a secret shorter than MinSecretLength must be refused")
	}
	if got := r.String("short and 1234567"); got != "short and 1234567" {
		t.Errorf("a refused secret changed the text: %q", got)
	}
	if !r.Add("12345678") {
		t.Error("a secret of exactly MinSecretLength must be accepted")
	}
}

func TestRemoveStopsRedacting(t *testing.T) {
	r := New(WithoutDefaults())
	r.Add("run-scoped-token-1")
	r.Add("run-scoped-token-2")
	r.Remove("run-scoped-token-1")
	got := r.String("one run-scoped-token-1 two run-scoped-token-2")
	if got != "one run-scoped-token-1 two "+Mask {
		t.Errorf("after Remove: %q", got)
	}
}

func TestLongestFormWinsSoASecretIsNeverHalfReplaced(t *testing.T) {
	r := New(WithoutDefaults())
	r.Add("abcdefgh")
	r.Add("abcdefgh-and-more")
	if got := r.String("x abcdefgh-and-more y"); got != "x "+Mask+" y" {
		t.Errorf("got %q, want the longer secret replaced whole", got)
	}
}

func TestWithoutDefaultsOnlyKnowsRegisteredSecrets(t *testing.T) {
	r := New(WithoutDefaults())
	if got := r.String("a token " + ghToken); got != "a token "+ghToken {
		t.Errorf("a redactor without defaults changed unregistered text: %q", got)
	}
}

func TestRedactingJSONKeepsItValid(t *testing.T) {
	r := New()
	r.Add("exact-canary-secret-value")
	payload, err := json.Marshal(map[string]any{
		"cmd":     "curl -H 'Authorization: Bearer abcdefghijklmnop1234567890' " + ghToken,
		"note":    "exact-canary-secret-value and a key\n-----BEGIN PRIVATE KEY-----\nMIIabc\n-----END PRIVATE KEY-----",
		"api_key": "plain-api-key-value",
		"usage":   map[string]int{"input_tokens": 123456},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Bytes(payload)
	if !json.Valid(got) {
		t.Fatalf("redaction broke the JSON: %s", got)
	}
	for _, leak := range []string{ghToken, "exact-canary-secret-value", "abcdefghijklmnop1234567890", "MIIabc", "plain-api-key-value"} {
		if strings.Contains(string(got), leak) {
			t.Errorf("%q survived in %s", leak, got)
		}
	}
	var back map[string]any
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if usage := back["usage"].(map[string]any); usage["input_tokens"] != float64(123456) {
		t.Errorf("the usage count changed: %v", usage)
	}
}

func TestRedactionIsIdempotentAndLeavesTheInputAlone(t *testing.T) {
	r := New()
	r.Add("exact-canary-secret-value")
	in := []byte("use exact-canary-secret-value and " + ghToken)
	orig := string(in)
	once := r.Bytes(in)
	twice := r.Bytes(once)
	if string(in) != orig {
		t.Error("Bytes changed its input")
	}
	if string(once) != string(twice) {
		t.Errorf("redacting twice changed the result: %q then %q", once, twice)
	}
}

func TestConcurrentUse(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			secret := fmt.Sprintf("secret-number-%02d-xxxxxxxx", i)
			r.Add(secret)
			if got := r.String("a " + secret + " b"); strings.Contains(got, secret) {
				t.Errorf("secret %d survived", i)
			}
			r.Remove(secret)
		}()
	}
	wg.Wait()
}
