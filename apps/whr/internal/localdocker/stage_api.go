// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"context"
	"io"
)

// StageOptions selects the source and primary. Prepare is an inspection and
// temporary-capture operation; consent is supplied only to Apply after preview.
type StageOptions struct {
	Source, Target string
	Smoke          bool
	Diagnostics    io.Writer
}

// LifecycleConsent contains explicit caller decisions. Confirmed acknowledges
// the disclosed pause-writers condition; Quiescence separately acknowledges it.
// This stage-only API cannot authorize home deletion.
type LifecycleConsent struct {
	TrustDigest           string
	Confirmed, Quiescence bool
}

// LifecycleTransaction holds an immutable prepared preview. Apply is one-use;
// Close removes temporary execution capture only, never retained candidate work.
type LifecycleTransaction interface {
	Preview() LifecycleResult
	Apply(context.Context, LifecycleConsent) (LifecycleResult, error)
	Close() error
}
