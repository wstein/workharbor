package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"
)

// taskCard is the API's task as `whr show` reads it (design §9).
type taskCard struct {
	taskRow
	Runs []struct {
		ID    string `json:"id"`
		State string `json:"state"`
	} `json:"runs"`
	Open      []decisionRow `json:"open_decisions"`
	Candidate *struct {
		Branch  string `json:"branch"`
		SHA     string `json:"sha"`
		PRURL   string `json:"pr_url"`
		CI      string `json:"ci"`
		Pushed  bool   `json:"pushed"`
		Files   int64  `json:"files"`
		Added   int64  `json:"added"`
		Removed int64  `json:"removed"`
	} `json:"candidate"`
	UsageLine string `json:"usage_line"`
}

func newShow(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "show <task>",
		Short:             "Show a task's review card: diff stat, CI, open Decisions and usage",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeTasks,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := s.resolveTask(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "GET", "/v1/tasks/"+url.PathEscape(id), nil, "")
			if err != nil {
				return err
			}
			var card taskCard
			if err := json.Unmarshal(data, &card); err != nil {
				return fmt.Errorf("the task is not what this whr expects: %w", err)
			}
			return s.emit(raw, func(w io.Writer) error { return printCard(w, card) })
		},
	}
}

// printCard writes the review card. A part with no data source yet (tests, the
// agent's notes) is said to be unavailable, not left to look empty.
func printCard(w io.Writer, c taskCard) error {
	p := func(format string, a ...any) { fmt.Fprintf(w, format+"\n", a...) }
	p("Task:     %s  %s %s  (%s)", clean(c.ID), clean(c.Repo), clean(c.Issue), clean(c.State))
	if a := agentName(c.taskRow); a != "" {
		p("Agent:    %s", clean(a))
	}
	if k := c.Candidate; k == nil {
		p("Revision: none yet")
	} else {
		pushed := "not pushed"
		if k.Pushed {
			pushed = "pushed"
		}
		p("Revision: %s @ %s (%s)", clean(k.Branch), clean(k.SHA), pushed)
		if k.Files > 0 || k.Added > 0 || k.Removed > 0 {
			p("Diff:     %d file(s), +%d -%d", k.Files, k.Added, k.Removed)
		} else {
			p("Diff:     not measured")
		}
		ci := k.CI
		if ci == "" {
			ci = "pending"
		}
		p("CI:       %s (for %s)", clean(ci), clean(k.SHA))
		if k.PRURL != "" {
			p("PR:       %s", clean(k.PRURL))
		}
	}
	p("Tests:    not available")
	p("Notes:    not available")
	if c.UsageLine != "" {
		p("Usage:    %s", clean(c.UsageLine))
	} else {
		p("Usage:    none reported")
	}
	if len(c.Open) == 0 {
		p("Decisions: none open")
		return nil
	}
	p("Decisions: %d open", len(c.Open))
	for _, d := range c.Open {
		p("  %s  %s  %s", clean(d.ID), clean(d.Kind), clean(d.Subject))
	}
	return nil
}
