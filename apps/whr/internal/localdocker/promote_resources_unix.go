// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"strings"
)

func (p *LocalPromotion) bindNewPrimary(got stageContainerDetails) error {
	if !got.Running || !fullID(got.ID) || got.ID == p.primaryObservation.ID || got.ID == p.candidateObservation.ID || strings.TrimPrefix(got.Name, "/") != p.primaryConfig.Name || got.ImageID != p.receipt.Verification.ImageID || got.ConfigImage != p.receipt.Verification.ImageID || got.User != p.primaryConfig.ContainerUser || got.Hostname != p.primaryConfig.Hostname || got.Labels["devcontainer.local_folder"] != p.receipt.OriginalSource || got.Labels["workharbor.stage.operation"] != p.receipt.OperationID || got.Labels["workharbor.promote.operation"] != p.receipt.OperationID {
		return stageConflict("promotion new primary native identity, image or user changed")
	}
	expected := map[string]map[string]string{}
	workspace := map[string]string{"type": "bind", "source": p.receipt.OriginalSource, "target": p.primaryConfig.Workspace}
	expected[p.primaryConfig.Workspace] = workspace
	for _, raw := range p.primaryConfig.Mounts {
		text, ok := raw.(string)
		if !ok {
			return stageConflict("promotion original mount representation changed")
		}
		m, e := stageNativeMount(text)
		if e != nil {
			return e
		}
		if _, dup := expected[m["target"]]; dup {
			return stageConflict("promotion original mount destination duplicated")
		}
		expected[m["target"]] = m
	}
	if len(got.Mounts) != len(expected) {
		return stageConflict("promotion new primary mount inventory differs")
	}
	for _, m := range got.Mounts {
		want, ok := expected[m.Destination]
		source := m.Source
		if m.Type == "volume" {
			source = m.Name
		}
		if !ok || m.Type != want["type"] || source != want["source"] || m.RW == (want["readonly"] == "true") {
			return stageConflict("promotion new primary mount differs from admitted original configuration")
		}
		delete(expected, m.Destination)
	}
	env := map[string]string{}
	for _, raw := range got.Environment {
		k, v, ok := strings.Cut(raw, "=")
		if !ok {
			return stageConflict("promotion new primary native environment malformed")
		}
		if _, dup := env[k]; dup {
			return stageConflict("promotion new primary native environment duplicated")
		}
		env[k] = v
	}
	for key, raw := range p.primaryConfig.ContainerEnv {
		value, ok := raw.(string)
		if !ok || env[key] != value {
			return stageConflict("promotion new primary admitted environment changed")
		}
	}
	return nil
}
func (p *LocalPromotion) checkFinalPrimary(ctx context.Context) error {
	if p.newPrimary == nil {
		return stageConflict("promotion new primary has not been observed")
	}
	got, e := readStageContainer(ctx, p.runner, p.newPrimary.ID)
	if e != nil {
		return e
	}
	if e = p.bindNewPrimary(got); e != nil {
		return e
	}
	if p.primaryVerified && got.Healthcheck && (got.Health == nil || *got.Health != "healthy") {
		return stageConflict("promotion verified primary health changed")
	}
	if stageContainerBindingDigest(got) != stageContainerBindingDigest(*p.newPrimary) {
		return stageConflict("promotion new primary immutable native configuration changed")
	}
	return nil
}
func (p *LocalPromotion) checkHomeCleanup(ctx context.Context) error {
	if err := p.checkConfiguration(); err != nil {
		return err
	}
	if err := p.checkPersistedConfiguration(); err != nil {
		return err
	}
	m, e := readStageMetadata(ctx, p.runner)
	if e != nil {
		return e
	}
	if e = p.checkPromotionInputs(ctx, m); e != nil {
		return e
	}
	if e = p.checkFinalPrimary(ctx); e != nil {
		return e
	}
	name := p.homePlan.Mount.Name
	if strings.HasPrefix(name, "anchorage-home-") || name == p.primaryConfig.Home || name != "whr-stage-home-"+p.homePlan.Nonce {
		return stageConflict("promotion protected home cannot be removed")
	}
	for _, c := range m.Containers {
		if c.ID == p.candidateObservation.ID {
			return stageConflict("promotion candidate still exists before home cleanup")
		}
		for _, mount := range c.Mounts {
			if mount.Type == "volume" && mount.Name == name {
				return stageConflict("promotion candidate home remains mounted by a container")
			}
		}
	}
	home, e := readStageVolume(ctx, p.runner, name)
	if e != nil {
		return e
	}
	if stageDigest(home) != stageDigest(p.homeObservation) {
		return stageConflict("promotion candidate home creation observation changed before deletion")
	}
	return nil
}

func (p *LocalPromotion) checkFinalBindings(ctx context.Context) (failure error) {
	defer func() {
		if failure != nil {
			p.invalidatePublicVerification()
		}
	}()
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
	if err = p.checkFinalPrimary(ctx); err != nil {
		return err
	}
	image, err := readStageImage(ctx, p.runner, p.receipt.Verification.ImageID)
	if err != nil {
		return err
	}
	if stageDigest(image.Metadata) != p.receipt.ImageMetadataDigest {
		return stageConflict("promotion verified image metadata changed after cleanup")
	}
	return nil
}
