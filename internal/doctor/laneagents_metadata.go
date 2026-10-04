package doctor

import (
	"context"
	"errors"
	"regexp"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/skillset"
)

type LaneMetadata struct {
	Selection      domain.SkillSelection
	Root           string
	NativeVersion  string
	ArtifactSHA256 string
	SchemaSHA256   string
	ConfigSHA256   string
}

type LaneEvidence interface {
	ReadLaneMetadata(context.Context) (LaneMetadata, error)
}

var laneDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func LaneMetadataCheck(store skillset.Store, expected LaneMetadata, evidence LaneEvidence) Check {
	return Check{Name: "lane-binding-metadata", Step: 5, Run: func(ctx context.Context) (Status, string) {
		if err := validateLaneMetadata(store, expected); err != nil {
			return Fail, "expected lane metadata: " + oneLine(err.Error())
		}
		if evidence == nil {
			return Fail, "required effective native metadata is missing; refuse inherited bindings"
		}
		actual, err := evidence.ReadLaneMetadata(ctx)
		if err != nil {
			return NotVerified, "effective native metadata unavailable; mandatory binding remains unsupported"
		}
		if err := validateLaneMetadata(store, actual); err != nil {
			return Fail, "effective lane metadata: " + oneLine(err.Error())
		}
		if actual != expected {
			return Fail, "effective tuple or package/project/instruction/native/config provenance differs from the explicit expected record"
		}
		return NotVerified, "recorded tuple and provenance match; metadata equality does not establish canonical roles, native enforcement or account access; support remains unverified"
	}}
}

func validateLaneMetadata(store skillset.Store, record LaneMetadata) error {
	selection := record.Selection
	if selection.Mode != "package" {
		return errors.New("metadata comparison requires an explicit recorded package selection")
	}
	pin := skillset.Pin{Identity: selection.Identity, Source: selection.Source, Commit: selection.Commit, ManifestSHA256: selection.ManifestSHA256, InventorySHA256: selection.InventorySHA256, ContractVersion: selection.ContractVersion}
	pkg, err := store.Load(pin)
	if err != nil {
		return err
	}
	if record.Root != pkg.Target() || selection.Entrypoint != pkg.Manifest.Entrypoint {
		return errors.New("package root or inventoried entrypoint differs")
	}
	binding := skillset.Binding{Name: selection.Adapter, Version: selection.AdapterVersion, Model: selection.Model, Effort: selection.Effort}
	found := false
	for _, declared := range pkg.Manifest.Adapters {
		found = found || binding == declared
	}
	if !found {
		return errors.New("recorded adapter/version/model/effort is not an exact declared tuple")
	}
	if selection.ProjectRevision == "" || !laneDigest.MatchString(selection.ProjectSHA256) || !laneDigest.MatchString(selection.InstructionSHA256) {
		return errors.New("reviewed project and composed instruction provenance are missing")
	}
	if record.NativeVersion == "" || !laneDigest.MatchString(record.ArtifactSHA256) || !laneDigest.MatchString(record.SchemaSHA256) || !laneDigest.MatchString(record.ConfigSHA256) {
		return errors.New("exact native version/artifact/schema/config provenance is missing")
	}
	return nil
}
