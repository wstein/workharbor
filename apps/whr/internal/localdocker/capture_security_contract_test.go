// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func captureControl(ctx context.Context, limits *stageCaptureLimits, phase func(string, string), command func(*exec.Cmd)) context.Context {
	return context.WithValue(ctx, stageCaptureControlKey{}, stageCaptureControl{Limits: limits, Phase: phase, Command: command})
}
func TestLocalStageGitHostEnvironmentAndCommandIsolation(t *testing.T) {
	src := captureRegressionRepo(t)
	home := capturePhysicalTemp(t)
	marker := filepath.Join(home, "executed")
	hook := filepath.Join(home, "host-hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	poison := filepath.Join(home, "host-config")
	if err := os.WriteFile(poison, []byte("[core]\nfsmonitor = "+hook+"\nhooksPath = "+home+"\n[alias]\nstatus = !"+hook+"\n[credential]\nhelper = !"+hook+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"GIT_DIR": "FORGED-GIT-DIR", "GIT_WORK_TREE": "FORGED-WORK-TREE", "GIT_CONFIG_PARAMETERS": "FORGED-CONFIG", "GIT_CONFIG_SYSTEM": poison, "GIT_CONFIG_GLOBAL": poison, "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.fsmonitor", "GIT_CONFIG_VALUE_0": hook, "SSH_AUTH_SOCK": "FORGED-SSH", "AWS_SECRET_ACCESS_KEY": "FORGED-CREDENTIAL"} {
		t.Setenv(key, value)
	}
	cmd := stageGitCommand(context.Background(), home, filepath.Join(src, ".git"), "", "", false, "--work-tree="+src, "status", "--porcelain")
	env := map[string]string{}
	for _, item := range cmd.Env {
		k, v, _ := strings.Cut(item, "=")
		env[k] = v
	}
	for key, want := range map[string]string{"HOME": home, "TMPDIR": home, "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_COUNT": "0", "GIT_NO_REPLACE_OBJECTS": "1", "GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0", "GIT_NO_LAZY_FETCH": "1", "GIT_ALLOW_PROTOCOL": ""} {
		if got, ok := env[key]; !ok || got != want {
			t.Errorf("isolated environment policy missing for %s", key)
		}
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "SSH_AUTH_SOCK", "AWS_SECRET_ACCESS_KEY"} {
		if _, ok := env[key]; ok {
			t.Errorf("inherited forbidden variable name: %s", key)
		}
	}
	for _, arg := range []string{"core.hooksPath=/dev/null", "core.fsmonitor=false"} {
		if !slices.Contains(cmd.Args, arg) {
			t.Errorf("required isolated Git option missing: %s", arg)
		}
	}
	if err := runStageGit(cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("host configuration executed")
	}
	transport := stageGitEnv(home, "", "", true)
	if !slices.Contains(transport, "GIT_ALLOW_PROTOCOL=file") {
		t.Fatal("clone transport policy is not file-only")
	}
	var configArgs []string
	ctx := captureControl(context.Background(), nil, nil, func(cmd *exec.Cmd) {
		if slices.Contains(cmd.Args, "config") {
			configArgs = append([]string{}, cmd.Args...)
		}
	})
	if _, _, err := stageConfigEntries(ctx, home, stageGitSnapshot{CommonDir: filepath.Join(src, ".git")}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(configArgs, "--no-includes") {
		t.Fatal("source config reader must explicitly disable includes")
	}
}
func TestLocalStageCapturePublishedLimits(t *testing.T) {
	limits := defaultStageCaptureLimits()
	if limits.WorkingEntries != 100000 || limits.Objects != 1000000 || limits.FileBytes != 64<<20 || limits.WorkingBytes != 2<<30 || limits.ObjectBytes != 8<<30 || limits.SubmoduleDepth != 8 || limits.SubmoduleCount != 32 {
		t.Fatal("published capture resource ceilings changed without contract reconciliation")
	}
}
func TestLocalStageCaptureWorkingQuotaBoundaries(t *testing.T) {
	for _, kind := range []string{"file", "total", "entries"} {
		for _, over := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/N", true: "/N+1"}[over], func(t *testing.T) {
				src := capturePhysicalTemp(t)
				limits := defaultStageCaptureLimits()
				limits.FileBytes = 8
				limits.WorkingBytes = 16
				limits.WorkingEntries = 4
				files := []int{8}
				captureReason := "capture per-file working-byte quota exceeded"
				copyReason := "captured working file identity or quota"
				switch kind {
				case "file":
					if over {
						files[0]++
					}
				case "total":
					files = []int{8, 8}
					if over {
						files = append(files, 1)
					}
					captureReason = "capture total working-byte quota exceeded"
				case "entries":
					files = []int{1, 1, 1}
					if over {
						files = append(files, 1)
					}
					captureReason = "capture filesystem entry quota exceeded"
					copyReason = "captured working entry quota exceeded"
				}
				for i, size := range files {
					if err := os.WriteFile(filepath.Join(src, string(rune('a'+i))), []byte(strings.Repeat("x", size)), 0600); err != nil {
						t.Fatal(err)
					}
				}
				ctx := captureControl(context.Background(), &limits, nil, nil)
				_, _, err := scanStageFiles(ctx, src, "")
				if over {
					requireCaptureConflict(t, err, captureReason)
				} else if err != nil {
					t.Fatal(err)
				}
				err = copyStageWorkingSkip(ctx, src, capturePhysicalTemp(t), nil)
				if over {
					requireCaptureConflict(t, err, copyReason)
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
func TestLocalStageCaptureDriftCheckpointsAndStatusEquality(t *testing.T) {
	for _, phase := range []string{"files-copied", "repository-copied", "repositories-copied", "before-candidate-status"} {
		t.Run(phase, func(t *testing.T) {
			src := captureRegressionRepo(t)
			called := false
			ctx := captureControl(context.Background(), nil, func(got, path string) {
				if got != phase {
					return
				}
				called = true
				target := filepath.Join(src, ".git", "HEAD")
				data := []byte("malformed\n")
				if phase == "files-copied" {
					target = filepath.Join(src, "file")
					data = []byte("changed\n")
				}
				if phase == "before-candidate-status" {
					target = filepath.Join(path, "file")
					data = []byte("changed\n")
				}
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
			}, nil)
			parent := capturePhysicalTemp(t)
			if phase == "files-copied" {
				_, err := captureStageFiles(ctx, src, parent)
				requireCaptureConflict(t, err, "source inputs changed since capture")
			} else {
				files := captureRegressionFiles(t, src)
				_, err := captureStageGit(ctx, src, files.Path, filepath.Join(parent, "clone"))
				want := map[string]string{"repository-copied": "source Git state changed during capture", "repositories-copied": "source Git state changed during recursive capture", "before-candidate-status": "Git admitted source and candidate status differ"}[phase]
				requireCaptureConflict(t, err, want)
			}
			if !called {
				t.Fatal("capture checkpoint not exercised")
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 0 {
				t.Fatalf("drift retained private capture: %v %v", entries, err)
			}
		})
	}
}
func TestLocalStageCaptureSymlinkRefusalReasons(t *testing.T) {
	for _, kind := range []string{"absolute_inside", "relative_outside", "gitdir_pointer"} {
		t.Run(kind, func(t *testing.T) {
			src := captureRegressionRepo(t)
			if kind == "gitdir_pointer" {
				real := filepath.Join(capturePhysicalTemp(t), "admin")
				if err := os.Rename(filepath.Join(src, ".git"), real); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(capturePhysicalTemp(t), "admin-link")
				if err := os.Symlink(real, link); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(src, ".git"), []byte("gitdir: "+link+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				_, err := readStageGitSnapshotContext(context.Background(), src)
				requireCaptureConflict(t, err, "Git administration path is symlinked or unavailable")
				return
			}
			target := filepath.Join(src, "file")
			reason := "capture absolute symlink cannot retain captured execution binding"
			if kind == "relative_outside" {
				outside := filepath.Join(capturePhysicalTemp(t), "file")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				target, err = filepath.Rel(src, outside)
				if err != nil {
					t.Fatal(err)
				}
				reason = "capture symlink outside capture"
			}
			if err := os.Symlink(target, filepath.Join(src, "link")); err != nil {
				t.Fatal(err)
			}
			_, _, err := scanStageFiles(context.Background(), src, "")
			requireCaptureConflict(t, err, reason)
		})
	}
}
func TestLocalStageGitObjectAdministrationSingleAudit(t *testing.T) {
	src := captureRegressionRepo(t)
	objects := filepath.Join(src, ".git", "objects")
	audit := &stageObjectAudit{checked: map[string]bool{}}
	ctx := context.WithValue(context.Background(), stageObjectAuditKey{}, audit)
	if _, err := stageObjectStoreBinding(ctx, objects); err != nil {
		t.Fatal(err)
	}
	// Same operation and physical identity reuse the completed administration
	// audit. A fresh operation must audit again and refuse this unsafe node.
	if err := os.Symlink("missing", filepath.Join(objects, "post-audit-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := stageObjectStoreBinding(ctx, objects); err != nil {
		t.Fatalf("same store audited twice: %v", err)
	}
	_, err := stageObjectStoreBinding(context.Background(), objects)
	requireCaptureConflict(t, err, "Git object administration is symlinked")
}

func captureSecurityGit(t *testing.T, src string, limits *stageCaptureLimits) (stageGitCapture, error) {
	t.Helper()
	files := captureRegressionFiles(t, src)
	return captureStageGit(captureControl(context.Background(), limits, nil, nil), src, files.Path, filepath.Join(capturePhysicalTemp(t), "clone"))
}
func TestLocalStageGitObjectAndSubmoduleQuotaBoundaries(t *testing.T) {
	src := captureRegressionRepo(t)
	baseline, err := captureSecurityGit(t, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"objects", "object_bytes"} {
		for _, over := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/N", true: "/N+1"}[over], func(t *testing.T) {
				limits := defaultStageCaptureLimits()
				want := "Git object count quota exceeded"
				if kind == "objects" {
					limits.Objects = baseline.ObjectCount
					if over {
						limits.Objects--
					}
				} else {
					limits.ObjectBytes = baseline.ObjectBytes
					if over {
						limits.ObjectBytes--
					}
					want = "Git expanded object byte quota exceeded"
				}
				_, err := captureSecurityGit(t, src, &limits)
				if over {
					requireCaptureConflict(t, err, want)
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	child := captureRegressionRepo(t)
	fixtureGit(t, src, "-c", "protocol.file.allow=always", "submodule", "add", child, "module")
	fixtureGit(t, src, "commit", "-am", "module")
	baseline, err = captureSecurityGit(t, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var size int64
	for _, repo := range baseline.Repositories {
		count += repo.ObjectCount
		size += repo.ObjectBytes
	}
	for _, kind := range []string{"depth", "submodules", "combined_objects", "combined_bytes"} {
		for _, over := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/N", true: "/N+1"}[over], func(t *testing.T) {
				limits := defaultStageCaptureLimits()
				want := "Git operation object graph quota exceeded"
				switch kind {
				case "depth":
					limits.SubmoduleDepth = 1
					if over {
						limits.SubmoduleDepth = 0
					}
					want = "populated Git submodule depth quota exceeded"
				case "submodules":
					limits.SubmoduleCount = 1
					if over {
						limits.SubmoduleCount = 0
					}
					want = "populated Git submodule count quota exceeded"
				case "combined_objects":
					limits.Objects = count
					if over {
						limits.Objects--
					}
				case "combined_bytes":
					limits.ObjectBytes = size
					if over {
						limits.ObjectBytes--
					}
				}
				_, err := captureSecurityGit(t, src, &limits)
				if over {
					requireCaptureConflict(t, err, want)
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestLocalStageGitAtomicPublicationPreservesRacedEmptyDestination(t *testing.T) {
	src := captureRegressionRepo(t)
	files := captureRegressionFiles(t, src)
	clone := filepath.Join(capturePhysicalTemp(t), "clone")
	var protected os.FileInfo
	ctx := captureControl(context.Background(), nil, func(phase, _ string) {
		if phase == "repositories-copied" {
			if err := os.Mkdir(clone, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			protected, err = os.Lstat(clone)
			if err != nil {
				t.Fatal(err)
			}
		}
	}, nil)
	_, err := captureStageGit(ctx, src, files.Path, clone)
	requireCaptureConflict(t, err, "Git clone publication failed or destination changed")
	current, err := os.Lstat(clone)
	if err != nil || !os.SameFile(protected, current) {
		t.Fatal("raced empty destination was replaced")
	}
}
func TestLocalStageGitCleanupRetainsReplacedPrivateRoot(t *testing.T) {
	src := captureRegressionRepo(t)
	files := captureRegressionFiles(t, src)
	parent := capturePhysicalTemp(t)
	clone := filepath.Join(parent, "clone")
	var staged string
	var foreign os.FileInfo
	ctx := captureControl(context.Background(), nil, func(phase, _ string) {
		if phase == "repositories-copied" {
			if err := os.Rename(staged, filepath.Join(parent, "retained-original")); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(staged, 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			foreign, err = os.Lstat(staged)
			if err != nil {
				t.Fatal(err)
			}
		}
	}, func(cmd *exec.Cmd) {
		if slices.Contains(cmd.Args, "clone") {
			staged = cmd.Args[len(cmd.Args)-1]
		}
	})
	result, err := captureStageGit(ctx, src, files.Path, clone)
	requireCaptureConflict(t, err, "temporary capture retained after cleanup failure")
	if result.Clone != staged {
		t.Fatal("cleanup refusal omitted retained root inventory")
	}
	current, err := os.Lstat(staged)
	if err != nil || !os.SameFile(current, foreign) {
		t.Fatal("cleanup removed a replaced root")
	}
	if _, err := os.Stat(filepath.Join(parent, "retained-original", ".git")); err != nil {
		t.Fatal("original private payload disappeared")
	}
	if _, err := os.Lstat(clone); !os.IsNotExist(err) {
		t.Fatal("unverified root was published")
	}
}
func TestLocalStageGitStartAndIndexIOFailuresAreClassified(t *testing.T) {
	cmd := exec.Command(filepath.Join(capturePhysicalTemp(t), "CANARY\nmissing"))
	requireCaptureConflict(t, runStageGit(cmd), "Git process start failed")
	src := captureRegressionRepo(t)
	_, _, err := stageReadIndex(context.Background(), filepath.Join(src, ".git"), "index", "sha1", capturePhysicalTemp(t))
	requireCaptureConflict(t, err, "private Git index publication failed")
}

func TestLocalStageGitAlternatesBeyondUnapprovedStoreCap(t *testing.T) {
	root := capturePhysicalTemp(t)
	previous := ""
	for i := 0; i < 33; i++ {
		store := filepath.Join(root, fmt.Sprintf("store-%02d", i))
		if err := os.MkdirAll(filepath.Join(store, "info"), 0700); err != nil {
			t.Fatal(err)
		}
		if previous != "" {
			if err := os.WriteFile(filepath.Join(previous, "info", "alternates"), []byte(store+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		previous = store
	}
	if _, err := stageObjectStoreBinding(context.Background(), filepath.Join(root, "store-00")); err != nil {
		t.Fatalf("unapproved object-store ceiling changed admission: %v", err)
	}
}
