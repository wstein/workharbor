// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"strings"
)

// PreflightInspect is the single full selected-container inspect of FR-004.
// The raw response is consumed only in memory; no environment fields are logged.
func (c Client) PreflightInspect(ctx context.Context, endpoint, id string) ([]byte, error) {
	return c.read(ctx, endpoint, "container", "inspect", "--format", "{{json .}}", id)
}

// Reachable uses exactly the FR-004 liveness inspect. A daemon error reply
// (including container-not-found) proves reachability regardless of exit status.
// The fixed CLI messages are classified without logging raw stderr. See P2's
// upstream-source evidence; unknown/failed/timed-out responses do not prove life.
func (c Client) Reachable(ctx context.Context, endpoint, id string) bool {
	r, err := c.runRead(ctx, endpoint, "container", "inspect", "--format", "{{.State.Running}}", id)
	if err != nil {
		return false
	}
	text := strings.TrimSpace(string(r.stdout))
	if r.status == 0 && (text == "true" || text == "false") {
		return true
	}
	diagnostic := strings.TrimSpace(string(r.stderr))
	return strings.HasPrefix(diagnostic, "Error response from daemon:") || diagnostic == "Error: No such object: "+id || diagnostic == "Error: No such container: "+id || strings.HasPrefix(diagnostic, "request returned ") || strings.HasPrefix(diagnostic, "template parsing error:")
}
