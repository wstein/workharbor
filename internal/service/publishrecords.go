package service

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/wstein/workharbor/internal/domain"
)

// publishRecords returns the latest check receipt and the failed publish steps
// recorded for one revision of a task.
func (s *Service) publishRecords(ctx context.Context, task domain.ID, sha string) (*domain.CheckReceipt, []domain.PublishAttempt, error) {
	r, err := s.checkReceipt(ctx, task, sha)
	if err != nil {
		return nil, nil, err
	}
	evs, err := s.store.EventsOfKind(ctx, task, domain.EventPublishAttempt)
	if err != nil {
		return nil, nil, err
	}
	var attempts []domain.PublishAttempt
	for _, e := range evs {
		var p domain.PublishAttempt
		if json.Unmarshal(e.Payload, &p) == nil && p.SHA == sha {
			attempts = append(attempts, p)
		}
	}
	return r, attempts, nil
}

// checkReceipt returns the latest receipt of the check that ran on sha, or nil.
func (s *Service) checkReceipt(ctx context.Context, task domain.ID, sha string) (*domain.CheckReceipt, error) {
	evs, err := s.store.EventsOfKind(ctx, task, domain.EventCheckReceipt)
	if err != nil {
		return nil, err
	}
	var out *domain.CheckReceipt
	for _, e := range evs {
		var r domain.CheckReceipt
		if json.Unmarshal(e.Payload, &r) == nil && r.SHA == sha {
			rc := r
			out = &rc
		}
	}
	return out, nil
}

// tailRunes keeps the last n characters of s.
func tailRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[len(r)-n:])
}
