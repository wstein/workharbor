// Package confirm defines the confirmation record v1: a neutral, versioned
// record of a human answer (land, push, decision, ...) with a canonical byte
// encoding and a domain-separated digest. See issue #333 for the design.
//
// The CLI record has assurance "local": an honest log, not proof against an
// agent running as the same user. Assurance is self-asserted; binding the
// answer requires a verified passkey assertion through a trusted consumer flow.
package confirm

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Version is the only record version this package accepts.
const Version = 1

// Schema is the fixed schema name of a confirmation record.
const Schema = "workharbor.confirmation"

// DigestPrefix is the domain separator hashed before the canonical bytes.
const DigestPrefix = "workharbor-confirm-v1\x00"

// Actions a record can confirm.
const (
	ActionLand          = "land"
	ActionPush          = "push"
	ActionChangeConfirm = "change.confirm"
	ActionTokensRevoke  = "tokens.revoke"
	ActionDecision      = "decision"
)

// Channels an answer arrived through.
const (
	ChannelCLI   = "cli"
	ChannelWeb   = "web"
	ChannelRelay = "relay"
)

// Answer modes.
const (
	ModeYN       = "yn"
	ModeTypedSHA = "typed_sha"
	ModeOption   = "option"
	ModeDeny     = "deny"
)

// Assurance levels.
const (
	AssuranceNone    = "none"
	AssuranceLocal   = "local"
	AssurancePasskey = "passkey"
)

// Evidence kinds. Kinds starting with "ai-" are never accepted.
const (
	EvidenceReviewNote       = "review-note"
	EvidenceCheck            = "check"
	EvidencePathClass        = "path-class"
	EvidenceBase             = "base"
	EvidenceAudit            = "audit"
	EvidencePasskeyAssertion = "passkey-assertion"
	EvidenceDecisionLog      = "decision-log"
)

// Record is a confirmation record v1. Optional fields are omitted from the
// canonical encoding when empty.
type Record struct {
	V         int            `json:"v"`
	Schema    string         `json:"schema"`
	Action    string         `json:"action"`
	Subject   Subject        `json:"subject"`
	Question  string         `json:"question,omitempty"`
	Answer    Answer         `json:"answer"`
	At        string         `json:"at"`
	Channel   string         `json:"channel"`
	By        string         `json:"by"`
	Assurance string         `json:"assurance"`
	Evidence  []Evidence     `json:"evidence,omitempty"`
	Ext       map[string]any `json:"ext,omitempty"`
}

// Subject names what was confirmed. At least one field is set.
type Subject struct {
	Commit   string `json:"commit,omitempty"`   // full lowercase hex, 40 or 64 digits
	Branch   string `json:"branch,omitempty"`   //
	Issue    string `json:"issue,omitempty"`    // "owner/repo#n"
	Decision string `json:"decision,omitempty"` // decision id such as "H96"
	Ref      string `json:"ref,omitempty"`      // opaque reference
}

// Answer is the human's answer.
type Answer struct {
	Mode  string `json:"mode"`
	Value string `json:"value,omitempty"`
}

// Evidence points at something that backed the answer.
type Evidence struct {
	Kind   string `json:"kind"`
	Name   string `json:"name,omitempty"`
	Object string `json:"object,omitempty"` // full lowercase hex object id
	Ref    string `json:"ref,omitempty"`
	Result string `json:"result,omitempty"`
	Value  string `json:"value,omitempty"`
}

// Errors returned by Validate and Decode. Match with errors.Is.
var (
	ErrUnknownVersion  = errors.New("confirm: unknown record version")
	ErrUnknownField    = errors.New("confirm: unknown top-level field")
	ErrUnknownAction   = errors.New("confirm: unknown action")
	ErrUnknownChannel  = errors.New("confirm: unknown channel")
	ErrBadSchema       = errors.New("confirm: bad schema name")
	ErrBadSubject      = errors.New("confirm: bad subject")
	ErrBadAnswer       = errors.New("confirm: bad answer")
	ErrBadTime         = errors.New("confirm: at is not RFC 3339 UTC whole seconds with Z")
	ErrBadBy           = errors.New("confirm: bad by")
	ErrBadAssurance    = errors.New("confirm: bad assurance")
	ErrBadEvidence     = errors.New("confirm: bad evidence")
	ErrAIEvidence      = errors.New("confirm: ai evidence kind not accepted")
	ErrEvidenceOrder   = errors.New("confirm: evidence not sorted by canonical bytes")
	ErrBadExt          = errors.New("confirm: bad ext")
	ErrNotCanonical    = errors.New("confirm: not canonical encoding")
	ErrSyntax          = errors.New("confirm: invalid encoding")
	ErrMissingRequired = errors.New("confirm: missing required field")
)

var (
	hexRe      = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	issueRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+#[1-9][0-9]*$`)
	shortHexRe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// Digest returns the lowercase hex sha256 of DigestPrefix and the canonical
// bytes of r. It does not validate r.
func (r Record) Digest() (string, error) {
	b, err := Encode(r)
	if err != nil {
		return "", err
	}
	return DigestOf(b), nil
}

// DigestOf hashes already-canonical bytes.
func DigestOf(canonical []byte) string {
	h := sha256.New()
	h.Write([]byte(DigestPrefix))
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil))
}

// Validate checks v1 record fields, not extension value encodability or
// cryptographic assurance. Success does not imply approval; deny and yn:no
// are valid answers. Consumers must check the answer and operation binding.
func (r Record) Validate() error {
	if r.V != Version {
		return fmt.Errorf("%w: %d", ErrUnknownVersion, r.V)
	}
	if r.Schema != Schema {
		return fmt.Errorf("%w: %q", ErrBadSchema, r.Schema)
	}
	switch r.Action {
	case ActionLand, ActionPush, ActionChangeConfirm, ActionTokensRevoke, ActionDecision:
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAction, r.Action)
	}
	switch r.Channel {
	case ChannelCLI, ChannelWeb, ChannelRelay:
	default:
		return fmt.Errorf("%w: %q", ErrUnknownChannel, r.Channel)
	}
	if err := r.validateSubject(); err != nil {
		return err
	}
	if err := r.validateAnswer(); err != nil {
		return err
	}
	if err := validateAt(r.At); err != nil {
		return err
	}
	if r.By == "" || strings.ContainsAny(r.By, "@ \t\r\n") {
		return fmt.Errorf("%w: need a name, never an email", ErrBadBy)
	}
	switch r.Assurance {
	case AssuranceNone, AssuranceLocal, AssurancePasskey:
	default:
		return fmt.Errorf("%w: %q", ErrBadAssurance, r.Assurance)
	}
	if err := r.validateEvidence(); err != nil {
		return err
	}
	for k := range r.Ext {
		if !strings.Contains(k, ".") || strings.HasPrefix(k, ".") || strings.HasSuffix(k, ".") {
			return fmt.Errorf("%w: key %q is not namespaced", ErrBadExt, k)
		}
	}
	return nil
}

func (r Record) validateSubject() error {
	s := r.Subject
	if s == (Subject{}) {
		return fmt.Errorf("%w: empty", ErrBadSubject)
	}
	if s.Commit != "" && !hexRe.MatchString(s.Commit) {
		return fmt.Errorf("%w: commit must be a full lowercase hex id", ErrBadSubject)
	}
	if s.Issue != "" && !issueRe.MatchString(s.Issue) {
		return fmt.Errorf("%w: issue must be owner/repo#n", ErrBadSubject)
	}
	if (r.Action == ActionLand || r.Action == ActionPush) && s.Commit == "" {
		return fmt.Errorf("%w: %s needs subject.commit", ErrBadSubject, r.Action)
	}
	return nil
}

func (r Record) validateAnswer() error {
	a := r.Answer
	switch a.Mode {
	case ModeYN:
		if a.Value != "yes" && a.Value != "no" {
			return fmt.Errorf("%w: yn value must be yes or no", ErrBadAnswer)
		}
	case ModeTypedSHA:
		if !shortHexRe.MatchString(a.Value) || !strings.HasPrefix(r.Subject.Commit, a.Value) {
			return fmt.Errorf("%w: typed_sha must be a prefix (7+ hex) of subject.commit", ErrBadAnswer)
		}
	case ModeOption:
		if a.Value == "" {
			return fmt.Errorf("%w: option needs a value", ErrBadAnswer)
		}
	case ModeDeny:
		if a.Value != "" {
			return fmt.Errorf("%w: deny has no value", ErrBadAnswer)
		}
	default:
		return fmt.Errorf("%w: mode %q", ErrBadAnswer, a.Mode)
	}
	return nil
}

func validateAt(at string) error {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil || t.UTC().Format("2006-01-02T15:04:05Z") != at {
		return fmt.Errorf("%w: %q", ErrBadTime, at)
	}
	return nil
}

func (r Record) validateEvidence() error {
	var prev []byte
	for i, e := range r.Evidence {
		if strings.HasPrefix(e.Kind, "ai-") {
			return fmt.Errorf("%w: %q", ErrAIEvidence, e.Kind)
		}
		switch e.Kind {
		case EvidenceReviewNote, EvidenceCheck, EvidencePathClass, EvidenceBase,
			EvidenceAudit, EvidencePasskeyAssertion, EvidenceDecisionLog:
		default:
			return fmt.Errorf("%w: kind %q", ErrBadEvidence, e.Kind)
		}
		if e.Name == "" && e.Object == "" && e.Ref == "" && e.Result == "" && e.Value == "" {
			return fmt.Errorf("%w: %s carries nothing but its kind", ErrBadEvidence, e.Kind)
		}
		if e.Object != "" && !hexRe.MatchString(e.Object) {
			return fmt.Errorf("%w: object must be a full lowercase hex id", ErrBadEvidence)
		}
		cur := encodeEvidence(e)
		if i > 0 && string(prev) >= string(cur) {
			return ErrEvidenceOrder
		}
		prev = cur
	}
	return nil
}
