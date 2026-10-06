package confirm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixtures are the canonical bytes from the #333 design; the digests are
// the ones pinned there.
var fixtures = []struct{ name, digest string }{
	{"land-cli", "e384bb14aa73efed070d8a421d4ee3216ba04b666a323a2d8063ff097993e57e"},
	{"decision-relay", "ada2e2d9f9217c11d3c460a2690d0b4e32ad4e9a547aa0129ea7607bfad3b6c8"},
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	if name == "escapes" { // decision-relay with a non-ASCII question, built in code
		r, err := Decode(readFixture(t, "decision-relay"))
		if err != nil {
			t.Fatal(err)
		}
		r.Ext = map[string]any{"crewbook.answered": "H97:y"}
		r.Question = "caf\u00e9 \U0001F600"
		b, err := Encode(r)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	b, err := os.ReadFile(filepath.Join("testdata", "v1", name+".json")) //nolint:gosec // fixed test fixture names
	if err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSuffix(b, []byte("\n")) // editorconfig wants a final newline; the record has none
}

func TestGolden(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			raw := readFixture(t, f.name)
			r, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			again, err := Encode(r)
			if err != nil || !bytes.Equal(again, raw) {
				t.Fatalf("re-encode differs: %v\n%s\n%s", err, again, raw)
			}
			if got := DigestOf(raw); got != f.digest {
				t.Fatalf("digest %s, want %s", got, f.digest)
			}
			if d, _ := r.Digest(); d != f.digest {
				t.Fatalf("Record.Digest %s", d)
			}
		})
	}
}

func TestEncodeEscapes(t *testing.T) {
	r, _ := Decode(readFixture(t, "decision-relay"))
	r.Question = "caf\u00e9 \U0001F600 \"q\" \\ \n"
	b, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b {
		if c >= 0x7f || c < 0x20 {
			t.Fatalf("non-ASCII byte %#x in canonical output", c)
		}
	}
	want := `"question":"caf\u00e9 \ud83d\ude00 \"q\" \\ \u000a"`
	if !bytes.Contains(b, []byte(want)) {
		t.Fatalf("escapes: %s", b)
	}
	back, err := Decode(b)
	if err != nil || back.Question != r.Question {
		t.Fatalf("round trip: %v", err)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, f := range fixtures {
		r1, err := Decode(readFixture(t, f.name))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := Encode(r1)
		r2, err := Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		b2, _ := Encode(r2)
		if !bytes.Equal(b, b2) {
			t.Fatal("not stable")
		}
	}
}

func rep(t *testing.T, name, old, repl string) []byte {
	t.Helper()
	s := string(readFixture(t, name))
	if !strings.Contains(s, old) {
		t.Fatalf("%q not in fixture", old)
	}
	return []byte(strings.Replace(s, old, repl, 1))
}

func TestDecodeRejects(t *testing.T) {
	const land = "land-cli"
	tests := []struct {
		name string
		in   []byte
		want error
	}{
		{"unknown v", rep(t, land, `"v":1`, `"v":2`), ErrUnknownVersion},
		{"unknown top-level field", rep(t, land, `{"action"`, `{"zzz":"x","action"`), ErrUnknownField},
		{"unknown action", rep(t, land, `"action":"land"`, `"action":"merge"`), ErrUnknownAction},
		{"unknown channel", rep(t, land, `"channel":"cli"`, `"channel":"sms"`), ErrUnknownChannel},
		{"short commit", rep(t, land, `4f1c2ab9e0d3c5a7b18f6e2d4c0a9b8e7f6d5c4b`, `4f1c2ab`), ErrBadSubject},
		{"uppercase commit", rep(t, land, `4f1c2ab9e0d3c5a7b18f6e2d4c0a9b8e7f6d5c4b`, `4F1C2AB9E0D3C5A7B18F6E2D4C0A9B8E7F6D5C4B`), ErrBadSubject},
		{"at offset", rep(t, land, `2026-10-06T09:12:00Z`, `2026-10-06T11:12:00+02:00`), ErrBadTime},
		{"at fractional", rep(t, land, `2026-10-06T09:12:00Z`, `2026-10-06T09:12:00.5Z`), ErrBadTime},
		{"at lowercase z", rep(t, land, `2026-10-06T09:12:00Z`, `2026-10-06T09:12:00z`), ErrBadTime},
		{"ai evidence", rep(t, land, `"kind":"base"`, `"kind":"ai-summary"`), ErrAIEvidence},
		{"unknown evidence kind", rep(t, land, `"kind":"base"`, `"kind":"vibes"`), ErrBadEvidence},
		{"evidence unsorted", rep(t, land,
			`{"kind":"check","name":"check-local","result":"pass"},{"kind":"check","name":"commitlint","result":"pass"}`,
			`{"kind":"check","name":"commitlint","result":"pass"},{"kind":"check","name":"check-local","result":"pass"}`), ErrEvidenceOrder},
		{"evidence duplicate", rep(t, land,
			`{"kind":"check","name":"commitlint","result":"pass"}`,
			`{"kind":"check","name":"check-local","result":"pass"}`), ErrEvidenceOrder},
		{"keys unsorted", rep(t, land, `"by":"human","channel":"cli"`, `"channel":"cli","by":"human"`), ErrNotCanonical},
		{"whitespace", rep(t, land, `"v":1`, `"v": 1`), ErrSyntax},
		{"raw non-ASCII", rep(t, land, `feat/example`, "feat/éx"), ErrNotCanonical},
		{"uppercase escape", rep(t, "escapes", `\u00e9`, `\u00E9`), ErrNotCanonical},
		{"float", rep(t, land, `"v":1`, `"v":1.0`), ErrSyntax},
		{"exponent", rep(t, land, `"v":1`, `"v":1e0`), ErrSyntax},
		{"leading zero", rep(t, land, `"v":1`, `"v":01`), ErrSyntax},
		{"null", rep(t, land, `"by":"human"`, `"by":null`), ErrSyntax},
		{"duplicate key", rep(t, land, `{"action"`, `{"at":"x","action"`), ErrSyntax},
		{"duplicate key same", rep(t, land, `"by":"human"`, `"by":"human","by":"human"`), ErrSyntax},
		{"trailing bytes", append(readFixture(t, land), '\n'), ErrSyntax},
		{"invalid UTF-8", rep(t, land, `feat/example`, "feat/\xff"), ErrSyntax},
		{"lone surrogate", rep(t, "escapes", `\ud83d\ude00`, `\ud83d`), ErrSyntax},
		{"empty", nil, ErrSyntax},
		{"not object", []byte(`[]`), ErrSyntax},
		{"missing at", rep(t, land, `"at":"2026-10-06T09:12:00Z",`, ``), ErrMissingRequired},
		{"unknown answer key", rep(t, land, `"answer":{`, `"answer":{"x":"y",`), ErrBadAnswer},
		{"unknown subject key", rep(t, land, `"subject":{`, `"subject":{"x":"y",`), ErrBadSubject},
		{"email by", rep(t, land, `"by":"human"`, `"by":"w@example.org"`), ErrBadBy},
		{"bad assurance", rep(t, land, `"assurance":"local"`, `"assurance":"strong"`), ErrBadAssurance},
		{"typed_sha not prefix", rep(t, land, `"value":"4f1c2ab"`, `"value":"4f1c2ac"`), ErrBadAnswer},
		{"typed_sha too short", rep(t, land, `"value":"4f1c2ab"`, `"value":"4f1c2a"`), ErrBadAnswer},
		{"ext not namespaced", rep(t, "escapes", `crewbook.answered`, `answered`), ErrBadExt},
		{"wrong schema", rep(t, land, `workharbor.confirmation`, `other`), ErrBadSchema},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	base := func() Record {
		r, err := Decode(readFixture(t, "land-cli"))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	tests := []struct {
		name string
		mut  func(*Record)
		want error
	}{
		{"land without commit", func(r *Record) { r.Subject = Subject{Issue: "o/r#1"} }, ErrBadSubject},
		{"empty subject", func(r *Record) { r.Subject = Subject{} }, ErrBadSubject},
		{"yn bad value", func(r *Record) { r.Answer = Answer{Mode: ModeYN, Value: "maybe"} }, ErrBadAnswer},
		{"deny with value", func(r *Record) { r.Answer = Answer{Mode: ModeDeny, Value: "x"} }, ErrBadAnswer},
		{"unknown mode", func(r *Record) { r.Answer = Answer{Mode: "shrug"} }, ErrBadAnswer},
		{"option empty", func(r *Record) { r.Answer = Answer{Mode: ModeOption} }, ErrBadAnswer},
		{"unknown version", func(r *Record) { r.V = 2 }, ErrUnknownVersion},
		{"evidence kind only", func(r *Record) { r.Evidence = []Evidence{{Kind: EvidenceCheck}} }, ErrBadEvidence},
		{"evidence short object", func(r *Record) { r.Evidence = []Evidence{{Kind: EvidenceBase, Object: "0a1b"}} }, ErrBadEvidence},
		{"issue zero", func(r *Record) { r.Subject.Issue = "o/r#0" }, ErrBadSubject},
		{"bad issue", func(r *Record) { r.Subject.Issue = "#53" }, ErrBadSubject},
		{"empty by", func(r *Record) { r.By = "" }, ErrBadBy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base()
			tt.mut(&r)
			if err := r.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
	// Accepted forms stay accepted.
	r := base()
	r.Answer = Answer{Mode: ModeYN, Value: "yes"}
	r.Subject.Commit = strings.Repeat("a", 64)
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r = base()
	r.Answer = Answer{Mode: ModeDeny}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDigestChangesPerField(t *testing.T) {
	base, err := Decode(readFixture(t, "land-cli"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := base.Digest()
	muts := map[string]func(*Record){
		"v":                func(r *Record) { r.V = 2 },
		"schema":           func(r *Record) { r.Schema += "x" },
		"action":           func(r *Record) { r.Action = ActionPush },
		"subject.commit":   func(r *Record) { r.Subject.Commit = strings.Repeat("b", 40) },
		"subject.branch":   func(r *Record) { r.Subject.Branch = "other" },
		"subject.issue":    func(r *Record) { r.Subject.Issue = "o/r#2" },
		"subject.decision": func(r *Record) { r.Subject.Decision = "H1" },
		"subject.ref":      func(r *Record) { r.Subject.Ref = "r" },
		"question":         func(r *Record) { r.Question = "other?" },
		"answer.mode":      func(r *Record) { r.Answer.Mode = ModeYN },
		"answer.value":     func(r *Record) { r.Answer.Value = "4f1c2ac" },
		"at":               func(r *Record) { r.At = "2026-10-06T09:12:01Z" },
		"channel":          func(r *Record) { r.Channel = ChannelWeb },
		"by":               func(r *Record) { r.By = "other" },
		"assurance":        func(r *Record) { r.Assurance = AssurancePasskey },
		"evidence kind":    func(r *Record) { r.Evidence[1].Kind = EvidenceAudit },
		"evidence name":    func(r *Record) { r.Evidence[1].Name = "x" },
		"evidence result":  func(r *Record) { r.Evidence[1].Result = "fail" },
		"evidence object":  func(r *Record) { r.Evidence[0].Object = strings.Repeat("c", 40) },
		"evidence ref":     func(r *Record) { r.Evidence[1].Ref = "x" },
		"evidence value":   func(r *Record) { r.Evidence[1].Value = "x" },
		"evidence add":     func(r *Record) { r.Evidence = append(r.Evidence, Evidence{Kind: EvidenceAudit, Ref: "z"}) },
		"evidence gone":    func(r *Record) { r.Evidence = nil },
		"ext":              func(r *Record) { r.Ext = map[string]any{"a.b": "c"} },
	}
	seen := map[string]string{}
	for name, mut := range muts {
		r := base
		r.Evidence = append([]Evidence(nil), base.Evidence...)
		mut(&r)
		got, err := r.Digest()
		if err != nil {
			t.Fatal(name, err)
		}
		if got == want {
			t.Errorf("%s: digest unchanged", name)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s collides with %s", name, prev)
		}
		seen[got] = name
	}
}

func TestDigestDomainSeparation(t *testing.T) {
	raw := readFixture(t, "land-cli")
	if DigestOf(raw) == DigestOf(append([]byte("x"), raw...)) {
		t.Fatal("digest ignores input")
	}
	if DigestPrefix != "workharbor-confirm-v1\x00" {
		t.Fatal("prefix changed")
	}
}

// The strict parser must fail for the stated reason, not by accident later.
func TestDecodeSyntaxReason(t *testing.T) {
	const land = "land-cli"
	bs := string(rune(92))
	tests := []struct {
		name, msg string
		in        []byte
	}{
		{"null", "null not allowed", rep(t, land, `"by":"human"`, `"by":null`)},
		{"float", "floats not allowed", rep(t, land, `"v":1`, `"v":1.0`)},
		{"utf8", "invalid UTF-8", rep(t, land, `feat/example`, "feat/\xff")},
		{"surrogate", "lone surrogate", rep(t, "escapes", bs+"ud83d"+bs+"ude00", bs+"ud83d")},
		{"low surrogate", "lone surrogate", rep(t, "escapes", bs+"ud83d"+bs+"ude00", bs+"ude00")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(tt.in)
			if !errors.Is(err, ErrSyntax) || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("got %v, want %q", err, tt.msg)
			}
		})
	}
}
