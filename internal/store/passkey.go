package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wstein/workharbor/internal/domain"
)

// Passkey is an enrolled authenticator: the credential record the WebAuthn library
// produced (its public key, sign counter and flags, as JSON) and a name for the
// human. Nothing in it is secret.
type Passkey struct {
	ID         string // the credential ID, base64url
	Name       string
	Credential []byte
	CreatedAt  time.Time
	LastUsed   time.Time // zero if it has not signed in yet
}

// AddPasskey stores a passkey. A credential ID is stored once.
func (s *Store) AddPasskey(ctx context.Context, p Passkey) error {
	if p.ID == "" || len(p.Credential) == 0 {
		return errors.New("store: a passkey needs an ID and a credential")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO passkeys (id, name, credential, created_at) VALUES (?, ?, ?, ?)`, p.ID, p.Name, string(p.Credential), toNano(p.CreatedAt))
	if err != nil {
		var exists int
		if s.db.QueryRowContext(ctx, `SELECT 1 FROM passkeys WHERE id = ?`, p.ID).Scan(&exists) == nil {
			return domain.NewConflict(RuleInUse, "this passkey is already enrolled")
		}
		return fmt.Errorf("store: add passkey: %w", err)
	}
	return nil
}

// Passkeys lists the enrolled passkeys, oldest first.
func (s *Store) Passkeys(ctx context.Context) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, credential, created_at, last_used FROM passkeys ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: passkeys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Passkey
	for rows.Next() {
		var p Passkey
		var cred string
		var created, used int64
		if err := rows.Scan(&p.ID, &p.Name, &cred, &created, &used); err != nil {
			return nil, err
		}
		p.Credential, p.CreatedAt = []byte(cred), fromNano(created)
		if used != 0 {
			p.LastUsed = fromNano(used)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePasskey stores the credential record after a ceremony (the sign counter and
// the backup state move) and when it was used.
func (s *Store) UpdatePasskey(ctx context.Context, id string, credential []byte, used time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE passkeys SET credential = ?, last_used = ? WHERE id = ?`, string(credential), toNano(used), id)
	if err != nil {
		return fmt.Errorf("store: update passkey: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &domain.NotFoundError{Kind: "passkey", ID: id}
	}
	return nil
}

// RevokePasskey removes a passkey: it signs nothing in again.
func (s *Store) RevokePasskey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: revoke passkey: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return &domain.NotFoundError{Kind: "passkey", ID: id}
	}
	return nil
}
