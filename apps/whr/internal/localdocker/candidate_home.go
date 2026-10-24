// SPDX-License-Identifier: EUPL-1.2
package localdocker

import "io"

// These compiling signatures establish the first-unit library contract for its
// red-first tests. The following behavior commit implements admission and binding.
type CandidateHomeScope struct {
	OperationID, DaemonID, Endpoint, PrimaryID, PrimaryHome, Destination string
}
type CandidateHomeMount struct {
	Type, Name, Destination string
	ReadOnly                bool
}
type CandidateHomePlan struct {
	OperationID, Nonce, DaemonID, Endpoint, PrimaryID, PrimaryHome string
	Mount                                                          CandidateHomeMount
}
type CandidateHomeObservation struct {
	DaemonID, Endpoint, PrimaryID, CandidateID, CandidateName string
	Labels                                                    map[string]string
	Mounts                                                    []CandidateHomeMount
}
type CandidateHomeIdentity struct {
	Plan                       CandidateHomePlan
	CandidateID, CandidateName string
}
type CandidateHomeIntent struct{ plan CandidateHomePlan }
type CandidateHomeBinding struct{ identity CandidateHomeIdentity }

func NewCandidateHomeIntent(scope CandidateHomeScope) (CandidateHomeIntent, error) {
	return CandidateHomeIntent{}, nil
}
func newCandidateHomeIntent(scope CandidateHomeScope, random io.Reader) (CandidateHomeIntent, error) {
	return CandidateHomeIntent{}, nil
}
func (i CandidateHomeIntent) Plan() CandidateHomePlan { return i.plan }
func (i CandidateHomeIntent) Bind(observed CandidateHomeObservation) (CandidateHomeBinding, error) {
	return CandidateHomeBinding{}, nil
}
func (b CandidateHomeBinding) Identity() CandidateHomeIdentity               { return b.identity }
func (b CandidateHomeBinding) Check(observed CandidateHomeObservation) error { return nil }
