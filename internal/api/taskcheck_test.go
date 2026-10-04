package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
	"github.com/wstein/workharbor/internal/service"
)

func TestTaskViewCarriesTheCheckReceiptAndTheFailedPublishSteps(t *testing.T) {
	t.Parallel()
	retry := time.Date(2026, 10, 1, 9, 1, 0, 0, time.UTC)
	v := service.TaskView{
		Task:      domain.Task{ID: "t1", State: domain.TaskReadyForReview},
		Candidate: &domain.ReviewCandidate{Branch: "agent/x", SHA: "abc"},
		Check:     &domain.CheckReceipt{SHA: "abc", Command: "make check", Source: "pre-commit", Code: 1, Millis: 1500, Output: "FAIL\n"},
		PublishAttempts: []domain.PublishAttempt{
			{SHA: "abc", Attempt: 1, Transient: true, Error: "timeout", RetryAt: retry},
			{SHA: "abc", Attempt: 2, Error: "not a fast-forward"},
		},
	}
	raw, err := json.Marshal(taskOf(v))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"check":{"sha":"abc","command":"make check","source":"pre-commit","exit_status":1,"duration_ms":1500,"output":"FAIL\n"}`,
		`"publish_attempts":[{"sha":"abc","attempt":1,"transient":true,"error":"timeout","retry_at":"2026-10-01T09:01:00Z"},{"sha":"abc","attempt":2,"transient":false,"error":"not a fast-forward"}]`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	plain, _ := json.Marshal(taskOf(service.TaskView{Task: domain.Task{ID: "t2"}}))
	if strings.Contains(string(plain), `"check"`) || strings.Contains(string(plain), "publish_attempts") {
		t.Errorf("an unchecked task shows a check: %s", plain)
	}
}
