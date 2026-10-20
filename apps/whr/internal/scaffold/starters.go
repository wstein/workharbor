// SPDX-License-Identifier: EUPL-1.2
package scaffold

// UnsupportedStarters lists the frozen catalog without generating or executing
// any template. Admission remains a separate provider task.
func UnsupportedStarters() string {
	return "Starter catalog (unsupported; pending provider admission and real-host acceptance): buildpack-deps-image, buildpack-deps-dockerfile. Features and Compose are unsupported. Reuse an existing devcontainer with --devcontainer <relative-path>."
}
