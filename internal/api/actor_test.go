package api

import (
	"strings"
	"testing"
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
