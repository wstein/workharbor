// Package passkey is the passkey sign-in and the step-up approvals of design D45
// (issue #101): WebAuthn with user verification required and discoverable
// credentials, bound to whr's forwarded HTTPS name. The WebAuthn protocol itself
// (CBOR, COSE, attestation, origin and RP ID checks, flags, the sign counter) is
// the go-webauthn library's: writing it by hand would be the riskier choice.
//
// Enrolment is only started from the host (a one-time token minted by the CLI),
// never from a web session; a step-up challenge names the Decision and, for a
// review, its commit SHA; every challenge is server-side, used once and expires in
// two minutes; there is no password and no TOTP fallback.
package passkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
)

// Timings of the ceremonies (design D45).
const (
	EnrolTTL      = 5 * time.Minute
	ChallengeTTL  = 2 * time.Minute
	maxPending    = 16
	maxNameLength = 60
	// maxCeremonies bounds the challenges held in memory, so unauthenticated
	// requests to begin a sign-in cannot grow the map without limit.
	maxCeremonies = 64
	// The sign-in is rate limited as a whole, not per client: there is one owner
	// and the clients arrive through a forwarder that hides their address. Five
	// refused assertions in a minute stop sign-in for the rest of that minute, and
	// at most 30 sign-ins may begin in a minute.
	loginWindow   = time.Minute
	maxLoginFails = 5
	maxLoginBegin = 30
)

// Errors a caller can tell apart.
var (
	ErrBadToken     = errors.New("passkey: the enrolment link is not valid, has expired or was already used")
	ErrBadCeremony  = errors.New("passkey: the challenge is not valid, has expired or was already used")
	ErrNotEnrolled  = errors.New("passkey: no passkey is enrolled")
	ErrBusy         = errors.New("passkey: too many sign-ins are open: wait a moment")
	ErrTooMany      = errors.New("passkey: too many sign-in attempts: wait a minute")
	ErrCloned       = errors.New("passkey: the authenticator's counter went backwards: it may be cloned, so it was refused")
	ErrNotVerified  = errors.New("passkey: the authenticator did not verify the user")
	ErrWrongBinding = errors.New("passkey: the assertion is for another decision or commit")
)

// Config is where the passkeys are valid. RPID is the host name of whr's forwarded
// HTTPS name (no scheme, no port) and Origin its origin (https://host[:port]).
type Config struct {
	RPID        string
	Origin      string
	DisplayName string
	Now         func() time.Time
	// OnFirstEnrolled is called after the first passkey was enrolled: the web
	// sessions started with the API token end then (D45). OnRevoked is called with
	// the ID of a passkey that was revoked: its web sessions end. Both are optional.
	OnFirstEnrolled func()
	OnRevoked       func(id string)
}

// Binding is what a step-up challenge names: the Decision and, for a review, the
// commit SHA the human is approving.
type Binding struct {
	Decision string
	SHA      string
}

// Info describes an enrolled passkey for the host's list.
type Info struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"created_at"`
	LastUsed       time.Time `json:"last_used,omitzero"`
	BackedUp       bool      `json:"backed_up"`
	BackupEligible bool      `json:"backup_eligible"`
}

// Service runs the ceremonies.
type Service struct {
	wa  *webauthn.WebAuthn
	st  *store.Store
	cfg Config
	now func() time.Time

	mu        sync.Mutex
	enrolling map[[sha256.Size]byte]enrolment
	ceremony  map[string]ceremony
	begins    []time.Time // sign-ins begun within loginWindow
	fails     []time.Time // refused sign-in assertions within loginWindow
}

type enrolment struct {
	name    string
	expires time.Time
}

type ceremony struct {
	kind    string // enrol, login, stepup
	session webauthn.SessionData
	name    string  // enrolment: the passkey's name
	holder  string  // step-up: the web session it belongs to
	bind    Binding // step-up: what it names
	expires time.Time
}

// New returns the service. The origin must be https (or a loopback http, for a test
// double) and its host must be the RP ID.
func New(cfg Config, st *store.Store) (*Service, error) {
	u, err := url.Parse(cfg.Origin)
	if err != nil || u.Host == "" || u.Path != "" && u.Path != "/" || u.User != nil {
		return nil, fmt.Errorf("passkey: %q is not an origin", cfg.Origin)
	}
	if u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1")) {
		return nil, fmt.Errorf("passkey: the origin %q must be https", cfg.Origin)
	}
	if cfg.RPID == "" || cfg.RPID != u.Hostname() {
		return nil, fmt.Errorf("passkey: the RP ID %q must be the host of the origin %q", cfg.RPID, cfg.Origin)
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "workharbor"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	wa, err := webauthn.New(&webauthn.Config{RPID: cfg.RPID, RPDisplayName: cfg.DisplayName, RPOrigins: []string{strings.TrimRight(cfg.Origin, "/")}})
	if err != nil {
		return nil, fmt.Errorf("passkey: %w", err)
	}
	return &Service{wa: wa, st: st, cfg: cfg, now: cfg.Now, enrolling: map[[sha256.Size]byte]enrolment{}, ceremony: map[string]ceremony{}}, nil
}

// Origin returns the origin passkeys are bound to.
func (s *Service) Origin() string { return strings.TrimRight(s.cfg.Origin, "/") }

// owner is the one human of this supervisor (D40): a fixed, opaque user handle.
type owner struct{ creds []webauthn.Credential }

// OwnerID is the user handle of the one human of this supervisor.
func OwnerID() []byte { return ownerID }

var ownerID = func() []byte { h := sha256.Sum256([]byte("workharbor-owner")); return h[:] }()

func (o owner) WebAuthnID() []byte                         { return ownerID }
func (o owner) WebAuthnName() string                       { return "owner" }
func (o owner) WebAuthnDisplayName() string                { return "workharbor owner" }
func (o owner) WebAuthnCredentials() []webauthn.Credential { return o.creds }

func (s *Service) owner(ctx context.Context) (owner, []store.Passkey, error) {
	rows, err := s.st.Passkeys(ctx)
	if err != nil {
		return owner{}, nil, err
	}
	o := owner{}
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal(r.Credential, &c); err != nil {
			return owner{}, nil, fmt.Errorf("passkey: the stored credential %s is not readable: %w", r.ID, err)
		}
		o.creds = append(o.creds, c)
	}
	return o, rows, nil
}

// Enrolled reports whether any passkey is enrolled.
func (s *Service) Enrolled(ctx context.Context) (bool, error) {
	rows, err := s.st.Passkeys(ctx)
	return len(rows) > 0, err
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("passkey: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (s *Service) sweepLocked() {
	now := s.now()
	for k, e := range s.enrolling {
		if !now.Before(e.expires) {
			delete(s.enrolling, k)
		}
	}
	for k, c := range s.ceremony {
		if !now.Before(c.expires) {
			delete(s.ceremony, k)
		}
	}
}

// NewEnrolment mints a one-time enrolment token, valid for EnrolTTL: the host's
// first step. The token is kept only as a hash, never logged, and is spent by
// EnrolBegin.
func (s *Service) NewEnrolment(name string) (token string, expires time.Time, err error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "passkey"
	}
	if len(name) > maxNameLength || strings.ContainsFunc(name, func(r rune) bool { return r < ' ' }) {
		return "", time.Time{}, errors.New("passkey: the name must be at most 60 characters, without control characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if len(s.enrolling) >= maxPending {
		return "", time.Time{}, errors.New("passkey: too many enrolment links are open: wait for them to expire")
	}
	token = random(32)
	expires = s.now().Add(EnrolTTL)
	s.enrolling[sha256.Sum256([]byte(token))] = enrolment{name: name, expires: expires}
	return token, expires, nil
}

// CheckToken reports whether an enrolment token is still good, without spending it:
// the enrolment page uses it to decide whether to show the button.
func (s *Service) CheckToken(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	_, ok := s.enrolling[sha256.Sum256([]byte(token))]
	return ok
}

// EnrolBegin spends the token and starts the registration: it returns the options
// for navigator.credentials.create and the ceremony the browser must return to.
// User verification and a discoverable credential are required.
func (s *Service) EnrolBegin(ctx context.Context, token string) (options any, ceremonyID string, err error) {
	key := sha256.Sum256([]byte(token))
	s.mu.Lock()
	s.sweepLocked()
	en, ok := s.enrolling[key]
	delete(s.enrolling, key) // single use, whatever happens next
	s.mu.Unlock()
	if !ok {
		return nil, "", ErrBadToken
	}
	o, _, err := s.owner(ctx)
	if err != nil {
		return nil, "", err
	}
	yes := true
	creation, session, err := s.wa.BeginRegistration(o,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			RequireResidentKey: &yes, ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(excluded(o.creds)),
	)
	if err != nil {
		return nil, "", fmt.Errorf("passkey: %w", err)
	}
	id, err := s.keep(ceremony{kind: "enrol", session: *session, name: en.name})
	if err != nil {
		return nil, "", err
	}
	return creation, id, nil
}

func excluded(cs []webauthn.Credential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Descriptor())
	}
	return out
}

// keep stores a ceremony under a new ID, or refuses when too many are open.
func (s *Service) keep(c ceremony) (string, error) {
	id := random(24)
	c.expires = s.now().Add(ChallengeTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if len(s.ceremony) >= maxCeremonies {
		return "", ErrBusy
	}
	s.ceremony[id] = c
	return id, nil
}

// recent drops the times older than the window and returns what is left.
func (s *Service) recent(ts []time.Time) []time.Time {
	cut := s.now().Add(-loginWindow)
	out := ts[:0]
	for _, t := range ts {
		if t.After(cut) {
			out = append(out, t)
		}
	}
	return out
}

// allowLogin counts a sign-in beginning and reports whether the limits let it.
func (s *Service) allowLogin(begin bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fails, s.begins = s.recent(s.fails), s.recent(s.begins)
	if len(s.fails) >= maxLoginFails || (begin && len(s.begins) >= maxLoginBegin) {
		return false
	}
	if begin {
		s.begins = append(s.begins, s.now())
	}
	return true
}

func (s *Service) failLogin() {
	s.mu.Lock()
	s.fails = append(s.fails, s.now())
	s.mu.Unlock()
}

// take returns a ceremony of a kind and forgets it: a challenge is used once,
// whether the response turns out right or not.
func (s *Service) take(id, kind string) (ceremony, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	c, ok := s.ceremony[id]
	delete(s.ceremony, id)
	if !ok || c.kind != kind {
		return ceremony{}, ErrBadCeremony
	}
	return c, nil
}

// EnrolFinish checks the browser's registration (origin, RP ID, challenge, user
// verification) and stores the public key and credential ID.
func (s *Service) EnrolFinish(ctx context.Context, ceremonyID string, r *http.Request) (Info, error) {
	c, err := s.take(ceremonyID, "enrol")
	if err != nil {
		return Info{}, err
	}
	o, _, err := s.owner(ctx)
	if err != nil {
		return Info{}, err
	}
	cred, err := s.wa.FinishRegistration(o, c.session, r)
	if err != nil {
		return Info{}, fmt.Errorf("passkey: the registration was refused: %w", err)
	}
	if !cred.Flags.UserVerified {
		return Info{}, ErrNotVerified
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return Info{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	p := store.Passkey{ID: id, Name: c.name, Credential: raw, CreatedAt: s.now()}
	first := len(o.creds) == 0
	if err := s.st.AddPasskey(ctx, p); err != nil {
		return Info{}, err
	}
	if first && s.cfg.OnFirstEnrolled != nil {
		s.cfg.OnFirstEnrolled()
	}
	return infoOf(p, *cred), nil
}

func infoOf(p store.Passkey, c webauthn.Credential) Info {
	return Info{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastUsed: p.LastUsed, BackedUp: c.Flags.BackupState, BackupEligible: c.Flags.BackupEligible}
}

// List returns the enrolled passkeys.
func (s *Service) List(ctx context.Context) ([]Info, error) {
	_, rows, err := s.owner(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(rows))
	for _, r := range rows {
		var c webauthn.Credential
		_ = json.Unmarshal(r.Credential, &c)
		out = append(out, infoOf(r, c))
	}
	return out, nil
}

// Revoke removes a passkey by its ID or by a unique prefix of it. Revoking the
// last one leaves the web UI without a sign-in until the host enrols another.
func (s *Service) Revoke(ctx context.Context, idOrPrefix string) error {
	rows, err := s.st.Passkeys(ctx)
	if err != nil {
		return err
	}
	var hit []string
	for _, r := range rows {
		if r.ID == idOrPrefix {
			hit = []string{r.ID}
			break
		}
		if idOrPrefix != "" && strings.HasPrefix(r.ID, idOrPrefix) {
			hit = append(hit, r.ID)
		}
	}
	switch len(hit) {
	case 0:
		return &domain.NotFoundError{Kind: "passkey", ID: idOrPrefix}
	case 1:
		if err := s.st.RevokePasskey(ctx, hit[0]); err != nil {
			return err
		}
		if s.cfg.OnRevoked != nil {
			s.cfg.OnRevoked(hit[0])
		}
		return nil
	}
	return fmt.Errorf("passkey: %q matches several passkeys: give more of the ID", idOrPrefix)
}

// LoginBegin starts a sign-in with a discoverable credential: the browser offers the
// passkeys it holds for this site.
func (s *Service) LoginBegin(ctx context.Context) (options any, ceremonyID string, err error) {
	if !s.allowLogin(true) {
		return nil, "", ErrTooMany
	}
	if ok, err := s.Enrolled(ctx); err != nil || !ok {
		if err == nil {
			err = ErrNotEnrolled
		}
		return nil, "", err
	}
	assertion, session, err := s.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", fmt.Errorf("passkey: %w", err)
	}
	id, err := s.keep(ceremony{kind: "login", session: *session})
	if err != nil {
		return nil, "", err
	}
	return assertion, id, nil
}

// LoginFinish checks the assertion and returns the ID of the passkey that signed in.
func (s *Service) LoginFinish(ctx context.Context, ceremonyID string, r *http.Request) (id string, err error) {
	if !s.allowLogin(false) {
		return "", ErrTooMany
	}
	// Only an assertion that was tried against a real ceremony counts as refused.
	// A finish with an unknown or expired ceremony ID costs nothing to make and
	// could otherwise be sent by anyone who reaches the forwarder to keep the human
	// out for as long as they like.
	c, err := s.take(ceremonyID, "login")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			s.failLogin()
		}
	}()
	o, _, err := s.owner(ctx)
	if err != nil {
		return "", err
	}
	handler := func(_, userHandle []byte) (webauthn.User, error) {
		if string(userHandle) != string(ownerID) {
			return nil, errors.New("unknown user handle")
		}
		return o, nil
	}
	cred, err := s.wa.FinishDiscoverableLogin(handler, c.session, r)
	if err != nil {
		return "", fmt.Errorf("passkey: the sign-in was refused: %w", err)
	}
	return s.afterAssertion(ctx, cred)
}

// afterAssertion applies what every successful assertion owes: user verification,
// the clone check, and the stored sign counter and backup state.
func (s *Service) afterAssertion(ctx context.Context, cred *webauthn.Credential) (string, error) {
	if cred.Authenticator.CloneWarning {
		return "", ErrCloned
	}
	if !cred.Flags.UserVerified {
		return "", ErrNotVerified
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	raw, err := json.Marshal(cred)
	if err != nil {
		return "", err
	}
	if err := s.st.UpdatePasskey(ctx, id, raw, s.now()); err != nil {
		return "", err
	}
	return id, nil
}

// challengeFor is what a step-up challenge commits to: a random nonce, the Decision
// and the SHA, so an assertion signed for one cannot be replayed for another even if
// the server's own bookkeeping were wrong.
func challengeFor(nonce []byte, b Binding) []byte {
	h := sha256.New()
	fmt.Fprintf(h, "workharbor-stepup\x00%d:%s\x00%d:%s\x00", len(b.Decision), b.Decision, len(b.SHA), b.SHA)
	h.Write(nonce)
	return h.Sum(nil)
}

// StepUpBegin starts a fresh user-verified assertion for one Decision (and, for a
// review, its SHA). holder is the web session it belongs to: no other session can
// finish it.
func (s *Service) StepUpBegin(ctx context.Context, holder string, b Binding) (options any, ceremonyID string, err error) {
	if b.Decision == "" || holder == "" {
		return nil, "", errors.New("passkey: a step-up names a decision and belongs to a session")
	}
	o, _, err := s.owner(ctx)
	if err != nil {
		return nil, "", err
	}
	if len(o.creds) == 0 {
		return nil, "", ErrNotEnrolled
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}
	assertion, session, err := s.wa.BeginLogin(o, webauthn.WithUserVerification(protocol.VerificationRequired), webauthn.WithChallenge(challengeFor(nonce, b)))
	if err != nil {
		return nil, "", fmt.Errorf("passkey: %w", err)
	}
	id, err := s.keep(ceremony{kind: "stepup", session: *session, holder: holder, bind: b})
	if err != nil {
		return nil, "", err
	}
	return assertion, id, nil
}

// StepUpFinish checks the assertion against the challenge it answers, for the
// session it was made for, and returns what the challenge named. The caller
// compares that with the Decision it is about to answer.
func (s *Service) StepUpFinish(ctx context.Context, ceremonyID, holder string, r *http.Request) (Binding, error) {
	c, err := s.take(ceremonyID, "stepup")
	if err != nil {
		return Binding{}, err
	}
	if c.holder != holder {
		return Binding{}, ErrBadCeremony
	}
	o, _, err := s.owner(ctx)
	if err != nil {
		return Binding{}, err
	}
	cred, err := s.wa.FinishLogin(o, c.session, r)
	if err != nil {
		return Binding{}, fmt.Errorf("passkey: the approval was refused: %w", err)
	}
	if _, err := s.afterAssertion(ctx, cred); err != nil {
		return Binding{}, err
	}
	return c.bind, nil
}
