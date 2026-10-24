// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/url"
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// CandidateHomeScope contains admitted identities, never source paths or user
// names. Endpoint must already identify the physical admitted Unix socket; this
// pure library checks its canonical spelling, not filesystem or daemon identity.
// OperationID comes from the separate stage operation, not from the home nonce.
type CandidateHomeScope struct {
	OperationID, DaemonID, Endpoint, PrimaryID, PrimaryHome, Destination string
}

// CandidateHomeMount is the home-only projection for later official-CLI mount
// configuration and fresh metadata comparison. It is not an allocation command.
// ReadOnly is the inverse of Docker inspect's RW field.
type CandidateHomeMount struct {
	Type, Name, Destination string
	ReadOnly                bool
}

// CandidateHomePlan is a value copy suitable for inclusion in a private durable
// intent. Returning it neither writes that intent nor proves volume ownership.
// Nonce is the home name's independent 128-bit suffix and allocation label value.
type CandidateHomePlan struct {
	OperationID, Nonce, DaemonID, Endpoint, PrimaryID, PrimaryHome string
	Mount                                                          CandidateHomeMount
}

// CandidateHomeObservation is a caller's fresh, complete candidate mount
// inventory from the same admitted daemon. Labels alone cannot establish
// ownership. The caller must resolve the primary and canonical candidate name,
// and separately inspect all daemon mounts, primary homes and retained attempts.
type CandidateHomeObservation struct {
	DaemonID, Endpoint, PrimaryID, CandidateID, CandidateName string
	Labels                                                    map[string]string
	Mounts                                                    []CandidateHomeMount
}

// CandidateHomeIdentity is a copy of the intended scope and actual candidate
// identity for a later private receipt. It is not a persisted receipt or a stage
// verification ticket.
type CandidateHomeIdentity struct {
	Plan                       CandidateHomePlan
	CandidateID, CandidateName string
}

// CandidateHomeIntent is an immutable, non-effectful home plan. Its zero value
// cannot admit a binding. Before allocation the stage orchestrator must hold the
// daemon lock, check complete fresh collisions, fsync its intent, recheck absence,
// create and correlate fresh volume metadata with operation/nonce/driver/creation
// observation. This library performs none of those actions or ownership proofs.
type CandidateHomeIntent struct{ plan CandidateHomePlan }

// CandidateHomeBinding retains only immutable value identities, not caller maps
// or slices. Matching observations do not prove exclusive allocation, full-daemon
// isolation or protection against a same-Docker-user race.
type CandidateHomeBinding struct{ identity CandidateHomeIdentity }

// NewCandidateHomeIntent draws 128 bits from crypto/rand, independently of the
// caller's operation ID. It performs no Docker, filesystem, hook or CLI actions.
// Invalid scope or a conflicting generated identity returns conflict (68), with
// no retry. Failure to obtain all entropy returns a sanitized internal error (74).
func NewCandidateHomeIntent(scope CandidateHomeScope) (CandidateHomeIntent, error) {
	return newCandidateHomeIntent(scope, rand.Reader)
}

func newCandidateHomeIntent(scope CandidateHomeScope, random io.Reader) (CandidateHomeIntent, error) {
	if !candidateHomeScopeValid(scope) {
		return CandidateHomeIntent{}, candidateHomeConflict()
	}
	var nonce [16]byte
	if random == nil {
		return CandidateHomeIntent{}, exitcode.Errorf(exitcode.PathUnclassified, "candidate home entropy unavailable")
	}
	if _, err := io.ReadFull(random, nonce[:]); err != nil {
		return CandidateHomeIntent{}, exitcode.Errorf(exitcode.PathUnclassified, "candidate home entropy unavailable")
	}
	id := hex.EncodeToString(nonce[:])
	name := "whr-stage-home-" + id
	if id == scope.OperationID || name == scope.PrimaryHome {
		return CandidateHomeIntent{}, candidateHomeConflict()
	}
	return CandidateHomeIntent{plan: CandidateHomePlan{
		OperationID: scope.OperationID, Nonce: id, DaemonID: scope.DaemonID,
		Endpoint: scope.Endpoint, PrimaryID: scope.PrimaryID, PrimaryHome: scope.PrimaryHome,
		Mount: CandidateHomeMount{Type: "volume", Name: name, Destination: scope.Destination},
	}}, nil
}

// Plan returns a copy; editing it cannot change the intent or an existing binding.
func (i CandidateHomeIntent) Plan() CandidateHomePlan { return i.plan }

// Bind admits the exact fixed candidate name/role, full distinct container ID
// and writable home mount against this intent. It does not inspect a daemon or
// persist a receipt. A caller must correlate the freshly allocated volume and
// durable intent separately before starting or cleaning up any resource.
func (i CandidateHomeIntent) Bind(observed CandidateHomeObservation) (CandidateHomeBinding, error) {
	if i.plan.Nonce == "" || observed.DaemonID != i.plan.DaemonID || observed.Endpoint != i.plan.Endpoint ||
		observed.PrimaryID != i.plan.PrimaryID || !fullID(observed.CandidateID) || observed.CandidateID == i.plan.PrimaryID ||
		observed.CandidateName != "whr-anchorage-staging" || observed.Labels["anchorage-staging"] != "1" {
		return CandidateHomeBinding{}, candidateHomeConflict()
	}
	role, recognized := workspaceRole(observed.Labels)
	if !recognized || role != "staging" {
		return CandidateHomeBinding{}, candidateHomeConflict()
	}
	for _, label := range []string{"anchorage-temp", "anchorage-primary"} {
		if _, ambiguous := observed.Labels[label]; ambiguous {
			return CandidateHomeBinding{}, candidateHomeConflict()
		}
	}
	matches := 0
	for _, mount := range observed.Mounts {
		if mount.Name != i.plan.Mount.Name && mount.Destination != "/home/captain" && mount.Destination != "/home/vscode" {
			continue
		}
		if mount != i.plan.Mount {
			return CandidateHomeBinding{}, candidateHomeConflict()
		}
		matches++
	}
	if matches != 1 {
		return CandidateHomeBinding{}, candidateHomeConflict()
	}
	return CandidateHomeBinding{identity: CandidateHomeIdentity{
		Plan: i.plan, CandidateID: observed.CandidateID, CandidateName: observed.CandidateName,
	}}, nil
}

// Identity returns an independent value for a future receipt writer.
func (b CandidateHomeBinding) Identity() CandidateHomeIdentity { return b.identity }

// Check refuses identity or mount drift, including a replacement candidate that
// has the same name and labels. It is a pure comparison, never a deletion gate
// by itself: complete fresh daemon metadata and all FR-067 guards still apply.
func (b CandidateHomeBinding) Check(observed CandidateHomeObservation) error {
	if b.identity.CandidateID == "" || observed.CandidateID != b.identity.CandidateID {
		return candidateHomeConflict()
	}
	_, err := (CandidateHomeIntent{plan: b.identity.Plan}).Bind(observed)
	return err
}

func candidateHomeScopeValid(scope CandidateHomeScope) bool {
	if !lowerHex32(scope.OperationID) || !candidateHomeText(scope.DaemonID, 1024) || !fullID(scope.PrimaryID) ||
		!volumeName(scope.PrimaryHome) || len(scope.PrimaryHome) > 1024 ||
		(scope.Destination != "/home/captain" && scope.Destination != "/home/vscode") ||
		!candidateHomeText(scope.Endpoint, 4096) {
		return false
	}
	u, err := url.Parse(scope.Endpoint)
	return err == nil && u.Scheme == "unix" && u.Host == "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery &&
		u.Fragment == "" && u.RawPath == "" && u.Opaque == "" && u.Path != "/" && filepath.IsAbs(u.Path) &&
		filepath.Clean(u.Path) == u.Path && scope.Endpoint == "unix://"+u.Path
}

func lowerHex32(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func candidateHomeText(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func candidateHomeConflict() error {
	return exitcode.Errorf(exitcode.PathConflict, "candidate home identity or mount binding refused")
}
