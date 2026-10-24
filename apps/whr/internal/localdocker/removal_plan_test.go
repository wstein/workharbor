// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func removalFixture() []inspection {
	return []inspection{{ID: strings.Repeat("a", 64), Name: "/sample", State: "running", Labels: map[string]string{"devcontainer.local_folder": "/fixture"}, Mounts: []mount{{Type: "volume", Name: "sample-home", Destination: "/home/captain", RW: true}}}}
}

func TestLocalRmContainerOnlyPreservesHomeVolume(t *testing.T) {
	plan, err := planRemoval(removalFixture(), RemovalOptions{Target: "sample"})
	if err != nil || len(plan.Containers) != 1 || !plan.PreserveHomeVolume || len(plan.Volumes) != 0 {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
	if plan.Containers[0].ID != strings.Repeat("a", 64) {
		t.Fatal("plan must retain full container ID")
	}
}

func TestLocalRmWithVolumesAndForceRemovesHomeVolume(t *testing.T) {
	plan, err := planRemoval(removalFixture(), RemovalOptions{Target: "sample", Volumes: true, Force: true})
	if err != nil || len(plan.Volumes) != 1 || plan.Volumes[0].Name != "sample-home" || plan.PreserveHomeVolume {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
	if len(plan.Volumes[0].Mounts) != 1 || plan.Volumes[0].Mounts[0].Destination != "/home/captain" {
		t.Fatal("volume preview must identify mounts")
	}
}

func TestLocalRmVolumesWithoutForceRefused(t *testing.T) {
	_, err := planRemoval(removalFixture(), RemovalOptions{Target: "sample", Volumes: true})
	if exitcode.FromError(err) != exitcode.NeedsHuman {
		t.Fatalf("want70 got%v", err)
	}
}

func TestLocalRmRefusesForeignUnlabelledContainer(t *testing.T) {
	all := removalFixture()
	all[0].Labels = nil
	plan, err := planRemoval(all, RemovalOptions{Target: "sample"})
	if exitcode.FromError(err) != exitcode.Conflict || len(plan.Containers) != 0 {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
}

func TestLocalRmNeverRemovesBundleDockOrReadOnlyVolumes(t *testing.T) {
	for _, change := range []string{"name", "readonly", "tool-path"} {
		t.Run(change, func(t *testing.T) {
			all := removalFixture()
			switch change {
			case "name":
				all[0].Mounts[0].Name = "bundledock-toolbox"
			case "readonly":
				all[0].Mounts[0].RW = false
			case "tool-path":
				all[0].Mounts = append(all[0].Mounts, mount{Type: "volume", Name: "sample-home", Destination: "/opt/bdock/toolbox", RW: true})
			}
			plan, err := planRemoval(all, RemovalOptions{Target: "sample", Volumes: true, Force: true})
			if exitcode.FromError(err) != exitcode.Conflict || len(plan.Containers) != 0 {
				t.Fatalf("plan=%+v error=%v", plan, err)
			}
		})
	}
}

func TestLocalRmRefusesDeletingPrimaryHomeVolumeFromStagingOrTemp(t *testing.T) {
	for _, role := range []string{"staging", "temp"} {
		for _, shared := range []bool{false, true} {
			t.Run(role+map[bool]string{false: "-prefix", true: "-primary-mount"}[shared], func(t *testing.T) {
				all := removalFixture()
				all[0].Labels = map[string]string{"anchorage-" + role: "1"}
				if shared {
					primary := removalFixture()[0]
					primary.ID = strings.Repeat("b", 64)
					primary.Name = "/primary"
					primary.State = "exited"
					all = append(all, primary)
				} else {
					all[0].Mounts[0].Name = "anchorage-home-primary"
				}
				plan, err := planRemoval(all, RemovalOptions{Role: role, Volumes: true, Force: true})
				if exitcode.FromError(err) != exitcode.Conflict || len(plan.Containers) != 0 {
					t.Fatalf("plan=%+v error=%v", plan, err)
				}
			})
		}
	}
}

func TestLocalRmRefusesVolumeMountedByOtherContainer(t *testing.T) {
	for _, state := range []string{"running", "exited"} {
		t.Run(state, func(t *testing.T) {
			all := removalFixture()
			other := all[0]
			other.ID = strings.Repeat("b", 64)
			other.Name = "/foreign"
			other.Labels = nil
			other.State = state
			all = append(all, other)
			plan, err := planRemoval(all, RemovalOptions{Target: "sample", Volumes: true, Force: true})
			if exitcode.FromError(err) != exitcode.Conflict || len(plan.Containers) != 0 {
				t.Fatalf("plan=%+v error=%v", plan, err)
			}
		})
	}
}

func TestLocalRmSelectorErrorsReturnNoPlan(t *testing.T) {
	for _, opts := range []RemovalOptions{{}, {Role: "primary"}, {Target: "sample", Role: "temp"}, {Role: "temp", Label: "x"}, {Label: "=bad"}} {
		plan, err := planRemoval(removalFixture(), opts)
		if exitcode.FromError(err) != exitcode.Usage || len(plan.Containers) != 0 {
			t.Fatalf("opts=%+v plan=%+v error=%v", opts, plan, err)
		}
	}
}
