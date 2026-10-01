// Package redact removes secrets from text before it is written anywhere:
// events, transcripts, Decision inputs, audit entries and logs (design §5.4,
// §7.3). Audit entries are never purged, so redacting later would be too late.
//
// A Redactor knows two kinds of secret. Exact secrets, such as the scoped
// tokens the credential service issues for a run, are registered and replaced
// in every form they take in text. Well-known token formats are replaced
// whether or not anyone registered them. It is a floor and not a proof: a
// secret in a format it has never seen and that was not registered passes.
package redact

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Mask replaces a secret.
const Mask = "[REDACTED]"

// MinSecretLength is the shortest secret Add accepts. Registering something
// shorter would redact ordinary words.
const MinSecretLength = 8

type rule struct {
	re   *regexp.Regexp
	repl string
}

// rules are the well-known formats, applied in order.
var rules = []rule{
	{regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,255})\b`), Mask},
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`), Mask},
	{regexp.MustCompile(`\bsk-(?:ant-|proj-)?[A-Za-z0-9_-]{20,}`), Mask},
	{regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA)[A-Z0-9]{16}\b`), Mask},
	{regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}`), Mask},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`), Mask},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), Mask},
	// A private key block. The body stops at the first quote or brace, so a
	// block inside a JSON string never swallows the JSON around it, and an
	// unterminated block is redacted as far as it goes.
	{regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[A-Za-z0-9+/=\s\\]*(?:-----END [A-Z0-9 ]*PRIVATE KEY-----)?`), Mask},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{16,}`), "${1} " + Mask},
	// A password in a URL: scheme://user:password@host.
	{regexp.MustCompile(`\b([a-z][a-z0-9+.-]*://[^/\s:@"']+):[^/\s@"']+@`), "${1}:" + Mask + "@"},
	// A value assigned to a secret-looking name. The value must contain a
	// letter and be at least six characters, so numeric counts such as
	// "input_tokens": 123456 are left alone: usage records must stay correct.
	{regexp.MustCompile(`(?i)("?[A-Za-z0-9_.-]*(?:token|secret|password|passwd|passphrase|api[_-]?key|apikey|authorization|credential|private[_-]?key)s?"?\s*[:=]\s*"?)(?:[0-9._+/=-]*[A-Za-z])[A-Za-z0-9._+/=-]{5,}`), "${1}" + Mask},
}

// Redactor replaces secrets in text. It is safe for concurrent use.
type Redactor struct {
	mu       sync.RWMutex
	secrets  map[string][]string // a registered secret -> the forms it is replaced in
	variants []string            // every form, longest first
	defaults bool
}

// Option configures New.
type Option func(*Redactor)

// WithoutDefaults leaves out the well-known formats, so only registered
// secrets are replaced. Tests use it as the control for the canary test.
func WithoutDefaults() Option { return func(r *Redactor) { r.defaults = false } }

// New returns a Redactor that knows the well-known token formats.
func New(opts ...Option) *Redactor {
	r := &Redactor{secrets: map[string][]string{}, defaults: true}
	for _, o := range opts {
		o(r)
	}
	return r
}

// forms returns the ways a secret may appear in text: as it is, escaped for
// JSON, escaped for a URL, and base64 or hex encoded. Forms shorter than
// MinSecretLength are dropped.
func forms(secret string) []string {
	set := map[string]struct{}{secret: {}}
	if b, err := json.Marshal(secret); err == nil {
		set[strings.Trim(string(b), `"`)] = struct{}{}
	}
	set[url.QueryEscape(secret)] = struct{}{}
	set[url.PathEscape(secret)] = struct{}{}
	raw := []byte(secret)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		set[enc.EncodeToString(raw)] = struct{}{}
	}
	set[hex.EncodeToString(raw)] = struct{}{}

	var out []string
	for f := range set {
		if len(f) >= MinSecretLength {
			out = append(out, f)
		}
	}
	return out
}

// Add registers an exact secret, usually a scoped token issued for a run. It
// reports false, and registers nothing, if the secret is shorter than
// MinSecretLength.
func (r *Redactor) Add(secret string) bool {
	if len(secret) < MinSecretLength {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets[secret] = forms(secret)
	r.rebuild()
	return true
}

// Remove forgets a secret, for example when the run that held it ends.
func (r *Redactor) Remove(secret string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.secrets, secret)
	r.rebuild()
}

func (r *Redactor) rebuild() {
	// A new slice every time: String reads the old one without the lock.
	seen := map[string]struct{}{}
	variants := make([]string, 0, len(r.variants)+8)
	for _, fs := range r.secrets {
		for _, f := range fs {
			if _, ok := seen[f]; !ok {
				seen[f] = struct{}{}
				variants = append(variants, f)
			}
		}
	}
	// Longest first, so a secret is never half replaced by one of its parts.
	sort.Slice(variants, func(i, j int) bool { return len(variants[i]) > len(variants[j]) })
	r.variants = variants
}

// String returns s with every known secret replaced by Mask.
func (r *Redactor) String(s string) string {
	r.mu.RLock()
	variants := r.variants
	defaults := r.defaults
	r.mu.RUnlock()

	for _, v := range variants {
		if strings.Contains(s, v) {
			s = strings.ReplaceAll(s, v, Mask)
		}
	}
	if defaults {
		for _, ru := range rules {
			s = ru.re.ReplaceAllString(s, ru.repl)
		}
	}
	return s
}

// Bytes returns a redacted copy of b; b is not changed. Replacing a secret
// with Mask keeps a JSON document valid.
func (r *Redactor) Bytes(b []byte) []byte { return []byte(r.String(string(b))) }
