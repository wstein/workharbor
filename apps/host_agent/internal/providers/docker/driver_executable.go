// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func quoteNative(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// Executable selects only the installed product agent and its fixed private mode.
// Source configuration never supplies this path, socket, deadline or argv prefix.
func (d *DriverServer) Executable(ctx context.Context, agent string) (string, error) {
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil || !physicalExecutable(agent) || !ownedSocket(d.Socket) || !ownedDirectory(d.root) {
		return "", fail("provider_refused")
	}
	path := filepath.Join(d.root, "docker-driver")
	script := "#!/bin/sh\nexec " + quoteNative(agent) + " __devcontainer-docker " + quoteNative(d.Socket) + " " + strconv.FormatInt(deadline.UnixMilli(), 10) + " \"$@\"\n"
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		return "", fail("provider_unavailable")
	}
	_, we := f.WriteString(script)
	se := f.Sync()
	ce := f.Close()
	if we != nil || se != nil || ce != nil {
		return "", fail("provider_unavailable")
	}
	return path, nil
}
