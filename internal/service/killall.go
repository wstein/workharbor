package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/domain"
)

// KillReport says what a kill-all did.
type KillReport struct {
	Cancelled     []domain.ID `json:"cancelled"`
	TokensRevoked int         `json:"tokens_revoked"`
	Problems      []string    `json:"problems"`
}

// KillAll is the kill switch (design §7.7): it stops every run, cancels every
// unfinished task and revokes the forge tokens the supervisor holds. It goes on
// after a failure, so one task that cannot be cancelled does not leave the
// others running, and says what failed; the token revocation runs even if a
// task could not be cancelled. What it cannot revoke are the agent's own
// credentials, which stay with the human (D40). The attempt is an audit entry in
// the supervisor's stream, whatever happened, and an error is returned only if
// that entry could not be written or nothing could be listed.
func (s *Service) KillAll(ctx context.Context, actor string) (KillReport, error) {
	rep := KillReport{Cancelled: []domain.ID{}, Problems: []string{}}
	tasks, err := s.store.Tasks(ctx, true)
	if err != nil {
		return rep, fmt.Errorf("kill-all: list the tasks: %w", err)
	}
	// Every session first: cancelling a task saves it, which is slower than a
	// stop, and a run must not keep working while the others are saved.
	for _, t := range tasks {
		agg, err := s.store.LoadTask(ctx, t.ID)
		if err != nil {
			continue // reported by the cancel below
		}
		if r, ok := agg.LiveRun(); ok {
			s.stopSession(r.ID)
		}
	}
	for _, t := range tasks {
		if err := s.Cancel(ctx, t.ID); err != nil {
			rep.Problems = append(rep.Problems, fmt.Sprintf("cancel task %s: %v", t.ID, err))
			continue
		}
		rep.Cancelled = append(rep.Cancelled, t.ID)
	}
	if s.cfg.RevokeTokens != nil {
		n, err := s.cfg.RevokeTokens(ctx)
		rep.TokensRevoked = n
		if err != nil {
			rep.Problems = append(rep.Problems, err.Error())
		}
	}
	saved, aerr := s.store.Append(ctx, domain.NewKillAllEvent(domain.KillAll{
		Actor: actor, Cancelled: rep.Cancelled, TokensRevoked: rep.TokensRevoked, Problems: rep.Problems,
	}, s.clock.Now()))
	if aerr != nil {
		return rep, errors.Join(fmt.Errorf("kill-all: write the audit entry: %w", aerr))
	}
	s.publish(saved)
	return rep, nil
}

// ErrNoRevoker means the supervisor holds no forge token it could revoke.
var ErrNoRevoker = errors.New("service: there is no forge token to revoke")

// RevokeForgeTokens revokes the forge tokens the supervisor holds and nothing
// else: the secret operation the web UI offers behind a passkey step-up (D45).
// The attempt is an audit entry in the supervisor's stream whatever happened, with
// who did it and how many tokens went, never a token.
func (s *Service) RevokeForgeTokens(ctx context.Context, actor string) (int, error) {
	if s.cfg.RevokeTokens == nil {
		return 0, ErrNoRevoker
	}
	n, rerr := s.cfg.RevokeTokens(ctx)
	rec := domain.TokensRevoked{Actor: actor, Revoked: n}
	if rerr != nil {
		rec.Problem = rerr.Error()
	}
	saved, aerr := s.store.Append(ctx, domain.NewTokensRevokedEvent(rec, s.clock.Now()))
	if aerr != nil {
		return n, errors.Join(rerr, fmt.Errorf("revoke tokens: write the audit entry: %w", aerr))
	}
	s.publish(saved)
	return n, rerr
}
