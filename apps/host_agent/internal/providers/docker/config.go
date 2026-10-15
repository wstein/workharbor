// SPDX-License-Identifier: EUPL-1.2
// Package docker implements a closed provider boundary; no general Docker executor.
package docker

import (
	"context"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Scope struct {
	OwnerID, HostID, WorkspaceID, RuntimeID, AllocationOperationID, StorageID string
	Generation                                                                uint64
}

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var hexID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func fail(code string) error { return &config.Error{Code: code} }
func (s Scope) valid() bool {
	for _, id := range []string{s.OwnerID, s.HostID, s.WorkspaceID, s.RuntimeID, s.AllocationOperationID, s.StorageID} {
		if !uuid.MatchString(id) {
			return false
		}
	}
	return s.Generation > 0 && s.Generation <= 9223372036854775807
}
func (s Scope) Name() string { return "workharbor-runtime-" + s.RuntimeID }
func (s Scope) Labels() map[string]string {
	return map[string]string{"workharbor.provider": "docker", "workharbor.owner": s.OwnerID, "workharbor.host": s.HostID, "workharbor.workspace": s.WorkspaceID, "workharbor.runtime": s.RuntimeID, "workharbor.generation": strconv.FormatUint(s.Generation, 10), "workharbor.allocation-operation": s.AllocationOperationID, "workharbor.storage": s.StorageID}
}

type ImageConfig struct {
	Metadata string
	Volumes  map[string]any
	OnBuild  []string
}
type Mount struct {
	Type, Source, Destination string
	RW                        bool
}
type Container struct {
	ID, Name, Image, User, UsernsMode string
	Running                           bool
	Labels                            map[string]string
	Mounts                            []Mount
}

func validData(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, ",\x00\r\n")
}
func clone(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, v := range x {
			m[k] = clone(v)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, v := range x {
			a[i] = clone(v)
		}
		return a
	default:
		return v
	}
}

// FullOverride replaces the CLI document, preserving every admitted source field.
func FullOverride(source map[string]any, s Scope, data string) (map[string]any, error) {
	if e := config.ValidateSource(source); e != nil {
		return nil, e
	}
	if !s.valid() || !validData(data) {
		return nil, fail("invalid_configuration")
	}
	m := clone(source).(map[string]any)
	for k, v := range map[string]any{"workspaceFolder": "/workspace", "workspaceMount": "type=bind,source=" + data + ",target=/workspace", "userEnvProbe": "none", "updateRemoteUserUID": false, "waitFor": "postStartCommand", "shutdownAction": "none", "runArgs": []any{"--name", s.Name()}} {
		m[k] = v
	}
	return m, nil
}

// ValidateImage rejects raw inherited capabilities before the CLI can discard them.
func ValidateImage(ctx context.Context, p config.JSONC, image ImageConfig) ([]map[string]any, error) {
	if len(image.Volumes) != 0 || len(image.OnBuild) != 0 {
		return nil, fail("unsupported_configuration")
	}
	if image.Metadata == "" {
		return []map[string]any{}, nil
	}
	value, e := p.Parse(ctx, []byte(image.Metadata), true)
	if e != nil {
		return nil, e
	}
	var values []any
	switch v := value.(type) {
	case []any:
		values = v
	case map[string]any:
		values = []any{v}
	default:
		return nil, fail("unsupported_configuration")
	}
	entries := make([]map[string]any, 0, len(values))
	for _, v := range values {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fail("unsupported_configuration")
		}
		if e := validateMetadataEntry(m); e != nil {
			return nil, e
		}

		entries = append(entries, clone(m).(map[string]any))
	}
	return entries, nil
}

// VerifyRuntime treats discovery as a closed exact-match operation, never first-match.
func VerifyRuntime(s Scope, data string, containers []Container) (Container, error) {
	if !s.valid() || !validData(data) {
		return Container{}, fail("runtime_conflict")
	}
	if len(containers) == 0 {
		return Container{}, fail("runtime_absent")
	}
	if len(containers) != 1 {
		return Container{}, fail("runtime_conflict")
	}
	c := containers[0]
	if !hexID.MatchString(c.ID) || c.Name != "/"+s.Name() || !hexID.MatchString(strings.TrimPrefix(c.Image, "sha256:")) {
		return Container{}, fail("runtime_conflict")
	}
	for key, value := range s.Labels() {
		if c.Labels[key] != value {
			return Container{}, fail("runtime_conflict")
		}
	}
	if len(c.Mounts) != 1 {
		return Container{}, fail("runtime_conflict")
	}
	m := c.Mounts[0]
	if m.Type != "bind" || m.Source != data || m.Destination != "/workspace" || !m.RW {
		return Container{}, fail("runtime_conflict")
	}
	return c, nil
}

type Target struct {
	Scope                          Scope
	ContainerID, ImageID, DataPath string
}

func VerifyTarget(target Target, containers []Container) (Container, error) {
	if !hexID.MatchString(target.ContainerID) || !hexID.MatchString(strings.TrimPrefix(target.ImageID, "sha256:")) {
		return Container{}, fail("runtime_conflict")
	}
	c, e := VerifyRuntime(target.Scope, target.DataPath, containers)
	if e != nil {
		return Container{}, e
	}
	if c.ID != target.ContainerID || strings.TrimPrefix(c.Image, "sha256:") != strings.TrimPrefix(target.ImageID, "sha256:") {
		return Container{}, fail("runtime_conflict")
	}
	return c, nil
}

type EngineInfo struct {
	OSType, Architecture, ServerVersion string
	SecurityOptions                     []string
}
type User struct {
	Container, Remote string
	UID, GID          uint32
}

var numericPair = regexp.MustCompile(`^(0|[1-9][0-9]*):(0|[1-9][0-9]*)$`)

func parseUser(value string) (uint32, uint32, bool) {
	if !numericPair.MatchString(value) {
		return 0, 0, false
	}
	parts := strings.Split(value, ":")
	uid, e := strconv.ParseUint(parts[0], 10, 32)
	gid, ge := strconv.ParseUint(parts[1], 10, 32)
	return uint32(uid), uint32(gid), e == nil && ge == nil && uid != 4294967295 && gid != 4294967295
}

// AdmitUsers derives ownership only from explicit enforced source pairs. Image,
// metadata and Dockerfile user defaults confer no filesystem ownership authority.
func AdmitUsers(source, merged map[string]any, engine EngineInfo, initial *User) (User, error) {
	if e := config.ValidateSource(source); e != nil {
		return User{}, e
	}
	container, cok := source["containerUser"].(string)
	remote, rok := source["remoteUser"].(string)
	_, _, cv := parseUser(container)
	uid, gid, rv := parseUser(remote)
	if !cok || !rok || !cv || !rv || (container != remote && container != "0:0") || merged["containerUser"] != container || merged["remoteUser"] != remote {
		return User{}, fail("unsupported_configuration")
	}
	if !supportedEngine(engine) {
		return User{}, fail("unsupported_configuration")
	}
	if initial != nil && (initial.UID != uid || initial.GID != gid) {
		return User{}, fail("unsupported_configuration")
	}
	return User{Container: container, Remote: remote, UID: uid, GID: gid}, nil
}
