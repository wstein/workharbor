//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"time"
)

// This records a fresh absence observation after separately completed guarded
// removal. It does not resume, reverify, or remove any prior operation resource.
type stageRemovedObservation struct {
	OperationID   string                `json:"operation_id"`
	ReceiptDigest string                `json:"receipt_digest"`
	CandidateID   string                `json:"candidate_id"`
	Home          string                `json:"home"`
	Endpoint      stageEndpointIdentity `json:"endpoint"`
	ObservedAt    string                `json:"observed_at"`
}

func (s *LocalStage) archiveRemovedUnverified(ctx context.Context) error {
	rows, err := s.store.Records()
	if err != nil || len(rows) == 0 {
		return err
	}
	last := rows[len(rows)-1]
	if last.State != "staged_unverified" || last.Observation != "observed" {
		return nil
	}
	receipt, err := s.store.loadReceipt(last.OperationID)
	if err != nil {
		return err
	}
	if receipt.EndpointIdentity != s.endpoint || receipt.Candidate == nil || receipt.Candidate.ID == nil || !fullID(*receipt.Candidate.ID) || receipt.Home == nil || receipt.Home.Name == nil || receipt.Home.ID == nil || *receipt.Home.ID != *receipt.Home.Name {
		return stageConflict("removed unverified receipt resource binding invalid")
	}
	if receipt.UnverifiedReason == nil || *receipt.UnverifiedReason == "missing_smoke" && !s.options.Smoke {
		return stageConflict("fresh stage requires explicit smoke after missing smoke")
	}
	for _, effect := range receipt.ActionInventory {
		if effect.State == "attempted" || effect.State == "uncertain" {
			return stageConflict("retained attempt requires owner reconciliation")
		}
	}
	var execution stageExecutionBinding
	if err = readPromotionArtifact(s.store, last.OperationID, "execution-binding.json", receipt.ExecutionManifestDigest, &execution); err != nil {
		return err
	}
	var settings stageSettings
	if err = readPromotionArtifact(s.store, last.OperationID, "stage-settings.json", execution.SettingsDigest, &settings); err != nil {
		return err
	}
	var plan CandidateHomePlan
	if err = readPromotionArtifact(s.store, last.OperationID, "candidate-home.json", settings.HomePlanDigest, &plan); err != nil {
		return err
	}
	var home stageVolumeDetails
	if err = readPromotionArtifact(s.store, last.OperationID, "candidate-home-observation.json", settings.HomeObservationDigest, &home); err != nil {
		return err
	}
	if plan.Mount.Name != *receipt.Home.Name || home.Name != plan.Mount.Name || home.Labels["workharbor.stage.operation"] != receipt.OperationID || home.Labels["workharbor.stage.nonce"] != plan.Nonce {
		return stageConflict("removed unverified home correlation changed")
	}
	metadata, err := readStageMetadata(ctx, s.runner)
	if err != nil {
		return err
	}
	if metadata.Daemon.ID != s.endpoint.DaemonID {
		return stageConflict("daemon identity changed")
	}
	for _, container := range metadata.Containers {
		if container.ID == *receipt.Candidate.ID {
			return stageConflict("unverified candidate still exists")
		}
	}
	names, err := readStageVolumeNames(ctx, s.runner)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == home.Name {
			return stageConflict("unverified candidate home still exists")
		}
	}
	observation := stageRemovedObservation{OperationID: receipt.OperationID, ReceiptDigest: stageReceiptDigest(receipt), CandidateID: *receipt.Candidate.ID, Home: home.Name, Endpoint: s.endpoint, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	data, err := stageArtifactBytes(observation)
	if err != nil {
		return err
	}
	if err = s.store.writeArtifact(receipt.OperationID, "removed-observation.json", data, true); err != nil {
		return err
	}
	return s.store.archiveComplete(rows)
}
