package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/wstein/workharbor/internal/passkey/passkeytest"
	"github.com/wstein/workharbor/internal/store"
)

// fakeChanges is what whr serve gives the UI, with the calls counted.
type fakeChanges struct {
	mu       sync.Mutex
	open     []Change
	revoke   bool
	confirms []string
	revokes  int
	failWith error
}

func (f *fakeChanges) Open(context.Context) ([]Change, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Change(nil), f.open...), nil
}

func (f *fakeChanges) Confirm(_ context.Context, id, by string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	f.confirms = append(f.confirms, id+" by "+by)
	return nil
}

func (f *fakeChanges) CanRevokeTokens() bool { return f.revoke }

func (f *fakeChanges) RevokeTokens(context.Context, string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokes++
	return 2, nil
}

func (f *fakeChanges) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.confirms), f.revokes
}

func changeRig(t *testing.T, f *fakeChanges) *pkRig {
	t.Helper()
	return newPKRigWith(t, nil, func(o *Options) { o.Changes = f })
}

func twoChanges() []Change {
	return []Change{
		{ID: "c1", Repo: "o/a", From: "integration on develop", To: "prototype on scratch", Text: `workflow o/a integration on "develop" to prototype on "scratch"`},
		{ID: "c2", Repo: "o/b", From: "integration on develop", To: "published", Text: `workflow o/b integration on "develop" to published on ""`},
	}
}

// finishAt finishes a step-up at any address with an assertion made on the options
// of another begin, as a hostile page could.
func (p *pkRig) finishAt(b *browser, csrf, base string, cr, opts ceremonyReply, auth *passkeytest.Authn) *reply {
	p.t.Helper()
	var assertion protocol.CredentialAssertion
	if err := json.Unmarshal(opts.Options, &assertion); err != nil {
		p.t.Fatal(err)
	}
	resp, _ := b.postJSON(base+"/finish", bodyOf(auth.Assert(&assertion, pkOrigin)), map[string]string{"X-CSRF-Token": csrf, "X-Ceremony": cr.Ceremony})
	return resp
}

// The page lists what waits and offers the passkey; with no passkey enrolled it
// offers nothing and says where to go.
func TestTheChangesPageOffersAPasskeyOnlyWhenOneIsEnrolled(t *testing.T) {
	f := &fakeChanges{open: twoChanges(), revoke: true}
	p := changeRig(t, f)
	b := p.browser()
	// no passkey yet: the token signs in, and nothing here is answerable
	if resp, _ := b.do("POST", "/login", map[string][]string{"token": {token}}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign in: %d", resp.StatusCode)
	}
	_, page := b.do("GET", "/changes", nil)
	if strings.Contains(page, "data-stepup") || !strings.Contains(page, "whr passkey add") || !strings.Contains(page, "--accept-workflow-change") {
		t.Errorf("without a passkey the page offers a step-up:\n%s", page)
	}
	if !strings.Contains(page, "o/a") || !strings.Contains(page, "integration on develop") || !strings.Contains(page, "prototype on scratch") {
		t.Errorf("the change is not named:\n%s", page)
	}
	csrf := csrfOf(t, b)
	if resp, _ := p.beginAt(b, csrf, "/changes/c1/stepup"); resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusNotFound {
		t.Errorf("a step-up began with no passkey enrolled: %d", resp.StatusCode)
	}

	p.enrol(p.browser())
	b = p.signIn()
	_, page = b.do("GET", "/changes", nil)
	if !strings.Contains(page, `data-url="/changes/c1/stepup"`) || !strings.Contains(page, `data-url="/secrets/revoke-tokens/stepup"`) {
		t.Errorf("the passkey buttons are missing:\n%s", page)
	}
	if strings.Contains(page, "<form method=\"post\" action=\"/changes") {
		t.Errorf("a plain form confirms a change:\n%s", page)
	}
}

// A change is confirmed only by an assertion for exactly that change and what it
// says now, once, and never by the session alone.
func TestAChangeIsConfirmedOnlyWithAPasskeyThatNamesIt(t *testing.T) {
	f := &fakeChanges{open: twoChanges(), revoke: true}
	p := changeRig(t, f)
	p.enrol(p.browser())
	b := p.signIn()
	csrf := csrfOf(t, b)

	// no CSRF header, no session, an unknown change, an assertion that is not there
	if resp, _ := b.postJSON("/changes/c1/stepup/begin", []byte(`{}`), nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("no CSRF header: %d", resp.StatusCode)
	}
	if resp, _ := p.browser().postJSON("/changes/c1/stepup/begin", []byte(`{}`), nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no session: %d", resp.StatusCode)
	}
	if resp, _ := p.beginAt(b, csrf, "/changes/nope/stepup"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown change: %d", resp.StatusCode)
	}
	if resp, _ := b.postJSON("/changes/c1/stepup/finish", []byte(`{}`), map[string]string{"X-CSRF-Token": csrf}); resp.StatusCode < 400 {
		t.Errorf("a session alone confirmed a change: %d", resp.StatusCode)
	}

	// an assertion for another change does not confirm this one
	_, crA := p.beginAt(b, csrf, "/changes/c1/stepup")
	_, crB := p.beginAt(b, csrf, "/changes/c2/stepup")
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", crA, crB, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("an assertion for another change: %d", resp.StatusCode)
	}
	// a ceremony of one change cannot confirm another
	_, crC := p.beginAt(b, csrf, "/changes/c1/stepup")
	if resp := p.finishAt(b, csrf, "/changes/c2/stepup", crC, crC, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a ceremony of one change confirmed another: %d", resp.StatusCode)
	}
	// the revocation of the tokens is not a workflow change either
	_, crD := p.beginAt(b, csrf, "/secrets/revoke-tokens/stepup")
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", crD, crD, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("the revocation's assertion confirmed a change: %d", resp.StatusCode)
	}
	// an assertion without user verification is refused
	_, crE := p.beginAt(b, csrf, "/changes/c1/stepup")
	weak := *p.auth
	weak.UV = false
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", crE, crE, &weak); resp.StatusCode < 400 {
		t.Errorf("an assertion without user verification confirmed a change: %d", resp.StatusCode)
	}
	if c, r := f.counts(); c != 0 || r != 0 {
		t.Fatalf("something was done without a valid assertion: %d confirms, %d revokes", c, r)
	}

	// what the change says now must be what the challenge named: changed since, refused
	_, crF := p.beginAt(b, csrf, "/changes/c1/stepup")
	f.mu.Lock()
	f.open[0].Text = `workflow o/a integration on "develop" to published on ""`
	f.mu.Unlock()
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", crF, crF, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a change that changed after it was shown was confirmed: %d", resp.StatusCode)
	}
	f.mu.Lock()
	f.open[0].Text = twoChanges()[0].Text
	f.mu.Unlock()

	// the right assertion confirms, once
	_, cr := p.beginAt(b, csrf, "/changes/c1/stepup")
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", cr, cr, p.auth); resp.StatusCode != http.StatusOK {
		t.Fatalf("the right assertion: %d", resp.StatusCode)
	}
	if got := f.confirms; len(got) != 1 || got[0] != "c1 by web+passkey" {
		t.Errorf("confirms = %v", got)
	}
	// replayed: the ceremony is spent
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", cr, cr, p.auth); resp.StatusCode < 400 {
		t.Errorf("a replayed assertion: %d", resp.StatusCode)
	}
	if c, _ := f.counts(); c != 1 {
		t.Errorf("a replay confirmed again: %d", c)
	}
}

// A change that is no longer waiting, or whose repository moved on, says so and
// does not look like a success.
func TestAChangeThatIsStaleIsRefusedWithAMessage(t *testing.T) {
	f := &fakeChanges{open: twoChanges(), failWith: store.ErrStaleChange}
	p := changeRig(t, f)
	p.enrol(p.browser())
	b := p.signIn()
	csrf := csrfOf(t, b)
	_, cr := p.beginAt(b, csrf, "/changes/c1/stepup")
	if resp := p.finishAt(b, csrf, "/changes/c1/stepup", cr, cr, p.auth); resp.StatusCode != http.StatusConflict {
		t.Errorf("a stale change: %d, want 409", resp.StatusCode)
	}
}

// The revocation of the forge tokens is a secret operation: a passkey that names
// it, once, and only when there is something to revoke.
func TestTheForgeTokensAreRevokedOnlyWithAPasskeyThatNamesTheOperation(t *testing.T) {
	f := &fakeChanges{open: twoChanges(), revoke: true}
	p := changeRig(t, f)
	p.enrol(p.browser())
	b := p.signIn()
	csrf := csrfOf(t, b)
	base := "/secrets/revoke-tokens/stepup"

	if resp, _ := b.postJSON(base+"/finish", []byte(`{}`), map[string]string{"X-CSRF-Token": csrf}); resp.StatusCode < 400 {
		t.Errorf("a session alone revoked the tokens: %d", resp.StatusCode)
	}
	// an assertion for a change does not revoke
	_, crChange := p.beginAt(b, csrf, "/changes/c1/stepup")
	if resp := p.finishAt(b, csrf, base, crChange, crChange, p.auth); resp.StatusCode != http.StatusForbidden {
		t.Errorf("a change's assertion revoked the tokens: %d", resp.StatusCode)
	}
	if _, r := f.counts(); r != 0 {
		t.Fatalf("revoked without the right assertion: %d", r)
	}
	_, cr := p.beginAt(b, csrf, base)
	if resp := p.finishAt(b, csrf, base, cr, cr, p.auth); resp.StatusCode != http.StatusOK {
		t.Fatalf("the right assertion: %d", resp.StatusCode)
	}
	if resp := p.finishAt(b, csrf, base, cr, cr, p.auth); resp.StatusCode < 400 {
		t.Errorf("a replayed assertion: %d", resp.StatusCode)
	}
	if _, r := f.counts(); r != 1 {
		t.Errorf("revocations = %d, want 1", r)
	}

	// no tokens to revoke: not offered, not answered
	none := changeRig(t, &fakeChanges{open: twoChanges()})
	none.enrol(none.browser())
	nb := none.signIn()
	ncsrf := csrfOf(t, nb)
	_, page := nb.do("GET", "/changes", nil)
	if strings.Contains(page, "revoke-tokens") {
		t.Errorf("the revocation is offered with no tokens:\n%s", page)
	}
	if resp, _ := none.beginAt(nb, ncsrf, base); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a step-up for nothing: %d", resp.StatusCode)
	}
}
