package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/passkey"
	"github.com/wstein/workharbor/internal/passkey/passkeytest"
	"github.com/wstein/workharbor/internal/store"
)

const pkOrigin = "https://whr.example.test"

// pkRig is the UI with the real passkey ceremonies and a virtual authenticator.
type pkRig struct {
	*rig
	svc  *passkey.Service
	auth *passkeytest.Authn
}

func newPKRig(t *testing.T) *pkRig { return newPKRigWith(t, nil) }

// newPKRigWith is newPKRig with the passkey service wrapped, to make it fail.
func newPKRigWith(t *testing.T, wrap func(*passkey.Service) Passkeys, more ...func(*Options)) *pkRig {
	t.Helper()
	st, err := store.Open(bg, filepath.Join(t.TempDir(), "workharbor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &rig{t: t, be: &fake{}, now: t0}
	r.auth, err = NewTokenAuth([]byte(token), func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now })
	if err != nil {
		t.Fatal(err)
	}
	svc, err := passkey.New(passkey.Config{
		RPID: "whr.example.test", Origin: pkOrigin,
		OnFirstEnrolled: func() { r.auth.EndSessions(func(id string) bool { return id == "" }) },
		OnRevoked:       func(id string) { r.auth.EndSessions(func(p string) bool { return p == id }) },
	}, st)
	if err != nil {
		t.Fatal(err)
	}
	var keys Passkeys = svc
	if wrap != nil {
		keys = wrap(svc)
	}
	opt := Options{Auth: r.auth, Store: st, Passkeys: keys, Heartbeat: 20 * time.Millisecond, Now: func() time.Time { return t0 }, OnError: func(err error) { t.Errorf("internal error: %v", err) }}
	for _, m := range more {
		m(&opt)
	}
	ui, err := New(r.be, opt)
	if err != nil {
		t.Fatal(err)
	}
	r.srv = httptest.NewServer(ui.Handler())
	t.Cleanup(r.srv.Close)
	return &pkRig{rig: r, svc: svc, auth: passkeytest.New(t, passkey.OwnerID())}
}

// postJSON sends a JSON body with headers, as the page's script does.
func (b *browser) postJSON(path string, body []byte, headers map[string]string) (*reply, []byte) {
	b.r.t.Helper()
	req, err := http.NewRequestWithContext(bg, "POST", b.r.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		b.r.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.r.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return &reply{StatusCode: resp.StatusCode, Header: resp.Header, cookies: resp.Cookies()}, raw
}

func bodyOf(r *http.Request) []byte {
	b, _ := io.ReadAll(r.Body)
	return b
}

type ceremonyReply struct {
	Options  json.RawMessage `json:"options"`
	Ceremony string          `json:"ceremony"`
}

// enrol runs an enrolment through the HTTP endpoints, with no session.
func (p *pkRig) enrol(b *browser) { p.enrolAs(b, p.auth) }

func (p *pkRig) enrolAs(b *browser, authn *passkeytest.Authn) {
	p.t.Helper()
	tok, _, err := p.svc.NewEnrolment("phone")
	if err != nil {
		p.t.Fatal(err)
	}
	resp, raw := b.postJSON("/passkey/enrol/begin", []byte(`{"token":"`+tok+`"}`), nil)
	if resp.StatusCode != 200 {
		p.t.Fatalf("enrol begin: %d %s", resp.StatusCode, raw)
	}
	var cr ceremonyReply
	if err := json.Unmarshal(raw, &cr); err != nil {
		p.t.Fatal(err)
	}
	var creation protocol.CredentialCreation
	if err := json.Unmarshal(cr.Options, &creation); err != nil {
		p.t.Fatal(err)
	}
	resp, raw = b.postJSON("/passkey/enrol/finish", bodyOf(authn.Register(&creation, pkOrigin)), map[string]string{"X-Ceremony": cr.Ceremony})
	if resp.StatusCode != 200 {
		p.t.Fatalf("enrol finish: %d %s", resp.StatusCode, raw)
	}
}

// signIn runs a passkey sign-in and returns the browser with a session.
func (p *pkRig) signIn() *browser { return p.signInAs(p.auth) }

func (p *pkRig) signInAs(authn *passkeytest.Authn) *browser {
	p.t.Helper()
	jar, _ := cookiejar.New(nil)
	b := &browser{r: p.rig, hd: http.Header{}, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	resp, raw := b.postJSON("/passkey/login/begin", []byte(`{}`), nil)
	if resp.StatusCode != 200 {
		p.t.Fatalf("login begin: %d %s", resp.StatusCode, raw)
	}
	var cr ceremonyReply
	_ = json.Unmarshal(raw, &cr)
	var assertion protocol.CredentialAssertion
	if err := json.Unmarshal(cr.Options, &assertion); err != nil {
		p.t.Fatal(err)
	}
	resp, raw = b.postJSON("/passkey/login/finish", bodyOf(authn.Assert(&assertion, pkOrigin)), map[string]string{"X-Ceremony": cr.Ceremony})
	if resp.StatusCode != 200 || len(resp.Cookies()) == 0 {
		p.t.Fatalf("login finish: %d %s", resp.StatusCode, raw)
	}
	return b
}

func TestEnrolmentIsOnlyFromAHostLinkAndNeedsNoSession(t *testing.T) {
	p := newPKRig(t)
	b := p.browser()
	// the page of a good link has the button; a bad one says so and has no token
	tok, _, _ := p.svc.NewEnrolment("x")
	if resp, body := b.do("GET", "/enrol?token="+tok, nil); resp.StatusCode != 200 || !strings.Contains(body, `data-passkey="enrol"`) || !strings.Contains(body, `data-token="`+tok+`"`) {
		t.Errorf("a good link: %d\n%s", resp.StatusCode, body)
	}
	if resp, body := b.do("GET", "/enrol?token=forged", nil); resp.StatusCode != 200 || strings.Contains(body, "data-token") || !strings.Contains(body, "whr passkey add") {
		t.Errorf("a bad link: %d\n%s", resp.StatusCode, body)
	}
	// a web session cannot start an enrolment: there is no route for it
	b.signIn()
	for _, path := range []string{"/passkey/enrol", "/passkeys", "/enrol/new"} {
		if resp, _ := b.do("POST", path, nil); resp.StatusCode == 200 {
			t.Errorf("POST %s answered 200", path)
		}
	}
	// a forged token and a replayed one are refused, whatever the session
	resp, _ := b.postJSON("/passkey/enrol/begin", []byte(`{"token":"forged"}`), nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a forged token: %d", resp.StatusCode)
	}
	p.enrol(b)
	if resp, _ := b.postJSON("/passkey/enrol/begin", []byte(`{"token":"`+tok+`"}`), nil); resp.StatusCode == 200 {
		// tok was never begun: it is still good once; use it, then replay it
		resp, _ = b.postJSON("/passkey/enrol/begin", []byte(`{"token":"`+tok+`"}`), nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("a replayed token: %d", resp.StatusCode)
		}
	}
	// the request must come from this site
	hdr := map[string]string{"Origin": "https://evil.example"}
	if resp, _ := b.postJSON("/passkey/enrol/begin", []byte(`{"token":"x"}`), hdr); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site enrolment: %d", resp.StatusCode)
	}
}

func TestOnceAPasskeyIsEnrolledTheTokenNoLongerSignsInAndThePasskeyDoes(t *testing.T) {
	p := newPKRig(t)
	b := p.browser()
	// before: the token signs in, and the page says it stops when a passkey exists
	if _, body := b.do("GET", "/login", nil); !strings.Contains(body, `name="token"`) || !strings.Contains(body, "whr passkey add") {
		t.Error("before a passkey, the token form is the way in")
	}
	p.enrol(b)

	_, page := b.do("GET", "/login", nil)
	if strings.Contains(page, `name="token"`) || !strings.Contains(page, `data-passkey="login"`) {
		t.Errorf("after a passkey the page offers the token or no passkey:\n%s", page)
	}
	resp, body := b.do("POST", "/login", map[string][]string{"token": {token}})
	if resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 || !strings.Contains(body, "no longer signs in") {
		t.Errorf("the token after enrolment: %d, cookies %v", resp.StatusCode, resp.Cookies())
	}

	// the passkey signs in; a wrong assertion does not
	s := p.signIn()
	if resp, _ := s.do("GET", "/", nil); resp.StatusCode != 200 {
		t.Errorf("signed in with a passkey: %d", resp.StatusCode)
	}
	_, raw := b.postJSON("/passkey/login/begin", []byte(`{}`), nil)
	var cr ceremonyReply
	_ = json.Unmarshal(raw, &cr)
	var assertion protocol.CredentialAssertion
	_ = json.Unmarshal(cr.Options, &assertion)
	p.auth.UV = false // the authenticator did not verify the user
	resp, _ = b.postJSON("/passkey/login/finish", bodyOf(p.auth.Assert(&assertion, pkOrigin)), map[string]string{"X-Ceremony": cr.Ceremony})
	if resp.StatusCode != http.StatusUnauthorized || len(resp.Cookies()) != 0 {
		t.Errorf("a sign-in without user verification: %d, cookies %v", resp.StatusCode, resp.Cookies())
	}
	// a guess at a ceremony ID gets nothing
	if resp, _ := b.postJSON("/passkey/login/finish", []byte(`{}`), map[string]string{"X-Ceremony": "guess"}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an unknown ceremony: %d", resp.StatusCode)
	}
}

func reviewDecision() domain.Decision {
	return domain.Decision{ID: "d-review", TaskID: "t1", Kind: domain.DecisionReview, Blocking: true, SHA: "aaa111", Subject: "Ready to push? 2 commits", Options: []string{"allow", "deny"}}
}

func csrfOf(t *testing.T, b *browser) string {
	t.Helper()
	csrf, _ := b.form("/inbox")
	return csrf
}

// stepUp begins a step-up for a decision as the page's script would.
func (p *pkRig) stepUpBegin(b *browser, csrf, decision string) (*reply, ceremonyReply) {
	p.t.Helper()
	return p.beginAt(b, csrf, "/decisions/"+decision+"/stepup")
}

// beginAt begins a step-up at any of the step-up addresses.
func (p *pkRig) beginAt(b *browser, csrf, base string) (*reply, ceremonyReply) {
	p.t.Helper()
	resp, raw := b.postJSON(base+"/begin", []byte(`{}`), map[string]string{"X-CSRF-Token": csrf})
	var cr ceremonyReply
	_ = json.Unmarshal(raw, &cr)
	return resp, cr
}

func (p *pkRig) answerWith(b *browser, csrf, decision, option string, cr ceremonyReply, auth *passkeytest.Authn) *reply {
	p.t.Helper()
	var assertion protocol.CredentialAssertion
	if err := json.Unmarshal(cr.Options, &assertion); err != nil {
		p.t.Fatal(err)
	}
	resp, _ := b.postJSON("/decisions/"+decision+"/stepup/finish?option="+option+"&reason=ok", bodyOf(auth.Assert(&assertion, pkOrigin)), map[string]string{"X-CSRF-Token": csrf, "X-Ceremony": cr.Ceremony})
	return resp
}

// A review is answered with a fresh passkey assertion for exactly that Decision and
// commit, and never by the session alone.
func TestAReviewIsAnsweredOnlyWithAPasskeyForThatDecisionAndCommit(t *testing.T) {
	p := newPKRig(t)
	p.be.inbox = []domain.Decision{reviewDecision(), {ID: "d-other", TaskID: "t1", Kind: domain.DecisionReview, Blocking: true, SHA: "bbb222", Options: []string{"allow", "deny"}}, {ID: "d-tool", TaskID: "t1", Kind: domain.DecisionApproval, Options: []string{"allow", "deny"}}}
	p.enrol(p.browser())
	b := p.signIn()
	csrf := csrfOf(t, b)

	// the inbox offers the passkey buttons, no plain form for the review
	_, inbox := b.do("GET", "/inbox", nil)
	if !strings.Contains(inbox, `data-stepup="allow" data-decision="d-review"`) || strings.Contains(inbox, "/decisions/d-review/answer") {
		t.Errorf("the review is not answered with a passkey:\n%s", inbox)
	}
	// the form POST still refuses it, whatever the session
	if resp, _ := b.do("POST", "/decisions/d-review/answer", map[string][]string{"csrf": {csrf}, "key": {"k1"}, "option": {"allow"}}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a session alone answered a review: %d", resp.StatusCode)
	}
	// the script's calls need the CSRF header and a decision that needs a passkey
	if resp, _ := b.postJSON("/decisions/d-review/stepup/begin", []byte(`{}`), nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("no CSRF header: %d", resp.StatusCode)
	}
	if resp, _ := p.stepUpBegin(b, csrf, "d-tool"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a tool approval needs no passkey: %d", resp.StatusCode)
	}
	if resp, _ := p.stepUpBegin(b, csrf, "nope"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown decision: %d", resp.StatusCode)
	}
	anon := p.browser()
	if resp, _ := anon.postJSON("/decisions/d-review/stepup/begin", []byte(`{}`), nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no session: %d", resp.StatusCode)
	}

	// an assertion made for ANOTHER decision's challenge does not answer this one
	_, crA := p.stepUpBegin(b, csrf, "d-review")
	_, crB := p.stepUpBegin(b, csrf, "d-other")
	mixed := ceremonyReply{Options: crB.Options, Ceremony: crA.Ceremony}
	if resp := p.answerWith(b, csrf, "d-review", "allow", mixed, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("an assertion for another decision: %d", resp.StatusCode)
	}
	// a ceremony of one decision cannot answer another
	_, crC := p.stepUpBegin(b, csrf, "d-review")
	if resp := p.answerWith(b, csrf, "d-other", "allow", crC, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("the ceremony of d-review answered d-other: %d", resp.StatusCode)
	}
	// another web session cannot finish this session's ceremony
	_, crD := p.stepUpBegin(b, csrf, "d-review")
	other := p.signIn()
	otherCSRF := csrfOf(t, other)
	if resp := p.answerWith(other, otherCSRF, "d-review", "allow", crD, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("another session finished it: %d", resp.StatusCode)
	}
	if _, _, a := p.be.calls(); a != 0 {
		t.Fatalf("a refused step-up reached the service (%d answers)", a)
	}

	// the commit changed between the begin and the finish: the old approval is void
	_, crE := p.stepUpBegin(b, csrf, "d-review")
	moved := reviewDecision()
	moved.SHA = "ccc333"
	p.be.inbox[0] = moved
	if resp := p.answerWith(b, csrf, "d-review", "allow", crE, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("an approval for an earlier commit: %d", resp.StatusCode)
	}
	p.be.inbox[0] = reviewDecision()

	// the right assertion answers, once, with the SHA
	_, crF := p.stepUpBegin(b, csrf, "d-review")
	if resp := p.answerWith(b, csrf, "d-review", "allow", crF, p.auth); resp.StatusCode != 200 {
		t.Fatalf("the right assertion: %d", resp.StatusCode)
	}
	if len(p.be.answers) != 1 || p.be.answerIDs[0] != "d-review" || p.be.answers[0].Option != "allow" || p.be.answers[0].SHA != "aaa111" || p.be.answers[0].By != "web+passkey" || p.be.answers[0].Reason != "ok" {
		t.Errorf("the service got %v %+v", p.be.answerIDs, p.be.answers)
	}
	// a replay of the same ceremony is refused and answers nothing more
	if resp := p.answerWith(b, csrf, "d-review", "allow", crF, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a replay: %d", resp.StatusCode)
	}
	if _, _, a := p.be.calls(); a != 1 {
		t.Errorf("%d answers", a)
	}
	// an option the decision does not offer
	_, crG := p.stepUpBegin(b, csrf, "d-review")
	if resp := p.answerWith(b, csrf, "d-review", "merge", crG, p.auth); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown option: %d", resp.StatusCode)
	}
}

// A passkey that does not verify the user, or whose counter went backwards, answers nothing.
func TestAStepUpNeedsUserVerificationAndAHonestCounter(t *testing.T) {
	p := newPKRig(t)
	p.be.inbox = []domain.Decision{reviewDecision()}
	p.enrol(p.browser())
	b := p.signIn()
	csrf := csrfOf(t, b)

	_, cr := p.stepUpBegin(b, csrf, "d-review")
	p.auth.UV = false
	if resp := p.answerWith(b, csrf, "d-review", "allow", cr, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("no user verification: %d", resp.StatusCode)
	}
	p.auth.UV = true
	p.auth.Counter = 0 // a clone that was used less
	_, cr = p.stepUpBegin(b, csrf, "d-review")
	if resp := p.answerWith(b, csrf, "d-review", "allow", cr, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a counter that went backwards: %d", resp.StatusCode)
	}
	if _, _, a := p.be.calls(); a != 0 {
		t.Errorf("%d answers", a)
	}
}

// Without an enrolled passkey the review keeps pointing at the host.
func TestWithoutAPasskeyAReviewHasNoWebAnswer(t *testing.T) {
	p := newPKRig(t)
	p.be.inbox = []domain.Decision{reviewDecision()}
	b := p.browser()
	b.signIn()
	_, inbox := b.do("GET", "/inbox", nil)
	if strings.Contains(inbox, "data-stepup") || !strings.Contains(inbox, "whr passkey add") {
		t.Errorf("no passkey is enrolled, so the page must say how to enrol one:\n%s", inbox)
	}
	csrf, _ := b.form("/inbox")
	if resp, _ := b.postJSON("/decisions/d-review/stepup/begin", []byte(`{}`), map[string]string{"X-CSRF-Token": csrf}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a step-up with nothing enrolled: %d", resp.StatusCode)
	}
}

// The limits of the sign-in answer 429, not a refusal that looks like a wrong passkey.
func TestTooManySignInsAnswer429(t *testing.T) {
	p := newPKRig(t)
	b := p.browser()
	p.enrol(b)
	var last int
	for range 700 {
		resp, _ := b.postJSON("/passkey/login/begin", []byte(`{}`), nil)
		last = resp.StatusCode
		if last != 200 {
			break
		}
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("after many sign-ins began: %d, want 429", last)
	}
}

// The first passkey ends every session the API token started: from then on the web
// UI is by passkey, and a stolen token's session does not outlive the switch.
func TestEnrollingTheFirstPasskeyEndsTheTokenSessions(t *testing.T) {
	p := newPKRig(t)
	tokenSession := p.browser()
	tokenSession.signIn() // before any passkey, the token signs in
	if resp, _ := tokenSession.do("GET", "/inbox", nil); resp.StatusCode != 200 {
		t.Fatalf("a token session before the passkey: %d", resp.StatusCode)
	}
	p.enrol(p.browser())
	if resp, _ := tokenSession.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("the token session survived the first passkey: %d", resp.StatusCode)
	}
	// a second passkey is not "the first": passkey sessions are untouched
	signed := p.signIn()
	other := passkeytest.New(t, passkey.OwnerID())
	p.enrolAs(p.browser(), other)
	if resp, _ := signed.do("GET", "/inbox", nil); resp.StatusCode != 200 {
		t.Errorf("a second passkey ended a passkey session: %d", resp.StatusCode)
	}
}

// Each session is tied to the passkey that signed it in, and revoking the passkey
// (whr passkey rm) ends those sessions and their live streams, and no others.
func TestRevokingAPasskeyEndsItsSessions(t *testing.T) {
	// The heartbeat is longer than ended() waits, so only EndSessions itself can
	// close the stream in time: the heartbeat's own session check would hide a
	// sweep that skipped the watchers.
	p := newPKRigWith(t, nil, func(o *Options) { o.Heartbeat = time.Minute })
	p.be.events = make(chan domain.Event)
	phone, laptop := p.auth, passkeytest.New(t, passkey.OwnerID())
	p.enrolAs(p.browser(), phone)
	p.enrolAs(p.browser(), laptop)
	onPhone, onLaptop := p.signInAs(phone), p.signInAs(laptop)
	stream := onPhone.openStream()
	defer func() { _ = stream.Body.Close() }()

	list, err := p.svc.List(bg)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	// the phone's passkey is the first one
	if err := p.svc.Revoke(bg, list[0].ID[:12]); err != nil {
		t.Fatal(err)
	}
	if resp, _ := onPhone.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("a session of the revoked passkey: %d", resp.StatusCode)
	}
	if !ended(stream) {
		t.Error("the stream of a revoked passkey's session stayed open")
	}
	if resp, _ := onLaptop.do("GET", "/inbox", nil); resp.StatusCode != 200 {
		t.Errorf("the other passkey's session ended: %d", resp.StatusCode)
	}
}

// failingEnrolled is a passkey service whose store cannot say whether a passkey is
// enrolled.
type failingEnrolled struct{ *passkey.Service }

func (failingEnrolled) Enrolled(context.Context) (bool, error) {
	return false, errors.New("database is locked")
}

// If the store cannot say whether a passkey is enrolled, the token does not sign in.
func TestAnEnrolledErrorFailsClosed(t *testing.T) {
	p := newPKRigWith(t, func(s *passkey.Service) Passkeys { return failingEnrolled{s} })
	b := p.browser()
	resp, body := b.do("POST", "/login", url.Values{"token": {token}})
	if resp.StatusCode == http.StatusSeeOther || len(resp.Cookies()) != 0 {
		t.Fatalf("the token signed in although enrolment could not be checked: %d %s", resp.StatusCode, body)
	}
	if resp, _ := b.do("GET", "/inbox", nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("a session without a sign-in: %d", resp.StatusCode)
	}
}

// A sign-in that checked before a sweep ended the sessions must not start one
// after it: EndSessions bumps a generation that a stamped request is compared to.
func TestASignInThatCheckedBeforeASweepStartsNoSession(t *testing.T) {
	auth, err := NewTokenAuth([]byte(token), nil)
	if err != nil {
		t.Fatal(err)
	}
	stamped := func() *http.Request {
		r := httptest.NewRequestWithContext(bg, "POST", "/login", nil)
		return r.WithContext(context.WithValue(r.Context(), generationKey{}, auth.Generation()))
	}
	// no sweep in between: it starts
	w := httptest.NewRecorder()
	if !auth.StartFor(w, stamped(), "") || len(w.Result().Cookies()) != 1 {
		t.Fatal("a sign-in with no sweep in between did not start")
	}
	// the sweep ran after the stamp: it does not, and sets no cookie
	r := stamped()
	auth.EndSessions(func(string) bool { return true })
	w = httptest.NewRecorder()
	if auth.StartFor(w, r, "k1") || len(w.Result().Cookies()) != 0 {
		t.Error("a sign-in that checked before the sweep started a session after it")
	}
	// a request that was never stamped is not refused
	if !auth.StartFor(httptest.NewRecorder(), httptest.NewRequestWithContext(bg, "POST", "/login", nil), "") {
		t.Error("an unstamped request was refused")
	}
}
