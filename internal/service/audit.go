package service

import (
	"context"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/store"
)

// VerifyAudit checks the hash chain over the audit entries (design §7.7). With a
// head the human recorded earlier it also catches entries cut off the end.
func (s *Service) VerifyAudit(ctx context.Context, head *store.AuditHead) (store.AuditCheck, error) {
	return s.store.VerifyAudit(ctx, head)
}

// AuditHead is the end of the chain, to be recorded somewhere the supervisor
// cannot write.
func (s *Service) AuditHead(ctx context.Context) (store.AuditHead, error) {
	return s.store.AuditHead(ctx)
}

// CommitAudit returns the audit entries that name a commit, oldest first: the
// pin, the push, the CI result, the pull request and the review Decision with its
// answer.
func (s *Service) CommitAudit(ctx context.Context, sha string) ([]domain.Event, error) {
	return s.store.AuditForSHA(ctx, sha)
}
