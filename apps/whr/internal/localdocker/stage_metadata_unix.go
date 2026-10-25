//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

type stageDaemonInfo struct {
	ID, OSType, OperatingSystem, KernelVersion, Name, Architecture string
}
type stageMetadata struct {
	Daemon     stageDaemonInfo
	Containers []inspection
	Statuses   map[string]string
}

const stageDaemonProjection = `{"ID":{{json .ID}},"OSType":{{json .OSType}},"OperatingSystem":{{json .OperatingSystem}},"KernelVersion":{{json .KernelVersion}},"Name":{{json .Name}},"Architecture":{{json .Architecture}}}`

func readStageMetadata(parent context.Context, runner *stageRunner) (stageMetadata, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var result stageMetadata
	remaining := maxOutput
	read := func(args ...string) ([]byte, error) {
		out, e := runner.Run(ctx, "metadata", "docker", args, nil, 5*time.Second)
		if e != nil {
			return nil, e
		}
		remaining -= len(out.Stdout) + len(out.Stderr)
		if remaining < 0 {
			return nil, stageConflict("lifecycle metadata output quota")
		}
		return out.Stdout, nil
	}
	b, e := read("info", "--format", stageDaemonProjection)
	if e != nil {
		return result, e
	}
	decoder := newStageDockerDecoder(b)
	if e = decodeDockerRecord(decoder, &result.Daemon); e != nil || decoder.Decode(new(any)) != io.EOF || result.Daemon.ID == "" || len(result.Daemon.ID) > 256 {
		return result, stageConflict("lifecycle daemon identity unavailable")
	}
	result.Containers, result.Statuses, e = inspectAllWithReader(ctx, read)
	return result, e
}

func newStageDockerDecoder(data []byte) *json.Decoder { return json.NewDecoder(bytes.NewReader(data)) }

func selectStagePrimary(all []inspection, selector string) (inspection, error) {
	var found []inspection
	for _, item := range all {
		if item.ID == selector || strings.TrimPrefix(item.Name, "/") == selector {
			_, recognized := workspaceRole(item.Labels)
			if recognized {
				found = append(found, item)
			}
		}
	}
	if len(found) == 0 {
		return inspection{}, exitcode.Errorf(exitcode.PathTargetAbsent, "local stage primary target absent")
	}
	if len(found) > 1 {
		return inspection{}, exitcode.Errorf(exitcode.PathBadArguments, "local stage primary target ambiguous")
	}
	role, _ := workspaceRole(found[0].Labels)
	if role != "primary" {
		return inspection{}, stageConflict("selected target must be primary")
	}
	if found[0].State != "running" {
		return inspection{}, stageConflict("primary must be running")
	}
	return found[0], nil
}
