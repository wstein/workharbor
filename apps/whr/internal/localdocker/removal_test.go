// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

// Each snapshot is supplied independently of the production inspector/planner.
// The strict fake logs only mutations; reads use prewritten controlled metadata.
func removalDocker(t *testing.T, frames [][]inspection, volumeFailure bool, replies ...string) (Client, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "mutations")
	counter := filepath.Join(dir, "counter")
	for n, all := range frames {
		var listing, records strings.Builder
		for _, c := range all {
			fmt.Fprintf(&listing, "{\"ID\":%q,\"Status\":\"fixture\"}\n", c.ID)
			raw, _ := json.Marshal(c)
			fmt.Fprintf(&records, "%s\n", raw)
		}
		for k, v := range map[string]string{"list": listing.String(), "inspect": records.String()} {
			if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(n+1)+"-"+k), []byte(v), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(counter, []byte("0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	containerReply, volumeReply := `printf '%s\n' "$5"`, `printf '%s\n' "$5"`
	if len(replies) == 2 {
		containerReply = "printf '%s\\n' '" + strings.ReplaceAll(replies[0], "'", "'\"'\"'") + "'"
		volumeReply = "printf '%s\\n' '" + strings.ReplaceAll(replies[1], "'", "'\"'\"'") + "'"
	}
	script := fmt.Sprintf(`test "$1" = '--host=unix:///var/run/docker.sock' || exit 98
case "$2 $3" in
 'container ls') n=$(cat %q); n=$((n+1)); printf '%%s\n' "$n" > %q; cat %q/"$n-list";;
 'container inspect') n=$(cat %q); test "$4" = --format && test "$5" = '%s' || exit 97; cat %q/"$n-inspect";;
 'rm -f') test "$#" -eq 5 && test "$4" = -- || exit 96; printf 'container:%%s\n' "$5" >> %q; %s;;
 'volume rm') test "$#" -eq 5 && test "$4" = -- || exit 95; printf 'volume:%%s\n' "$5" >> %q; %s %s;;
 *) exit 94;;
esac`, counter, counter, dir, counter, expectedInspectFormat, dir, log, containerReply, log, map[bool]string{false: "", true: "exit 1;"}[volumeFailure], volumeReply)
	return Client{Executable: fakeDocker(t, script)}, log
}
func mutationText(t *testing.T, log string) string {
	t.Helper()
	raw, e := os.ReadFile(log)
	if e != nil && !os.IsNotExist(e) {
		t.Fatal(e)
	}
	return string(raw)
}

func TestLocalRmDryRunExecutesNoMutations(t *testing.T) {
	c, log := removalDocker(t, [][]inspection{removalFixture()}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	result := tx.Preview()
	if !result.DryRun || len(result.Actions) != 2 || !strings.Contains(fmt.Sprint(result.Volumes), "sample-home") {
		t.Fatalf("%+v", result)
	}
	result.Actions[0].Target = "tampered"
	result.Volumes[0].Name = "tampered"
	if tx.Preview().Actions[0].Target != strings.Repeat("a", 64) || tx.Preview().Volumes[0].Name != "sample-home" {
		t.Fatal("preview aliases execution plan")
	}
	if got := mutationText(t, log); got != "" {
		t.Fatalf("mutations:%s", got)
	}
}
func TestLocalRmExecutesFixedOrderAndRechecksAfterConfirmation(t *testing.T) {
	c, log := removalDocker(t, [][]inspection{removalFixture(), removalFixture(), {}}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.Apply(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DryRun || len(result.Actions) != 2 || !result.Actions[0].Applied || !result.Actions[1].Applied {
		t.Fatalf("%+v", result)
	}
	want := "container:" + strings.Repeat("a", 64) + "\nvolume:sample-home\n"
	if got := mutationText(t, log); got != want {
		t.Fatalf("got%q want%q", got, want)
	}
	_, err = tx.Apply(context.Background())
	if err == nil || mutationText(t, log) != want {
		t.Fatal("transaction repeated mutations")
	}
}
func TestLocalRmPartialVolumeFailureExits73(t *testing.T) {
	c, log := removalDocker(t, [][]inspection{removalFixture(), removalFixture(), {}}, true)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.Apply(context.Background())
	if exitcode.FromError(err) != exitcode.PartialFailure || !result.Actions[0].Applied || result.Actions[1].Applied || !result.Actions[1].Attempted || !strings.Contains(err.Error(), "sample-home") {
		t.Fatalf("result=%+v error=%v mutations=%s", result, err, mutationText(t, log))
	}
}
func TestLocalRmChangedOwnershipRefusesBeforeMutation(t *testing.T) {
	changed := removalFixture()
	changed[0].Labels = nil
	c, log := removalDocker(t, [][]inspection{removalFixture(), changed}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Apply(context.Background())
	if exitcode.FromError(err) != exitcode.Conflict || mutationText(t, log) != "" {
		t.Fatalf("%v %s", err, mutationText(t, log))
	}
}
func TestLocalRmNewVolumeUserRefusesBeforeMutation(t *testing.T) {
	changed := removalFixture()
	foreign := changed[0]
	foreign.ID = strings.Repeat("b", 64)
	foreign.Name = "/foreign"
	foreign.Labels = nil
	changed = append(changed, foreign)
	c, log := removalDocker(t, [][]inspection{removalFixture(), changed}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Apply(context.Background())
	if exitcode.FromError(err) != exitcode.Conflict || mutationText(t, log) != "" {
		t.Fatalf("%v %s", err, mutationText(t, log))
	}
}

func TestLocalRmRejectsIncompleteOrDuplicateSafetyMetadata(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, record := range []string{
		fmt.Sprintf(`{"Id":%q,"Name":"/sample","Labels":{"workharbor.workspace":"x"}}`, id),
		fmt.Sprintf(`{"Id":%q,"Name":"/sample","Labels":{"workharbor.workspace":"x"},"Mounts":null}`, id),
		fmt.Sprintf(`{"Id":%q,"Name":"/sample","Labels":{"workharbor.workspace":"x"},"Mounts":[],"Mounts":[]}`, id),
		fmt.Sprintf(`{"Id":%q,"Name":"/sample","Labels":{"workharbor.workspace":"x","workharbor.workspace":"y"},"Mounts":[]}`, id),
	} {
		_, err := fixtureDocker(t, record).PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample"})
		if exitcode.FromError(err) != exitcode.Conflict {
			t.Fatalf("unsafe record accepted: %s: %v", record, err)
		}
	}
}

func TestLocalRmBatchChecksAllTargetsBeforeFirstMutation(t *testing.T) {
	all := removalFixture()
	all[0].Labels = map[string]string{"anchorage-temp": "1"}
	other := removalFixture()[0]
	other.ID = strings.Repeat("b", 64)
	other.Name = "/other"
	other.Labels = map[string]string{"anchorage-temp": "1"}
	other.Mounts[0].Name = "anchorage-home-primary"
	all = append(all, other)
	c, log := removalDocker(t, [][]inspection{all}, false)
	_, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Role: "temp", Volumes: true, Force: true})
	if exitcode.FromError(err) != exitcode.Conflict || mutationText(t, log) != "" {
		t.Fatalf("error=%v mutations=%s", err, mutationText(t, log))
	}
}

func TestLocalRmNewlyMountedVolumeAfterContainerRemovalIsPartial(t *testing.T) {
	foreign := removalFixture()[0]
	foreign.ID = strings.Repeat("b", 64)
	foreign.Labels = nil
	c, log := removalDocker(t, [][]inspection{removalFixture(), removalFixture(), {foreign}}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.Apply(context.Background())
	if exitcode.FromError(err) != exitcode.PartialFailure || !result.Actions[0].Applied || result.Actions[1].Attempted || strings.Contains(mutationText(t, log), "volume:") {
		t.Fatalf("result=%+v error=%v mutations=%s", result, err, mutationText(t, log))
	}
}

func TestLocalRmMutationAdmitsOnlyFixedLocalVectors(t *testing.T) {
	log := filepath.Join(t.TempDir(), "executed")
	exe := fakeDocker(t, "printf executed > '"+log+"'")
	c := Client{Executable: exe}
	for _, args := range [][]string{{"rm", "-f", "--", "sample"}, {"rm", "-f", "--", strings.Repeat("a", 12)}, {"rm", "-f", "--", strings.Repeat("a", 64), "extra"}, {"volume", "rm", "--", "-v"}, {"volume", "rm", "--", "bundledock-toolbox"}, {"volume", "rm", "--", "home;touch fixture"}, {"system", "prune"}} {
		if _, err := c.mutate(context.Background(), "unix:///fixture", args...); err == nil {
			t.Fatalf("admitted:%q", args)
		}
	}
	for _, endpoint := range []string{"tcp://localhost:2375", "ssh://fixture", "unix://relative"} {
		if _, err := c.mutate(context.Background(), endpoint, "rm", "-f", "--", strings.Repeat("a", 64)); err == nil {
			t.Fatalf("admitted endpoint:%s", endpoint)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("refused mutation started a child")
	}
}

func TestLocalRmBatchRechecksBeforeEachContainer(t *testing.T) {
	first := removalFixture()[0]
	first.Labels = map[string]string{"anchorage-temp": "1"}
	second := removalFixture()[0]
	second.ID = strings.Repeat("b", 64)
	second.Name = "/second"
	second.Labels = map[string]string{"anchorage-temp": "1"}
	second.Mounts[0].Name = "other-home"
	initial := []inspection{first, second}
	changed := second
	changed.Labels = nil
	c, log := removalDocker(t, [][]inspection{initial, initial, {changed}}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Role: "temp"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tx.Apply(context.Background())
	if exitcode.FromError(err) != exitcode.PartialFailure || !result.Actions[0].Applied || result.Actions[1].Attempted || mutationText(t, log) != "container:"+first.ID+"\n" {
		t.Fatalf("result=%+v error=%v mutations=%s", result, err, mutationText(t, log))
	}
}

func TestLocalRmZeroExitWithoutExactAcknowledgementIsUncertain(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, container, volume string
		removedContainer        bool
	}{
		{"container-empty", "", "sample-home", false}, {"container-wrong", "wrong", "sample-home", false},
		{"volume-empty", id, "", true}, {"volume-wrong", id, "wrong", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, log := removalDocker(t, [][]inspection{removalFixture(), removalFixture(), {}}, false, tc.container, tc.volume)
			tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := tx.Apply(context.Background())
			if exitcode.FromError(err) != exitcode.PartialFailure || !result.Actions[0].Attempted || result.Actions[0].Applied != tc.removedContainer || result.Actions[1].Applied || result.Actions[1].Attempted != tc.removedContainer {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			want := "container:" + id + "\n"
			if tc.removedContainer {
				want += "volume:sample-home\n"
			}
			if got := mutationText(t, log); got != want {
				t.Fatalf("mutations=%q want%q", got, want)
			}
		})
	}
}
