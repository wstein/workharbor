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
)

const stageContainerProjection = `{"Id":{{json .Id}},"Name":{{json .Name}},"ImageID":{{json .Image}},"ConfigImage":{{json .Config.Image}},"User":{{json .Config.User}},"Hostname":{{json .Config.Hostname}},"State":{{json .State.Status}},"Running":{{json .State.Running}},"Healthcheck":{{json .Config.Healthcheck}},"Health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}null{{end}},"Labels":{{json .Config.Labels}},"Mounts":{{json .Mounts}},"Environment":{{json .Config.Env}},"Entrypoint":{{json .Config.Entrypoint}},"Command":{{json .Config.Cmd}}}`
const stageImageProjection = `{"Id":{{json .Id}},"Metadata":{{json (index .Config.Labels "devcontainer.metadata")}}}`
const stageVolumeProjection = `{"Name":{{json .Name}},"Driver":{{json .Driver}},"CreatedAt":{{json .CreatedAt}},"Labels":{{json .Labels}}}`

func readStageContainer(ctx context.Context, r *stageRunner, id string) (stageContainerDetails, error) {
	var result stageContainerDetails
	if !fullID(id) {
		return result, stageConflict("container observation identity invalid")
	}
	out, e := r.Run(ctx, "metadata", "docker", []string{"container", "inspect", "--format", stageContainerProjection, id}, nil, 5*time.Second)
	if e != nil {
		return result, e
	}
	var raw struct {
		ID                                                string `json:"Id"`
		Name, ImageID, ConfigImage, User, Hostname, State string
		Running                                           bool
		Healthcheck                                       *struct{ Test []string }
		Health                                            *string
		Labels                                            map[string]string
		Mounts                                            []mount
		Environment, Entrypoint, Command                  []string
	}
	d := json.NewDecoder(bytes.NewReader(out.Stdout))
	if e = decodeDockerRecord(d, &raw); e != nil || d.Decode(new(any)) != io.EOF || raw.ID != id || !stageImageID(raw.ImageID) || raw.Name == "" || raw.Running != (raw.State == "running") {
		return result, stageConflict("container observation identity or shape changed")
	}
	healthcheck := raw.Healthcheck != nil && len(raw.Healthcheck.Test) > 0 && raw.Healthcheck.Test[0] != "NONE"
	if raw.Health != nil && *raw.Health != "starting" && *raw.Health != "healthy" && *raw.Health != "unhealthy" {
		return result, stageConflict("container health observation unsupported")
	}
	result = stageContainerDetails{ID: raw.ID, Name: raw.Name, ImageID: raw.ImageID, ConfigImage: raw.ConfigImage, User: raw.User, Hostname: raw.Hostname, State: raw.State, Running: raw.Running, Healthcheck: healthcheck, Health: raw.Health, Labels: raw.Labels, Mounts: raw.Mounts, Environment: raw.Environment, Entrypoint: raw.Entrypoint, Command: raw.Command}
	if result.Labels == nil {
		result.Labels = map[string]string{}
	}
	if result.Mounts == nil {
		result.Mounts = []mount{}
	}
	if result.Environment == nil {
		result.Environment = []string{}
	}
	if result.Entrypoint == nil {
		result.Entrypoint = []string{}
	}
	if result.Command == nil {
		result.Command = []string{}
	}
	return result, nil
}

func stageContainerBindingDigest(value stageContainerDetails) string {
	value.State = ""
	value.Running = false
	value.Health = nil
	return stageDigest(value)
}
func stageImageID(value string) bool {
	return len(value) == 71 && strings.HasPrefix(value, "sha256:") && fullID(strings.TrimPrefix(value, "sha256:"))
}

func readStageImage(ctx context.Context, r *stageRunner, image string) (stageImageDetails, error) {
	result := stageImageDetails{Metadata: []map[string]any{}}
	if !stageImageID(image) {
		return result, stageConflict("immutable image observation identity required")
	}
	out, e := r.Run(ctx, "metadata", "docker", []string{"image", "inspect", "--format", stageImageProjection, "--", image}, nil, 5*time.Second)
	if e != nil {
		return result, e
	}
	var raw struct {
		ID       string `json:"Id"`
		Metadata *string
	}
	d := json.NewDecoder(bytes.NewReader(out.Stdout))
	if e = decodeDockerRecord(d, &raw); e != nil || d.Decode(new(any)) != io.EOF || raw.ID != image {
		return result, stageConflict("image observation identity or shape changed")
	}
	result.ID = raw.ID
	if raw.Metadata != nil && *raw.Metadata != "" {
		md := json.NewDecoder(strings.NewReader(*raw.Metadata))
		md.UseNumber()
		if e = decodeDockerRecord(md, &result.Metadata); e != nil || md.Decode(new(any)) != io.EOF || result.Metadata == nil {
			return result, stageConflict("image lifecycle metadata shape unsupported")
		}
	}
	return result, nil
}

func readStageVolume(ctx context.Context, r *stageRunner, name string) (stageVolumeDetails, error) {
	var result stageVolumeDetails
	if !volumeName(name) {
		return result, stageConflict("volume observation name invalid")
	}
	out, e := r.Run(ctx, "metadata", "docker", []string{"volume", "inspect", "--format", stageVolumeProjection, "--", name}, nil, 5*time.Second)
	if e != nil {
		return result, e
	}
	d := json.NewDecoder(bytes.NewReader(out.Stdout))
	if e = decodeDockerRecord(d, &result); e != nil || d.Decode(new(any)) != io.EOF || result.Name != name || result.Driver == "" || result.CreatedAt == "" {
		return result, stageConflict("volume observation identity or shape changed")
	}
	if _, e = time.Parse(time.RFC3339Nano, result.CreatedAt); e != nil {
		return result, stageConflict("volume creation observation invalid")
	}
	if result.Labels == nil {
		result.Labels = map[string]string{}
	}
	return result, nil
}
