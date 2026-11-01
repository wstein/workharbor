// SPDX-License-Identifier: EUPL-1.2

// Package sandbox is the provider-neutral model of an execution instance of
// Feature 008: ids, ownership labels and the ownership gate, the lifecycle
// state table and retention. It is pure: it starts no process, touches no
// file and calls no provider. It never names a provider kind (FR-034).
package sandbox

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// Errors that are not refusals. The command layer maps them to exit codes in
// task T004: ErrInvalidID, ErrInvalidName to usage (64), ErrNotFound to not
// found (65), ErrNameTaken to conflict (68), ErrConfirmationRequired to
// needs-human (70) and ErrIDExhausted to internal (74).
var (
	ErrInvalidID            = errors.New("sandbox: invalid id")
	ErrIDExhausted          = errors.New("sandbox: no unused id found")
	ErrInvalidName          = errors.New("sandbox: invalid name")
	ErrNameTaken            = errors.New("sandbox: name already used by a live instance of the workspace")
	ErrNotFound             = errors.New("sandbox: unknown instance")
	ErrConfirmationRequired = errors.New("sandbox: confirmation required")
)

// ID is a sandbox id: "sbx-" and 16 lowercase base32 characters (FR-002).
type ID string

const (
	idPrefix    = "sbx-"
	idEntropy   = 10 // bytes; 80 bits make exactly 16 base32 characters
	idAttempts  = 8
	nameMaxLen  = 63
	labelMaxLen = 128
)

var (
	idShape   = regexp.MustCompile(`^sbx-[a-z2-7]{16}$`)
	nameShape = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	encoding  = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// Valid reports whether id has the id format.
func (id ID) Valid() bool { return idShape.MatchString(string(id)) }

// ParseID validates s as an id.
func ParseID(s string) (ID, error) {
	if !idShape.MatchString(s) {
		return "", ErrInvalidID
	}
	return ID(s), nil
}

// NewID draws an id from r, which is the CSPRNG when nil. Tests inject a
// deterministic reader. A short or failing source is an error, never a weak id.
func NewID(r io.Reader) (ID, error) {
	if r == nil {
		r = rand.Reader
	}
	var b [idEntropy]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("sandbox: reading entropy for an id: %w", err)
	}
	enc := []byte(encoding.EncodeToString(b[:]))
	for i, c := range enc {
		if c >= 'A' && c <= 'Z' {
			enc[i] = c + ('a' - 'A')
		}
	}
	return ID(idPrefix + string(enc)), nil
}

// NewUniqueID draws ids until taken reports one unused, so an id is never
// reused (FR-002). taken must also know discarded instances. It gives up with
// ErrIDExhausted after a bounded number of draws.
func NewUniqueID(r io.Reader, taken func(ID) bool) (ID, error) {
	for i := 0; i < idAttempts; i++ {
		id, err := NewID(r)
		if err != nil {
			return "", err
		}
		if taken == nil || !taken(id) {
			return id, nil
		}
	}
	return "", ErrIDExhausted
}

// ValidName checks an optional instance name. The empty name is valid. A name
// that looks like an id is refused so a name can never shadow an id.
func ValidName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > nameMaxLen || !nameShape.MatchString(name) || idShape.MatchString(name) {
		return ErrInvalidName
	}
	return nil
}

// NameAvailable reports ErrNameTaken when a non-discarded instance of the
// workspace already has the name. An empty name is always available.
func NameAvailable(records []Record, workspace, name string) error {
	if name == "" {
		return nil
	}
	for _, r := range records {
		if r.Workspace == workspace && r.Name == name && r.State != StateDiscarded {
			return ErrNameTaken
		}
	}
	return nil
}
