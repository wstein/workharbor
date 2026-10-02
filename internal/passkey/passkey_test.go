package passkey

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/wstein/workharbor/internal/passkey/passkeytest"
	"github.com/wstein/workharbor/internal/store"
)

const origin = "https://whr.example.test"

var bg = context.Background()

type rig struct {
	t   *testing.T
	svc *Service
	st  *store.Store
	mu  sync.Mutex
	now time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := store.Open(bg, filepath.Join(t.TempDir(), "workharbor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &rig{t: t, st: st, now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	r.svc, err = New(Config{RPID: "whr.example.test", Origin: origin, Now: func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }}, st)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) advance(d time.Duration) { r.mu.Lock(); r.now = r.now.Add(d); r.mu.Unlock() }

// enrol runs a whole enrolment of a with a fresh token.
func (r *rig) enrol(a *passkeytest.Authn, name string) Info {
	r.t.Helper()
	tok, _, err := r.svc.NewEnrolment(name)
	if err != nil {
		r.t.Fatal(err)
	}
	opts, cer, err := r.svc.EnrolBegin(bg, tok)
	if err != nil {
		r.t.Fatal(err)
	}
	info, err := r.svc.EnrolFinish(bg, cer, a.Register(opts.(*protocol.CredentialCreation), origin))
	if err != nil {
		r.t.Fatal(err)
	}
	return info
}

func TestEnrolmentStoresOnlyThePublicKeyAndRequiresUserVerification(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	tok, exp, err := r.svc.NewEnrolment("my phone")
	if err != nil || !exp.Equal(r.now.Add(EnrolTTL)) || len(tok) != 64 {
		t.Fatalf("token %q %v %v", tok, exp, err)
	}
	opts, cer, err := r.svc.EnrolBegin(bg, tok)
	if err != nil {
		t.Fatal(err)
	}
	creation := opts.(*protocol.CredentialCreation)
	sel := creation.Response.AuthenticatorSelection
	if sel.UserVerification != protocol.VerificationRequired || sel.ResidentKey != protocol.ResidentKeyRequirementRequired || sel.RequireResidentKey == nil || !*sel.RequireResidentKey {
		t.Errorf("the registration must require user verification and a discoverable credential: %+v", sel)
	}
	info, err := r.svc.EnrolFinish(bg, cer, a.Register(creation, origin))
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "my phone" || info.ID != passkeytest.B64(a.CredID) {
		t.Errorf("info %+v", info)
	}
	rows, _ := r.st.Passkeys(bg)
	if len(rows) != 1 || strings.Contains(string(rows[0].Credential), "PrivateKey") {
		t.Fatalf("rows %+v", rows)
	}
	if ok, _ := r.svc.Enrolled(bg); !ok {
		t.Error("not enrolled after enrolment")
	}
}

func TestAnEnrolmentLinkWorksOnceAndExpires(t *testing.T) {
	r := newRig(t)
	tok, _, _ := r.svc.NewEnrolment("a")
	if !r.svc.CheckToken(tok) {
		t.Fatal("a fresh token is refused")
	}
	if _, _, err := r.svc.EnrolBegin(bg, tok); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.svc.EnrolBegin(bg, tok); !errors.Is(err, ErrBadToken) {
		t.Errorf("a second use = %v, want ErrBadToken", err)
	}
	if r.svc.CheckToken(tok) {
		t.Error("a spent token still checks out")
	}
	old, _, _ := r.svc.NewEnrolment("b")
	r.advance(EnrolTTL + time.Second)
	if _, _, err := r.svc.EnrolBegin(bg, old); !errors.Is(err, ErrBadToken) {
		t.Errorf("an expired token = %v", err)
	}
	for _, bad := range []string{"", "forged", strings.Repeat("0", 64)} {
		if _, _, err := r.svc.EnrolBegin(bg, bad); !errors.Is(err, ErrBadToken) {
			t.Errorf("token %q = %v", bad, err)
		}
	}
	if _, _, err := r.svc.NewEnrolment("a\nb"); err == nil {
		t.Error("a name with a control character was accepted")
	}
}

func TestAnAuthenticatorThatDoesNotVerifyTheUserIsRefused(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	a.UV = false
	tok, _, _ := r.svc.NewEnrolment("x")
	opts, cer, _ := r.svc.EnrolBegin(bg, tok)
	if _, err := r.svc.EnrolFinish(bg, cer, a.Register(opts.(*protocol.CredentialCreation), origin)); err == nil {
		t.Fatal("a passkey without user verification was enrolled")
	}
	if ok, _ := r.svc.Enrolled(bg); ok {
		t.Error("a refused enrolment left a passkey")
	}
}

func TestARegistrationFromAnotherOriginIsRefused(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	tok, _, _ := r.svc.NewEnrolment("x")
	opts, cer, _ := r.svc.EnrolBegin(bg, tok)
	if _, err := r.svc.EnrolFinish(bg, cer, a.Register(opts.(*protocol.CredentialCreation), "https://evil.example")); err == nil {
		t.Fatal("a registration from another origin was accepted")
	}
	// the challenge is spent: the right answer now is refused too
	if _, err := r.svc.EnrolFinish(bg, cer, a.Register(opts.(*protocol.CredentialCreation), origin)); !errors.Is(err, ErrBadCeremony) {
		t.Errorf("a retried challenge = %v", err)
	}
}

func TestSigningInChecksTheAssertionAndTheCounter(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	info := r.enrol(a, "phone")

	opts, cer, err := r.svc.LoginBegin(bg)
	if err != nil {
		t.Fatal(err)
	}
	assertion := opts.(*protocol.CredentialAssertion)
	if assertion.Response.UserVerification != protocol.VerificationRequired {
		t.Errorf("sign-in must require user verification: %v", assertion.Response.UserVerification)
	}
	id, err := r.svc.LoginFinish(bg, cer, a.Assert(assertion, origin))
	if err != nil || id != info.ID {
		t.Fatalf("sign-in: %q %v", id, err)
	}
	rows, _ := r.st.Passkeys(bg)
	if rows[0].LastUsed.IsZero() || !strings.Contains(string(rows[0].Credential), `"signCount":1`) {
		t.Errorf("the counter and the time were not stored: %+v %s", rows[0].LastUsed, rows[0].Credential)
	}

	// a replay of the same response: the challenge is spent
	if _, err := r.svc.LoginFinish(bg, cer, a.Assert(assertion, origin)); !errors.Is(err, ErrBadCeremony) {
		t.Errorf("a replay = %v", err)
	}
	// a counter that goes backwards says the authenticator may be cloned
	opts, cer, _ = r.svc.LoginBegin(bg)
	a.Counter = 0 // a copy of the key that was used less
	if _, err := r.svc.LoginFinish(bg, cer, a.AssertAt(opts.(*protocol.CredentialAssertion).Response.Challenge, "whr.example.test", origin)); err == nil {
		t.Error("a counter that went backwards signed in")
	}
}

func TestSigningInWithoutUserVerificationOrFromAnotherSiteIsRefused(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	r.enrol(a, "phone")
	opts, cer, _ := r.svc.LoginBegin(bg)
	a.UV = false
	if _, err := r.svc.LoginFinish(bg, cer, a.Assert(opts.(*protocol.CredentialAssertion), origin)); err == nil {
		t.Error("an assertion without user verification signed in")
	}
	a.UV = true
	opts, cer, _ = r.svc.LoginBegin(bg)
	if _, err := r.svc.LoginFinish(bg, cer, a.Assert(opts.(*protocol.CredentialAssertion), "https://evil.example")); err == nil {
		t.Error("an assertion made for another origin signed in")
	}
	// nothing enrolled: no sign-in at all, and no fallback
	r2 := newRig(t)
	if _, _, err := r2.svc.LoginBegin(bg); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("no passkeys = %v", err)
	}
}

func TestARevokedPasskeyCannotSignInAndTheListSaysWhatIsEnrolled(t *testing.T) {
	r := newRig(t)
	a, b := passkeytest.New(t, ownerID), passkeytest.New(t, ownerID)
	ia, ib := r.enrol(a, "phone"), r.enrol(b, "tablet")
	list, err := r.svc.List(bg)
	if err != nil || len(list) != 2 || list[0].Name != "phone" || list[1].Name != "tablet" {
		t.Fatalf("%+v %v", list, err)
	}
	if err := r.svc.Revoke(bg, ia.ID[:8]); err != nil { // by a prefix
		t.Fatal(err)
	}
	opts, cer, _ := r.svc.LoginBegin(bg)
	if _, err := r.svc.LoginFinish(bg, cer, a.Assert(opts.(*protocol.CredentialAssertion), origin)); err == nil {
		t.Error("a revoked passkey signed in")
	}
	opts, cer, _ = r.svc.LoginBegin(bg)
	if id, err := r.svc.LoginFinish(bg, cer, b.Assert(opts.(*protocol.CredentialAssertion), origin)); err != nil || id != ib.ID {
		t.Errorf("the other passkey: %q %v", id, err)
	}
	if err := r.svc.Revoke(bg, "nope"); err == nil {
		t.Error("a passkey that does not exist was revoked")
	}
	if err := r.svc.Revoke(bg, ""); err == nil {
		t.Error("an empty ID revoked something")
	}
}

// A step-up names one Decision and one commit; an assertion for another is refused.
func TestAStepUpIsBoundToTheDecisionAndTheCommitAndUsedOnce(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	r.enrol(a, "phone")
	bind := Binding{Decision: "d1", SHA: "aaa111"}

	opts, cer, err := r.svc.StepUpBegin(bg, "session-1", bind)
	if err != nil {
		t.Fatal(err)
	}
	assertion := opts.(*protocol.CredentialAssertion)
	if assertion.Response.UserVerification != protocol.VerificationRequired || len(assertion.Response.AllowedCredentials) != 1 {
		t.Errorf("a step-up needs user verification from an enrolled passkey: %+v", assertion.Response)
	}
	// the challenge commits to the decision and the SHA: it is not the one of another binding
	if string(assertion.Response.Challenge) == string(challengeFor(nil, bind)) {
		t.Error("the challenge has no nonce")
	}
	req := a.Assert(assertion, origin)
	got, err := r.svc.StepUpFinish(bg, cer, "session-1", req)
	if err != nil || got != bind {
		t.Fatalf("step-up: %+v %v", got, err)
	}
	// used once
	if _, err := r.svc.StepUpFinish(bg, cer, "session-1", a.Assert(assertion, origin)); !errors.Is(err, ErrBadCeremony) {
		t.Errorf("a replay = %v", err)
	}

	// an assertion made for another decision's challenge does not answer this one
	oA, cA, _ := r.svc.StepUpBegin(bg, "session-1", Binding{Decision: "d1", SHA: "aaa111"})
	oB, _, _ := r.svc.StepUpBegin(bg, "session-1", Binding{Decision: "d2", SHA: "aaa111"})
	other := a.Assert(oB.(*protocol.CredentialAssertion), origin)
	if _, err := r.svc.StepUpFinish(bg, cA, "session-1", other); err == nil {
		t.Error("an assertion for decision d2 answered decision d1")
	}
	_ = oA
	// ... nor does one made for another commit
	oC, cC, _ := r.svc.StepUpBegin(bg, "session-1", Binding{Decision: "d1", SHA: "aaa111"})
	oD, _, _ := r.svc.StepUpBegin(bg, "session-1", Binding{Decision: "d1", SHA: "bbb222"})
	if _, err := r.svc.StepUpFinish(bg, cC, "session-1", a.Assert(oD.(*protocol.CredentialAssertion), origin)); err == nil {
		t.Error("an assertion for another SHA answered this one")
	}
	_ = oC
	// another web session cannot finish it
	oE, cE, _ := r.svc.StepUpBegin(bg, "session-1", bind)
	if _, err := r.svc.StepUpFinish(bg, cE, "session-2", a.Assert(oE.(*protocol.CredentialAssertion), origin)); !errors.Is(err, ErrBadCeremony) {
		t.Errorf("another session = %v", err)
	}
}

func TestAStepUpChallengeExpiresInTwoMinutes(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	r.enrol(a, "phone")
	opts, cer, _ := r.svc.StepUpBegin(bg, "s", Binding{Decision: "d1"})
	req := a.Assert(opts.(*protocol.CredentialAssertion), origin)
	r.advance(ChallengeTTL + time.Second)
	if _, err := r.svc.StepUpFinish(bg, cer, "s", req); !errors.Is(err, ErrBadCeremony) {
		t.Errorf("an expired challenge = %v", err)
	}
	if _, _, err := r.svc.StepUpBegin(bg, "", Binding{Decision: "d1"}); err == nil {
		t.Error("a step-up without a session was started")
	}
	if _, _, err := r.svc.StepUpBegin(bg, "s", Binding{}); err == nil {
		t.Error("a step-up without a decision was started")
	}
	r2 := newRig(t)
	if _, _, err := r2.svc.StepUpBegin(bg, "s", Binding{Decision: "d1"}); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("no passkeys = %v", err)
	}
}

func TestTheOriginMustBeHTTPSAndTheRPIDItsHost(t *testing.T) {
	st, _ := store.Open(bg, filepath.Join(t.TempDir(), "w.db"))
	t.Cleanup(func() { _ = st.Close() })
	for name, cfg := range map[string]Config{
		"http":          {RPID: "whr.example.test", Origin: "http://whr.example.test"},
		"a path":        {RPID: "whr.example.test", Origin: "https://whr.example.test/x"},
		"another rp id": {RPID: "example.test", Origin: "https://whr.example.test"},
		"credentials":   {RPID: "whr.example.test", Origin: "https://user@whr.example.test"},
		"no rp id":      {Origin: "https://whr.example.test"},
	} {
		if _, err := New(cfg, st); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := New(Config{RPID: "localhost", Origin: "http://localhost:8787"}, st); err != nil {
		t.Errorf("a loopback test origin: %v", err)
	}
}

// The challenges held in memory are bounded, so requests that begin a ceremony
// cannot grow the map without limit.
func TestTheNumberOfOpenCeremoniesIsBounded(t *testing.T) {
	r := newRig(t)
	r.enrol(passkeytest.New(t, ownerID), "phone")
	for i := range maxCeremonies {
		if _, _, err := r.svc.StepUpBegin(bg, "session", Binding{Decision: "d", SHA: "s"}); err != nil {
			t.Fatalf("step-up %d: %v", i, err)
		}
	}
	if _, _, err := r.svc.StepUpBegin(bg, "session", Binding{Decision: "d", SHA: "s"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("a ceremony past the cap: %v", err)
	}
	if n := len(r.svc.ceremony); n != maxCeremonies {
		t.Errorf("%d ceremonies held, want %d", n, maxCeremonies)
	}
	// expired ones make room again
	r.advance(ChallengeTTL + time.Second)
	if _, _, err := r.svc.StepUpBegin(bg, "session", Binding{Decision: "d", SHA: "s"}); err != nil {
		t.Errorf("after the challenges expired: %v", err)
	}
}

// Sign-in is rate limited as a whole: too many beginnings, or five refused
// assertions, stop it for the rest of the minute.
func TestSignInIsRateLimited(t *testing.T) {
	r := newRig(t)
	a := passkeytest.New(t, ownerID)
	r.enrol(a, "phone")

	for i := range maxLoginBegin {
		if _, _, err := r.svc.LoginBegin(bg); err != nil {
			t.Fatalf("begin %d: %v", i, err)
		}
	}
	if _, _, err := r.svc.LoginBegin(bg); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a 31st begin in a minute: %v", err)
	}
	r.advance(loginWindow + time.Second)
	if _, _, err := r.svc.LoginBegin(bg); err != nil {
		t.Fatalf("a minute later: %v", err)
	}

	// refused assertions (here: challenges nobody issued) count, and five stop everything,
	// even a sign-in that would have been right
	r.advance(loginWindow + time.Second)
	opts, cer, err := r.svc.LoginBegin(bg)
	if err != nil {
		t.Fatal(err)
	}
	for i := range maxLoginFails {
		if _, err := r.svc.LoginFinish(bg, "guess", passkeytest.Post(map[string]string{})); err == nil || errors.Is(err, ErrTooMany) {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	good := a.Assert(opts.(*protocol.CredentialAssertion), origin)
	if _, err := r.svc.LoginFinish(bg, cer, good); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a sign-in after five refusals: %v", err)
	}
	if _, _, err := r.svc.LoginBegin(bg); !errors.Is(err, ErrTooMany) {
		t.Errorf("a begin after five refusals: %v", err)
	}
	r.advance(loginWindow + time.Second)
	opts, cer, err = r.svc.LoginBegin(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.LoginFinish(bg, cer, a.Assert(opts.(*protocol.CredentialAssertion), origin)); err != nil {
		t.Errorf("a right sign-in after the minute: %v", err)
	}
}
