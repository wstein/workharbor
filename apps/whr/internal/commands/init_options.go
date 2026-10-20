// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"github.com/spf13/cobra"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
)

// initOptions captures typed flags for the shared proposal model without I/O.
func initOptions(c *cobra.Command) (scaffold.Options, error) {
	var o scaffold.Options
	for _, setting := range []struct {
		name   string
		target **string
	}{{"name", &o.Name}, {"project-url", &o.ProjectURL}, {"start-dir", &o.StartDir}, {"human-shell", &o.HumanShell}, {"console-mode", &o.ConsoleMode}, {"shortcuts", &o.Shortcuts}, {"devcontainer", &o.Devcontainer}, {"template", &o.Template}} {
		if !c.Flags().Changed(setting.name) {
			continue
		}
		value, err := c.Flags().GetString(setting.name)
		if err != nil {
			return o, exitcode.Errorf(exitcode.PathUnclassified, "init flag registration is incomplete")
		}
		*setting.target = &value
	}
	for _, flag := range []struct {
		name   string
		target *bool
	}{{"non-interactive", &o.NonInteractive}, {"yes", &o.Yes}, {"dry-run", &o.DryRun}, {"no-agents-block", &o.NoAgentsBlock}} {
		value, err := c.Flags().GetBool(flag.name)
		if err != nil {
			return o, exitcode.Errorf(exitcode.PathUnclassified, "init flag registration is incomplete")
		}
		*flag.target = value
	}
	overwrite, err := c.Flags().GetStringArray("overwrite")
	if err != nil {
		return o, exitcode.Errorf(exitcode.PathUnclassified, "init overwrite registration is incomplete")
	}
	o.Overwrite = overwrite
	// Check flag values before mode/consent or filesystem failures. The pure
	// model checks the current schema; an absent devcontainer is expected in
	// this empty probe and is the only error deferred to actual discovery.
	probe := o
	probe.NonInteractive = false
	probe.DryRun = true
	if _, err := scaffold.Build(scaffold.Snapshot{}, probe); err != nil && exitcode.FromError(err) != exitcode.NeedsHuman {
		return o, err
	}
	if !o.NonInteractive && !o.DryRun {
		return o, exitcode.Errorf(exitcode.PathBadArguments, "interactive init is not in this release")
	}
	if o.NonInteractive && !o.Yes && !o.DryRun {
		return o, exitcode.Errorf(exitcode.PathHumanDecisionMissing, "--non-interactive requires --yes (or --dry-run)")
	}
	return o, nil
}
