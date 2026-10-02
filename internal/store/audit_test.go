package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

const (
	sha1hex = "0123456789abcdef0123456789abcdef01234567"
	sha2hex = "89abcdef0123456789abcdef0123456789abcdef"
)

func withSHA(task string, kind domain.EventKind, sha string) domain.Event {
	return domain.Event{TaskID: domain.ID(task), Kind: kind, Tier: domain.TierAudit, Payload: []byte(`{"sha":"` + sha + `"}`)}
}

func mustAppend(t *testing.T, s *Store, events ...domain.Event) []domain.Event {
	t.Helper()
	out, err := s.Append(bg, events...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// dropGuards removes the append-only triggers, as someone with the database
// file could, so a test can tamper with it. The chain is what must notice.
func dropGuards(t *testing.T, s *Store) {
	t.Helper()
	for _, trg := range []string{"events_no_update", "events_audit_no_delete", "audit_chain_no_update", "audit_chain_no_delete"} {
		if _, err := s.db.ExecContext(bg, `DROP TRIGGER `+trg); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEveryAuditEntryIsChainedAndTranscriptIsNot(t *testing.T) {
	s := openTemp(t)
	mustAppend(t, s, audit("t1", domain.EventTaskState), transcript("t1", "hello"), audit("t2", domain.EventRunState), transcript("t2", "world"), audit("t1", domain.EventDecisionRaised))
	var n int
	if err := s.db.QueryRowContext(bg, `SELECT COUNT(*) FROM audit_chain`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("chain rows = %d, %v; want 3 (the audit entries only)", n, err)
	}
	res, err := s.VerifyAudit(bg, nil)
	if err != nil || res.Checked != 3 || res.Unchained != 0 {
		t.Fatalf("verify = %+v, %v", res, err)
	}
	head, err := s.AuditHead(bg)
	if err != nil || head != res.Head || head.Seq != 5 || len(head.Hash) != 64 {
		t.Errorf("head = %+v, verify head %+v, %v", head, res.Head, err)
	}
	if _, err := s.VerifyAudit(bg, &head); err != nil {
		t.Errorf("verify against the recorded head: %v", err)
	}
	// A purge removes transcript content and leaves the chain intact.
	if _, err := s.Purge(bg, PurgeSpec{TaskID: "t1", Actor: "werner", All: true}); err != nil {
		t.Fatal(err)
	}
	if res, err := s.VerifyAudit(bg, nil); err != nil || res.Checked != 4 { // the purge's own audit entry joins the chain
		t.Errorf("after a purge: %+v, %v", res, err)
	}
}

func TestAChangedEntryBreaksTheChainAtThatEntry(t *testing.T) {
	s := openTemp(t)
	mustAppend(t, s, audit("t1", domain.EventTaskState), audit("t1", domain.EventRunState), audit("t1", domain.EventDecisionRaised))
	dropGuards(t, s)
	if _, err := s.db.ExecContext(bg, `UPDATE events SET payload = ? WHERE seq = 2`, []byte(`{"to":"forged"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := s.VerifyAudit(bg, nil)
	var broken *AuditBroken
	if !errors.As(err, &broken) || broken.Seq != 2 || !strings.Contains(broken.Reason, "changed") {
		t.Errorf("err = %v, want entry 2 changed", err)
	}
}

func TestAnEntryInsertedOutsideAppendIsNoticed(t *testing.T) {
	s := openTemp(t)
	mustAppend(t, s, audit("t1", domain.EventTaskState))
	dropGuards(t, s)
	if _, err := s.db.ExecContext(bg, `INSERT INTO events (task_id, kind, tier, payload, at) VALUES ('t1', 'task.state', 'audit', '{}', 1)`); err != nil {
		t.Fatal(err)
	}
	_, err := s.VerifyAudit(bg, nil)
	var broken *AuditBroken
	if !errors.As(err, &broken) || broken.Seq != 2 || !strings.Contains(broken.Reason, "no chain row") {
		t.Errorf("err = %v, want entry 2 without a chain row", err)
	}
}

func TestACutOffTailIsOnlyNoticedAgainstARecordedHead(t *testing.T) {
	s := openTemp(t)
	mustAppend(t, s, audit("t1", domain.EventTaskState), audit("t1", domain.EventRunState), audit("t1", domain.EventDecisionRaised))
	head, _ := s.AuditHead(bg)
	dropGuards(t, s)
	for _, q := range []string{`DELETE FROM audit_chain WHERE seq = 3`, `DELETE FROM events WHERE seq = 3`} {
		if _, err := s.db.ExecContext(bg, q); err != nil {
			t.Fatal(err)
		}
	}
	if res, err := s.VerifyAudit(bg, nil); err != nil || res.Checked != 2 {
		t.Fatalf("a shorter chain is still a valid chain: %+v, %v", res, err)
	}
	var broken *AuditBroken
	if _, err := s.VerifyAudit(bg, &head); !errors.As(err, &broken) || !strings.Contains(broken.Reason, "recorded head") {
		t.Errorf("against the recorded head: %v", err)
	}
}

func TestAuditBeforeTheChainIsCountedNotCovered(t *testing.T) {
	s := openTemp(t)
	// Two audit entries written before the chain existed: no chain row.
	for range 2 {
		if _, err := s.db.ExecContext(bg, `INSERT INTO events (task_id, kind, tier, payload, at) VALUES ('t0', 'task.state', 'audit', '{}', 1)`); err != nil {
			t.Fatal(err)
		}
	}
	mustAppend(t, s, audit("t1", domain.EventTaskState))
	res, err := s.VerifyAudit(bg, nil)
	if err != nil || res.Unchained != 2 || res.Checked != 1 {
		t.Errorf("verify = %+v, %v", res, err)
	}
}

func TestTheAuditTrailOfACommit(t *testing.T) {
	s := openTemp(t)
	mustAppend(t, s,
		withSHA("t1", domain.EventRevisionPinned, sha1hex),
		audit("t1", domain.EventTaskState),
		withSHA("t2", domain.EventRevisionPinned, sha2hex),
		withSHA("t1", domain.EventRevisionPushed, sha1hex),
		withSHA("t1", domain.EventCIRecorded, strings.ToUpper(sha1hex)), // not a lower-case object name: not indexed
		withSHA("t1", domain.EventPRRecorded, "abc123"),                 // not a full SHA
	)
	got, err := s.AuditForSHA(bg, sha1hex)
	if err != nil || len(got) != 2 || got[0].Kind != domain.EventRevisionPinned || got[1].Kind != domain.EventRevisionPushed || got[0].Seq >= got[1].Seq {
		t.Fatalf("trail = %+v, %v", got, err)
	}
	if other, _ := s.AuditForSHA(bg, sha2hex); len(other) != 1 || other[0].TaskID != "t2" {
		t.Errorf("trail of the other commit = %+v", other)
	}
	if none, err := s.AuditForSHA(bg, "ffffffffffffffffffffffffffffffffffffffff"); err != nil || len(none) != 0 {
		t.Errorf("unknown commit = %+v, %v", none, err)
	}
	for _, bad := range []string{"", "abc", "main", sha1hex + "0", "--all"} {
		if _, err := s.AuditForSHA(bg, bad); err == nil {
			t.Errorf("%q must not be accepted as a SHA", bad)
		}
	}
	// Moving an entry to another commit is a change the chain notices.
	dropGuards(t, s)
	if _, err := s.db.ExecContext(bg, `UPDATE audit_chain SET sha = ? WHERE seq = 4`, sha2hex); err != nil {
		t.Fatal(err)
	}
	var broken *AuditBroken
	if _, err := s.VerifyAudit(bg, nil); !errors.As(err, &broken) || broken.Seq != 4 || !strings.Contains(broken.Reason, "commit") {
		t.Errorf("err = %v, want entry 4: the commit it names was changed", err)
	}
}

// Every field is length-prefixed, so moving bytes between fields changes the hash.
func TestTheHashDoesNotConfuseFields(t *testing.T) {
	at := time.Unix(1, 0).UnixNano()
	a := auditHash("p", 1, "t1", "kind", "audit", at, []byte("ab"))
	b := auditHash("p", 1, "t1k", "ind", "audit", at, []byte("ab"))
	c := auditHash("p", 1, "t1", "kind", "audit", at, []byte("a"))
	d := auditHash("q", 1, "t1", "kind", "audit", at, []byte("ab"))
	if a == b || a == c || a == d || a != auditHash("p", 1, "t1", "kind", "audit", at, []byte("ab")) {
		t.Error("hash collisions between different events")
	}
}

func TestTheChainRowsAreAppendOnly(t *testing.T) {
	s := openTemp(t)
	mustAppend(t, s, audit("t1", domain.EventTaskState))
	if _, err := s.db.ExecContext(bg, `UPDATE audit_chain SET hash = 'x' WHERE seq = 1`); err == nil {
		t.Error("a chain row was updated")
	}
	if _, err := s.db.ExecContext(bg, `DELETE FROM audit_chain WHERE seq = 1`); err == nil {
		t.Error("a chain row was deleted")
	}
}
