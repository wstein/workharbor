// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

const candidateHome = "whr-stage-home-0123456789abcdef0123456789abcdef"

func candidateCleanupFixture(role, destination string) []inspection {
	all := removalFixture()
	all[0].Labels = map[string]string{"anchorage-" + role: "1", "cleanup.fixture": "candidate"}
	all[0].Mounts[0].Name, all[0].Mounts[0].Destination = candidateHome, destination
	return all
}
func candidateCleanupSelector(selector, role string) RemovalOptions {
	switch selector {
	case "id":
		return RemovalOptions{Target: strings.Repeat("a", 64)}
	case "role":
		return RemovalOptions{Role: role}
	default:
		return RemovalOptions{Label: "anchorage-" + role + "=1"}
	}
}

// Refusal at both planning and public backend boundaries must expose no plan
// that could be executed. Missing force takes precedence over inspecting guards.
func assertCandidateCleanupRefused(t *testing.T, all []inspection, o RemovalOptions, want int) {
	t.Helper()
	plan, err := planRemoval(all, o)
	if int(exitcode.FromError(err)) != want || len(plan.Containers) != 0 || len(plan.Volumes) != 0 {
		t.Fatalf("planner: plan=%+v error=%v; want %d", plan, err, want)
	}
	c, log := removalDocker(t, [][]inspection{all}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), o)
	if int(exitcode.FromError(err)) != want || tx != nil || mutationText(t, log) != "" {
		t.Fatalf("backend: tx=%v error=%v mutations=%q; want %d", tx, err, mutationText(t, log), want)
	}
	if want == 70 && mutationText(t, filepath.Join(filepath.Dir(log), "calls")) != "" {
		t.Fatal("missing force reached Docker inspection")
	}
}

func TestCandidateCleanupPreservesWholeAnchorageGuard(t *testing.T) {
	for _, role := range []string{"staging", "temp"} {
		for _, selector := range []string{"id", "role", "label"} {
			for _, destination := range []string{"/home/captain", "/home/vscode"} {
				for _, flags := range []struct {
					name           string
					volumes, force bool
				}{{"container-only", false, false}, {"force-only", false, true}, {"volumes-only", true, false}, {"volumes-force", true, true}} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", role, selector, destination, flags.name), func(t *testing.T) {
						all := candidateCleanupFixture(role, destination)
						// Candidate-like suffixes do not weaken the whole protected prefix.
						all[0].Mounts[0].Name = "anchorage-home-" + strings.TrimPrefix(candidateHome, "whr-stage-home-")
						o := candidateCleanupSelector(selector, role)
						o.Volumes, o.Force = flags.volumes, flags.force
						want := 68
						if flags.volumes && !flags.force {
							want = 70
						}
						assertCandidateCleanupRefused(t, all, o, want)
					})
				}
			}
		}
	}
}

func TestCandidateCleanupPrimarySharedAndProtectedRefused(t *testing.T) {
	for _, role := range []string{"staging", "temp"} {
		for _, destination := range []string{"/home/captain", "/home/vscode"} {
			for _, hazard := range []string{"primary-running", "primary-exited", "shared-running", "shared-exited", "readonly", "global-readonly", "tool-root", "tool-descendant", "global-tool", "bundledock", "foreign"} {
				for _, selector := range []string{"id", "role", "label"} {
					// A role selector cannot match a foreign container.
					if hazard == "foreign" && selector == "role" {
						continue
					}
					t.Run(fmt.Sprintf("%s/%s/%s/%s", role, destination, hazard, selector), func(t *testing.T) {
						all := candidateCleanupFixture(role, destination)
						other := removalFixture()[0]
						other.ID, other.Name = strings.Repeat("b", 64), "/other"
						other.Mounts[0].Name = candidateHome
						switch hazard {
						case "primary-running", "primary-exited", "shared-running", "shared-exited":
							other.State = strings.Split(hazard, "-")[1]
							if strings.HasPrefix(hazard, "shared") {
								other.Labels = nil
							}
							all = append(all, other)
						case "readonly":
							all[0].Mounts[0].RW = false
						case "global-readonly":
							other.Labels = nil
							other.Mounts[0].Destination, other.Mounts[0].RW = "/cache", false
							all = append(all, other)
						case "tool-root", "tool-descendant":
							tool := all[0].Mounts[0]
							tool.Destination = "/opt/bdock"
							if hazard == "tool-descendant" {
								tool.Destination += "/toolbox/bin"
							}
							all[0].Mounts = append(all[0].Mounts, tool)
						case "global-tool":
							other.Labels = nil
							other.Mounts[0].Destination = "/opt/bdock/toolbox"
							all = append(all, other)
						case "bundledock":
							all[0].Mounts[0].Name = "bundledock-" + candidateHome
						case "foreign":
							all[0].Labels = map[string]string{"cleanup.fixture": "candidate"}
						}
						o := candidateCleanupSelector(selector, role)
						if hazard == "foreign" && selector == "label" {
							o.Label = "cleanup.fixture=candidate"
						}
						o.Volumes, o.Force = true, true
						assertCandidateCleanupRefused(t, all, o, 68)
						if strings.HasPrefix(hazard, "primary-") {
							o.Volumes = false
							assertCandidateCleanupRefused(t, all, o, 68)
							o.Volumes = true
						}
						o.Force = false
						assertCandidateCleanupRefused(t, all, o, 70)
						// Previewing safe metadata never authorizes unsafe metadata at Apply.
						o.Force = true
						safe := candidateCleanupFixture(role, destination)
						c, log := removalDocker(t, [][]inspection{safe, all}, false)
						tx, err := c.PrepareRemoval(context.Background(), socketPorts(), o)
						if err != nil {
							t.Fatal(err)
						}
						result, err := tx.Apply(context.Background())
						if exitcode.FromError(err) != exitcode.Conflict || mutationText(t, log) != "" {
							t.Fatalf("apply: result=%+v error=%v mutations=%q", result, err, mutationText(t, log))
						}
						for _, a := range result.Actions {
							if a.Attempted || a.Applied {
								t.Fatalf("refused action: %+v", a)
							}
						}
					})
				}
			}
		}
	}
}

func TestCandidateCleanupMissingMountMetadataRefused(t *testing.T) {
	all := candidateCleanupFixture("staging", "/home/captain")
	all[0].Mounts = nil
	c, log := removalDocker(t, [][]inspection{all}, false)
	tx, err := c.PrepareRemoval(context.Background(), socketPorts(), RemovalOptions{Target: all[0].ID, Volumes: true, Force: true})
	if exitcode.FromError(err) != exitcode.Conflict || tx != nil || mutationText(t, log) != "" {
		t.Fatalf("tx=%v error=%v mutations=%q", tx, err, mutationText(t, log))
	}
}

func TestCandidateCleanupRemovesOnlyExplicitCandidate(t *testing.T) {
	for _, role := range []string{"staging", "temp"} {
		for _, destination := range []string{"/home/captain", "/home/vscode"} {
			for _, volumes := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/volumes=%t", role, destination, volumes), func(t *testing.T) {
					all := candidateCleanupFixture(role, destination)
					primary := removalFixture()[0]
					primary.ID, primary.Name = strings.Repeat("b", 64), "/primary"
					primary.Mounts[0].Name = "anchorage-home-primary"
					all = append(all, primary)
					o := RemovalOptions{Target: all[0].ID, Volumes: volumes, Force: true}
					plan, err := planRemoval(all, o)
					if err != nil || len(plan.Containers) != 1 || plan.Containers[0].ID != all[0].ID || plan.PreserveHomeVolume == volumes {
						t.Fatalf("plan=%+v err=%v", plan, err)
					}
					c, log := removalDocker(t, [][]inspection{all, all, {primary}}, false)
					tx, err := c.PrepareRemoval(context.Background(), socketPorts(), o)
					if err != nil {
						t.Fatal(err)
					}
					preview := tx.Preview()
					want := "container:" + all[0].ID + "\n"
					if volumes {
						want += "volume:" + candidateHome + "\n"
						if len(preview.Volumes) != 1 || preview.Volumes[0].Name != candidateHome || len(preview.Volumes[0].Mounts) != 1 || preview.Volumes[0].Mounts[0].ContainerID != all[0].ID || preview.Volumes[0].Mounts[0].Destination != destination {
							t.Fatalf("preview=%+v", preview)
						}
					} else if len(preview.Volumes) != 0 || len(preview.PreservedHomeVolumes) != 1 || preview.PreservedHomeVolumes[0] != candidateHome {
						t.Fatalf("preview=%+v", preview)
					}
					result, err := tx.Apply(context.Background())
					if err != nil || mutationText(t, log) != want || len(result.Actions) != 1+len(preview.Volumes) {
						t.Fatalf("result=%+v err=%v mutations=%q want=%q", result, err, mutationText(t, log), want)
					}
					for _, a := range result.Actions {
						if !a.Attempted || !a.Applied {
							t.Fatalf("unapplied: %+v", a)
						}
					}
				})
			}
		}
	}
}
