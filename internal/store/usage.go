package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// AppendUsage writes a usage audit entry, and in the same transaction the usage
// windows it reports: the latest reading of a window of an account replaces an
// older one, and a late event never replaces a newer one.
func (s *Store) AppendUsage(ctx context.Context, account string, ev domain.Event) (domain.Event, error) {
	if ev.Kind != domain.EventUsage || ev.Tier != domain.TierAudit {
		return domain.Event{}, fmt.Errorf("store: %s is not a usage audit entry", ev.Kind)
	}
	var u domain.UsageRecorded
	if err := json.Unmarshal(ev.Payload, &u); err != nil {
		return domain.Event{}, fmt.Errorf("store: usage payload: %w", err)
	}
	var saved domain.Event
	err := s.Update(ctx, func(tx *Tx) error {
		out, err := tx.Append(ctx, ev)
		if err != nil {
			return err
		}
		saved = out[0]
		for _, w := range u.Windows {
			var resets int64
			if !w.ResetsAt.IsZero() {
				resets = w.ResetsAt.UnixNano()
			}
			if _, err := tx.tx.ExecContext(ctx, `INSERT INTO usage_windows (account, name, utilization, resets_at, at) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (account, name) DO UPDATE SET utilization = excluded.utilization, resets_at = excluded.resets_at, at = excluded.at
				WHERE excluded.at >= usage_windows.at`, account, w.Name, w.Utilization, resets, saved.At.UnixNano()); err != nil {
				return fmt.Errorf("store: usage window %s: %w", w.Name, err)
			}
		}
		return nil
	})
	return saved, err
}

// WindowReading is the latest reading of one usage window of an account.
type WindowReading struct {
	Account     string    `json:"account"`
	Name        string    `json:"name"`
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at,omitzero"`
	At          time.Time `json:"at"`
}

// UsageWindows returns the latest reading of every window of an account.
func (s *Store) UsageWindows(ctx context.Context, account string) ([]WindowReading, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account, name, utilization, resets_at, at FROM usage_windows WHERE account = ? ORDER BY name`, account)
	if err != nil {
		return nil, fmt.Errorf("store: usage windows: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []WindowReading
	for rows.Next() {
		var w WindowReading
		var resets, at int64
		if err := rows.Scan(&w.Account, &w.Name, &w.Utilization, &resets, &at); err != nil {
			return nil, err
		}
		if resets != 0 {
			w.ResetsAt = fromNano(resets)
		}
		w.At = fromNano(at)
		out = append(out, w)
	}
	return out, rows.Err()
}

// UsageGroup says how UsageTotals groups the turns.
type UsageGroup string

// The groupings of a usage report. Days and months are in UTC.
const (
	GroupAll   UsageGroup = "all"
	GroupRun   UsageGroup = "run"
	GroupTask  UsageGroup = "task"
	GroupRepo  UsageGroup = "repo"
	GroupDay   UsageGroup = "day"
	GroupMonth UsageGroup = "month"
	// GroupAgent groups by the task's agent, <workspace>/<role>; a task made before
	// agents existed has the key "". GroupModel groups by the model of the turn.
	GroupAgent UsageGroup = "agent"
	GroupModel UsageGroup = "model"
)

var groupKey = map[UsageGroup]string{
	GroupAll:   `'all'`,
	GroupRun:   `COALESCE(json_extract(CAST(payload AS TEXT), '$.run_id'), '')`,
	GroupTask:  `task_id`,
	GroupRepo:  `COALESCE(json_extract(CAST(payload AS TEXT), '$.repo'), '')`,
	GroupDay:   `strftime('%Y-%m-%d', at / 1000000000, 'unixepoch')`,
	GroupMonth: `strftime('%Y-%m', at / 1000000000, 'unixepoch')`,
	GroupAgent: `COALESCE((SELECT w.name || '/' || a.role FROM tasks t JOIN agents a ON a.id = t.agent_id JOIN workspaces w ON w.id = a.workspace_id WHERE t.id = events.task_id), '')`,
	GroupModel: `COALESCE(json_extract(CAST(payload AS TEXT), '$.model'), '')`,
}

// UsageFilter selects the turns to total. A zero field selects everything.
type UsageFilter struct {
	TaskID domain.ID
	RunID  domain.ID
	Repo   string
	Since  time.Time // inclusive
	Until  time.Time // exclusive
}

// UsageRow is the total of the turns of one group and one auth mode. Rows of
// different auth modes are never added together: the same cost is real spend
// with an API key and notional on a subscription. A turn whose tokens or cost
// the agent did not report is counted, not read as zero.
type UsageRow struct {
	Key  string `json:"key"`
	Auth string `json:"auth"`

	Turns             int64              `json:"turns"`
	Tokens            domain.UsageTokens `json:"tokens"`
	TurnsWithoutToken int64              `json:"turns_without_tokens"`

	ReportedMicroUSD int64 `json:"reported_micro_usd"`
	// EstimatedMicroUSD is the cost of the turns whose figure is an estimate, never
	// added to the reported one: a report says which is which (design §5.7, #48).
	EstimatedMicroUSD int64 `json:"estimated_micro_usd"`
	TurnsWithoutCost  int64 `json:"turns_without_cost"`

	// APIMillis and WallMillis are the time the agent reported for the turns on the
	// model API and in all, in milliseconds.
	APIMillis  int64 `json:"api_ms"`
	WallMillis int64 `json:"wall_ms"`
	// Runs is how many runs the turns belong to.
	Runs int64 `json:"runs"`

	First time.Time `json:"first"`
	Last  time.Time `json:"last"`
}

const usageTemplate = `SELECT {KEY} AS k, COALESCE(json_extract(CAST(payload AS TEXT), '$.auth'), '') AS auth, COUNT(*),
	COALESCE(SUM(json_extract(CAST(payload AS TEXT), '$.tokens.input')), 0),
	COALESCE(SUM(json_extract(CAST(payload AS TEXT), '$.tokens.output')), 0),
	COALESCE(SUM(json_extract(CAST(payload AS TEXT), '$.tokens.cache_read')), 0),
	COALESCE(SUM(json_extract(CAST(payload AS TEXT), '$.tokens.cache_write')), 0),
	SUM(json_extract(CAST(payload AS TEXT), '$.tokens') IS NULL),
	COALESCE(SUM(CASE WHEN json_extract(CAST(payload AS TEXT), '$.cost.source') = 'reported' THEN json_extract(CAST(payload AS TEXT), '$.cost.micro_usd') END), 0),
	COALESCE(SUM(CASE WHEN json_extract(CAST(payload AS TEXT), '$.cost.source') = 'estimated' THEN json_extract(CAST(payload AS TEXT), '$.cost.micro_usd') END), 0),
	SUM(json_extract(CAST(payload AS TEXT), '$.cost') IS NULL), MIN(at), MAX(at),
	COALESCE(SUM(json_extract(CAST(payload AS TEXT), '$.api_ms')), 0),
	COALESCE(SUM(json_extract(CAST(payload AS TEXT), '$.wall_ms')), 0),
	COUNT(DISTINCT json_extract(CAST(payload AS TEXT), '$.run_id'))
	FROM events WHERE {WHERE} GROUP BY k, auth ORDER BY k, auth`

// fillUsageQuery splices the grouping expression and the conditions into the
// template. Both come from constants of this file, never from a caller.
func fillUsageQuery(key, where string) string {
	return strings.NewReplacer("{KEY}", key, "{WHERE}", where).Replace(usageTemplate)
}

// UsageTotals totals the usage audit entries that match the filter, grouped. It
// reads the audit entries, which a purge of the transcript never removes, and
// it is the one set of counters the budgets of §7.4 read as well.
func (s *Store) UsageTotals(ctx context.Context, f UsageFilter, group UsageGroup) ([]UsageRow, error) {
	key, ok := groupKey[group]
	if !ok {
		return nil, fmt.Errorf("store: unknown usage grouping %q", group)
	}
	where := []string{`kind = ?`}
	args := []any{string(domain.EventUsage)}
	if f.TaskID != "" {
		where, args = append(where, `task_id = ?`), append(args, string(f.TaskID))
	}
	if f.RunID != "" {
		where, args = append(where, `json_extract(CAST(payload AS TEXT), '$.run_id') = ?`), append(args, string(f.RunID))
	}
	if f.Repo != "" {
		where, args = append(where, `json_extract(CAST(payload AS TEXT), '$.repo') = ?`), append(args, f.Repo)
	}
	if !f.Since.IsZero() {
		where, args = append(where, `at >= ?`), append(args, f.Since.UnixNano())
	}
	if !f.Until.IsZero() {
		where, args = append(where, `at < ?`), append(args, f.Until.UnixNano())
	}
	// Only constants of this file are spliced into the template; every value is
	// an argument.
	query := fillUsageQuery(key, strings.Join(where, ` AND `))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: usage totals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UsageRow
	for rows.Next() {
		var r UsageRow
		var first, last int64
		if err := rows.Scan(&r.Key, &r.Auth, &r.Turns, &r.Tokens.Input, &r.Tokens.Output, &r.Tokens.CacheRead, &r.Tokens.CacheWrite,
			&r.TurnsWithoutToken, &r.ReportedMicroUSD, &r.EstimatedMicroUSD, &r.TurnsWithoutCost, &first, &last, &r.APIMillis, &r.WallMillis, &r.Runs); err != nil {
			return nil, err
		}
		r.First, r.Last = fromNano(first), fromNano(last)
		out = append(out, r)
	}
	return out, rows.Err()
}

// BalanceReading is the latest balance an agent reported.
type BalanceReading struct {
	Agent             string    `json:"agent"`
	RemainingMicroUSD int64     `json:"remaining_micro_usd"`
	At                time.Time `json:"at"`
}

// LatestBalance returns the latest balance the agent reported with a turn, or
// nil when it never reported one. Like the windows it is a reading, never a
// total: it is read from the usage audit entries, so a purge does not change it.
func (s *Store) LatestBalance(ctx context.Context, agent string) (*BalanceReading, error) {
	var remaining, at int64
	err := s.db.QueryRowContext(ctx, `SELECT json_extract(CAST(payload AS TEXT), '$.balance.remaining_micro_usd'), at FROM events
		WHERE kind = ? AND json_extract(CAST(payload AS TEXT), '$.agent') = ? AND json_extract(CAST(payload AS TEXT), '$.balance') IS NOT NULL
		ORDER BY at DESC, seq DESC LIMIT 1`, string(domain.EventUsage), agent).Scan(&remaining, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no reading is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("store: latest balance: %w", err)
	}
	return &BalanceReading{Agent: agent, RemainingMicroUSD: remaining, At: fromNano(at)}, nil
}

// BudgetWarned reports whether the soft threshold of a budget was already
// recorded for a task, so a budget warns once and not on every turn after it.
func (s *Store) BudgetWarned(ctx context.Context, task domain.ID, b domain.BudgetBreach) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE task_id = ? AND kind = ?
		AND json_extract(CAST(payload AS TEXT), '$.scope') = ? AND json_extract(CAST(payload AS TEXT), '$.metric') = ?
		AND COALESCE(json_extract(CAST(payload AS TEXT), '$.run_id'), '') = ?`,
		string(task), string(domain.EventBudgetWarned), string(b.Scope), string(b.Metric), string(b.RunID)).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("store: budget warnings: %w", err)
	}
	return n > 0, nil
}

const bucketTemplate = `SELECT at, COALESCE(json_extract(CAST(payload AS TEXT), '$.auth'), ''),
		json_extract(CAST(payload AS TEXT), '$.tokens') IS NULL,
		COALESCE(json_extract(CAST(payload AS TEXT), '$.tokens.input'), 0), COALESCE(json_extract(CAST(payload AS TEXT), '$.tokens.output'), 0),
		COALESCE(json_extract(CAST(payload AS TEXT), '$.tokens.cache_read'), 0), COALESCE(json_extract(CAST(payload AS TEXT), '$.tokens.cache_write'), 0),
		json_extract(CAST(payload AS TEXT), '$.cost') IS NULL, COALESCE(json_extract(CAST(payload AS TEXT), '$.cost.source'), ''),
		COALESCE(json_extract(CAST(payload AS TEXT), '$.cost.micro_usd'), 0),
		COALESCE(json_extract(CAST(payload AS TEXT), '$.api_ms'), 0), COALESCE(json_extract(CAST(payload AS TEXT), '$.wall_ms'), 0),
		COALESCE(json_extract(CAST(payload AS TEXT), '$.run_id'), '')
		FROM events WHERE {WHERE} ORDER BY at, seq`

// UsageBuckets totals the usage turns by calendar day or month in a time zone, for
// the supervisor's own day: a turn belongs to the day its time falls on in loc, so a
// run that spans midnight is split between the two days by its turns. Rows are one
// bucket and one auth mode, as UsageTotals gives them, oldest first. unit is "day"
// or "month".
func (s *Store) UsageBuckets(ctx context.Context, f UsageFilter, loc *time.Location, unit string) ([]UsageRow, error) {
	layout := map[string]string{"day": "2006-01-02", "month": "2006-01"}[unit]
	if layout == "" {
		return nil, fmt.Errorf("store: unknown usage bucket %q", unit)
	}
	if loc == nil {
		loc = time.UTC
	}
	where := []string{`kind = ?`}
	args := []any{string(domain.EventUsage)}
	if f.TaskID != "" {
		where, args = append(where, `task_id = ?`), append(args, string(f.TaskID))
	}
	if f.Repo != "" {
		where, args = append(where, `json_extract(CAST(payload AS TEXT), '$.repo') = ?`), append(args, f.Repo)
	}
	if !f.Since.IsZero() {
		where, args = append(where, `at >= ?`), append(args, f.Since.UnixNano())
	}
	if !f.Until.IsZero() {
		where, args = append(where, `at < ?`), append(args, f.Until.UnixNano())
	}
	rows, err := s.db.QueryContext(ctx, strings.Replace(bucketTemplate, "{WHERE}", strings.Join(where, ` AND `), 1), args...)
	if err != nil {
		return nil, fmt.Errorf("store: usage buckets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	type key struct{ bucket, auth string }
	byKey := map[key]*UsageRow{}
	runs := map[key]map[string]bool{}
	var order []key
	for rows.Next() {
		var at, micro, api, wall int64
		var auth, source, run string
		var noTokens, noCost bool
		var in, out, cr, cw int64
		if err := rows.Scan(&at, &auth, &noTokens, &in, &out, &cr, &cw, &noCost, &source, &micro, &api, &wall, &run); err != nil {
			return nil, err
		}
		k := key{fromNano(at).In(loc).Format(layout), auth}
		r := byKey[k]
		if r == nil {
			r = &UsageRow{Key: k.bucket, Auth: auth, First: fromNano(at)}
			byKey[k], runs[k] = r, map[string]bool{}
			order = append(order, k)
		}
		r.Turns++
		r.Last = fromNano(at)
		if noTokens {
			r.TurnsWithoutToken++
		} else {
			r.Tokens.Input += in
			r.Tokens.Output += out
			r.Tokens.CacheRead += cr
			r.Tokens.CacheWrite += cw
		}
		switch {
		case noCost:
			r.TurnsWithoutCost++
		case source == "reported":
			r.ReportedMicroUSD += micro
		case source == "estimated":
			r.EstimatedMicroUSD += micro
		}
		r.APIMillis += api
		r.WallMillis += wall
		if run != "" {
			runs[k][run] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]UsageRow, 0, len(order))
	for _, k := range order {
		r := byKey[k]
		r.Runs = int64(len(runs[k]))
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Auth < out[j].Auth
	})
	return out, nil
}
