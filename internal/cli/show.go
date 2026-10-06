package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// taskCard is the API's task as `whr show` reads it (design §9).
type taskCard struct {
	taskRow
	Runs []struct {
		ID             string `json:"id"`
		State          string `json:"state"`
		TerminalReason string `json:"terminal_reason"`
		DurationMillis *int64 `json:"duration_ms"`
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
	Check *struct {
		SHA        string `json:"sha"`
		Command    string `json:"command"`
		Source     string `json:"source"`
		ExitStatus int    `json:"exit_status"`
		TimedOut   bool   `json:"timed_out"`
		DurationMS int64  `json:"duration_ms"`
		Output     string `json:"output"`
	} `json:"check"`
	PublishAttempts []struct {
		Attempt   int    `json:"attempt"`
		Transient bool   `json:"transient"`
		Error     string `json:"error"`
		RetryAt   string `json:"retry_at"`
	} `json:"publish_attempts"`
	AgentMayRun []struct {
		RunID string `json:"run_id"`
		EnvID string `json:"env_id"`
		Path  string `json:"path"`
		Error string `json:"error"`
	} `json:"agent_may_run"`
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
	for _, r := range c.Runs {
		reason := r.TerminalReason
		if reason == "" && (r.State == "stopped" || r.State == "failed") {
			reason = "unknown"
		}
		if reason != "" {
			p("Run:      %s %s; reason %s", clean(r.ID), clean(r.State), clean(reason))
		}
	}
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
	if k := c.Check; k != nil {
		result := "passed"
		switch {
		case k.TimedOut:
			result = "timed out"
		case k.ExitStatus != 0:
			result = fmt.Sprintf("exit status %d", k.ExitStatus)
		}
		p("Check:    %s (from %s) %s in %s, on %s", clean(k.Command), clean(k.Source), result, (time.Duration(k.DurationMS) * time.Millisecond).String(), clean(k.SHA))
		if k.ExitStatus != 0 || k.TimedOut {
			for _, line := range lastLines(k.Output, 20) {
				p("  | %s", clean(line))
			}
		}
	} else if c.Candidate != nil {
		p("Check:    no receipt for %s", clean(c.Candidate.SHA))
	}
	for _, a := range c.PublishAttempts {
		kind := "refused"
		if a.Transient {
			kind = "will retry"
			if a.RetryAt != "" {
				kind += " after " + clean(a.RetryAt)
			}
		}
		p("Publish:  attempt %d failed (%s): %s", a.Attempt, kind, clean(a.Error))
	}
	for _, n := range c.AgentMayRun {
		p("Warning:  agent may still run (run %s, environment %s, path %s): %s", clean(n.RunID), clean(n.EnvID), clean(n.Path), clean(n.Error))
	}
	p("Tests:    not available")
	p("Notes:    not available")
	if c.UsageLine != "" {
		p("Usage:    %s", clean(c.UsageLine))
	} else {
		p("Usage:    tokens and cost unknown (none reported)")
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

// lastLines returns the last n lines of s.
func lastLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
