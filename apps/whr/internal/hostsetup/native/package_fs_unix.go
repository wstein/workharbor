// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package native

// BrewExecutable supplies the read-only package-manager availability port using
// the existing rooted metadata adapter. It never opens or runs a package command;
// fixed-path process execution remains a separate explicitly supplied capability.
func (h *Host) BrewExecutable(p string) (bool, error) { return h.Detector().BrewExecutable(p) }
