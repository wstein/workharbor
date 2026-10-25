//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The known greeting/smoke body actually consumes HEAD/branch, local status and
// upstream-ref information. Bind that projection and the per-context config,
// rather than assigning the exit-only status predicate to arbitrary Git code.
func stageConsumedGitInputDigest(ctx context.Context, repository, current stageGitSnapshot, identity bool) (string, error) {
	original, err := readStageGitSnapshotContext(ctx, repository.Source, false)
	if err != nil {
		return "", err
	}
	if err = stageAdmitConsumedGitConfig(ctx, original); err != nil {
		return "", err
	}
	if err = stageAdmitConsumedGitConfig(ctx, current); err != nil {
		return "", err
	}
	if !identity {
		if err = stageValidateGitStatus(ctx, current); err != nil {
			return "", err
		}
		return stageDigest(map[string]any{"predicate": "status_success", "original_config": original.ConfigDigest, "candidate_config": current.ConfigDigest}), nil
	}
	if original.Head != current.Head || original.HeadRef != current.HeadRef {
		return "", stageConflict("known Git execution version differs between original and candidate")
	}
	source, err := stageKnownGitProjection(ctx, original)
	if err != nil {
		return "", err
	}
	candidate, err := stageKnownGitProjection(ctx, current)
	if err != nil {
		return "", err
	}
	return stageDigest(map[string]any{"original": source, "candidate": candidate}), nil
}
func stageKnownGitProjection(ctx context.Context, snapshot stageGitSnapshot) (map[string]any, error) {
	status, err := stageReadGitStatusDigest(ctx, snapshot, true)
	if err != nil {
		return nil, err
	}
	upstream, oid, counts, err := stageConsumedUpstream(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	return map[string]any{"head": snapshot.Head, "branch": snapshot.HeadRef, "config": snapshot.ConfigDigest, "upstream": upstream, "upstream_oid": oid, "counts": counts, "status": status}, nil
}

// Resolve only the selected branch's upstream from inert config data. Other
// branches and operation result refs are not inputs to the inspected greeting.
func stageConsumedUpstream(parent context.Context, snapshot stageGitSnapshot) (string, string, string, error) {
	ctx, _, err := pinStageGit(parent)
	if err != nil {
		return "", "", "", err
	}
	private, err := os.MkdirTemp("", "whr-git-upstream-")
	if err != nil {
		return "", "", "", stageConflict("consumed Git upstream storage unavailable")
	}
	defer os.RemoveAll(private)
	if err = os.Chmod(private, 0700); err != nil {
		return "", "", "", stageConflict("consumed Git upstream storage mode")
	}
	config, _, err := stageConfigEntries(ctx, private, snapshot)
	if err != nil {
		return "", "", "", err
	}
	if !strings.HasPrefix(snapshot.HeadRef, "refs/heads/") {
		return "", "", "no upstream", nil
	}
	prefix := "branch." + strings.TrimPrefix(snapshot.HeadRef, "refs/heads/")
	remotes, merges := config[prefix+".remote"], config[prefix+".merge"]
	if len(remotes) == 0 || len(merges) == 0 {
		return "", "", "no upstream", nil
	}
	unsupported := func() (string, string, string, error) {
		return "", "", "", stageConflict("Git execution upstream dependency capability unsupported")
	}
	if len(remotes) != 1 || len(merges) != 1 {
		return unsupported()
	}
	remote, merge := remotes[0], merges[0]
	target := ""
	if remote == "." {
		target = merge
	} else {
		for _, spec := range config["remote."+remote+".fetch"] {
			spec = strings.TrimPrefix(spec, "+")
			from, to, ok := strings.Cut(spec, ":")
			if !ok || strings.HasPrefix(from, "^") || strings.Count(from, "*") != strings.Count(to, "*") || strings.Count(from, "*") > 1 {
				return unsupported()
			}
			mapped := ""
			if strings.Contains(from, "*") {
				before, after, _ := strings.Cut(from, "*")
				if strings.HasPrefix(merge, before) && strings.HasSuffix(merge, after) && len(merge) >= len(before)+len(after) {
					mapped = strings.Replace(to, "*", merge[len(before):len(merge)-len(after)], 1)
				}
			} else if from == merge {
				mapped = to
			}
			if mapped != "" {
				if target != "" && target != mapped {
					return unsupported()
				}
				target = mapped
			}
		}
	}
	if target == "" {
		return "", "", "no upstream", nil
	}
	if !stageRef(target) {
		return unsupported()
	}
	oid := snapshot.Refs[target]
	if oid == "" {
		return target, "", "no upstream", nil
	}
	reader := filepath.Join(private, "reader.git")
	if err = initStageGit(ctx, private, reader, snapshot.ObjectFormat); err != nil {
		return "", "", "", err
	}
	output, err := stageGitRead(ctx, private, reader, snapshot.ObjectDir, "", nil, "rev-list", "--left-right", "--count", oid+"..."+snapshot.Head)
	if err != nil {
		return "", "", "", stageConflict("consumed Git upstream counts unavailable")
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return unsupported()
	}
	for _, value := range fields {
		if n, e := strconv.ParseUint(value, 10, 64); e != nil || n > uint64(1<<63-1) {
			return unsupported()
		}
	}
	return target, oid, strings.Join(fields, " "), nil
}

func stageAdmitConsumedGitConfig(parent context.Context, snapshot stageGitSnapshot) error {
	ctx, _, err := pinStageGit(parent)
	if err != nil {
		return err
	}
	private, err := os.MkdirTemp("", "whr-git-input-config-")
	if err != nil {
		return stageConflict("consumed Git configuration storage unavailable")
	}
	defer os.RemoveAll(private)
	if err = os.Chmod(private, 0700); err != nil {
		return stageConflict("consumed Git configuration storage mode")
	}
	// Per-repository status controls are not executable configuration. Active
	// extra rules are not inferred by this bounded known-body capability.
	for _, name := range []string{"exclude", "attributes"} {
		info, e := os.Lstat(filepath.Join(snapshot.CommonDir, "info"))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return stageConflict("Git execution status control capability unsupported")
		}
		data, e := stageAdminFile(snapshot.CommonDir, filepath.Join("info", name), 256<<10)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				return stageConflict("Git execution status control capability unsupported")
			}
		}
	}
	for name := range snapshot.Refs {
		if strings.HasPrefix(name, "refs/replace/") {
			return stageConflict("Git execution replacement dependency capability unsupported")
		}
	}
	if _, e := os.Lstat(filepath.Join(snapshot.CommonDir, "info", "grafts")); e == nil || !os.IsNotExist(e) {
		return stageConflict("Git execution graft dependency capability unsupported")
	}
	values, _, err := stageConfigEntries(ctx, private, snapshot)
	if err != nil {
		return err
	}
	for key := range values {
		if strings.HasPrefix(key, "include.") || strings.HasPrefix(key, "includeif.") || strings.HasPrefix(key, "filter.") || key == "core.fsmonitor" || key == "core.excludesfile" || key == "core.attributesfile" || key == "core.hookspath" || key == "diff.external" {
			return stageConflict("Git execution configuration dependency capability unsupported")
		}
	}
	return nil
}
