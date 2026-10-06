package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// StampRunBounds stamps only bounds represented by this change's durable events.
// Legacy rows without sufficient audit evidence remain unknown.
func (a *TaskAggregate) StampRunBounds(now time.Time) {
	for i := range a.events {
		if a.events[i].At.IsZero() {
			a.events[i].At = now.UTC()
		}
		e := a.events[i]
		at := e.At
		if at.IsZero() {
			at = now
		}
		if e.Kind == EventRunStarted {
			var p RunStarted
			if json.Unmarshal(e.Payload, &p) == nil {
				if r, err := a.run(p.RunID); err == nil && r.AdmittedAt.IsZero() {
					r.AdmittedAt = at.UTC()
					r.DurationAt = at.UTC()
				}
			}
		}
		if e.Kind == EventRunState {
			var p StateChanged
			if json.Unmarshal(e.Payload, &p) == nil && RunState(p.To).Terminal() {
				if r, err := a.run(p.ID); err == nil && r.EndedAt.IsZero() {
					r.EndedAt = at.UTC()
					if !r.AdmittedAt.IsZero() && !at.Before(r.AdmittedAt) {
						r.DurationMillis = max(r.DurationMillis, at.Sub(r.AdmittedAt).Milliseconds())
					}
				}
			}
		}
	}
}

// AccountDuration advances audit-backed elapsed high-water marks. Missing bounds
// or a backwards supervisor clock refuse accounting rather than renew an allowance.
func (a *TaskAggregate) AccountDuration(now time.Time) error { return a.accountDuration(now, "") }

// AccountRunDuration needs only this run's lifetime when no cumulative limit is enabled.
func (a *TaskAggregate) AccountRunDuration(now time.Time, run ID) error {
	return a.accountDuration(now, run)
}

func (a *TaskAggregate) accountDuration(now time.Time, only ID) error {
	for _, r := range a.runs {
		if only != "" && r.ID != only {
			continue
		}
		end := now
		if r.State.Terminal() {
			end = r.EndedAt
		}
		if r.AdmittedAt.IsZero() || end.IsZero() || end.Before(r.AdmittedAt) || (!r.State.Terminal() && now.Before(r.DurationAt)) {
			return fmt.Errorf("run %s: duration accounting unknown or supervisor clock moved backwards; resolve durable run bounds before execution", r.ID)
		}
	}
	for _, r := range a.runs {
		if only != "" && r.ID != only {
			continue
		}
		end := now
		if r.State.Terminal() {
			end = r.EndedAt
		}
		elapsed := max(r.DurationMillis, end.Sub(r.AdmittedAt).Milliseconds())
		if elapsed != r.DurationMillis || (!r.State.Terminal() && !now.Equal(r.DurationAt)) {
			r.DurationMillis = elapsed
			if !r.State.Terminal() {
				r.DurationAt = now.UTC()
			}
			a.record("run.duration", struct {
				RunID  ID        `json:"run_id"`
				Millis int64     `json:"millis"`
				At     time.Time `json:"at"`
			}{r.ID, r.DurationMillis, r.DurationAt})
		}
	}
	return nil
}

// AdvanceDuration retains a monotonic process reading above the UTC-derived floor.
func (a *TaskAggregate) AdvanceDuration(run ID, millis int64) {
	r, err := a.run(run)
	if err != nil || r.State.Terminal() || millis <= r.DurationMillis {
		return
	}
	r.DurationMillis = millis
	a.record("run.duration", struct {
		RunID  ID        `json:"run_id"`
		Millis int64     `json:"millis"`
		At     time.Time `json:"at"`
	}{r.ID, r.DurationMillis, r.DurationAt})
}
