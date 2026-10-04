package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/skillset"
)

type laneEvidenceFunc func(context.Context) (LaneMetadata, error)

func (read laneEvidenceFunc) ReadLaneMetadata(ctx context.Context) (LaneMetadata, error) {
	return read(ctx)
}

func laneMetadataFixture(t *testing.T) (skillset.Store, LaneMetadata) {
	t.Helper()
	cfg := lanePackageFixture(t)
	pin := cfg.Package
	digest := strings.Repeat("c", 64)
	return skillset.Store{Root: cfg.Store}, LaneMetadata{
		Selection: domain.SkillSelection{Mode: "package", Identity: pin.Identity, Source: pin.Source, Commit: pin.Commit, ManifestSHA256: pin.ManifestSHA256, InventorySHA256: pin.InventorySHA256, ContractVersion: pin.ContractVersion, Entrypoint: "SKILL.md", Adapter: "codex", AdapterVersion: "fixture-version", Model: "fixture-model", Effort: "low", ProjectRevision: "reviewed-revision", ProjectSHA256: digest, InstructionSHA256: digest},
		Root:      "/skills/" + pin.InventorySHA256, NativeVersion: "fixture-native-version", ArtifactSHA256: digest, SchemaSHA256: digest, ConfigSHA256: digest,
	}
}

func TestLaneMetadataRefusesDriftAndInheritedBindings(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*LaneMetadata)
	}{
		{"source pin", func(record *LaneMetadata) { record.Selection.Source = "https://example.test/other" }},
		{"commit", func(record *LaneMetadata) { record.Selection.Commit = strings.Repeat("d", 40) }},
		{"missing model", func(record *LaneMetadata) { record.Selection.Model = "" }},
		{"model alias", func(record *LaneMetadata) { record.Selection.Model = "Fixture-model" }},
		{"missing effort", func(record *LaneMetadata) { record.Selection.Effort = "" }},
		{"wrong effort", func(record *LaneMetadata) { record.Selection.Effort = "medium" }},
		{"wrong provider", func(record *LaneMetadata) { record.Selection.Adapter = "copilot" }},
		{"version", func(record *LaneMetadata) { record.Selection.AdapterVersion = "other" }},
		{"path root", func(record *LaneMetadata) { record.Root += "/../other" }},
		{"missing skills", func(record *LaneMetadata) { record.Selection.Entrypoint = "" }},
		{"project revision", func(record *LaneMetadata) { record.Selection.ProjectRevision = "other" }},
		{"instruction digest", func(record *LaneMetadata) { record.Selection.InstructionSHA256 = strings.Repeat("d", 64) }},
		{"native version", func(record *LaneMetadata) { record.NativeVersion = "other" }},
		{"artifact", func(record *LaneMetadata) { record.ArtifactSHA256 = strings.Repeat("d", 64) }},
		{"schema", func(record *LaneMetadata) { record.SchemaSHA256 = "" }},
		{"config provenance", func(record *LaneMetadata) { record.ConfigSHA256 = strings.Repeat("d", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, expected := laneMetadataFixture(t)
			actual := expected
			test.change(&actual)
			check := LaneMetadataCheck(store, expected, laneEvidenceFunc(func(context.Context) (LaneMetadata, error) { return actual, nil }))
			if results := Run(context.Background(), []Check{check}, nil); !Failed(results) {
				t.Fatalf("drift accepted: %+v", results)
			}
		})
	}
}

func TestLaneMetadataNeverPromotesMatchingFixtureToSupport(t *testing.T) {
	store, expected := laneMetadataFixture(t)
	for _, test := range []struct {
		name     string
		evidence LaneEvidence
		status   Status
	}{
		{"matching fixture", laneEvidenceFunc(func(context.Context) (LaneMetadata, error) { return expected, nil }), NotVerified},
		{"missing evidence", nil, Fail},
		{"unavailable", laneEvidenceFunc(func(context.Context) (LaneMetadata, error) { return LaneMetadata{}, errors.New("private detail") }), NotVerified},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, detail := LaneMetadataCheck(store, expected, test.evidence).Run(context.Background())
			if status != test.status || strings.Contains(detail, "private detail") {
				t.Fatalf("%s: %s", status, detail)
			}
		})
	}
}
