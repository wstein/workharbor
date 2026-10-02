package store

import (
	"context"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// FeatureAnswers are the human's answers about the feature sources of one
// repository: every reference answered (allowed or denied, so a denied one is not asked
// again) and those allowed.
type FeatureAnswers struct {
	Allowed  map[string]bool
	Answered map[string]bool
}

// SetFeatureSource records the human's answer for a feature reference of a
// repository; the latest answer replaces an earlier one.
func (s *Store) SetFeatureSource(ctx context.Context, repo, ref string, allowed bool, decision domain.ID, at time.Time) error {
	if repo == "" || ref == "" {
		return fmt.Errorf("store: a feature source answer needs a repository and a reference")
	}
	allow := 0
	if allowed {
		allow = 1
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO feature_sources (repo, ref, allowed, decision_id, answered_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (repo, ref) DO UPDATE SET allowed = excluded.allowed, decision_id = excluded.decision_id, answered_at = excluded.answered_at`,
		repo, ref, allow, string(decision), toNano(at)); err != nil {
		return fmt.Errorf("store: feature source answer for %s: %w", ref, err)
	}
	return nil
}

// FeatureSources returns the answers recorded for a repository.
func (s *Store) FeatureSources(ctx context.Context, repo string) (FeatureAnswers, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ref, allowed FROM feature_sources WHERE repo = ?`, repo)
	if err != nil {
		return FeatureAnswers{}, fmt.Errorf("store: feature sources of %s: %w", repo, err)
	}
	defer func() { _ = rows.Close() }()
	out := FeatureAnswers{Allowed: map[string]bool{}, Answered: map[string]bool{}}
	for rows.Next() {
		var ref string
		var allowed int
		if err := rows.Scan(&ref, &allowed); err != nil {
			return FeatureAnswers{}, err
		}
		out.Answered[ref] = true
		if allowed == 1 {
			out.Allowed[ref] = true
		}
	}
	return out, rows.Err()
}
