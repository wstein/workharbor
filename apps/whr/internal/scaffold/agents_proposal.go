// SPDX-License-Identifier: EUPL-1.2
package scaffold

import "github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"

func (p *Proposal) agentsFile(s Snapshot, o Options) error {
	e, exists := s.Entries["AGENTS.md"]
	plan := agentsBlockPlan{action: "create", data: renderAgentsBlock()}
	if exists {
		if e.Kind != "file" {
			return exitcode.Errorf(exitcode.PathConflict, "AGENTS.md must be a regular file")
		}
		size := int64(len(e.Data))
		if e.Info != nil {
			size = e.Info.Size()
		}
		var err error
		plan, err = planAgentsBlockSized(e.Data, size, o.permits("AGENTS.md"))
		if err != nil {
			return err
		}
	}
	action := Action{Action: plan.action, Kind: "file", Path: "AGENTS.md"}
	if plan.warning != "" {
		p.preview.AgentsBlockWarnings = append(p.preview.AgentsBlockWarnings, AgentsBlockWarning{Code: plan.warning, Path: "AGENTS.md"})
	}
	if plan.action == "create" || plan.action == "append_block" || plan.action == "replace_block" {
		if err := validAgentsOutput(plan.data); err != nil {
			return err
		}
		p.files["AGENTS.md"] = copyBytes(plan.data)
		p.observe(s, "AGENTS.md")
		diff := string(plan.data)
		if exists {
			diff = wholeDiff("AGENTS.md", e.Data, plan.data)
		}
		action.Diff, action.DiffTruncated = boundedDiff(diff)
	} else {
		p.files["AGENTS.md"] = copyBytes(e.Data)
	}
	p.preview.Actions = append(p.preview.Actions, action)
	return nil
}
func validAgentsOutput(data []byte) error {
	plan, err := planAgentsBlock(data, false)
	if err != nil || plan.action != "skip_existing" {
		return exitcode.Errorf(exitcode.PathConflict, "generated AGENTS.md failed marker integrity validation")
	}
	return nil
}
func (p *Proposal) validateAgentsOutput() error {
	for _, a := range p.preview.Actions {
		if a.Path == "AGENTS.md" && (a.Action == "create" || a.Action == "append_block" || a.Action == "replace_block") {
			return validAgentsOutput(p.files[a.Path])
		}
	}
	return nil
}
