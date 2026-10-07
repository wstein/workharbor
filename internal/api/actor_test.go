package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAuditActorIsDerivedFromCredentialAndNeverContainsIt(t *testing.T) {
	r := newRig(t)
	f := &fakePreviews{}
	withPreviews(t, r, f)

	if status, _, body := r.do("POST", "/v1/tasks/t1/previews", `{"port":3000}`); status != 201 {
		t.Fatalf("open = %d %s", status, body)
	}
	got := f.actors[0]
	if got == "api" || !strings.HasPrefix(got, "api:") || len(got) <= len("api:") {
		t.Fatalf("actor = %q, want api:<client id>", got)
	}
	if strings.Contains(got, token) || got != r.srv.Actor() {
		t.Errorf("actor %q leaks the token or differs from the server's", got)
	}

	other, err := New(r.be, Options{Token: []byte(token + "-other"), Store: r.st})
	if err != nil {
		t.Fatal(err)
	}
	same, err := New(r.be, Options{Token: []byte(token), Store: r.st})
	if err != nil {
		t.Fatal(err)
	}
	if other.Actor() == r.srv.Actor() {
		t.Error("different credentials share an actor")
	}
	if same.Actor() != r.srv.Actor() {
		t.Error("the same credential gives a different actor")
	}
	for _, a := range []string{r.srv.Actor(), other.Actor()} {
		if strings.Contains(a, token) || strings.Contains(a, "other") {
			t.Errorf("actor %q carries token material", a)
		}
	}
}

// openAs sends a preview request with the given bearer token.
func (r *rig) openAs(tok string) int {
	r.t.Helper()
	req, err := http.NewRequest("POST", r.ts.URL+"/v1/tasks/t1/previews", strings.NewReader(`{"port":3000}`)) //nolint:noctx // a test
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestEachClientHasItsOwnAuditActorAndNothingSecretLeaks(t *testing.T) {
	r := newRig(t)
	f := &fakePreviews{}
	r.ts.Close()
	tokA, tokB := "throwaway-client-a-4711", "throwaway-client-b-4712"
	var logged []string
	var err error
	r.srv, err = New(r.be, Options{
		Token: []byte(token), Store: r.st, Previews: f, Now: func() time.Time { return t0 },
		Clients: []Client{{Name: "ci-nightly", Token: []byte(tokA)}, {Name: "alice", Token: []byte(tokB)}},
		OnError: func(e error) { logged = append(logged, e.Error()) },
	})
	if err != nil {
		t.Fatal(err)
	}
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)

	for _, tok := range []string{tokA, tokB, token} {
		if st := r.openAs(tok); st != 201 {
			t.Fatalf("open = %d", st)
		}
	}
	want := []string{"api:ci-nightly", "api:alice", r.srv.Actor()}
	if !slices.Equal(f.actors, want) {
		t.Fatalf("actors = %v, want %v", f.actors, want)
	}
	if !strings.HasPrefix(want[2], "api:") || len(want[2]) != len("api:")+12 {
		t.Errorf("the default actor %q is not the derived id", want[2])
	}
	if st := r.openAs("throwaway-wrong"); st != 401 {
		t.Errorf("a wrong token = %d, want 401", st)
	}
	// No token or digest in an actor, an error body or a log line.
	out := strings.Join(append(append([]string{}, f.actors...), logged...), "\n")
	_, _, body := r.do("GET", "/v1/tasks/nope", "", "Authorization", "Bearer "+tokA+"x")
	out += body
	for _, tk := range []string{token, tokA, tokB} {
		sum := sha256.Sum256([]byte(tk))
		for _, secret := range []string{tk, hex.EncodeToString(sum[:]), hex.EncodeToString(sum[:6])} {
			if strings.Contains(out, secret) {
				t.Errorf("output leaks %d bytes of secret material", len(secret))
			}
		}
	}
}

func TestClientNamesAndTokensAreCheckedByNew(t *testing.T) {
	r := newRig(t)
	for _, cl := range [][]Client{
		{{Name: "default", Token: []byte("x")}},
		{{Name: "Bad", Token: []byte("x")}},
		{{Name: "a", Token: nil}},
		{{Name: "a", Token: []byte("x")}, {Name: "a", Token: []byte("y")}},
	} {
		if _, err := New(r.be, Options{Token: []byte(token), Store: r.st, Clients: cl}); err == nil {
			t.Errorf("clients %v were accepted", cl)
		}
	}
}
