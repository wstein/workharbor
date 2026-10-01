package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// emit prints the result of a command: the API's envelope with --json, the text
// the command builds otherwise. Both go to stdout, and only they do.
func (s *state) emit(raw []byte, text func(w io.Writer) error) error {
	if s.asJSON {
		_, err := s.env.Stdout.Write(raw)
		return err
	}
	return text(s.env.Stdout)
}

func table(w io.Writer, header []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		for i := range r {
			r[i] = clean(r[i])
		}
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}

// ---- task and decision references ----

type taskRow struct {
	ID      string `json:"id"`
	Repo    string `json:"repo"`
	Issue   string `json:"issue"`
	State   string `json:"state"`
	AgentID string `json:"agent_id"`
	Agent   string `json:"agent"`
}

func (s *state) tasks(ctx context.Context, all bool) ([]taskRow, []byte, error) {
	c, err := s.api()
	if err != nil {
		return nil, nil, err
	}
	path := "/v1/tasks"
	if !all {
		path += "?active=true"
	}
	raw, data, err := c.Do(ctx, "GET", path, nil, "")
	if err != nil {
		return nil, nil, err
	}
	var rows []taskRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, fmt.Errorf("the task list is not what this whr expects: %w", err)
	}
	return rows, raw, nil
}

// resolveTask turns a reference into a task ID: a full ID, a unique prefix of
// one, or an issue as `repo#42` or `#42` (design §9.1). An issue with several
// tasks means the newest unfinished one, and an ambiguous reference lists what
// it could be.
func (s *state) resolveTask(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", usageError{"a task is needed"}
	}
	all, _, err := s.tasks(ctx, true) // finished tasks are findable too
	if err != nil {
		return "", err
	}
	for _, t := range all {
		if t.ID == ref {
			return t.ID, nil
		}
	}
	var hits []taskRow
	switch {
	case strings.Contains(ref, "#"):
		repo, num, _ := strings.Cut(ref, "#")
		for _, t := range all {
			if t.Issue == "#"+num && (repo == "" || strings.EqualFold(t.Repo, repo)) {
				hits = append(hits, t)
			}
		}
		if len(hits) > 1 { // the newest unfinished one: the list is newest first
			for _, t := range hits {
				if !isFinished(t.State) {
					return t.ID, nil
				}
			}
		}
	default:
		for _, t := range all {
			if strings.HasPrefix(t.ID, ref) {
				hits = append(hits, t)
			}
		}
	}
	switch len(hits) {
	case 0:
		return "", notFoundError{fmt.Sprintf("no task matches %q", ref)}
	case 1:
		return hits[0].ID, nil
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return "", usageError{fmt.Sprintf("%q matches several tasks: %s", ref, strings.Join(ids, ", "))}
}

func isFinished(state string) bool {
	return state == "completed" || state == "cancelled" || state == "failed"
}

type decisionRow struct {
	ID      string   `json:"id"`
	TaskID  string   `json:"task_id"`
	Kind    string   `json:"kind"`
	Cause   string   `json:"cause"`
	Subject string   `json:"subject"`
	SHA     string   `json:"sha"`
	Options []string `json:"options"`
}

func (s *state) inbox(ctx context.Context) ([]decisionRow, []byte, error) {
	c, err := s.api()
	if err != nil {
		return nil, nil, err
	}
	raw, data, err := c.Do(ctx, "GET", "/v1/inbox", nil, "")
	if err != nil {
		return nil, nil, err
	}
	var rows []decisionRow
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, fmt.Errorf("the inbox is not what this whr expects: %w", err)
	}
	return rows, raw, nil
}

func (s *state) resolveDecision(ctx context.Context, ref string) (decisionRow, error) {
	rows, _, err := s.inbox(ctx)
	if err != nil {
		return decisionRow{}, err
	}
	var hits []decisionRow
	for _, d := range rows {
		if d.ID == ref {
			return d, nil
		}
		if strings.HasPrefix(d.ID, ref) {
			hits = append(hits, d)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) > 1 {
		return decisionRow{}, usageError{fmt.Sprintf("%q matches several decisions", ref)}
	}
	return decisionRow{}, notFoundError{fmt.Sprintf("no open decision matches %q", ref)}
}

// ---- commands ----

func newLs(s *state) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List tasks (unfinished ones, or all with --all)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows, raw, err := s.tasks(cmd.Context(), all)
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				out := make([][]string, len(rows))
				for i, t := range rows {
					out[i] = []string{t.ID, t.State, t.Issue, t.Repo, agentName(t)}
				}
				return table(w, []string{"ID", "STATE", "ISSUE", "REPO", "AGENT"}, out)
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include finished tasks")
	return cmd
}

func newRun(s *state) *cobra.Command {
	var agentRef, prompt, key string
	cmd := &cobra.Command{
		Use:   "run <issue-url> --agent <workspace>/<role>",
		Short: "Start a task from an issue on a named agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentRef == "" {
				return usageError{"--agent <workspace>/<role> is needed"}
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			if key == "" {
				key = newKey()
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/tasks", map[string]string{"issue_url": args[0], "agent": agentRef, "prompt": prompt}, key)
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				var r struct{ TaskID, RunID string }
				var m map[string]string
				if err := json.Unmarshal(data, &m); err != nil {
					return err
				}
				r.TaskID, r.RunID = m["task_id"], m["run_id"]
				_, err := fmt.Fprintf(w, "%s\t%s\n", clean(r.TaskID), clean(r.RunID))
				return err
			})
		},
	}
	cmd.Flags().StringVar(&agentRef, "agent", "", "the agent that works on it: <workspace>/<role>")
	cmd.Flags().StringVar(&prompt, "prompt", "", "text to put after the issue in the first message")
	cmd.Flags().StringVar(&key, "idempotency-key", "", "reuse a key to make a retry safe (default: a new one)")
	_ = cmd.RegisterFlagCompletionFunc("agent", s.completeAgents)
	return cmd
}

func newSay(s *state) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:               "say <task> [message|-]",
		Short:             "Send a message to the running agent; says how it was delivered",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: s.completeTasks,
		RunE: func(cmd *cobra.Command, args []string) error {
			msg, err := s.message(args[1:], file)
			if err != nil {
				return err
			}
			id, err := s.resolveTask(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			raw, data, err := c.Do(cmd.Context(), "POST", "/v1/tasks/"+url.PathEscape(id)+"/say", map[string]string{"message": msg}, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				var d struct {
					Delivery string `json:"delivery"`
				}
				if err := json.Unmarshal(data, &d); err != nil {
					return err
				}
				_, err := fmt.Fprintln(w, clean(d.Delivery))
				return err
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "read the message from a file")
	return cmd
}

// message reads the text of `say` from the argument, from a file, or from
// standard input when the argument is "-".
func (s *state) message(args []string, file string) (string, error) {
	switch {
	case file != "" && len(args) > 0:
		return "", usageError{"give the message as an argument or with -f, not both"}
	case file != "":
		b, err := os.ReadFile(file) //nolint:gosec // the user names their own file
		if err != nil {
			return "", err
		}
		return string(b), nil
	case len(args) == 1 && args[0] == "-":
		b, err := io.ReadAll(io.LimitReader(s.env.Stdin, 1<<20))
		return string(b), err
	case len(args) == 1:
		return args[0], nil
	}
	return "", usageError{"a message is needed: as an argument, with -f <file>, or - for standard input"}
}

func newCancel(s *state) *cobra.Command {
	return &cobra.Command{
		Use:               "cancel <task>",
		Short:             "Cancel a task",
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
			raw, _, err := c.Do(cmd.Context(), "POST", "/v1/tasks/"+url.PathEscape(id)+"/cancel", nil, newKey())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error { _, err := fmt.Fprintln(w, clean(id)); return err })
		},
	}
}

func newInbox(s *state) *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "List the decisions waiting for you",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows, raw, err := s.inbox(cmd.Context())
			if err != nil {
				return err
			}
			return s.emit(raw, func(w io.Writer) error {
				out := make([][]string, len(rows))
				for i, d := range rows {
					out[i] = []string{d.ID, d.TaskID, d.Kind, d.Subject, d.SHA, strings.Join(d.Options, ",")}
				}
				return table(w, []string{"DECISION", "TASK", "KIND", "SUBJECT", "SHA", "OPTIONS"}, out)
			})
		},
	}
}

// newApprove builds approve (allow) and reject (deny). A review Decision is
// answered for one commit: the commit has to be named with --sha, so the answer
// is for what the human was shown (whr inbox) and never for whatever the
// decision holds when the command runs.
func newApprove(s *state, allow bool) *cobra.Command {
	use, short, option := "approve <decision>", "Approve a decision (a review needs --sha <commit>)", "allow"
	if !allow {
		use, short, option = "reject <decision>", "Reject a decision", "deny"
	}
	var sha, reason string
	cmd := &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(1),
		ValidArgsFunction: s.completeDecisions,
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.answer(cmd.Context(), args[0], option, reason, sha)
		},
	}
	cmd.Flags().StringVar(&sha, "sha", "", "the commit you were shown, for a review decision")
	cmd.Flags().StringVar(&reason, "reason", "", "a reason, passed back to the agent")
	return cmd
}

func newAnswer(s *state) *cobra.Command {
	var reason, sha string
	cmd := &cobra.Command{
		Use:   "answer <decision> <option>",
		Short: "Answer a question with one of its options",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return s.completeDecisions(cmd, args, toComplete)
			}
			rows, _, err := s.inbox(cmd.Context())
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			for _, d := range rows {
				if d.ID == args[0] {
					return d.Options, cobra.ShellCompDirectiveNoFileComp
				}
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return s.answer(cmd.Context(), args[0], args[1], reason, sha)
		},
	}
	cmd.Flags().StringVar(&sha, "sha", "", "the commit you were shown, for a review decision")
	cmd.Flags().StringVar(&reason, "reason", "", "a reason, passed back to the agent")
	return cmd
}

func (s *state) answer(ctx context.Context, ref, option, reason, sha string) error {
	d, err := s.resolveDecision(ctx, ref)
	if err != nil {
		return err
	}
	if d.Kind == "review" && sha == "" {
		return usageError{fmt.Sprintf("decision %s is a review of commit %s: name the commit you were shown with --sha %s", d.ID, shortSHA(d.SHA), shortSHA(d.SHA))}
	}
	c, err := s.api()
	if err != nil {
		return err
	}
	body := map[string]string{"option": option}
	if reason != "" {
		body["reason"] = reason
	}
	if sha != "" {
		body["sha"] = sha
	}
	raw, data, err := c.Do(ctx, "POST", "/v1/decisions/"+url.PathEscape(d.ID)+"/answer", body, newKey())
	if err != nil {
		return err
	}
	return s.emit(raw, func(w io.Writer) error {
		var m map[string]string
		_ = json.Unmarshal(data, &m)
		if run := m["new_run_id"]; run != "" {
			_, err := fmt.Fprintf(w, "%s\t%s\n", clean(d.ID), clean(run))
			return err
		}
		_, err := fmt.Fprintln(w, clean(d.ID))
		return err
	})
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// ---- logs ----

type eventRow struct {
	Seq  int64           `json:"seq"`
	Kind string          `json:"kind"`
	Tier string          `json:"tier"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

func newLogs(s *state) *cobra.Command {
	var follow bool
	var since int64
	cmd := &cobra.Command{
		Use:               "logs <task> [-f] [--since <event-id>]",
		Short:             "Print a task's events; -f follows them live and reconnects",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: s.completeTasks,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := s.resolveTask(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if follow {
				return s.follow(cmd.Context(), id, since)
			}
			return s.dump(cmd.Context(), id, since)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep the connection open and print events as they happen")
	cmd.Flags().Int64Var(&since, "since", 0, "start after this event ID (the number printed first on each line)")
	return cmd
}

// printEvent writes one event: JSON lines with --json, and a tab-separated line
// otherwise (sequence number, time, kind, compact data).
func (s *state) printEvent(e eventRow) error {
	if s.asJSON {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(s.env.Stdout, "%s\n", b)
		return err
	}
	var data bytes.Buffer
	if len(e.Data) > 0 {
		_ = json.Compact(&data, e.Data)
	}
	seq := ""
	if e.Seq != 0 {
		seq = strconv.FormatInt(e.Seq, 10)
	}
	_, err := fmt.Fprintf(s.env.Stdout, "%s\t%s\t%s\t%s\n", seq, e.At.UTC().Format(time.RFC3339), clean(e.Kind), data.String())
	return err
}

// dump prints the stored events after since, a page at a time.
func (s *state) dump(ctx context.Context, task string, since int64) error {
	c, err := s.api()
	if err != nil {
		return err
	}
	const page = 500
	for {
		_, data, err := c.Do(ctx, "GET", fmt.Sprintf("/v1/tasks/%s/log?since=%d&limit=%d", url.PathEscape(task), since, page), nil, "")
		if err != nil {
			return err
		}
		var evs []eventRow
		if err := json.Unmarshal(data, &evs); err != nil {
			return fmt.Errorf("the event log is not what this whr expects: %w", err)
		}
		for _, e := range evs {
			if err := s.printEvent(e); err != nil {
				return err
			}
			since = e.Seq
		}
		if len(evs) < page {
			return nil
		}
	}
}

// follow streams events and reconnects with the last event ID it saw, so a
// dropped connection loses nothing (design §9.2). It ends when the user
// interrupts, which is not an error.
func (s *state) follow(ctx context.Context, task string, since int64) error {
	c, err := s.api()
	if err != nil {
		return err
	}
	last := since
	backoff := time.Second
	for {
		resp, err := c.Stream(ctx, "/v1/tasks/"+url.PathEscape(task)+"/events", strconv.FormatInt(last, 10))
		if err == nil {
			backoff = time.Second
			last, err = s.readStream(resp.Body, last)
			_ = resp.Body.Close()
		}
		if ctx.Err() != nil {
			return nil
		}
		var remote *RemoteError
		if errors.As(err, &remote) {
			return err // the server said no: reconnecting will not change it
		}
		fmt.Fprintf(s.env.Stderr, "whr: the event stream ended (%v); reconnecting from event %d\n", describe(err), last)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 15*time.Second {
			backoff *= 2
		}
	}
}

func describe(err error) string {
	if err == nil {
		return "closed by the server"
	}
	return oneLineError(err)
}

// readStream prints the events of one connection and returns the last durable
// sequence number it saw.
func (s *state) readStream(r io.Reader, last int64) (int64, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var data string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "data: "):
			data = line[6:]
		case line == "":
			if data == "" {
				continue
			}
			var e eventRow
			if err := json.Unmarshal([]byte(data), &e); err == nil {
				if err := s.printEvent(e); err != nil {
					return last, err
				}
				if e.Seq > last {
					last = e.Seq
				}
			}
			data = ""
		}
	}
	return last, sc.Err()
}

// ---- completion ----

func (s *state) completeTasks(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	rows, _, err := s.tasks(cmd.Context(), false)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, len(rows))
	for i, t := range rows {
		out[i] = t.ID + "\t" + clean(t.Repo+" "+t.Issue+" "+t.State)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func (s *state) completeDecisions(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	rows, _, err := s.inbox(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, len(rows))
	for i, d := range rows {
		out[i] = d.ID + "\t" + clean(d.Subject)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func (s *state) completeAgents(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	c, err := s.api()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	_, data, err := c.Do(cmd.Context(), "GET", "/v1/workspaces", nil, "")
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var ws []struct {
		Name   string `json:"name"`
		Agents []struct {
			Role string `json:"role"`
		} `json:"agents"`
	}
	if json.Unmarshal(data, &ws) != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var out []string
	for _, w := range ws {
		for _, a := range w.Agents {
			out = append(out, w.Name+"/"+a.Role)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// agentName shows an agent as <workspace>/<role>, falling back to its ID for a
// server that does not send the name.
func agentName(t taskRow) string {
	if t.Agent != "" {
		return t.Agent
	}
	return t.AgentID
}
