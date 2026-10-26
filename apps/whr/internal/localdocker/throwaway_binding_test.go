// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

type throwawayBindingFixture struct {
	stage    *LocalStage
	disposal candidateDisposal
	client   Client
	ports    stagePreparationPorts
	path     string
	state    []byte
}

func fixtureThrowawayBinding(t *testing.T) *throwawayBindingFixture {
	t.Helper()
	c, opts, ports, path := fixtureStageLifecycle(t, false)
	opts.Smoke = true
	s, err := c.prepareStage(context.Background(), opts, ports)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err = s.Apply(context.Background(), LifecycleConsent{TrustDigest: *s.Preview().Fingerprint, Confirmed: true, Quiescence: true}); err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return &throwawayBindingFixture{s, candidateDisposal{store: s.store, runner: s.runner, endpoint: s.endpoint, receipt: s.receipt, plan: s.homeIntent.plan}, c, ports, path, state}
}

// Refusals are read-only, so each case reuses admitted inputs with fresh copies.
func (f *throwawayBindingFixture) fresh(t *testing.T) *throwawayBindingFixture {
	t.Helper()
	if err := os.WriteFile(f.path, f.state, 0600); err != nil {
		t.Fatal(err)
	}
	copy := *f
	data, err := json.Marshal(f.disposal.receipt)
	if err != nil {
		t.Fatal(err)
	}
	copy.disposal.receipt = new(stageReceipt)
	if err = json.Unmarshal(data, copy.disposal.receipt); err != nil {
		t.Fatal(err)
	}
	return &copy
}

func writeThrowawayBindingState(t *testing.T, path string, state fakeStageState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func copyThrowawayContainer(t *testing.T, item map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func assertThrowawayBindingRefusal(t *testing.T, err error, reason string) {
	t.Helper()
	want := "local stage candidate cleanup " + reason
	if exitcode.FromError(err) != exitcode.Conflict || err == nil || err.Error() != want {
		t.Fatalf("refusal = %v (exit %d), want exit %d and %q", err, exitcode.FromError(err), exitcode.Conflict, want)
	}
}
func assertThrowawayNoMutation(t *testing.T, calls [][]string) {
	t.Helper()
	for _, call := range calls {
		if len(call) > 1 && ((call[0] == "container" && call[1] == "rm") || (call[0] == "volume" && call[1] == "rm") || call[0] == "rm") {
			t.Fatalf("refused binding reached removal: %v", call)
		}
	}
}

// Exact reasons distinguish the intended refusal from a later safety guard.
func TestThrowawayDisposalBindingRefusals(t *testing.T) {
	fixture := fixtureThrowawayBinding(t)
	cases := []struct{ name, reason string }{
		{"socket_device", "socket identity changed"}, {"socket_inode", "socket identity changed"}, {"socket_unavailable", "socket identity changed"},
		{"observed_daemon", "intent binding changed"}, {"intent_daemon", "intent binding changed"}, {"intent_endpoint", "intent binding changed"}, {"intent_operation", "intent binding changed"}, {"intent_nonce", "intent binding changed"}, {"intent_home_name", "intent binding changed"}, {"intent_primary_home", "intent binding changed"}, {"intent_anchorage_home", "intent binding changed"},
		{"candidate_ambiguous", "identity ambiguous"}, {"candidate_operation", "container binding changed"}, {"candidate_id", "container binding changed"},
		{"failed_replacement_digest", "failed replacement binding changed"}, {"rollback_original_stopped", "rollback resources changed"}, {"rollback_failed_running", "rollback resources changed"},
		{"home_driver", "home allocation binding changed"}, {"home_operation", "home allocation binding changed"}, {"home_nonce", "home allocation binding changed"},
		{"residual_driver", "home allocation binding changed"}, {"residual_operation", "home allocation binding changed"}, {"residual_nonce", "home allocation binding changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.fresh(t)
			d := &f.disposal
			state := readFakeStageState(t, f.path)
			ctx := context.Background()
			switch tc.name {
			case "socket_device":
				d.endpoint.SocketDevice++
			case "socket_inode":
				d.endpoint.SocketInode++
			case "socket_unavailable":
				d.endpoint.Endpoint = "unix:///absent-whr-binding-test.sock"
			case "observed_daemon":
				d.endpoint.DaemonID = "other"
				d.plan.DaemonID = "other"
			case "intent_daemon":
				d.plan.DaemonID = "other"
			case "intent_endpoint":
				d.plan.Endpoint = "unix:///other.sock"
			case "intent_operation":
				d.plan.OperationID = strings.Repeat("f", 32)
			case "intent_nonce":
				d.plan.Nonce = "invalid"
				d.plan.Mount.Name = "whr-stage-home-invalid"
			case "intent_home_name":
				d.plan.Mount.Name += "other"
			case "intent_primary_home":
				d.plan.PrimaryHome = d.plan.Mount.Name
			case "intent_anchorage_home":
				d.plan.Mount.Name = "anchorage-home-owner"
			case "candidate_ambiguous":
				d.receipt.Candidate.ID = nil
				other := copyThrowawayContainer(t, state.Candidate)
				other["Id"], other["Name"] = strings.Repeat("f", 64), "/another-candidate"
				other["Mounts"] = []any{}
				state.ExtraContainers = []map[string]any{other}
			case "candidate_operation":
				state.Candidate["Labels"].(map[string]any)["workharbor.stage.operation"] = "other"
			case "candidate_id":
				d.receipt.Candidate.ID = stageString(strings.Repeat("f", 64))
			case "failed_replacement_digest":
				failed, err := readStageContainer(ctx, d.runner, state.Candidate["Id"].(string))
				if err != nil {
					t.Fatal(err)
				}
				failed.Hostname += "changed"
				d.failed = &failed
				state.Candidate["State"], state.Candidate["Running"] = "exited", false
			case "rollback_original_stopped":
				d.failed = &stageContainerDetails{ID: state.Candidate["Id"].(string)}
				state.Primary["State"], state.Primary["Running"] = "exited", false
				state.Candidate["State"], state.Candidate["Running"] = "exited", false
			case "rollback_failed_running":
				d.failed = &stageContainerDetails{ID: state.Candidate["Id"].(string)}
			default:
				state.Candidate = nil
				switch {
				case strings.HasSuffix(tc.name, "driver"):
					state.Volume.Driver = "other"
				case strings.HasSuffix(tc.name, "operation"):
					state.Volume.Labels["workharbor.stage.operation"] = "other"
				case strings.HasSuffix(tc.name, "nonce"):
					state.Volume.Labels["workharbor.stage.nonce"] = "other"
				}
			}
			state.Calls = nil
			writeThrowawayBindingState(t, f.path, state)
			var err error
			switch {
			case strings.HasPrefix(tc.name, "socket_"), strings.HasPrefix(tc.name, "intent_"), tc.name == "observed_daemon":
				_, err = d.metadata(ctx)
			case strings.HasPrefix(tc.name, "candidate_"):
				_, err = d.candidate(ctx)
			case strings.HasPrefix(tc.name, "residual_"):
				_, err = (&candidateRemoval{candidateDisposal: *d}).removalPlan(ctx, nil, RemovalOptions{Target: *d.receipt.Candidate.ID})
			default:
				err = d.removeResources(ctx)
			}
			assertThrowawayBindingRefusal(t, err, tc.reason)
			assertThrowawayNoMutation(t, readFakeStageState(t, f.path).Calls)
		})
	}
}

func TestThrowawayRemovalBindingRefusals(t *testing.T) {
	fixture := fixtureThrowawayBinding(t)
	ctx := context.Background()
	if err := fixture.disposal.begin(ctx); err != nil {
		t.Fatal(err)
	}
	metadata, err := fixture.disposal.metadata(ctx)
	if err != nil {
		t.Fatal(err)
	}
	initialRows, err := fixture.stage.store.Records()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := json.Marshal(initialRows)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.stage.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, reason string }{
		{"selection_ambiguous", "selection ambiguous"}, {"journal_receipt_operation", "journal binding changed"}, {"journal_plan_operation", "journal binding changed"}, {"journal_receipt_digest", "journal binding changed"},
		{"receipt_id", "receipt identity changed"}, {"receipt_id_missing", "receipt identity changed"}, {"receipt_candidate_missing", "receipt identity changed"}, {"receipt_endpoint", "receipt identity changed"}, {"receipt_daemon", "receipt identity changed"}, {"receipt_socket_device", "receipt identity changed"}, {"receipt_socket_inode", "receipt identity changed"}, {"target_changed", "target changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture.fresh(t)
			all := append([]inspection(nil), metadata.Containers...)
			options := RemovalOptions{Target: *f.disposal.receipt.Candidate.ID}
			var rows []stageRecord
			if err := json.Unmarshal(journal, &rows); err != nil {
				t.Fatal(err)
			}
			store, err := openStageStore(f.ports.StateRoot, f.disposal.endpoint.DaemonID, f.disposal.endpoint.Endpoint)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			last := &rows[len(rows)-1]
			binding := last.Cleanup
			switch tc.name {
			case "selection_ambiguous":
				options = RemovalOptions{Role: "staging"}
				for _, item := range all {
					if item.ID == options.Target || item.Labels["anchorage-staging"] != "" {
						item.ID = strings.Repeat("f", 64)
						all = append(all, item)
						break
					}
				}
			case "journal_receipt_operation":
				binding.Receipt.OperationID = strings.Repeat("f", 32)
			case "journal_plan_operation":
				binding.Plan.OperationID = strings.Repeat("f", 32)
			case "receipt_id":
				binding.Receipt.Candidate.ID = stageString(strings.Repeat("f", 64))
			case "receipt_id_missing":
				binding.Receipt.Candidate.ID = nil
			case "receipt_candidate_missing":
				binding.Receipt.Candidate = nil
			case "receipt_endpoint":
				binding.Receipt.EndpointIdentity.Endpoint += ".changed"
			case "receipt_daemon":
				binding.Receipt.EndpointIdentity.DaemonID = "other"
			case "receipt_socket_device":
				binding.Receipt.EndpointIdentity.SocketDevice++
			case "receipt_socket_inode":
				binding.Receipt.EndpointIdentity.SocketInode++
			case "target_changed":
				state := readFakeStageState(t, f.path)
				state.Candidate = nil
				writeThrowawayBindingState(t, f.path, state)
			}
			last.ReceiptDigest = stageReceiptDigest(binding.Receipt)
			if tc.name == "journal_receipt_digest" {
				last.ReceiptDigest = "sha256:" + strings.Repeat("f", 64)
			}
			if err = store.write(rows, false); err != nil {
				t.Fatal(err)
			}
			endpoint := f.disposal.endpoint.Endpoint
			store.Close()
			state := readFakeStageState(t, f.path)
			state.Calls = nil
			writeThrowawayBindingState(t, f.path, state)
			removal, err := f.client.prepareCandidateRemoval(ctx, endpoint, all, options, f.ports.StateRoot)
			if removal != nil {
				removal.Close()
			}
			assertThrowawayBindingRefusal(t, err, tc.reason)
			assertThrowawayNoMutation(t, readFakeStageState(t, f.path).Calls)
		})
	}
}

func TestThrowawayRollbackBindingRefusals(t *testing.T) {
	p, path, _ := fixtureVerifiedPromotion(t)
	initial := readFakePromotionState(t, path)
	for _, tc := range []struct{ name, reason string }{
		{"restored_stopped", "restored primary binding changed"}, {"restored_name", "restored primary binding changed"}, {"restored_digest", "restored primary binding changed"}, {"failed_running", "failed replacement binding changed"}, {"failed_name", "failed replacement binding changed"}, {"failed_digest", "failed replacement binding changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFakePromotionState(t, path, initial)
			replacement := p.candidateObservation
			p.newPrimary = &replacement
			state := readFakePromotionState(t, path)
			state.Candidate["Name"] = "/" + p.failedPrimaryName()
			state.Candidate["State"], state.Candidate["Running"] = "exited", false
			switch tc.name {
			case "restored_stopped":
				state.Primary["State"], state.Primary["Running"] = "exited", false
			case "restored_name":
				state.Primary["Name"] = "/changed"
			case "restored_digest":
				state.Primary["Hostname"] = "changed"
			case "failed_running":
				state.Candidate["State"], state.Candidate["Running"] = "running", true
			case "failed_name":
				state.Candidate["Name"] = "/changed"
			case "failed_digest":
				state.Candidate["Hostname"] = "changed"
			}
			state.Calls = nil
			writeFakePromotionState(t, path, state)
			assertThrowawayBindingRefusal(t, p.disposeAfterRollback(), tc.reason)
			assertThrowawayNoMutation(t, readFakePromotionState(t, path).Calls)
			if p.disposalStarted {
				t.Fatal("rollback binding refusal started disposal")
			}
		})
	}
}

func TestThrowawayMixedStagingRemovalKeepsHomeExceptionScoped(t *testing.T) {
	for _, tc := range []struct {
		name, role, home       string
		volumes, refuse, label bool
	}{
		{name: "foreign_staging_home_preserved", role: "staging", home: "foreign-home"},
		{name: "foreign_temp_not_selected", role: "temp", home: "foreign-home"},
		{name: "foreign_temp_home_preserved", role: "temp", home: "foreign-home", label: true},
		{name: "foreign_temp_anchorage_home", role: "temp", home: "anchorage-home-other", label: true, refuse: true},
		{name: "foreign_temp_primary_home", role: "temp", home: "original-home", label: true, refuse: true},
		{name: "anchorage_home_without_flags", role: "staging", home: "anchorage-home-other", refuse: true},
		{name: "anchorage_home_with_flags", role: "staging", home: "anchorage-home-other", volumes: true, refuse: true},
		{name: "primary_home_without_flags", role: "staging", home: "original-home", refuse: true},
		{name: "primary_home_with_flags", role: "staging", home: "original-home", volumes: true, refuse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixtureThrowawayBinding(t)
			state := readFakeStageState(t, f.path)
			foreign := copyThrowawayContainer(t, state.Candidate)
			foreign["Id"], foreign["Name"] = strings.Repeat("f", 64), "/foreign-"+tc.role
			foreign["Labels"] = map[string]any{"anchorage-" + tc.role: "true"}
			foreign["Mounts"] = []map[string]any{{"Type": "volume", "Name": tc.home, "Source": tc.home, "Destination": "/home/vscode", "RW": true}}
			state.ExtraContainers = []map[string]any{foreign}
			if tc.home == "original-home" {
				for _, raw := range state.Primary["Mounts"].([]any) {
					m := raw.(map[string]any)
					if m["Type"] == "volume" {
						m["Name"], m["Source"] = tc.home, tc.home
					}
				}
			}
			options := RemovalOptions{Role: "staging", Volumes: tc.volumes, Force: tc.volumes}
			if tc.label {
				options.Role, options.Label = "", "cleanup-group=mixed"
				state.Candidate["Labels"].(map[string]any)["cleanup-group"] = "mixed"
				foreign["Labels"].(map[string]any)["cleanup-group"] = "mixed"
			}
			state.Calls = nil
			writeThrowawayBindingState(t, f.path, state)
			if err := f.stage.Close(); err != nil {
				t.Fatal(err)
			}
			tx, err := f.client.prepareRemovalAtEndpoint(context.Background(), f.disposal.endpoint.Endpoint, options, f.ports.StateRoot)
			if tc.refuse {
				if tx != nil {
					tx.Close()
				}
				if exitcode.FromError(err) != exitcode.Conflict || err == nil || err.Error() != "staging or temp cleanup would touch a primary home volume" {
					t.Fatalf("mixed protected home refusal: %v", err)
				}
				assertThrowawayNoMutation(t, readFakeStageState(t, f.path).Calls)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Close()
			preview := tx.Preview()
			if len(preview.Volumes) != 1 || preview.Volumes[0].Name != f.disposal.plan.Mount.Name {
				t.Fatalf("candidate exception widened to foreign home: %+v", preview)
			}
			if (tc.role == "staging" || tc.label) && (!preview.PreserveHomeVolume || !reflect.DeepEqual(preview.PreservedHomeVolumes, []string{tc.home})) {
				t.Fatalf("foreign home not preserved: %+v", preview)
			}
			if _, err = tx.Apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			final := readFakeStageState(t, f.path)
			if final.Candidate != nil || final.Volume != nil || final.Primary["Running"] != true {
				t.Fatal("mixed removal did not isolate candidate disposal")
			}
			if (len(final.ExtraContainers) == 1) != (tc.role == "temp" && !tc.label) {
				t.Fatalf("foreign %s selection: %+v", tc.role, final.ExtraContainers)
			}
			for _, call := range final.Calls {
				if len(call) > 1 && call[0] == "volume" && call[1] == "rm" && call[len(call)-1] != f.disposal.plan.Mount.Name {
					t.Fatalf("foreign home deletion: %v", call)
				}
			}
			assertNoCandidateRecords(t, f.ports.StateRoot)
		})
	}
}
