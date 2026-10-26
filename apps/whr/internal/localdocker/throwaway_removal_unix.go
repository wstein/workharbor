// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
)

type candidateRemoval struct {
	candidateDisposal
	root string
}

func (r *candidateRemoval) Close() error {
	r.store.Close()
	if err := os.RemoveAll(r.root); err != nil {
		return stageConflict("candidate cleanup temporary storage remains")
	}
	return nil
}

func stageStateRoot() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", stageConflict("private state home unavailable")
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) {
		return "", stageConflict("private state home must be absolute")
	}
	return filepath.Join(base, "workharbor", "local-stage", "v1"), nil
}

func (c Client) prepareCandidateRemoval(ctx context.Context, endpoint string, all []inspection, options RemovalOptions, stateRoot string) (*candidateRemoval, error) {
	// Classify the exact selector before consulting private stage state. Old or
	// externally created staging containers keep the generic removal contract.
	selected := []inspection{}
	matched := false
	key, value, hasValue := strings.Cut(options.Label, "=")
	for _, item := range all {
		role, known := workspaceRole(item.Labels)
		match := options.Target != "" && (item.ID == options.Target || strings.TrimPrefix(item.Name, "/") == options.Target)
		if options.Role != "" {
			match = known && role == options.Role
		}
		if options.Label != "" {
			v, exists := item.Labels[key]
			match = exists && (!hasValue || v == value)
		}
		if match {
			matched = true
		}
		if match && item.Labels["workharbor.stage.operation"] != "" && role == "staging" {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 && (matched || options.Target == "") {
		return nil, nil
	}
	if len(selected) > 1 {
		return nil, stageConflict("candidate cleanup selection ambiguous")
	}
	if stateRoot == "" {
		var err error
		stateRoot, err = stageStateRoot()
		if err != nil {
			return nil, err
		}
	}
	if _, err := os.Lstat(stateRoot); err != nil {
		if len(selected) == 0 && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, stageConflict("candidate cleanup private receipt unavailable")
	}
	root, err := os.MkdirTemp("", "whr-candidate-remove-")
	if err != nil {
		return nil, stageConflict("candidate cleanup private phase unavailable")
	}
	if err = os.Chmod(root, 0700); err != nil {
		os.RemoveAll(root)
		return nil, stageConflict("candidate cleanup private phase mode")
	}
	var store *stageStore
	keep := false
	defer func() {
		if !keep {
			if store != nil {
				store.Close()
			}
			os.RemoveAll(root)
		}
	}()
	docker := c.Executable
	if docker == "" {
		docker, err = exec.LookPath("docker")
		if err != nil {
			return nil, stageConflict("candidate cleanup Docker unavailable")
		}
	}
	runner, err := newStageRunner(ctx, root, endpoint, map[string]string{"docker": docker})
	if err != nil {
		return nil, err
	}
	metadata, err := readStageMetadata(ctx, runner)
	if err != nil {
		return nil, err
	}
	identity, err := stageSocketIdentity(endpoint)
	if err != nil {
		return nil, err
	}
	identity.DaemonID = metadata.Daemon.ID
	store, err = openStageStore(stateRoot, metadata.Daemon.ID, endpoint)
	if err != nil {
		return nil, err
	}
	op := ""
	if len(selected) > 0 {
		op = selected[0].Labels["workharbor.stage.operation"]
	} else {
		rows, err := store.Records()
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		op = rows[len(rows)-1].OperationID
	}
	rows, err := store.Records()
	if err != nil || len(rows) == 0 {
		return nil, stageConflict("candidate cleanup journal unavailable")
	}
	last := rows[len(rows)-1]
	var receipt stageReceipt
	var plan CandidateHomePlan
	var failed *stageContainerDetails
	if last.Cleanup != nil {
		receipt = last.Cleanup.Receipt
		plan = last.Cleanup.Plan
		failed = last.Cleanup.FailedReplacement
		if last.OperationID != receipt.OperationID || last.OperationID != plan.OperationID || last.ReceiptDigest != stageReceiptDigest(receipt) {
			return nil, stageConflict("candidate cleanup journal binding changed")
		}
	} else {
		receipt, err = store.loadReceipt(op)
		if err != nil {
			return nil, err
		}
		var execution stageExecutionBinding
		if err = readPromotionArtifact(store, op, "execution-binding.json", receipt.ExecutionManifestDigest, &execution); err != nil {
			return nil, err
		}
		var settings stageSettings
		if err = readPromotionArtifact(store, op, "stage-settings.json", execution.SettingsDigest, &settings); err != nil {
			return nil, err
		}
		if err = readPromotionArtifact(store, op, "candidate-home.json", settings.HomePlanDigest, &plan); err != nil {
			return nil, err
		}
	}
	if len(selected) == 0 {
		match := receipt.Candidate != nil && ((receipt.Candidate.ID != nil && options.Target == *receipt.Candidate.ID) || (receipt.Candidate.Name != nil && options.Target == *receipt.Candidate.Name))
		if !match {
			return nil, nil
		}
	}
	if receipt.EndpointIdentity != identity || receipt.Candidate == nil || (len(selected) > 0 && (receipt.Candidate.ID == nil || *receipt.Candidate.ID != selected[0].ID)) {
		return nil, stageConflict("candidate cleanup receipt identity changed")
	}
	disposal := candidateDisposal{store: store, runner: runner, endpoint: identity, receipt: &receipt, plan: plan, failed: failed}
	candidate, err := disposal.candidate(ctx)
	if err != nil {
		return nil, err
	}
	if len(selected) > 0 && (candidate == nil || candidate.ID != selected[0].ID) {
		return nil, stageConflict("candidate cleanup target changed")
	}
	keep = true
	return &candidateRemoval{candidateDisposal: disposal, root: root}, nil
}

func (r *Removal) applyCandidate(parent context.Context, result RemovalResult) (RemovalResult, error) {
	defer r.Close()
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	all, err := r.client.removalSnapshot(ctx, r.endpoint)
	if err == nil {
		var fresh removalPlan
		fresh, err = r.candidate.removalPlan(ctx, all, r.options)
		if err == nil && !sameRemovalTargets(r.plan, fresh) {
			err = stageConflict("local removal targets changed since preview")
		}
	}
	if err != nil {
		return result, err
	}
	// Other selected workspaces keep the generic removal contract. Their
	// homes are preserved unless their own ordinary deletion flags were given.
	for _, item := range r.plan.Containers {
		if r.candidate.receipt.Candidate.ID != nil && item.ID == *r.candidate.receipt.Candidate.ID {
			continue
		}
		other, prepareErr := r.client.prepareRemovalAtEndpoint(ctx, r.endpoint, RemovalOptions{Target: item.ID, Volumes: r.options.Volumes, Force: r.options.Force}, "")
		if prepareErr != nil {
			return result, prepareErr
		}
		removed, removeErr := other.Apply(ctx)
		_ = other.Close()
		for _, done := range removed.Actions {
			for i := range result.Actions {
				a := &result.Actions[i]
				if a.Kind == done.Kind && a.Target == done.Target {
					*a = done
				}
			}
		}
		if removeErr != nil {
			failure := exitcode.Errorf(exitcode.PathPartialFailure, "local removal incomplete: %s", terminal.Escape(removeErr.Error()))
			failure.Result = result
			return result, failure
		}
	}
	r.candidate.observe = func(action string, target *StageResource, state string) {
		kind := "container"
		value := ""
		if target.ID != nil {
			value = *target.ID
		}
		if action == "remove_home" {
			kind = "volume"
			if target.Name != nil {
				value = *target.Name
			}
		}
		for i := range result.Actions {
			a := &result.Actions[i]
			if a.Kind == kind && a.Target == value {
				a.Attempted = true
				a.Applied = state == "observed"
			}
		}
	}
	err = r.candidate.begin(ctx)
	if err == nil {
		err = r.candidate.removeResources(ctx)
	}
	if err == nil {
		for i := range result.Actions {
			if result.Actions[i].Kind == "container" || result.Actions[i].Kind == "volume" {
				result.Actions[i].Applied = true
			}
		}
		err = r.candidate.store.discardCandidateFilesObserved(r.candidate.plan.OperationID, func(kind, state string) {
			for i := range result.Actions {
				a := &result.Actions[i]
				if a.Kind == kind {
					a.Attempted = true
					a.Applied = state == "observed"
				}
			}
		})
		if err == nil {
			for i := len(result.Actions) - 2; i < len(result.Actions); i++ {
				result.Actions[i].Attempted = true
				result.Actions[i].Applied = true
			}
		}
	}
	if err != nil {
		// Fresh absence is evidence of completed resource disposal even if a later
		// action failed. Never remove the receipt to hide unfinished cleanup.
		metadata, observed := r.candidate.metadata(ctx)
		if observed == nil {
			for i := range result.Actions {
				a := &result.Actions[i]
				if a.Kind != "container" {
					continue
				}
				present := false
				for _, item := range metadata.Containers {
					if item.ID == a.Target {
						present = true
					}
				}
				a.Applied = !present
			}
		}
		failure := exitcode.Errorf(exitcode.PathPartialFailure, "local removal incomplete: %s", r.candidate.residual(err))
		failure.Result = result
		return result, failure
	}
	return result, nil
}

func (r *candidateRemoval) removalPlan(ctx context.Context, all []inspection, options RemovalOptions) (removalPlan, error) {
	candidate, err := r.candidate(ctx)
	if err != nil {
		return removalPlan{}, err
	}
	if candidate != nil {
		return planRemovalWithCandidate(all, options, r.plan.Mount.Name)
	}
	plan := removalPlan{Containers: []inspection{}, Volumes: []removalVolume{}, PreserveHomeVolume: false}
	for _, item := range all {
		for _, m := range item.Mounts {
			if m.Type == "volume" && m.Name == r.plan.Mount.Name {
				return removalPlan{}, stageConflict("candidate cleanup home remains mounted")
			}
		}
	}
	names, err := readStageVolumeNames(ctx, r.runner)
	if err != nil {
		return removalPlan{}, err
	}
	for _, name := range names {
		if name == r.plan.Mount.Name {
			home, err := readStageVolume(ctx, r.runner, name)
			if err != nil {
				return removalPlan{}, err
			}
			if home.Driver != "local" || home.Labels["workharbor.stage.operation"] != r.plan.OperationID || home.Labels["workharbor.stage.nonce"] != r.plan.Nonce {
				return removalPlan{}, stageConflict("candidate cleanup home allocation binding changed")
			}
			plan.Volumes = append(plan.Volumes, removalVolume{Name: name, Mounts: []mount{}})
		}
	}
	return plan, nil
}
