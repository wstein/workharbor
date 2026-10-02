package store

import (
	"context"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// FeatureAnswer is the human's latest answer about one feature reference of a
// repository. Digest is the manifest digest an allow was given for; a deny holds
// whatever digest the human saw and applies to the reference, whatever it resolves to.
type FeatureAnswer struct {
	Allowed bool
	Digest  string
}

// FeatureAnswers are the human's answers about the feature sources of one repository,
// by reference as the repository wrote it.
type FeatureAnswers map[string]FeatureAnswer

// Allows reports whether the reference is allowed for exactly this digest: an allow
// for another digest, or none, does not count (D38).
func (a FeatureAnswers) Allows(ref, digest string) bool {
	got, ok := a[ref]
	return ok && got.Allowed && digest != "" && got.Digest == digest
}

// SetFeatureSource records the human's answer for a feature reference of a
// repository, with the digest it resolved to; the latest answer replaces an earlier
// one. An allow needs a valid digest.
func (s *Store) SetFeatureSource(ctx context.Context, repo, ref, digest string, allowed bool, decision domain.ID, at time.Time) error {
	if repo == "" || ref == "" {
		return fmt.Errorf("store: a feature source answer needs a repository and a reference")
	}
	if !domain.ValidDigest(digest) {
		return fmt.Errorf("store: a feature source answer needs a manifest digest")
	}
	allow := 0
	if allowed {
		allow = 1
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO feature_sources (repo, ref, allowed, digest, decision_id, answered_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo, ref) DO UPDATE SET allowed = excluded.allowed, digest = excluded.digest, decision_id = excluded.decision_id, answered_at = excluded.answered_at`,
		repo, ref, allow, digest, string(decision), toNano(at)); err != nil {
		return fmt.Errorf("store: feature source answer for %s: %w", ref, err)
	}
	return nil
}

// FeatureSources returns the answers recorded for a repository.
func (s *Store) FeatureSources(ctx context.Context, repo string) (FeatureAnswers, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ref, allowed, digest FROM feature_sources WHERE repo = ?`, repo)
	if err != nil {
		return nil, fmt.Errorf("store: feature sources of %s: %w", repo, err)
	}
	defer func() { _ = rows.Close() }()
	out := FeatureAnswers{}
	for rows.Next() {
		var ref, digest string
		var allowed int
		if err := rows.Scan(&ref, &allowed, &digest); err != nil {
			return nil, err
		}
		out[ref] = FeatureAnswer{Allowed: allowed == 1, Digest: digest}
	}
	return out, rows.Err()
}
