// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"golang.org/x/sys/unix"
)

// Candidate disposal uses the existing private intent and fresh daemon facts.
// A name prefix or Docker label alone never admits a resource for deletion.
type candidateDisposal struct {
	store    *stageStore
	runner   *stageRunner
	endpoint stageEndpointIdentity
	receipt  *stageReceipt
	plan     CandidateHomePlan
	failed   *stageContainerDetails
	observe  func(string, *StageResource, string)
}

// The existing journal holds the cleanup binding until its final unlink. This
// is unfinished disposal metadata, never a completed-candidate archive.
type candidateCleanupBinding struct {
	Receipt           stageReceipt           `json:"receipt"`
	Plan              CandidateHomePlan      `json:"plan"`
	FailedReplacement *stageContainerDetails `json:"failed_replacement"`
}

func (d candidateDisposal) begin() error {
	rows, err := d.store.Records()
	if err != nil || len(rows) == 0 {
		return stageConflict("candidate cleanup durable intent unavailable")
	}
	last := rows[len(rows)-1]
	if last.OperationID != d.plan.OperationID {
		return stageConflict("candidate cleanup operation changed")
	}
	binding := candidateCleanupBinding{Receipt: *d.receipt, Plan: d.plan, FailedReplacement: d.failed}
	if last.Cleanup != nil {
		if stageDigest(*last.Cleanup) != stageDigest(binding) {
			return stageConflict("candidate cleanup binding changed")
		}
		return nil
	}
	if len(rows) >= stageMaxJournalRecords {
		return stageConflict("candidate cleanup journal quota")
	}
	rows = append(rows, stageRecord{Version: 1, OperationID: last.OperationID, Sequence: last.Sequence + 1, PreviousHash: last.Hash, State: last.State, Fingerprint: last.Fingerprint, Action: "dispose_candidate", Observation: "attempted", ReceiptDigest: stageReceiptDigest(*d.receipt), Cleanup: &binding})
	return d.store.write(rows, false)
}

func (d candidateDisposal) effect(ctx context.Context, action string, resource *StageResource, args []string, absent func() error) error {
	if d.observe != nil {
		d.observe(action, resource, "attempted")
	}
	_, err := d.runner.Run(ctx, action, "docker", args, nil, 30*time.Second)
	if err == nil {
		err = absent()
	}
	state := "observed"
	if err != nil {
		state = "uncertain"
	}
	if d.observe != nil {
		d.observe(action, resource, state)
	}
	return err
}

func (d candidateDisposal) metadata(ctx context.Context) (stageMetadata, error) {
	identity, err := stageSocketIdentity(d.endpoint.Endpoint)
	if err != nil || identity.SocketDevice != d.endpoint.SocketDevice || identity.SocketInode != d.endpoint.SocketInode {
		return stageMetadata{}, stageConflict("candidate cleanup socket identity changed")
	}
	metadata, err := readStageMetadata(ctx, d.runner)
	if err != nil {
		return metadata, err
	}
	if metadata.Daemon.ID != d.endpoint.DaemonID || d.plan.DaemonID != d.endpoint.DaemonID || d.plan.Endpoint != d.endpoint.Endpoint || d.plan.OperationID != d.receipt.OperationID || !lowerHex32(d.plan.Nonce) || d.plan.Mount.Name != "whr-stage-home-"+d.plan.Nonce || d.plan.Mount.Name == d.plan.PrimaryHome || strings.HasPrefix(d.plan.Mount.Name, "anchorage-home-") {
		return metadata, stageConflict("candidate cleanup intent binding changed")
	}
	return metadata, nil
}

func (d candidateDisposal) candidate(ctx context.Context) (*inspection, error) {
	metadata, err := d.metadata(ctx)
	if err != nil {
		return nil, err
	}
	var found *inspection
	for _, item := range metadata.Containers {
		boundID := d.receipt.Candidate != nil && d.receipt.Candidate.ID != nil && *d.receipt.Candidate.ID == item.ID
		if !boundID && item.Labels["workharbor.stage.operation"] != d.plan.OperationID {
			continue
		}
		// A promoted primary carries the stage operation too, but never the staging role.
		role, _ := workspaceRole(item.Labels)
		if !boundID && role != "staging" {
			continue
		}
		if found != nil {
			return nil, stageConflict("candidate cleanup identity ambiguous")
		}
		if item.Labels["workharbor.stage.operation"] != d.plan.OperationID || (d.receipt.Candidate != nil && d.receipt.Candidate.ID != nil && *d.receipt.Candidate.ID != item.ID) {
			return nil, stageConflict("candidate cleanup container binding changed")
		}
		mounts := []CandidateHomeMount{}
		for _, m := range item.Mounts {
			mounts = append(mounts, CandidateHomeMount{Type: m.Type, Name: m.Name, Destination: m.Destination, ReadOnly: !m.RW})
		}
		if _, err = (CandidateHomeIntent{plan: d.plan}).Bind(CandidateHomeObservation{DaemonID: d.endpoint.DaemonID, Endpoint: d.endpoint.Endpoint, PrimaryID: d.plan.PrimaryID, CandidateID: item.ID, CandidateName: strings.TrimPrefix(item.Name, "/"), Labels: item.Labels, Mounts: mounts}); err != nil {
			return nil, err
		}
		// Reuse the whole primary/shared/read-only/tool-volume guard without
		// manufacturing user --volumes or --force consent.
		if _, err = planRemovalWithCandidate(metadata.Containers, RemovalOptions{Target: item.ID}, d.plan.Mount.Name); err != nil {
			return nil, err
		}
		copy := item
		found = &copy
	}
	return found, nil
}

func (d candidateDisposal) removeResources(ctx context.Context) error {
	if d.failed != nil {
		metadata, err := d.metadata(ctx)
		if err != nil {
			return err
		}
		var failed *inspection
		originalRunning := false
		for _, item := range metadata.Containers {
			if item.ID == d.plan.PrimaryID {
				originalRunning = item.State == "running"
			}
			if item.ID == d.failed.ID {
				copy := item
				failed = &copy
			}
		}
		if failed != nil {
			if !originalRunning || failed.State == "running" {
				return stageConflict("candidate cleanup rollback resources changed")
			}
			observed, err := readStageContainer(ctx, d.runner, d.failed.ID)
			if err != nil {
				return err
			}
			if stageContainerBindingDigest(observed) != stageContainerBindingDigest(*d.failed) {
				return stageConflict("candidate cleanup failed replacement binding changed")
			}
			resource := &StageResource{Kind: "container", ID: stageString(d.failed.ID), Name: stageString(strings.TrimPrefix(d.failed.Name, "/")), DaemonID: d.endpoint.DaemonID}
			if err = d.effect(ctx, "remove_primary", resource, []string{"container", "rm", "--", d.failed.ID}, func() error {
				latest, err := d.metadata(ctx)
				if err != nil {
					return err
				}
				for _, item := range latest.Containers {
					if item.ID == d.failed.ID {
						return stageConflict("candidate cleanup failed replacement remains")
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	candidate, err := d.candidate(ctx)
	if err != nil {
		return err
	}
	if candidate != nil {
		resource := &StageResource{Kind: "container", ID: stageString(candidate.ID), Name: stageString(strings.TrimPrefix(candidate.Name, "/")), DaemonID: d.endpoint.DaemonID}
		if err = d.effect(ctx, "remove_candidate", resource, []string{"container", "rm", "--force", "--volumes", "--", candidate.ID}, func() error {
			remaining, err := d.candidate(ctx)
			if err != nil {
				return err
			}
			if remaining != nil {
				return stageConflict("candidate cleanup container remains")
			}
			return nil
		}); err != nil {
			return err
		}
	}
	metadata, err := d.metadata(ctx)
	if err != nil {
		return err
	}
	for _, item := range metadata.Containers {
		for _, m := range item.Mounts {
			if m.Type == "volume" && m.Name == d.plan.Mount.Name {
				return stageConflict("candidate cleanup home remains mounted")
			}
		}
	}
	names, err := readStageVolumeNames(ctx, d.runner)
	if err != nil {
		return err
	}
	exists := false
	for _, name := range names {
		if name == d.plan.Mount.Name {
			exists = true
		}
	}
	if exists {
		home, err := readStageVolume(ctx, d.runner, d.plan.Mount.Name)
		if err != nil {
			return err
		}
		if home.Driver != "local" || home.Labels["workharbor.stage.operation"] != d.plan.OperationID || home.Labels["workharbor.stage.nonce"] != d.plan.Nonce {
			return stageConflict("candidate cleanup home allocation binding changed")
		}
		resource := &StageResource{Kind: "volume", ID: stageString(home.Name), Name: stageString(home.Name), DaemonID: d.endpoint.DaemonID}
		if err = d.effect(ctx, "remove_home", resource, []string{"volume", "rm", "--", home.Name}, func() error {
			names, err := readStageVolumeNames(ctx, d.runner)
			if err != nil {
				return err
			}
			for _, name := range names {
				if name == home.Name {
					return stageConflict("candidate cleanup home remains")
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// Resources are confirmed absent before the clone/results, and records are last.
// The operation directory is opened through the already locked private store;
// Root.RemoveAll does not follow candidate-controlled symlinks out of that tree.
func (s *stageStore) discardCandidateFiles(op string) error {
	return s.discardCandidateFilesObserved(op, nil)
}
func (s *stageStore) discardCandidateFilesObserved(op string, observe func(string, string)) (failure error) {
	phase := "candidate_tree"
	if observe != nil {
		observe(phase, "attempted")
	}
	defer func() {
		if failure != nil && observe != nil {
			observe(phase, "uncertain")
		}
	}()
	var st unix.Stat_t
	if err := unix.Fstatat(int(s.dir.Fd()), op, &st, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		if observe != nil {
			observe(phase, "observed")
			phase = "candidate_records"
			observe(phase, "attempted")
		}
		if err = unix.Unlinkat(int(s.dir.Fd()), "journal.json", 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return stageConflict("candidate cleanup journal remains")
		}
		if err = s.dir.Sync(); err != nil {
			return stageConflict("candidate cleanup record deletion durability uncertain")
		}
		if observe != nil {
			observe(phase, "observed")
		}
		return nil
	}
	dir, err := s.operationDirectory(op)
	if err != nil {
		return err
	}
	defer dir.Close()
	root, err := os.OpenRoot(s.operationPath(op))
	if err != nil {
		return stageConflict("candidate cleanup operation tree unavailable")
	}
	defer root.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return stageConflict("candidate cleanup operation inventory unavailable")
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err = fs.WalkDir(root.FS(), entry.Name(), func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return root.Chmod(name, 0700)
			}
			return nil
		}); err != nil {
			return stageConflict("candidate cleanup tree permissions unavailable")
		}
		if err = root.RemoveAll(entry.Name()); err != nil {
			return stageConflict("candidate cleanup tree remains: " + terminal.Escape(entry.Name()))
		}
	}
	if observe != nil {
		observe(phase, "observed")
		phase = "candidate_records"
		observe(phase, "attempted")
	}
	// No candidate tree remains. Keep receipt and journal until every other
	// private artifact has been removed successfully.
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "receipt.json" || entry.Name() == "journal.json" {
			continue
		}
		if err = root.RemoveAll(entry.Name()); err != nil {
			return stageConflict("candidate cleanup metadata remains: " + terminal.Escape(entry.Name()))
		}
	}
	if err = root.RemoveAll("receipt.json"); err != nil {
		return stageConflict("candidate cleanup receipt remains")
	}
	if err = root.RemoveAll("journal.json"); err != nil {
		return stageConflict("candidate cleanup operation journal remains")
	}
	if err = unix.Unlinkat(int(s.dir.Fd()), op, unix.AT_REMOVEDIR); err != nil {
		return stageConflict("candidate cleanup operation directory remains")
	}
	if err = unix.Unlinkat(int(s.dir.Fd()), "journal.json", 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return stageConflict("candidate cleanup journal remains")
	}
	if err = s.dir.Sync(); err != nil {
		return stageConflict("candidate cleanup record deletion durability uncertain")
	}
	if observe != nil {
		observe(phase, "observed")
	}
	return nil
}

func (d candidateDisposal) dispose(ctx context.Context) error {
	if err := d.begin(); err != nil {
		return err
	}
	if err := d.removeResources(ctx); err != nil {
		return err
	}
	return d.store.discardCandidateFiles(d.plan.OperationID)
}

func (d candidateDisposal) residual(err error) string {
	return fmt.Sprintf("Candidate cleanup incomplete: %s. Inspect candidate/home and clone/results at %s; operation receipts/journal remain until resources and trees are absent.", terminal.Escape(err.Error()), terminal.Escape(filepath.Join(d.store.path, d.plan.OperationID)))
}

func (p *LocalPromotion) disposeAfterRollback() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if p.newPrimary == nil {
		return stageConflict("candidate cleanup failed replacement identity missing")
	}
	original, err := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
	if err != nil {
		return err
	}
	if !original.Running || original.Name != p.primaryObservation.Name || stageContainerBindingDigest(original) != stageContainerBindingDigest(p.primaryObservation) {
		return stageConflict("candidate cleanup restored primary binding changed")
	}
	failed, err := readStageContainer(ctx, p.runner, p.newPrimary.ID)
	if err != nil {
		return err
	}
	if failed.Running || failed.Name != "/"+p.failedPrimaryName() {
		return stageConflict("candidate cleanup failed replacement binding changed")
	}
	failedName := failed.Name
	failed.Name = p.newPrimary.Name
	if stageContainerBindingDigest(failed) != stageContainerBindingDigest(*p.newPrimary) {
		return stageConflict("candidate cleanup failed replacement binding changed")
	}
	failed.Name = failedName
	disposal := candidateDisposal{store: p.store, runner: p.runner, endpoint: p.endpoint, receipt: &p.receipt, plan: p.homePlan, failed: &failed, observe: func(action string, target *StageResource, state string) {
		recordCleanupEffect(&p.result.Effects, action, target, state)
	}}
	p.disposalStarted = true
	if err = disposal.dispose(ctx); err != nil {
		return err
	}
	p.candidateDisposed = true
	return nil
}

func recordCleanupEffect(effects *[]StageEffect, action string, target *StageResource, state string) {
	for i := range *effects {
		item := &(*effects)[i]
		if item.Action == action && item.Target.Resource != nil && item.Target.Resource.ID != nil && target.ID != nil && *item.Target.Resource.ID == *target.ID {
			item.State = state
			return
		}
	}
	*effects = append(*effects, StageEffect{Action: action, Target: StageTarget{Resource: target}, State: state})
}
