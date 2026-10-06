package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// Legacy bounds come only from sufficient immutable lifecycle audit events.
// Reconstruction is read-only until the next audited aggregate save.
func (tx *Tx) restoreLegacyBounds(ctx context.Context, snap *domain.Snapshot) error {
	needs := false
	for _, r := range snap.Runs {
		needs = needs || r.AdmittedAt.IsZero()
	}
	if !needs {
		return nil
	}
	rows, err := tx.tx.QueryContext(ctx, `SELECT kind,payload,at FROM events WHERE task_id=? AND tier='audit' AND kind IN ('run.started','run.state','run.duration') ORDER BY seq`, string(snap.Task.ID))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var payload []byte
		var at int64
		if err := rows.Scan(&kind, &payload, &at); err != nil {
			return err
		}
		if kind == string(domain.EventRunStarted) {
			var p domain.RunStarted
			if err := json.Unmarshal(payload, &p); err != nil {
				return err
			}
			for i := range snap.Runs {
				r := &snap.Runs[i]
				if r.ID == p.RunID && r.AdmittedAt.IsZero() {
					r.AdmittedAt = fromNano(at)
					r.DurationAt = fromNano(at)
				}
			}
		} else if kind == string(domain.EventRunState) {
			var p domain.StateChanged
			if err := json.Unmarshal(payload, &p); err != nil {
				return err
			}
			if domain.RunState(p.To).Terminal() {
				for i := range snap.Runs {
					r := &snap.Runs[i]
					if r.ID == p.ID && r.EndedAt.IsZero() {
						r.EndedAt = fromNano(at)
						r.TerminalReason = p.Reason
						if !r.AdmittedAt.IsZero() && !r.EndedAt.Before(r.AdmittedAt) {
							r.DurationMillis = max(r.DurationMillis, r.EndedAt.Sub(r.AdmittedAt).Milliseconds())
						}
					}
				}
			}
		} else {
			var p struct {
				RunID  domain.ID `json:"run_id"`
				Millis int64     `json:"millis"`
				At     time.Time `json:"at"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				return err
			}
			for i := range snap.Runs {
				if snap.Runs[i].ID == p.RunID {
					snap.Runs[i].DurationMillis = max(snap.Runs[i].DurationMillis, p.Millis)
					if p.At.After(snap.Runs[i].DurationAt) {
						snap.Runs[i].DurationAt = p.At.UTC()
					}
				}
			}
		}
	}
	return rows.Err()
}
