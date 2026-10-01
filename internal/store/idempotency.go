package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/wstein/workharbor/internal/domain"
)

// RuleKeyReuse is the conflict rule of a reused idempotency key.
const RuleKeyReuse domain.Rule = "idempotency-key-reuse"

// ErrKeyReuse is returned when an idempotency key is used again with a
// different request. It is a conflict (exit code 5).
var ErrKeyReuse = domain.NewConflict(RuleKeyReuse, "the idempotency key was already used with a different request")

const maxKeyLength = 255

// RequestHash derives the request hash for Do from the parts that make a
// request what it is (the command, the target and its arguments).
func RequestHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Do runs a mutating command under an idempotency key. fn does the work in a
// transaction and returns the response; the response is stored in that same
// transaction, so either the changes and the response both exist or neither
// does. Calling Do again with the same key and request returns the stored
// response without calling fn (replayed is true). The same key with a
// different request is ErrKeyReuse. If fn fails nothing is stored, and a retry
// runs fn again.
func (s *Store) Do(ctx context.Context, key, requestHash string, fn func(tx *Tx) ([]byte, error)) (response []byte, replayed bool, err error) {
	if key == "" || len(key) > maxKeyLength {
		return nil, false, fmt.Errorf("store: an idempotency key must be 1 to %d characters", maxKeyLength)
	}
	if requestHash == "" {
		return nil, false, errors.New("store: an idempotency key needs the hash of its request")
	}
	err = s.Update(ctx, func(tx *Tx) error {
		var storedHash string
		var stored []byte
		switch err := tx.tx.QueryRowContext(ctx, `SELECT request_hash, response FROM idempotency WHERE key = ?`, key).Scan(&storedHash, &stored); {
		case err == nil:
			if storedHash != requestHash {
				return ErrKeyReuse
			}
			response, replayed = stored, true
			return nil
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("store: %w", err)
		}
		out, err := fn(tx)
		if err != nil {
			return err
		}
		if out == nil {
			out = []byte{}
		}
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO idempotency (key, request_hash, response, created_at) VALUES (?, ?, ?, ?)`,
			key, requestHash, out, tx.s.now().UnixNano()); err != nil {
			return fmt.Errorf("store: %w", err)
		}
		response, replayed = out, false
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return response, replayed, nil
}
