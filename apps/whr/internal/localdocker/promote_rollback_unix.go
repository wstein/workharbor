//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

func (p *LocalPromotion) failedPrimaryName() string {
	return "whr-failed-primary-" + p.receipt.OperationID
}

// Only a reaped, known nonzero hook/smoke exit or observed unhealthy replacement
// reaches this routine. Resource
// recovery preserves both writable layers until original restart is verified.
// Successful recovery then disposes of the failed replacement and candidate.
// It cannot reverse shared data writes.
func (p *LocalPromotion) restoreOriginalResource() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if !p.originalRetained || p.originalRemoved || p.primaryVerified || p.newPrimary == nil {
		return stageConflict("resource recovery state is not eligible")
	}
	check := func(oldName, replacementName string, oldRunning, replacementRunning bool) error {

		if err := p.checkConfiguration(); err != nil {
			return err
		}
		if err := p.checkPersistedConfiguration(); err != nil {
			return err
		}
		metadata, err := readStageMetadata(ctx, p.runner)
		if err != nil {
			return err
		}
		if err = p.checkPromotionInputs(ctx, metadata); err != nil {
			return err
		}
		for _, effect := range p.result.Effects {
			if effect.State == "uncertain" || effect.State == "attempted" {
				return stageConflict("resource recovery has an unobserved effect")
			}
		}
		original, err := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if err != nil {
			return err
		}
		if original.Name != "/"+oldName || original.Running != oldRunning {
			return stageConflict("resource recovery original state changed")
		}
		original.Name = p.primaryObservation.Name
		if stageContainerBindingDigest(original) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("resource recovery original binding changed")
		}
		replacement, err := readStageContainer(ctx, p.runner, p.newPrimary.ID)
		if err != nil {
			return err
		}
		if replacement.Name != "/"+replacementName || replacement.Running != replacementRunning {
			return stageConflict("resource recovery replacement state changed")
		}
		replacement.Name = p.newPrimary.Name
		if stageContainerBindingDigest(replacement) != stageContainerBindingDigest(*p.newPrimary) {
			return stageConflict("resource recovery replacement binding changed")
		}
		for _, item := range metadata.Containers {
			name := strings.TrimPrefix(item.Name, "/")
			if (name == p.primaryConfig.Name && item.ID != p.primaryObservation.ID && item.ID != p.newPrimary.ID) || (name == p.failedPrimaryName() && item.ID != p.newPrimary.ID) {
				return stageConflict("resource recovery name occupied")
			}
			if item.ID == p.primaryObservation.ID || item.ID == p.newPrimary.ID {
				continue
			}
			for _, m := range item.Mounts {
				if m.Type == "volume" && m.Name == p.primaryConfig.Home {
					return stageConflict("resource recovery shared primary mount")
				}
				if m.Type == "bind" && (m.RW || m.readOnlyUnknown) {
					overlap, err := promotionRecoveryPathsOverlap(p.receipt.OriginalSource, m.Source)
					if err != nil {
						return err
					}
					if overlap {
						return stageConflict("resource recovery shared primary mount")
					}
				}
			}
		}
		return nil
	}
	retained, failed := p.retainedPrimaryName(), p.failedPrimaryName()
	oldName, newName := retained, p.primaryConfig.Name
	if err := check(oldName, newName, false, true); err != nil {
		return err
	}
	// Each new action is durable before launch. Any failed native command or
	// observation stops recovery; no second attempt or destructive compensation.
	effect := func(action, id, name string, args []string, observe func() error) error {
		target := &StageResource{Kind: "container", ID: stageString(id), Name: stageString(name), DaemonID: p.endpoint.DaemonID}
		p.result.Effects = append(p.result.Effects, StageEffect{Action: action, Target: StageTarget{Resource: target}, State: "planned"})
		return p.resourceEffect(ctx, action, target, 30*time.Second, args, observe)
	}
	if err := effect("stop_failed_primary", p.newPrimary.ID, newName, []string{"container", "stop", "--time", "10", "--", p.newPrimary.ID}, func() error {
		// The current action is observed only after exact postconditions are checked.
		got, err := readStageContainer(ctx, p.runner, p.newPrimary.ID)
		if err != nil {
			return err
		}
		if got.Running || stageContainerBindingDigest(got) != stageContainerBindingDigest(*p.newPrimary) {
			return stageConflict("resource recovery replacement stop changed")
		}
		return nil
	}); err != nil {
		return err
	}
	if err := check(oldName, newName, false, false); err != nil {
		return err
	}
	if err := effect("retain_failed_primary", p.newPrimary.ID, failed, []string{"container", "rename", "--", p.newPrimary.ID, failed}, func() error {
		got, err := readStageContainer(ctx, p.runner, p.newPrimary.ID)
		if err != nil {
			return err
		}
		if got.Running || got.Name != "/"+failed {
			return stageConflict("resource recovery replacement rename changed")
		}
		got.Name = p.newPrimary.Name
		if stageContainerBindingDigest(got) != stageContainerBindingDigest(*p.newPrimary) {
			return stageConflict("resource recovery replacement binding changed")
		}
		return nil
	}); err != nil {
		return err
	}
	newName = failed
	if err := check(oldName, newName, false, false); err != nil {
		return err
	}
	if err := effect("restore_primary_name", p.primaryObservation.ID, p.primaryConfig.Name, []string{"container", "rename", "--", p.primaryObservation.ID, p.primaryConfig.Name}, func() error {
		got, err := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if err != nil {
			return err
		}
		if got.Running || got.Name != p.primaryObservation.Name || stageContainerBindingDigest(got) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("resource recovery original rename changed")
		}
		return nil
	}); err != nil {
		return err
	}
	oldName = p.primaryConfig.Name
	if err := check(oldName, newName, false, false); err != nil {
		return err
	}
	if err := effect("restart_original_primary", p.primaryObservation.ID, oldName, []string{"container", "start", "--", p.primaryObservation.ID}, func() error {
		got, err := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if err != nil {
			return err
		}
		if !got.Running || stageContainerBindingDigest(got) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("resource recovery original restart changed")
		}
		return nil
	}); err != nil {
		return err
	}
	if err := check(oldName, newName, true, false); err != nil {
		return err
	}
	p.rollbackRestored = true
	p.result.Primary = p.receipt.Primary
	return nil
}

// A foreign bind can expose either the workspace or any part of it. Compare
// physical paths in both directions; string prefixes confuse sibling names.
func promotionRecoveryPathsOverlap(workspace, source string) (bool, error) {
	workspace, err := filepath.EvalSymlinks(workspace)
	if err != nil || !filepath.IsAbs(workspace) {
		return false, stageConflict("resource recovery workspace source unavailable")
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil || !filepath.IsAbs(source) {
		return false, stageConflict("resource recovery bind source unavailable")
	}
	for _, pair := range [][2]string{{workspace, source}, {source, workspace}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			return false, stageConflict("resource recovery bind source unavailable")
		}
		if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return true, nil
		}
	}
	return false, nil
}
