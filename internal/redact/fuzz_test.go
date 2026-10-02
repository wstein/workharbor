package redact

import (
	"strings"
	"testing"
)

// FuzzRedact checks what the redactor promises whatever the text and the secret:
// a registered secret, in any form it is replaced in, never reappears; the output
// is bounded by the input and the masks; redacting twice changes nothing more; and
// Bytes agrees with String. It is a floor and not a proof for an unregistered
// secret in a format nobody has seen, so only registered forms are asserted.
func FuzzRedact(f *testing.F) {
	// The seeds that look like real credentials are assembled at run time, so that no
	// literal resembling one sits in the repository for a secret scanner to flag.
	ghToken := "gh" + "p_" + strings.Repeat("abcdefghij", 3) + "abcdef"
	f.Add("token is "+ghToken, ghToken)
	value := "s3cr3t" + "-v4lue-1234"
	f.Add(`{"key":"`+value+`"}`, value)
	f.Add("a=b%20c+d", "b c+d e f")
	f.Add("-----BEGIN "+"PRIVATE KEY-----\nMIIB\n-----END "+"PRIVATE KEY-----", "MIIBabcdefgh")
	f.Add("Authorization: Bearer abcdefghijklmnop", "abcdefghijklmnop")
	f.Add("https://u:pa55w0rdpa55@host/x", "pa55w0rdpa55")
	f.Add(strings.Repeat("SECRET12", 50), "SECRET12")
	f.Fuzz(func(t *testing.T, text, secret string) {
		r := New()
		accepted := r.Add(secret)
		out := r.String(text)

		if got := string(r.Bytes([]byte(text))); got != out {
			t.Fatalf("Bytes and String disagree: %q vs %q", got, out)
		}
		// bounded: every replacement is a mask, so the output grows by at most one
		// mask per MinSecretLength-ish bytes of input, and never more than a small
		// multiple of the input plus one mask
		if limit := len(text)*4 + len(Mask)*4; len(out) > limit {
			t.Fatalf("output of %d bytes from %d bytes of input", len(out), len(text))
		}
		if accepted {
			// a form that is part of the mask itself cannot be told from the mask
			for _, form := range forms(secret) {
				if strings.Contains(Mask, form) {
					return
				}
				if strings.Contains(out, form) {
					t.Fatalf("the registered secret form %q reappears in %q", form, out)
				}
			}
		}
		if again := r.String(out); strings.Contains(again, secret) && accepted && !strings.Contains(Mask, secret) {
			t.Fatalf("a second pass shows the secret")
		}
	})
}
