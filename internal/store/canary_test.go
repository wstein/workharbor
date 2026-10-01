package store

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/redact"
)

// canaries are secrets of every kind. The redactor knows one of them
// exactly (a scoped run token); the others it must recognise by their format.
type canaries struct {
	exact    string // registered with the redactor
	github   string
	aws      string
	jwt      string
	keyBody  string // the body of a private key block
	bearer   string
	urlPass  string
	assigned string // the value of a secret-looking name
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func newCanaries(t *testing.T) canaries {
	t.Helper()
	return canaries{
		exact:    "run-token-" + randHex(t, 12),
		github:   "ghp_" + randHex(t, 18),
		aws:      "AKIA" + strings.ToUpper(randHex(t, 8)),
		jwt:      "eyJ" + randHex(t, 6) + "." + "eyJ" + randHex(t, 8) + "." + randHex(t, 12),
		keyBody:  "MIIE" + randHex(t, 16),
		bearer:   randHex(t, 14),
		urlPass:  "pw" + randHex(t, 8),
		assigned: "v" + randHex(t, 10),
	}
}

// secrets returns the parts that must never appear in a file.
func (c canaries) secrets() []string {
	return []string{c.exact, c.github, c.aws, c.jwt, c.keyBody, c.bearer, c.urlPass, c.assigned}
}

// text returns a sentence that carries every canary, so each write path gets
// all of them.
func (c canaries) text() string {
	return "ran curl -H 'Authorization: Bearer " + c.bearer + "' with " + c.exact + " and " + c.github +
		" as " + c.aws + " jwt " + c.jwt + " clone https://bot:" + c.urlPass + "@git.example.test/r.git" +
		" export API_KEY=" + c.assigned + " key -----BEGIN PRIVATE KEY----- " + c.keyBody + " -----END PRIVATE KEY-----"
}

// leaks returns the secrets found in any file of dir: the database, its
// write-ahead log and its shared-memory file.
func leaks(t *testing.T, dir string, secrets []string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no files to scan in %s: %v", dir, err)
	}
	var found []string
	for _, f := range files {
		data, err := os.ReadFile(f) //nolint:gosec // the files of a temporary directory this test created
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range secrets {
			if strings.Contains(string(data), s) {
				found = append(found, s)
			}
		}
	}
	return found
}

// writeEverywhere pushes the canaries through every path that persists text.
func writeEverywhere(t *testing.T, s *Store, c canaries) {
	t.Helper()
	text := c.text()

	// Task text, candidate text and the events their changes record.
	a := domain.NewTaskAggregate(domain.Task{ID: "t1", Repo: "wstein/workharbor " + c.github, Issue: "#15 " + text, State: domain.TaskRunning, CreatedAt: t0})
	a.AddEnvironment(&domain.Environment{ID: "e1", Backend: "apple", State: domain.EnvRunning})
	if err := a.StartRun(&domain.Run{ID: "r1", WorkspaceID: "w1", EnvID: "e1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PinRevision("r1", "agent/"+c.github, "aaa111"); err != nil {
		t.Fatal(err)
	}
	a.Candidates[0].PRURL = "https://github.com/x/y/pull/1?access_token=" + c.assigned
	if _, err := s.SaveTask(bg, a); err != nil {
		t.Fatal(err)
	}

	// A Decision: subject, input, options, reason, answer, actor, and the events.
	d, err := domain.Raise(domain.NewDecision{
		ID: "d1", TaskID: "t1", RunID: "r1", Kind: domain.DecisionApproval, Blocking: true,
		Subject: "Bash " + c.exact, Input: text, Options: []string{domain.AnswerAllow, domain.AnswerDeny}, Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Respond(domain.Response{By: "werner " + c.github, Option: domain.AnswerAllow, Reason: text, At: t0.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDecision(bg, d); err != nil {
		t.Fatal(err)
	}

	// Raw events: an audit entry and transcript content.
	if _, err := s.Append(bg,
		domain.Event{TaskID: "t1", Kind: "tool.call", Tier: domain.TierAudit, Payload: []byte(`{"cmd":"` + text + `"}`)},
		domain.Event{TaskID: "t1", Kind: "message", Tier: domain.TierTranscript, Payload: []byte(text)},
	); err != nil {
		t.Fatal(err)
	}

	// A stored idempotency response and a purge by an actor with a secret in its name.
	if _, _, err := s.Do(bg, "key-1", RequestHash("cmd"), func(*Tx) ([]byte, error) { return []byte(`{"echo":"` + text + `"}`), nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Purge(bg, PurgeSpec{TaskID: "t1", Actor: "retention " + c.exact, All: true}); err != nil {
		t.Fatal(err)
	}
}

func closeAndCheckpoint(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.ExecContext(bg, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// The canary test: no secret reaches any persisted artifact.
func TestNoSecretReachesTheDatabase(t *testing.T) {
	c := newCanaries(t)
	dir := t.TempDir()
	r := redact.New()
	if !r.Add(c.exact) {
		t.Fatal("the exact canary must be registrable")
	}
	s, err := Open(bg, filepath.Join(dir, "workharbor.db"), WithRedactor(r))
	if err != nil {
		t.Fatal(err)
	}
	writeEverywhere(t, s, c)
	closeAndCheckpoint(t, s)

	if found := leaks(t, dir, c.secrets()); len(found) != 0 {
		t.Fatalf("secrets reached the database files: %v", found)
	}
}

// The control: the same writes through a redactor that knows nothing leak
// every canary, which shows the scan can fail.
func TestTheCanaryScanFindsWhatARedactorMisses(t *testing.T) {
	c := newCanaries(t)
	dir := t.TempDir()
	s, err := Open(bg, filepath.Join(dir, "workharbor.db"), WithRedactor(redact.New(redact.WithoutDefaults())))
	if err != nil {
		t.Fatal(err)
	}
	writeEverywhere(t, s, c)
	closeAndCheckpoint(t, s)

	found := leaks(t, dir, c.secrets())
	if len(found) != len(c.secrets()) {
		t.Fatalf("the control leaked %d of %d canaries: the scan or the writes are not hostile enough", len(found), len(c.secrets()))
	}
}

// The default store redacts the formats it knows without any registration.
func TestTheDefaultStoreRedactsKnownFormats(t *testing.T) {
	c := newCanaries(t)
	dir := t.TempDir()
	s, err := Open(bg, filepath.Join(dir, "workharbor.db"))
	if err != nil {
		t.Fatal(err)
	}
	writeEverywhere(t, s, c)
	closeAndCheckpoint(t, s)

	// The exact run token is not registered here, so only it may be found.
	for _, leaked := range leaks(t, dir, c.secrets()) {
		if leaked != c.exact {
			t.Errorf("a well-known format reached the database without registration: %s", leaked)
		}
	}
}

func TestRedactionKeepsTheContextAndTheStructure(t *testing.T) {
	c := newCanaries(t)
	r := redact.New()
	r.Add(c.exact)
	s := openTemp(t, WithRedactor(r))
	if _, err := s.Append(bg, domain.Event{TaskID: "t1", Kind: "tool.call", Tier: domain.TierAudit, Payload: []byte(`{"cmd":"git push","token":"` + c.exact + `","input_tokens":123456}`)}); err != nil {
		t.Fatal(err)
	}
	events, _ := s.EventsSince(bg, "t1", 0, 0)
	got := string(events[0].Payload)
	if strings.Contains(got, c.exact) || !strings.Contains(got, "git push") || !strings.Contains(got, `"input_tokens":123456`) || !strings.Contains(got, redact.Mask) {
		t.Errorf("redacted payload = %s", got)
	}
}

func TestDoReturnsTheSameBytesItStores(t *testing.T) {
	c := newCanaries(t)
	r := redact.New()
	r.Add(c.exact)
	s := openTemp(t, WithRedactor(r))
	first, _, err := s.Do(bg, "k", RequestHash("x"), func(*Tx) ([]byte, error) { return []byte("token " + c.exact), nil })
	if err != nil {
		t.Fatal(err)
	}
	replay, replayed, err := s.Do(bg, "k", RequestHash("x"), func(*Tx) ([]byte, error) { return nil, nil })
	if err != nil || !replayed || string(replay) != string(first) || strings.Contains(string(first), c.exact) {
		t.Errorf("first %q, replay %q (%v, %v): both must be the redacted response", first, replay, replayed, err)
	}
}
