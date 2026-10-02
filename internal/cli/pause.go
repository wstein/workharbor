package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// newPause is `whr pause <task>` (provisional, issue #106): a hard interrupt of the
// task's running agent (D11). The run is paused, what it asked is superseded, and
// the environment keeps running.
func newPause(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "pause <task>",
		Short:             "Pause a task's run: the agent process is stopped, the environment keeps running",
		Long:              "A hard interrupt (D11): none of the agents can pause cooperatively. The agent process is stopped, the run is paused, and the questions and approvals it raised are superseded. `whr resume` starts it again from its session.",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeTasks,
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.taskPost(cmd, args[0], "pause", nil)
		},
	}
}

// newResume is `whr resume <task>` (provisional, issue #106).
func newResume(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "resume <task>",
		Short:             "Resume a paused run from the agent's session, with the supervisor's briefing",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeTasks,
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.taskPost(cmd, args[0], "resume", nil)
		},
	}
}

func (s *state) taskPost(cmd *cobra.Command, ref, verb string, body any) error {
	id, err := s.resolveTask(cmd.Context(), ref)
	if err != nil {
		return err
	}
	c, err := s.api()
	if err != nil {
		return err
	}
	raw, _, err := c.Do(cmd.Context(), "POST", "/v1/tasks/"+url.PathEscape(id)+"/"+verb, body, newKey())
	if err != nil {
		return err
	}
	return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(id)); return err })
}

// newPurge is `whr purge <task>` (provisional, issue #106; design §5.4): it deletes
// the stored transcript content of a task after saying what goes.
func newPurge(s *state) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "purge <task>",
		Short: "Delete a task's stored transcript; the audit entries, usage and Decisions stay",
		Long: "Deletes the stored transcript of a task: the agent's text, tool inputs and results and diffs. " +
			"The audit entries, the usage rows, the Decisions and the agent's own session stay, and the purge is recorded as an audit entry. " +
			"Refused while the task's run is running: pause it first. Without --yes it says what goes and asks.",
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
			_, data, err := c.Do(cmd.Context(), "GET", "/v1/tasks/"+url.PathEscape(id)+"/transcript", nil, "")
			if err != nil {
				return err
			}
			var size struct {
				Events int   `json:"events"`
				Bytes  int64 `json:"bytes"`
			}
			if err := json.Unmarshal(data, &size); err != nil {
				return fmt.Errorf("the size is not what this whr expects: %w", err)
			}
			if !yes {
				fmt.Fprintf(s.env.Stderr, "This deletes %d transcript event(s), %d byte(s), of %s. The audit entries, usage and Decisions stay.\nType %q to go on: ", size.Events, size.Bytes, clean(id), "purge")
				line, err := bufio.NewReader(s.env.Stdin).ReadString('\n')
				if err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				if strings.TrimSpace(line) != "purge" {
					fmt.Fprintln(s.env.Stderr)
					return usageError{"not confirmed: nothing was deleted"}
				}
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/tasks/"+url.PathEscape(id)+"/purge", map[string]bool{"confirm": true}, newKey())
			if err != nil {
				return err
			}
			var res struct {
				Events int   `json:"events"`
				Bytes  int64 `json:"bytes"`
			}
			if err := json.Unmarshal(data, &res); err != nil {
				return fmt.Errorf("the answer is not a purge report: %w", err)
			}
			if err := s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(id)); return err }); err != nil {
				return err
			}
			fmt.Fprintf(s.env.Stderr, "deleted %d event(s), %d byte(s)\n", res.Events, res.Bytes)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return cmd
}
