// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (p *LocalPromotion) checkPromotionInputs(ctx context.Context, metadata stageMetadata) error {
	physical, err := stageSocketIdentity(p.endpoint.Endpoint)
	if err != nil {
		return err
	}
	physical.DaemonID = metadata.Daemon.ID
	if physical != p.endpoint {
		return stageConflict("promotion endpoint identity changed")
	}
	if err = p.loadArtifacts(); err != nil {
		return err
	}
	if stageDigest(stageTrustTools(p.runner.Tools)) != stageDigest(stageTrustTools(p.receipt.ToolManifest)) {
		return stageConflict("promotion trusted tool identity changed")
	}
	current, err := p.store.loadReceipt(p.receipt.OperationID)
	if err != nil {
		return err
	}
	if stageReceiptDigest(current) != stageReceiptDigest(p.receipt) {
		return stageConflict("promotion receipt changed after preview")
	}
	for _, s := range p.snapshots {
		if err = checkStageOriginalBinding(ctx, s, p.result.ResultRefs); err != nil {
			return err
		}
	}
	if p.execution.OriginalDigest != p.execution.CandidateDigest {
		return stageConflict("promotion original execution inputs differ from verified candidate; fresh stage required")
	}
	sourceDigest, err := stageExecutionDigest(ctx, p.receipt.OriginalSource, p.settings.ExecutionPaths)
	if err != nil {
		return err
	}
	if sourceDigest != p.execution.OriginalDigest {
		return stageConflict("promotion original execution inputs changed; fresh stage required")
	}
	candidateDigest, err := stageExecutionDigest(ctx, p.snapshots[0].ClonePath, p.settings.ExecutionPaths)
	if err != nil {
		return err
	}
	if candidateDigest != p.execution.CandidateDigest {
		return stageConflict("promotion candidate execution inputs changed; fresh stage required")
	}
	if err = stageCheckExecutionGit(ctx, p.execution.GitInputs, p.snapshots); err != nil {
		return err
	}
	if err = p.checkHostFacts(); err != nil {
		return err
	}
	facts := ""
	for _, entry := range p.receipt.OutputInventory {
		if entry.Origin == "admitted_A" {
			facts = entry.Digest
		}
	}
	fingerprint := stageDigest(map[string]any{"source": p.receipt.OriginalSource, "files": p.receipt.InputManifestDigest, "repositories": stageTrustRepositories(p.snapshots), "tools": stageTrustTools(p.runner.Tools), "substitutions": stageTrustEnvironment(p.runner.Environment), "config": p.sourceConfig, "original_host_facts": facts, "workspace": p.primaryConfig.Workspace, "primary_id": p.primaryObservation.ID, "endpoint": p.endpoint})
	if fingerprint != p.receipt.TrustDigest {
		return stageConflict("promotion admitted substitution environment or source trust changed")
	}
	return nil
}

func (p *LocalPromotion) checkArtifactsAndBindings(ctx context.Context, metadata stageMetadata, originalRunning bool) error {
	if err := p.checkPromotionInputs(ctx, metadata); err != nil {
		return err
	}
	// The complete FR-067 plan is inspected even when preview supplies no deletion
	// flags. It authorizes no mutation; Apply separately requires explicit consent.
	plan, err := planRemoval(metadata.Containers, RemovalOptions{Target: *p.receipt.Candidate.ID, Volumes: true, Force: true})
	if err != nil {
		return err
	}
	if len(plan.Containers) != 1 || len(plan.Volumes) != 1 || plan.Volumes[0].Name != p.homePlan.Mount.Name {
		return stageConflict("promotion cleanup plan differs from owned candidate home")
	}
	candidate, err := readStageContainer(ctx, p.runner, *p.receipt.Candidate.ID)
	if err != nil {
		return err
	}
	if !candidate.Running || candidate.ImageID != p.receipt.Verification.ImageID || stageContainerBindingDigest(candidate) != stageContainerBindingDigest(p.candidateObservation) {
		return stageConflict("promotion candidate resource binding changed")
	}
	mounts := []CandidateHomeMount{}
	for _, m := range candidate.Mounts {
		mounts = append(mounts, CandidateHomeMount{Type: m.Type, Name: m.Name, Destination: m.Destination, ReadOnly: !m.RW})
	}
	if _, err = (CandidateHomeIntent{plan: p.homePlan}).Bind(CandidateHomeObservation{DaemonID: p.endpoint.DaemonID, Endpoint: p.endpoint.Endpoint, PrimaryID: p.homePlan.PrimaryID, CandidateID: candidate.ID, CandidateName: strings.TrimPrefix(candidate.Name, "/"), Labels: candidate.Labels, Mounts: mounts}); err != nil {
		return err
	}
	if candidate.Healthcheck {
		if candidate.Health == nil || *candidate.Health != "healthy" {
			return stageConflict("promotion candidate health is not passing")
		}
	} else if !p.settings.SmokeRequested || (p.receipt.Verification.Policy != "smoke" && p.receipt.Verification.Policy != "health_and_smoke") {
		return stageConflict("promotion candidate requires passing smoke verification")
	}
	home, err := readStageVolume(ctx, p.runner, p.homePlan.Mount.Name)
	if err != nil {
		return err
	}
	if stageDigest(home) != stageDigest(p.homeObservation) {
		return stageConflict("promotion candidate home creation observation changed")
	}
	image, err := readStageImage(ctx, p.runner, p.receipt.Verification.ImageID)
	if err != nil {
		return err
	}
	if stageDigest(image.Metadata) != p.receipt.ImageMetadataDigest {
		return stageConflict("promotion verified image metadata changed")
	}
	p.image = image
	if originalRunning {
		primary, err := readStageContainer(ctx, p.runner, p.primaryObservation.ID)
		if err != nil {
			return err
		}
		if !primary.Running || stageContainerBindingDigest(primary) != stageContainerBindingDigest(p.primaryObservation) {
			return stageConflict("promotion original primary binding changed")
		}
		if _, _, _, err = bindStagePrimary(inspection{ID: primary.ID, Name: primary.Name, State: primary.State, Labels: primary.Labels, Mounts: primary.Mounts}, p.receipt.OriginalSource); err != nil {
			return err
		}
	}
	return nil
}
func (p *LocalPromotion) checkHostFacts() error {
	foundA, foundW := false, false
	for _, entry := range p.receipt.OutputInventory {
		if entry.Origin != "admitted_A" && entry.Origin != "published_W" {
			continue
		}
		root := p.receipt.OriginalSource
		if entry.Origin == "published_W" {
			root = p.snapshots[0].ClonePath
		}
		if entry.Path != filepath.Join(root, ".devcontainer", "host.env") || entry.AdmissionOperationID != p.receipt.OperationID || entry.AdmissionInputDigest != p.receipt.TrustDigest {
			return stageConflict("promotion host facts provenance changed")
		}
		if entry.Origin == "admitted_A" {
			if entry.ProducerOperationID != nil || entry.ProducerInputDigest != nil {
				return stageConflict("promotion original host facts producer binding invalid")
			}
			foundA = true
		} else {
			if entry.ProducerOperationID == nil || *entry.ProducerOperationID != p.receipt.OperationID || entry.ProducerInputDigest == nil || *entry.ProducerInputDigest != p.receipt.TrustDigest {
				return stageConflict("promotion candidate host facts producer binding invalid")
			}
			foundW = true
		}
		data, digest, err := readStageHostFacts(entry.Path)
		if err != nil {
			return err
		}
		if digest != entry.Digest || int64(len(data)) != entry.Size {
			return stageConflict("promotion receipt-bound host facts changed")
		}
	}
	if p.settings.AnchorageFacts && (!foundA || !foundW) {
		return stageConflict("promotion admitted original and published candidate host facts required")
	}
	return nil
}
func (p *LocalPromotion) prepareConfiguration(ctx context.Context) error {
	baked := len(p.image.Metadata) > 0 && stageCheckBakedSourceMetadata(p.sourceConfig, p.image.Metadata) == nil
	config, err := stagePromoteConfig(p.sourceConfig, p.primaryConfig, p.receipt.Verification.ImageID, baked)
	if err != nil {
		return err
	}
	p.configuration = config
	p.configurationBytes, err = stageArtifactBytes(config)
	if err != nil {
		return stageConflict("promotion configuration encoding invalid")
	}
	p.configurationDigest = promotionBytesDigest(p.configurationBytes)
	dir := filepath.Join(p.root, "promote")
	if err = os.Mkdir(dir, 0700); err != nil {
		return stageConflict("promotion private configuration storage unavailable")
	}
	p.configurationPath = filepath.Join(dir, "devcontainer.json")
	if err = os.WriteFile(p.configurationPath, p.configurationBytes, 0600); err != nil {
		return stageConflict("promotion private configuration publication unavailable")
	}
	allowed := []string{}
	for _, v := range p.primaryConfig.Mounts {
		raw, ok := v.(string)
		if !ok {
			return stageConflict("promotion admitted mount representation unavailable")
		}
		m, err := stageNativeMount(raw)
		if err != nil {
			return err
		}
		if m["target"] != p.primaryConfig.HomeDestination {
			allowed = append(allowed, raw)
		}
	}
	p.runner.Policy = &stageDriverPolicy{PrimaryID: p.primaryObservation.ID, PrimaryName: p.primaryConfig.Name, PrimaryHome: p.primaryConfig.Home, OperationID: p.receipt.OperationID, WorkspaceSource: p.receipt.OriginalSource, WorkspaceDestination: p.primaryConfig.Workspace, Home: p.primaryConfig.Home, HomeDestination: p.primaryConfig.HomeDestination, AllowedMounts: allowed, PullReferences: []string{}, InitializerVolumes: map[string]string{}}
	driver, err := p.runner.DockerDriver("preview", "")
	if err != nil {
		return err
	}
	p.invocation = stageCLIInvocation{Workspace: p.receipt.OriginalSource, Config: p.configurationPath, Override: p.configurationPath, Driver: driver, UserData: filepath.Join(p.runner.Root, "user-data"), Labels: []string{"devcontainer.local_folder=" + p.receipt.OriginalSource, "workharbor.stage.operation=" + p.receipt.OperationID}, Promote: true}
	return p.readConfiguration(ctx)
}
func (p *LocalPromotion) checkConfiguration() error {
	data, err := readStageInput(p.configurationPath, 256<<10)
	if err != nil {
		return err
	}
	if promotionBytesDigest(data) != p.configurationDigest || !bytes.Equal(data, p.configurationBytes) {
		return stageConflict("promotion immutable configuration changed")
	}
	return nil
}
func (p *LocalPromotion) readConfiguration(ctx context.Context) error {
	if err := p.checkConfiguration(); err != nil {
		return err
	}
	args, err := p.invocation.Arguments("read-configuration")
	if err != nil {
		return err
	}
	out, err := p.runner.Run(ctx, "metadata", "devcontainer", args, nil, 5*time.Second)
	if err != nil {
		return err
	}
	var response struct{ Configuration, MergedConfiguration map[string]any }
	dec := json.NewDecoder(bytes.NewReader(out.Stdout))
	dec.UseNumber()
	if decodeDockerRecord(dec, &response) != nil || dec.Decode(new(any)) != io.EOF || response.Configuration == nil || response.MergedConfiguration == nil {
		return stageConflict("promotion official configuration capability unavailable")
	}
	if response.Configuration["image"] != p.receipt.Verification.ImageID || response.MergedConfiguration["image"] != p.receipt.Verification.ImageID {
		return stageConflict("promotion official configuration changed verified image")
	}
	return nil
}
