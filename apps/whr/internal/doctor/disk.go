// SPDX-License-Identifier: EUPL-1.2

package doctor

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strconv"
)

const (
	dockerRoot    = "/var/lib/docker"
	daemonJSON    = "/etc/docker/daemon.json"
	daemonJSONMax = 64 << 10
)

func (r *run) diskWorkdir() Check {
	const id = "disk.working_directory"
	if r.b.remaining() == 0 {
		return exhausted(id)
	}
	if r.cwd == "" {
		return unknown(id, reasonNotMeasurable, "The current directory is unknown.", "Run whr doctor from an existing directory.", nil)
	}
	return r.free(id, r.cwd, "The filesystem of the current directory", "Free disk space on the filesystem of the current directory and rerun whr doctor.")
}

func (r *run) free(id, path, what, fix string) Check {
	n, err := r.p.FreeBytes(path)
	ev := map[string]any{"path": path}
	if err != nil {
		return unknown(id, reasonNotMeasurable, what+" could not be measured.", "Check that the path is readable and rerun whr doctor.", ev)
	}
	ev["free_bytes"], ev["threshold_bytes"] = n, MinFreeBytes
	if n < MinFreeBytes {
		return failed(id, what+" has less than 2 GiB free ("+strconv.FormatUint(n, 10)+" bytes).", fix, ev)
	}
	return passed(id, what+" has at least 2 GiB free.", ev)
}

func (r *run) diskStorage() Check {
	const id = "disk.container_storage"
	fix := "Run whr doctor on the Linux host that runs Docker, as a user who may read " + dockerRoot + ", with a default data root."
	if r.b.remaining() == 0 {
		return exhausted(id)
	}
	if r.p.OS != "linux" {
		return unknown(id, reasonNotMeasurable, "Container storage is not discoverable without the runtime API on this platform.", fix, map[string]any{"os": r.p.OS})
	}
	if r.dockerHost != "" {
		if _, _, ok := r.dockerSocket(); !ok {
			return unknown(id, reasonNotMeasurable, "DOCKER_HOST does not name a local unix socket, so the data root is not known.", fix, nil)
		}
	}
	body, err := r.p.ReadFile(daemonJSON, daemonJSONMax)
	switch {
	case err == nil:
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) != nil {
			return unknown(id, reasonNotMeasurable, "/etc/docker/daemon.json is not a JSON object, so the data root is not known.", fix, map[string]any{"checked": daemonJSON})
		}
		for _, k := range []string{"data-root", "graph"} {
			if _, set := m[k]; set {
				return unknown(id, reasonNotMeasurable, "The runtime uses a non-default data root, which is not measured.", fix, map[string]any{"checked": daemonJSON, "key": k})
			}
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return unknown(id, reasonNotMeasurable, "/etc/docker/daemon.json could not be read, so the data root is not known.", fix, map[string]any{"checked": daemonJSON})
	}
	if err := r.p.CanRead(dockerRoot); err != nil {
		reason := "unreadable"
		if errors.Is(err, fs.ErrNotExist) {
			reason = "absent"
		}
		return unknown(id, reasonNotMeasurable, dockerRoot+" is not readable by this user.", fix, map[string]any{"path": dockerRoot, "state": reason})
	}
	return r.free(id, dockerRoot, "The container storage filesystem", "Free disk space under "+dockerRoot+" (for example prune unused images) and rerun whr doctor.")
}

var _ = os.DevNull
