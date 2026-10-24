// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func driftPins() []workspacecfg.BundlePin {
	digest := strings.Repeat("a", 64)
	return []workspacecfg.BundlePin{{Bundle: "toolbox", Reference: "quay.io/bundledock/toolbox:fixture@sha256:" + digest, Volume: "bundledock-toolbox-" + digest, Target: "/opt/bdock/toolbox"}}
}
func driftSource(t *testing.T) string {
	t.Helper()
	p := driftPins()[0]
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".devcontainer"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"initializeCommand":{"toolbox":["sh","${localWorkspaceFolder}/.devcontainer/initialize.sh",%q,%q,"toolbox"]},"mounts":[%q]}`, p.Reference, p.Volume, "type=volume,source="+p.Volume+",target="+p.Target+",readonly,volume-nocopy")
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer/devcontainer.json"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func driftRecord(source string, mounts []mount) inspection {
	return inspection{ID: strings.Repeat("a", 64), Name: "/primary", State: "running", Labels: map[string]string{"devcontainer.local_folder": source}, Mounts: mounts}
}
func driftFixture(t *testing.T, record inspection) Client {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return fixtureDocker(t, string(raw))
}
func syncedMount() mount {
	p := driftPins()[0]
	return mount{Type: "volume", Name: p.Volume, Destination: p.Target}
}

func TestBundleDriftAllInSync(t *testing.T) {
	source := driftSource(t)
	c := driftFixture(t, driftRecord(source, []mount{syncedMount()}))
	for _, selector := range []string{"primary", strings.Repeat("a", 64)} {
		got, err := c.Drift(context.Background(), socketPorts(), source, selector)
		if err != nil || got.Status != "in_sync" || len(got.Bundles) != 1 || got.Bundles[0].MountedIdentity == nil {
			t.Fatalf("%+v %v", got, err)
		}
	}
}
func TestBundleDriftDetected(t *testing.T) {
	source := driftSource(t)
	m := syncedMount()
	m.Name = "bundledock-toolbox-" + strings.Repeat("b", 64)
	got, err := driftFixture(t, driftRecord(source, []mount{m})).Drift(context.Background(), socketPorts(), source, "primary")
	if exitcode.FromError(err) != 68 || got.Status != "drift" || got.Bundles[0].MountedIdentity == nil {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestBundleDriftMissingMount(t *testing.T) {
	source := driftSource(t)
	got, err := driftFixture(t, driftRecord(source, []mount{})).Drift(context.Background(), socketPorts(), source, "primary")
	if exitcode.FromError(err) != 68 || got.Status != "cannot_determine" || len(got.Bundles) != 1 || got.Bundles[0].MountedIdentity != nil {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestBundleDriftUnreadablePins(t *testing.T) {
	source := t.TempDir()
	got, err := driftFixture(t, driftRecord(source, []mount{syncedMount()})).Drift(context.Background(), socketPorts(), source, "primary")
	if exitcode.FromError(err) != 68 || got.Reason != "pins_unavailable" || got.Status != "cannot_determine" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestBundleDriftUnknownBundle(t *testing.T) {
	m := syncedMount()
	m.Destination = "/opt/bdock/unknown-bundle"
	got := compareBundlePins(strings.Repeat("a", 64), driftPins(), []mount{syncedMount(), m})
	if got.Status != "cannot_determine" || len(got.Bundles) != 2 || got.Bundles[1].Reason != "undeclared_bundle" {
		t.Fatalf("%+v", got)
	}
}
func TestBundleDriftConservativeIdentityAndPrecedence(t *testing.T) {
	sync := syncedMount()
	cases := map[string][]mount{
		"opaque":          {{Type: "volume", Name: "custom-volume", Destination: sync.Destination}},
		"wrong_bundle":    {{Type: "volume", Name: "bundledock-other-" + strings.Repeat("a", 64), Destination: sync.Destination}},
		"bind":            {{Type: "bind", Name: sync.Name, Destination: sync.Destination}},
		"writable":        {{Type: "volume", Name: sync.Name, Destination: sync.Destination, RW: true}},
		"duplicate":       {sync, sync},
		"extra_elsewhere": {sync, {Type: "volume", Name: sync.Name, Destination: "/somewhere"}},
		"control":         {{Type: "volume", Name: "secret\x1b\n", Destination: sync.Destination}},
		"nil_metadata":    nil,
	}
	for name, ms := range cases {
		t.Run(name, func(t *testing.T) {
			got := compareBundlePins("id", driftPins(), ms)
			if got.Status != "cannot_determine" {
				t.Fatalf("%+v", got)
			}
			raw, _ := json.Marshal(got)
			if strings.Contains(string(raw), "secret") {
				t.Fatal("opaque identity escaped allowlist")
			}
		})
	}
	drift := sync
	drift.Name = "bundledock-toolbox-" + strings.Repeat("b", 64)
	got := compareBundlePins("id", driftPins(), []mount{drift, {Type: "volume", Name: "opaque", Destination: "/opt/bdock/extra"}})
	if got.Status != "cannot_determine" || got.Bundles[1].Status != "drift" {
		t.Fatalf("unknown must dominate drift: %+v", got)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(compareBundlePins("id", driftPins(), []mount{{Type: "volume", Name: "opaque", Destination: "/opt/bdock/extra"}, drift}))
	if string(a) != string(b) {
		t.Fatal("mount order changed report")
	}
}
func TestBundleDriftSelectionAndSource(t *testing.T) {
	source := driftSource(t)
	cases := []struct {
		name, selector, source string
		record                 inspection
	}{
		{"prefix", strings.Repeat("a", 12), source, driftRecord(source, []mount{syncedMount()})},
		{"wrong_source", "primary", driftSource(t), driftRecord(source, []mount{syncedMount()})},
		{"stopped", "primary", source, func() inspection { r := driftRecord(source, []mount{syncedMount()}); r.State = "exited"; return r }()},
		{"foreign", "primary", source, func() inspection { r := driftRecord(source, []mount{syncedMount()}); r.Labels = nil; return r }()},
		{"staging", "primary", source, func() inspection {
			r := driftRecord(source, []mount{syncedMount()})
			r.Labels["anchorage-staging"] = "1"
			return r
		}()},
		{"no_source_label", "primary", source, func() inspection {
			r := driftRecord(source, []mount{syncedMount()})
			r.Labels = map[string]string{"workharbor.workspace": "1"}
			return r
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := driftFixture(t, tc.record).Drift(context.Background(), socketPorts(), tc.source, tc.selector)
			if exitcode.FromError(err) != 68 || got.Status != "cannot_determine" {
				t.Fatalf("%+v %v", got, err)
			}
			if tc.name == "wrong_source" && got.Reason != "source_mismatch" {
				t.Fatalf("wrong source was not rejected by source gate: %+v %v", got, err)
			}
		})
	}
	// Physical label aliases refer to the same source. The explicit source itself
	// still goes through workspacecfg's no-follow root and leaf validation.
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Fatal(err)
	}
	got, err := driftFixture(t, driftRecord(alias, []mount{syncedMount()})).Drift(context.Background(), socketPorts(), source, "primary")
	if err != nil || got.Status != "in_sync" {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestBundleDriftZeroMutations(t *testing.T) {
	source := driftSource(t)
	raw, _ := json.Marshal(driftRecord(source, []mount{syncedMount()}))
	log := filepath.Join(t.TempDir(), "forbidden")
	body := fmt.Sprintf(`test -z "${GITHUB_TOKEN-}" && test -z "${DOCKER_AUTH_CONFIG-}" && test -z "${HOME-}" || exit 97
 test -d "$DOCKER_CONFIG" && test ! -e "$DOCKER_CONFIG/config.json" || exit 96
 case "$2 $3" in
 'container ls') test "$#" -eq 7 && test "$4" = --all && test "$5" = --no-trunc && test "$6" = --format && test "$7" = '{{json .}}' || exit 95; printf '%%s\n' '{"ID":"%s","Status":"fixture"}';;
 'container inspect') test "$#" -eq 6 && test "$4" = --format && test "$5" = '%s' && test "$6" = '%s' || exit 94; printf '%%s\n' '%s';;
 *) touch '%s';exit 99;;esac`, strings.Repeat("a", 64), expectedInspectFormat, strings.Repeat("a", 64), raw, log)
	t.Setenv("GITHUB_TOKEN", "secret-canary")
	t.Setenv("DOCKER_AUTH_CONFIG", "secret-canary")
	got, err := (Client{Executable: fakeDocker(t, body)}).Drift(context.Background(), socketPorts(), source, "primary")
	if err != nil || got.Status != "in_sync" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("forbidden Docker vector")
	}
}
func TestBundleDriftDeadlineAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		if cancelled {
			cancel()
		}
		p := doctor.Ports{DialUnix: func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }}
		start := time.Now()
		_, err := (Client{Executable: "must-not-run"}).Drift(ctx, p, "/source", "primary")
		cancel()
		want := exitcode.Timeout
		if cancelled {
			want = exitcode.Interrupted
		}
		if exitcode.FromError(err) != want || time.Since(start) > time.Second {
			t.Fatalf("cancelled=%v %v", cancelled, err)
		}
	}
}

func TestBundleDriftIncompleteReadOnlyEvidenceCannotMatch(t *testing.T) {
	source := driftSource(t)
	raw, _ := json.Marshal(driftRecord(source, []mount{syncedMount()}))
	for _, replacement := range []string{"", `,"RW":null`} {
		data := strings.Replace(string(raw), `,"RW":false`, replacement, 1)
		got, err := fixtureDocker(t, data).Drift(context.Background(), socketPorts(), source, "primary")
		if exitcode.FromError(err) != 68 || got.Status != "cannot_determine" {
			t.Fatalf("incomplete RW was called in sync: %+v %v", got, err)
		}
	}
}

func TestBundleDriftAmbiguousSelectorCannotMatch(t *testing.T) {
	source := driftSource(t)
	first := driftRecord(source, []mount{syncedMount()})
	second := first
	second.ID = strings.Repeat("b", 64)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	exe := fakeDocker(t, fmt.Sprintf(`case "$2 $3" in
 'container ls') printf '%%s\n' '{"ID":"%s","Status":"fixture"}' '{"ID":"%s","Status":"fixture"}';;
 'container inspect') printf '%%s\n' '%s' '%s';;
 *) exit 99;;esac`, first.ID, second.ID, a, b))
	got, err := (Client{Executable: exe}).Drift(context.Background(), socketPorts(), source, "primary")
	if exitcode.FromError(err) != 68 || got.Reason != "selection_undetermined" || got.ContainerID != nil {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestBundleDriftWrongValidSourceRefusedWithoutEffects(t *testing.T) {
	selectedSource := driftSource(t)
	wrongSource := driftSource(t) // Fully valid pins must not mask a missing source gate.
	raw, _ := json.Marshal(driftRecord(selectedSource, []mount{syncedMount()}))
	forbidden := filepath.Join(t.TempDir(), "forbidden-runtime-call")
	exe := fakeDocker(t, fmt.Sprintf(`case "$2 $3" in
 'container ls') printf '%%s\n' '{"ID":"%s","Status":"fixture"}';;
 'container inspect') printf '%%s\n' '%s';;
 *) touch '%s';exit 99;;esac`, strings.Repeat("a", 64), raw, forbidden))
	got, err := (Client{Executable: exe}).Drift(context.Background(), socketPorts(), wrongSource, "primary")
	if exitcode.FromError(err) != 68 || got.Status != "cannot_determine" || got.Reason != "source_mismatch" {
		t.Fatalf("independently valid wrong source escaped source gate: %+v %v", got, err)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("wrong source attempted a forbidden runtime call: %v", err)
	}
}

func TestBundleDriftExtraAliasedBundleCannotReportInSync(t *testing.T) {
	source := driftSource(t)
	path := filepath.Join(source, ".devcontainer/devcontainer.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["mounts"] = append(config["mounts"].([]any), "type=volume,src=bundledock-hidden-"+strings.Repeat("c", 64)+",dst=/opt/bdock/hidden,readonly")
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// The running container predates the added aliased declaration. Its existing
	// toolbox mount alone cannot establish alignment with the desired config.
	got, err := driftFixture(t, driftRecord(source, []mount{syncedMount()})).Drift(context.Background(), socketPorts(), source, "primary")
	if exitcode.FromError(err) != 68 || got.Status != "cannot_determine" || got.Reason != "pins_unavailable" {
		t.Fatalf("extra aliased bundle incorrectly aligned: %+v %v", got, err)
	}
}

func TestBundleDriftUnknownPinBeforeLaterDriftDominates(t *testing.T) {
	pin := func(name string) workspacecfg.BundlePin {
		digest := strings.Repeat("a", 64)
		return workspacecfg.BundlePin{Bundle: name, Reference: "quay.io/bundledock/" + name + "@sha256:" + digest, Volume: "bundledock-" + name + "-" + digest, Target: "/opt/bdock/" + name}
	}
	missing, drifted := pin("aaa"), pin("bbb")
	mounts := []mount{{Type: "volume", Name: "bundledock-bbb-" + strings.Repeat("b", 64), Destination: drifted.Target}}
	for _, pins := range [][]workspacecfg.BundlePin{{missing, drifted}, {drifted, missing}} {
		got := compareBundlePins(strings.Repeat("a", 64), pins, mounts)
		if got.Status != "cannot_determine" || got.Reason != "incomplete_evidence" || len(got.Bundles) != 2 || got.Bundles[0].Status != "cannot_determine" || got.Bundles[1].Status != "drift" {
			t.Fatalf("unknown pin lost precedence to later drift: %+v", got)
		}
	}
}

func TestBundleDriftNilMountMetadataHasIncompleteAggregateEvidence(t *testing.T) {
	got := compareBundlePins(strings.Repeat("a", 64), driftPins(), nil)
	if got.Status != "cannot_determine" || got.Reason != "incomplete_evidence" || len(got.Bundles) != 1 || got.Bundles[0].Reason != "mount_unavailable" {
		t.Fatalf("nil mount evidence was not classified per bundle and in aggregate: %+v", got)
	}
}

func TestBundleDriftNoncanonicalTargetCannotReportInSync(t *testing.T) {
	for _, target := range []string{"//opt/bdock/hidden", "/opt//bdock/hidden", "/opt/./bdock/hidden", "opt/bdock/hidden"} {
		t.Run(target, func(t *testing.T) {
			source := driftSource(t)
			configPath := filepath.Join(source, ".devcontainer/devcontainer.json")
			raw, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			// An opaque source ensures only the target spelling determines relevance.
			config["mounts"] = append(config["mounts"].([]any), "type=volume,source=opaque,target="+target+",readonly")
			raw, err = json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := driftFixture(t, driftRecord(source, []mount{syncedMount()})).Drift(context.Background(), socketPorts(), source, "primary")
			if exitcode.FromError(err) != 68 || got.Status != "cannot_determine" || got.Reason != "pins_unavailable" {
				t.Fatalf("noncanonical target incorrectly aligned: target=%q report=%+v err=%v", target, got, err)
			}
		})
	}
}
