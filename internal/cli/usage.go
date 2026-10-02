package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// usageReport is the API's usage report as this command reads it (design §5.7).
type usageReport struct {
	Group string `json:"group"`
	Rows  []struct {
		Key    string `json:"key"`
		Auth   string `json:"auth"`
		Turns  int64  `json:"turns"`
		Tokens struct {
			Input      int64 `json:"input"`
			Output     int64 `json:"output"`
			CacheRead  int64 `json:"cache_read"`
			CacheWrite int64 `json:"cache_write"`
		} `json:"tokens"`
		TurnsWithoutTokens int64 `json:"turns_without_tokens"`
		ReportedMicroUSD   int64 `json:"reported_micro_usd"`
		TurnsWithoutCost   int64 `json:"turns_without_cost"`
		Notional           bool  `json:"notional"`
	} `json:"rows"`
	Windows []struct {
		Name        string    `json:"name"`
		Utilization float64   `json:"utilization"`
		ResetsAt    time.Time `json:"resets_at"`
	} `json:"windows"`
	Balance *struct {
		RemainingMicroUSD int64 `json:"remaining_micro_usd"`
	} `json:"balance"`
}

// parseWhen reads a time for --since and --until: an RFC 3339 time, a date
// (midnight UTC), or how long ago as a Go duration or a number of days ("36h",
// "7d").
func parseWhen(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n >= 0 {
			return now.Add(-time.Duration(n) * 24 * time.Hour).UTC(), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d).UTC(), nil
	}
	return time.Time{}, usageError{fmt.Sprintf("%q is not a time: use 2026-10-01T00:00:00Z, 2026-10-01, or how long ago such as 36h or 7d", s)}
}

// dollars writes millionths of a dollar with four decimals.
func dollars(micro int64) string {
	t := (micro + 50) / 100
	return fmt.Sprintf("$%d.%04d", t/10_000, t%10_000)
}

// count writes a token count compactly: 999, 1.5k, 2.5M.
func count(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return strconv.FormatInt(n, 10)
}

func newUsage(s *state) *cobra.Command {
	var task, repo, since, until, by string
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Report the tokens and cost the agents reported",
		Long: "Totals of what the agents reported, from the audit entries (design §5.7): tokens, the cost the agent " +
			"reported, and the number of turns. A row is one group and one auth mode, so the notional cost of a " +
			"subscription is never added to the spend of an API key. A turn without tokens or without a cost is " +
			"counted, not read as zero: workharbor prices nothing itself. The usage windows of the account and the " +
			"balance the agent reported come with it; on a subscription the windows are the number that matters.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			now := time.Now()
			q := url.Values{}
			if task != "" {
				id, err := s.resolveTask(cmd.Context(), task)
				if err != nil {
					return err
				}
				q.Set("task", id)
			}
			if repo != "" {
				q.Set("repo", repo)
			}
			for name, v := range map[string]string{"since": since, "until": until} {
				if v == "" {
					continue
				}
				t, err := parseWhen(v, now)
				if err != nil {
					return err
				}
				q.Set(name, t.Format(time.RFC3339))
			}
			switch by {
			case "", "all", "run", "task", "repo", "day", "month":
			default:
				return usageError{"--by is all, run, task, repo, day or month"}
			}
			if by != "" {
				q.Set("by", by)
			}
			c, err := s.api()
			if err != nil {
				return err
			}
			path := "/v1/usage"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			raw, data, err := c.Do(cmd.Context(), "GET", path, nil, "")
			if err != nil {
				return err
			}
			var rep usageReport
			if err := json.Unmarshal(data, &rep); err != nil {
				return fmt.Errorf("the usage report is not what this whr expects: %w", err)
			}
			return s.emit(raw, func(w io.Writer) error { return printUsage(w, rep) })
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "only this task (an ID, a prefix, or an issue as #42)")
	cmd.Flags().StringVar(&repo, "repo", "", "only this repository (owner/name)")
	cmd.Flags().StringVar(&since, "since", "", "from this time: 2026-10-01, 2026-10-01T00:00:00Z, or how long ago (36h, 7d)")
	cmd.Flags().StringVar(&until, "until", "", "before this time (same forms as --since)")
	cmd.Flags().StringVar(&by, "by", "", "group by all, run, task (default), repo, day or month (UTC)")
	return cmd
}

// printUsage writes the report: on a subscription the usage windows lead, since
// they, not the notional cost, limit the human; then one row per group and auth
// mode, with the cost labelled as the agent's own report.
func printUsage(w io.Writer, rep usageReport) error {
	subscription := false
	for _, r := range rep.Rows {
		subscription = subscription || r.Notional
	}
	windows := func() error {
		if len(rep.Windows) == 0 {
			return nil
		}
		rows := make([][]string, len(rep.Windows))
		for i, win := range rep.Windows {
			resets := ""
			if !win.ResetsAt.IsZero() {
				resets = win.ResetsAt.UTC().Format(time.RFC3339)
			}
			rows[i] = []string{win.Name, fmt.Sprintf("%.0f%%", win.Utilization*100), resets}
		}
		if err := table(w, []string{"WINDOW", "USED", "RESETS"}, rows); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w)
		return err
	}
	if subscription {
		if err := windows(); err != nil {
			return err
		}
	}
	rows := make([][]string, len(rep.Rows))
	for i, r := range rep.Rows {
		cost := dollars(r.ReportedMicroUSD) + " reported"
		if r.ReportedMicroUSD == 0 && r.TurnsWithoutCost == r.Turns {
			cost = "not reported"
		}
		if r.Notional {
			cost += ", notional"
		}
		var unknown []string
		if r.TurnsWithoutTokens > 0 {
			unknown = append(unknown, fmt.Sprintf("%d turn(s) without tokens", r.TurnsWithoutTokens))
		}
		if r.TurnsWithoutCost > 0 && r.TurnsWithoutCost != r.Turns {
			unknown = append(unknown, fmt.Sprintf("%d turn(s) without a cost", r.TurnsWithoutCost))
		}
		in, out := count(r.Tokens.Input+r.Tokens.CacheRead+r.Tokens.CacheWrite), count(r.Tokens.Output)
		if r.TurnsWithoutTokens == r.Turns {
			in, out = "-", "-" // nothing was reported, which is not zero
		}
		rows[i] = []string{r.Key, r.Auth, strconv.FormatInt(r.Turns, 10), in, out, cost, strings.Join(unknown, "; ")}
	}
	key := strings.ToUpper(rep.Group)
	if key == "" {
		key = "KEY"
	}
	if err := table(w, []string{key, "AUTH", "TURNS", "IN", "OUT", "COST", "UNKNOWN"}, rows); err != nil {
		return err
	}
	if !subscription && len(rep.Windows) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
		if err := windows(); err != nil {
			return err
		}
	}
	if rep.Balance != nil {
		if _, err := fmt.Fprintf(w, "\nbalance: %s left (as the agent reported)\n", dollars(rep.Balance.RemainingMicroUSD)); err != nil {
			return err
		}
	}
	return nil
}
