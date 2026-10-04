package serve

import (
	"context"

	"github.com/wstein/workharbor/internal/forge"
	"github.com/wstein/workharbor/internal/policy"
)

// ForgeAccess is what the rest of serve may do with the forge. The raw
// forge.Adapter is held only here and in Build (real.go): every consumer gets
// one of these narrow members, and every effect goes through a forge.Guard
// built by Guard. TestRawForgeStaysAtTheRoot fails when another package names
// the adapter type (issue #247).
type ForgeAccess struct {
	// Guard returns a forge.Guard over the forge with the given pusher and
	// verifier, under the default table: the guarded publish and, with a nil
	// pusher and verifier, the guarded board.
	Guard func(p forge.Pusher, v forge.Verifier) *forge.Guard
	// DefaultBranch reads a repository's default branch from the forge; nil when
	// the adapter cannot name one.
	DefaultBranch forge.DefaultBrancher
	// RevokeTokens revokes the tokens the forge adapter holds; nil when it has no
	// way to (the GitHub App client has).
	RevokeTokens func(context.Context) (int, error)
}

// NewForgeAccess narrows a forge adapter to a ForgeAccess. It is called by Build
// and by tests with a fake, never by a consumer.
func NewForgeAccess(a forge.Adapter) ForgeAccess {
	if a == nil {
		return ForgeAccess{}
	}
	fa := ForgeAccess{Guard: func(p forge.Pusher, v forge.Verifier) *forge.Guard {
		return forge.NewGuard(a, p, policy.Default(), v)
	}}
	if db, ok := a.(forge.DefaultBrancher); ok {
		fa.DefaultBranch = db
	}
	if r, ok := a.(interface {
		RevokeTokens(context.Context) (int, error)
	}); ok {
		fa.RevokeTokens = r.RevokeTokens
	}
	return fa
}
