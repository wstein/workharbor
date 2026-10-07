package store

import (
	"context"
	"encoding/json"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/initiation"
)

// ConsumeInitiation atomically spends a marker and writes its audit event.
// A committed spend precedes the adapter call, even when that call fails.
func (s *Store) ConsumeInitiation(ctx context.Context, r initiation.Record) error {
	return s.Update(ctx, func(tx *Tx) error {
		result, err := tx.tx.ExecContext(ctx, `INSERT INTO initiations (id, kind, actor, channel, at, decision_id, task_id, run_id, action) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO NOTHING`, r.ID, r.Kind, r.Actor, r.Channel, r.At.UnixNano(), string(r.DecisionID), string(r.TaskID), string(r.RunID), r.Action)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return initiation.ErrNotInitiated
		}
		payload, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = tx.Append(ctx, domain.Event{TaskID: r.TaskID, Kind: domain.EventRunInitiated, Tier: domain.TierAudit, Payload: payload, At: r.At})
		return err
	})
}
