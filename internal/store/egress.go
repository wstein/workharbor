package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// EgressAnswers are the human's answers about the egress hosts of one
// repository: the hosts allowed, and every host answered (allowed or denied),
// so a denied host is not asked again.
type EgressAnswers struct {
	Allowed  []string // sorted
	Answered map[string]bool
	// Sources says what each answer was given for: a digest of the devcontainer.json
	// that requested the host, "lockfile", or "unknown" (design D47).
	Sources map[string]string
}

// SetEgressHost records the human's answer for a host of a repository: the
// latest answer replaces an earlier one, so an allow can be revoked. source is
// what the host was requested by (EgressAnswers.Sources); empty is "unknown".
func (s *Store) SetEgressHost(ctx context.Context, repo, host string, allowed bool, decision domain.ID, source string, at time.Time) error {
	if repo == "" || host == "" {
		return fmt.Errorf("store: egress answer needs a repository and a host")
	}
	allow := 0
	if allowed {
		allow = 1
	}
	if source == "" {
		source = "unknown"
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO egress_hosts (repo, hostname, allowed, decision_id, answered_at, source) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (repo, hostname) DO UPDATE SET allowed = excluded.allowed, decision_id = excluded.decision_id, answered_at = excluded.answered_at, source = excluded.source`,
		repo, host, allow, string(decision), toNano(at), source); err != nil {
		return fmt.Errorf("store: egress answer for %s: %w", host, err)
	}
	return nil
}

// EgressHosts returns the answers recorded for a repository.
func (s *Store) EgressHosts(ctx context.Context, repo string) (EgressAnswers, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hostname, allowed, source FROM egress_hosts WHERE repo = ? ORDER BY hostname`, repo)
	if err != nil {
		return EgressAnswers{}, fmt.Errorf("store: egress hosts of %s: %w", repo, err)
	}
	defer func() { _ = rows.Close() }()
	out := EgressAnswers{Answered: map[string]bool{}, Sources: map[string]string{}}
	for rows.Next() {
		var host, source string
		var allowed int
		if err := rows.Scan(&host, &allowed, &source); err != nil {
			return EgressAnswers{}, err
		}
		out.Answered[host] = true
		out.Sources[host] = source
		if allowed == 1 {
			out.Allowed = append(out.Allowed, host)
		}
	}
	sort.Strings(out.Allowed)
	return out, rows.Err()
}

// ForgetEgressHosts removes the answers for hosts of a repository, allowed or
// denied, so they count as unanswered: nothing is allowed and nothing is denied
// for good, and the human is asked again.
func (s *Store) ForgetEgressHosts(ctx context.Context, repo string, hosts []string) error {
	for _, h := range hosts {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM egress_hosts WHERE repo = ? AND hostname = ?`, repo, h); err != nil {
			return fmt.Errorf("store: forget the answer for %s: %w", h, err)
		}
	}
	return nil
}
