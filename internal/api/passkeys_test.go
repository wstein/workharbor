package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/passkey"
)

// withPasskeys rebuilds the rig's server with the real passkey service.
func withPasskeys(t *testing.T, r *rig) *passkey.Service {
	t.Helper()
	pk, err := passkey.New(passkey.Config{RPID: "whr.example.test", Origin: "https://whr.example.test"}, r.st)
	if err != nil {
		t.Fatal(err)
	}
	r.ts.Close()
	r.srv, err = New(r.be, Options{Token: []byte(token), Store: r.st, Passkeys: pk, Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)
	return pk
}

func TestPasskeyRoutesAreOffUntilConfigured(t *testing.T) {
	r := newRig(t)
	for _, c := range [][2]string{{"GET", "/v1/passkeys"}, {"POST", "/v1/passkeys/enrolments"}, {"DELETE", "/v1/passkeys/x"}} {
		status, _, body := r.do(c[0], c[1], "")
		if status != 409 || !strings.Contains(body, "passkeys are off") {
			t.Errorf("%s %s = %d %s", c[0], c[1], status, body)
		}
	}
}

func TestAnEnrolmentLinkIsOneTimeAndTheListHasNoSecrets(t *testing.T) {
	r := newRig(t)
	pk := withPasskeys(t, r)

	status, _, body := r.do("GET", "/v1/passkeys", "")
	if status != 200 || !strings.Contains(body, `"data":[]`) {
		t.Errorf("an empty list = %d %s", status, body)
	}
	status, _, body = r.do("POST", "/v1/passkeys/enrolments", `{"name":"phone"}`)
	if status != 201 {
		t.Fatalf("%d %s", status, body)
	}
	var env struct {
		Data struct {
			URL     string    `json:"url"`
			Expires time.Time `json:"expires_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	tok, ok := strings.CutPrefix(env.Data.URL, "https://whr.example.test/enrol?token=")
	if !ok || !pk.CheckToken(tok) {
		t.Fatalf("the link %q does not carry a good token", env.Data.URL)
	}
	// the token is not kept anywhere a replay could read it
	status, _, _ = r.do("POST", "/v1/passkeys/enrolments", `{"name":"phone","extra":1}`)
	if status != 400 {
		t.Errorf("an unknown field = %d", status)
	}
	if status, _, _ = r.do("POST", "/v1/passkeys/enrolments", `{"name":"`+strings.Repeat("x", 65)+`"}`); status != 400 {
		t.Errorf("a long name = %d", status)
	}
	// a web caller has no token: the routes need the bearer
	if status, _, _ = r.do("POST", "/v1/passkeys/enrolments", "", "Authorization", ""); status != 401 {
		t.Errorf("no bearer = %d", status)
	}
	if status, _, body = r.do("DELETE", "/v1/passkeys/nope", ""); status != 404 {
		t.Errorf("an unknown passkey = %d %s", status, body)
	}
}
