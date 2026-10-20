// SPDX-License-Identifier: EUPL-1.2
// Package wizard collects interactive inputs over the shared scaffold proposal.
package wizard

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/terminal"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

type step struct {
	name, label, fallback string
	value                 **string
	existing              bool
}
type session struct {
	ctx   context.Context
	input *bufio.Reader
	out   io.Writer
}

// Run returns only the exact reviewed proposal after explicit write consent.
// It never applies it or rediscovers files: Apply retains the original snapshot
// fingerprints and rejects any change made while the owner answers prompts.
// The input must unblock when ctx is cancelled (the CLI supplies such a reader).
func Run(ctx context.Context, snapshot scaffold.Snapshot, options scaffold.Options, input io.Reader, diagnostics io.Writer) (*scaffold.Proposal, error) {
	s := session{ctx, bufio.NewReader(input), diagnostics}
	if err := s.print("Workspace configuration only; no runtime, tools or Git changes.\nType back, review or cancel at any prompt; edit <flag> at review.\n"); err != nil {
		return nil, err
	}
	steps := []step{
		{"name", "Workspace name (optional)", "", &options.Name, false},
		{"project-url", "Project URL (optional)", "", &options.ProjectURL, false},
		{"start-dir", "Starting directory", ".", &options.StartDir, false},
		{"devcontainer", "Reuse existing devcontainer", ".devcontainer/devcontainer.json", &options.Devcontainer, false},
		{"human-shell", "Human shell (fish/bash/zsh/nu)", "fish", &options.HumanShell, false},
		{"console-mode", "Console mode (herdr/plain)", "herdr", &options.ConsoleMode, false},
		{"shortcuts", "Shared shortcuts (on/off)", "on", &options.Shortcuts, false},
	}
	if e, ok := snapshot.Entries["workharbor.json"]; ok {
		checked := workspacecfg.CheckManifest(e.Data)
		if checked.Manifest == nil || len(checked.Findings) != 0 {
			return nil, exitcode.Errorf(exitcode.PathConflict, "existing workharbor.json fails current validation")
		}
		m := checked.Manifest
		values := []string{m.Name, "", m.StartDir, m.DevcontainerPath, m.HumanShell, m.ConsoleMode, m.Shortcuts}
		if m.Project != nil {
			values[1] = m.Project.URL
		}
		for i, v := range values {
			if v != "" {
				steps[i].fallback = v
				steps[i].existing = true
			}
		}
	}
	// Only the proposal model knows the paths it can modify. These are preview
	// permissions, never write consent: every overwrite is confirmed below.
	preconfirmedAgents := false
	for _, name := range options.Overwrite {
		if name == "AGENTS.md" {
			preconfirmedAgents = true
		}
	}
	agentsEdit := false
	candidate := func() (*scaffold.Proposal, error) {
		o := options
		o.Overwrite = append([]string(nil), options.Overwrite...)
		if agentsEdit {
			o.Overwrite = append(o.Overwrite, "AGENTS.md")
		}
		for name, e := range snapshot.Entries {
			if e.Kind == "file" && (name == "workharbor.json" || strings.HasSuffix(name, "/.gitkeep")) {
				o.Overwrite = append(o.Overwrite, name)
			}
		}
		return scaffold.Build(snapshot, o)
	}
	if preconfirmedAgents {
		if _, err := candidate(); err != nil && exitcode.FromError(err) == exitcode.Conflict {
			return nil, err
		}
	}
	var shown []int
	for i, st := range steps {
		if !st.existing && (st.value == nil || *st.value == nil) {
			shown = append(shown, i)
		}
	}
	pos := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pos < len(shown) {
			idx := shown[pos]
			st := &steps[idx]
			def := st.fallback
			if st.value != nil && *st.value != nil {
				def = **st.value
			}
			answer, err := s.ask(st.label + " [" + terminal.Escape(def) + "]: ")
			if err != nil {
				return nil, err
			}
			switch answer {
			case "cancel":
				return nil, cancelled()
			case "back":
				if pos > 0 {
					pos--
				}
				continue
			case "review":
				pos = len(shown)
				continue
			}
			if answer == "" {
				answer = def
			}
			old := *st.value
			previous := snapshot
			if st.name == "devcontainer" {
				selected, err := snapshot.SelectDevcontainer(ctx, answer)
				if err != nil {
					if err := s.print(terminal.Escape(err.Error()) + "\n"); err != nil {
						return nil, err
					}
					continue
				}
				snapshot = selected
			}
			if answer == "" {
				*st.value = nil
			} else {
				v := answer
				*st.value = &v
			}
			if _, err := candidate(); err != nil && !(st.name != "devcontainer" && options.Devcontainer == nil && exitcode.FromError(err) == exitcode.NeedsHuman) {
				if preconfirmedAgents && exitcode.FromError(err) == exitcode.Conflict {
					return nil, err
				}
				*st.value = old
				snapshot = previous
				if err := s.print(terminal.Escape(err.Error()) + "\n"); err != nil {
					return nil, err
				}
				continue
			}
			pos++
			continue
		}
		proposal, err := candidate()
		if err != nil {
			return nil, err
		}
		if err := s.print(proposal.HumanPreview()); err != nil {
			return nil, err
		}
		answer, err := s.ask("Review: write, back, edit <flag>, review or cancel [cancel]: ")
		if err != nil {
			return nil, err
		}
		switch {
		case answer == "" || answer == "cancel":
			return nil, cancelled()
		case answer == "back":
			if len(shown) > 0 {
				pos = len(shown) - 1
			}
			continue
		case answer == "review":
			continue
		case strings.HasPrefix(answer, "edit "):
			name := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(answer, "edit ")), "--")
			found := false
			for i := range steps {
				if steps[i].name == name {
					shown = append(shown, i)
					pos = len(shown) - 1
					found = true
					break
				}
			}
			if !found {
				if err := s.print("Unknown setting; use a displayed flag name.\n"); err != nil {
					return nil, err
				}
			}
			continue
		case answer == "write":
			for _, action := range proposal.Preview().Actions {
				if action.Action == "overwrite" {
					yes, err := s.confirm(action.Path)
					if err != nil {
						return nil, err
					}
					if !yes {
						return nil, cancelled()
					}
				}
			}
			// Only safe AGENTS edits are offered. Declining this path preserves
			// the original proposal; other paths retain cancellation semantics.
			for _, warning := range proposal.Preview().AgentsBlockWarnings {
				if warning.Code != "agents_block.missing" && warning.Code != "agents_block.different_template" && warning.Code != "agents_block.modified" {
					continue
				}
				agentsEdit = true
				edited, err := candidate()
				agentsEdit = false
				if err != nil {
					return nil, err
				}
				if err := s.print(edited.HumanPreview()); err != nil {
					return nil, err
				}
				yes, err := s.confirm("AGENTS.md")
				if err != nil {
					return nil, err
				}
				if yes {
					proposal = edited
				} else if err := s.print(proposal.HumanPreview()); err != nil {
					return nil, err
				}
			}
			return proposal, nil
		default:
			if err := s.print("Choose write, back, edit <flag>, review or cancel.\n"); err != nil {
				return nil, err
			}
		}
	}
}
func cancelled() error {
	return exitcode.Errorf(exitcode.PathUserInterrupt, "wizard cancelled; no files written")
}
func (s *session) print(text string) error {
	if _, err := io.WriteString(s.out, text); err != nil {
		return exitcode.Errorf(exitcode.PathUnclassified, "writing wizard diagnostics failed")
	}
	return nil
}
func (s *session) ask(prompt string) (string, error) {
	if err := s.ctx.Err(); err != nil {
		return "", err
	}
	if err := s.print(prompt); err != nil {
		return "", err
	}
	// Bound each answer; never interpret an unterminated answer at EOF as consent.
	var b strings.Builder
	for {
		part, err := s.input.ReadSlice('\n')
		prefix := err == bufio.ErrBufferFull
		if err != nil && !prefix {
			return "", cancelled()
		}
		if b.Len()+len(part) > 4096 {
			return "", exitcode.Errorf(exitcode.PathBadArguments, "wizard answer exceeds 4096 bytes")
		}
		b.Write(part)
		if !prefix {
			break
		}
	}
	if err := s.ctx.Err(); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}
func (s *session) confirm(path string) (bool, error) {
	for {
		answer, err := s.ask(fmt.Sprintf("Modify %s? [y/N]: ", terminal.Escape(path)))
		if err != nil {
			return false, err
		}
		switch answer {
		case "y", "yes":
			return true, nil
		case "", "n", "no", "cancel", "back":
			return false, nil
		default:
			if err := s.print("Answer yes or no; declining cancels without changes.\n"); err != nil {
				return false, err
			}
		}
	}
}
