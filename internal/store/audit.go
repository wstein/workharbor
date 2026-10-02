package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/wstein/workharbor/internal/domain"
)

// chainVersion is part of every hash, so a later change of what is hashed is a
// new version and not a silent break.
const chainVersion = "whr-audit-v1"

// auditHash is the hash of one audit event and of the hash before it. Every
// field is length-prefixed, so no two events can hash alike by moving a byte
// from one field to the next.
func auditHash(prev string, seq int64, task, kind, tier string, at int64, payload []byte) string {
	h := sha256.New()
	field := func(b []byte) {
		h.Write([]byte(strconv.Itoa(len(b)) + ":"))
		h.Write(b)
	}
	for _, f := range []string{chainVersion, prev, strconv.FormatInt(seq, 10), task, kind, tier, strconv.FormatInt(at, 10)} {
		field([]byte(f))
	}
	field(payload)
	return hex.EncodeToString(h.Sum(nil))
}

var commitRe = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// commitOf returns the commit SHA an audit payload names, or "". Only a payload
// field called "sha" counts, and only a full lower-case hex object name.
func commitOf(payload []byte) string {
	var p struct {
		SHA string `json:"sha"`
	}
	if json.Unmarshal(payload, &p) != nil || !commitRe.MatchString(p.SHA) {
		return ""
	}
	return p.SHA
}

// chain writes the chain row of an audit event just inserted with this seq. It
// runs in the same transaction as the insert, so an audit event never exists
// without its row.
func (tx *Tx) chain(ctx context.Context, seq int64, e domain.Event, payload []byte) error {
	var prev string
	err := tx.tx.QueryRowContext(ctx, `SELECT hash FROM audit_chain ORDER BY seq DESC LIMIT 1`).Scan(&prev)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: audit chain head: %w", err)
	}
	hash := auditHash(prev, seq, string(e.TaskID), string(e.Kind), string(e.Tier), e.At.UnixNano(), payload)
	if _, err := tx.tx.ExecContext(ctx, `INSERT INTO audit_chain (seq, prev, hash, sha) VALUES (?, ?, ?, ?)`, seq, prev, hash, commitOf(payload)); err != nil {
		return fmt.Errorf("store: audit chain: %w", err)
	}
	return nil
}

// AuditHead is the end of the chain: the sequence number and hash of the latest
// chained audit entry. Recording it somewhere the supervisor cannot write (a
// note, another machine) is what makes a truncation of the log detectable:
// VerifyAudit can check the chain against it.
type AuditHead struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}

// AuditCheck is the result of VerifyAudit.
type AuditCheck struct {
	Checked   int       `json:"checked"`   // chained audit entries whose hash was recomputed
	Unchained int       `json:"unchained"` // audit entries from before the chain, not covered
	Head      AuditHead `json:"head"`
}

// AuditBroken is returned by VerifyAudit when the chain does not hold: the
// first entry where it fails and why.
type AuditBroken struct {
	Seq    int64
	Reason string
}

func (e *AuditBroken) Error() string {
	return fmt.Sprintf("audit log: entry %d: %s", e.Seq, e.Reason)
}

// VerifyAudit recomputes every hash of the chain in order. It fails at the first
// entry that was changed, that does not follow its predecessor, or that has no
// chain row although the chain had begun. If head is not nil, the chain must also
// end at it, which catches entries cut off the end.
func (s *Store) VerifyAudit(ctx context.Context, head *AuditHead) (AuditCheck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.seq, e.task_id, e.kind, e.tier, e.at, e.payload, COALESCE(c.prev, ''), COALESCE(c.hash, ''), COALESCE(c.sha, ''), c.seq IS NOT NULL
		FROM events e LEFT JOIN audit_chain c ON c.seq = e.seq WHERE e.tier = 'audit' ORDER BY e.seq`)
	if err != nil {
		return AuditCheck{}, fmt.Errorf("store: verify the audit log: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out AuditCheck
	prev := ""
	started := false
	for rows.Next() {
		var seq, at int64
		var task, kind, tier, rowPrev, rowHash, rowSHA string
		var payload []byte
		var has bool
		if err := rows.Scan(&seq, &task, &kind, &tier, &at, &payload, &rowPrev, &rowHash, &rowSHA, &has); err != nil {
			return AuditCheck{}, err
		}
		if !has {
			if started {
				return out, &AuditBroken{Seq: seq, Reason: "an audit entry has no chain row"}
			}
			out.Unchained++
			continue
		}
		started = true
		switch {
		case rowPrev != prev:
			return out, &AuditBroken{Seq: seq, Reason: "it does not follow the entry before it"}
		case rowHash != auditHash(prev, seq, task, kind, tier, at, payload):
			return out, &AuditBroken{Seq: seq, Reason: "the entry was changed"}
		case rowSHA != commitOf(payload):
			return out, &AuditBroken{Seq: seq, Reason: "the commit it names was changed"}
		}
		prev = rowHash
		out.Checked++
		out.Head = AuditHead{Seq: seq, Hash: rowHash}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if head != nil && (out.Head != *head) {
		return out, &AuditBroken{Seq: head.Seq, Reason: "the chain does not end at the recorded head: entries were cut off or replaced"}
	}
	return out, nil
}

// AuditHead returns the end of the chain, or the zero value when no audit
// entry is chained yet.
func (s *Store) AuditHead(ctx context.Context) (AuditHead, error) {
	var h AuditHead
	err := s.db.QueryRowContext(ctx, `SELECT seq, hash FROM audit_chain ORDER BY seq DESC LIMIT 1`).Scan(&h.Seq, &h.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return AuditHead{}, nil
	}
	if err != nil {
		return AuditHead{}, fmt.Errorf("store: audit head: %w", err)
	}
	return h, nil
}

// AuditForSHA returns the audit entries that name a commit, oldest first: the
// push, the CI result, the pull request, the review Decision and its answer.
func (s *Store) AuditForSHA(ctx context.Context, sha string) ([]domain.Event, error) {
	if !commitRe.MatchString(sha) {
		return nil, fmt.Errorf("store: %q is not a full commit SHA", sha)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.seq, e.task_id, e.kind, e.tier, e.payload, e.at FROM events e
		JOIN audit_chain c ON c.seq = e.seq WHERE c.sha = ? ORDER BY e.seq`, sha)
	if err != nil {
		return nil, fmt.Errorf("store: audit of %s: %w", sha, err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var task, kind, tier string
		var at int64
		if err := rows.Scan(&e.Seq, &task, &kind, &tier, &e.Payload, &at); err != nil {
			return nil, err
		}
		e.TaskID, e.Kind, e.Tier, e.At = domain.ID(task), domain.EventKind(kind), domain.Tier(tier), fromNano(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
