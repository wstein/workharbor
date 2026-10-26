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
type PromoteOptions struct {
	Target      string
	Diagnostics io.Writer
}

// LifecycleConsent contains explicit caller decisions. Confirmed acknowledges
// the disclosed pause-writers condition. TrustDigest remains independent of
// confirmation. Volumes and Force are legacy fields, ignored by new-candidate
// disposal; they are never synthesized as consent.
type LifecycleConsent struct {
	TrustDigest                           string
	Confirmed, Volumes, Force, Quiescence bool
}

// LifecycleTransaction holds an immutable prepared preview. Apply is one-use;
// Close removes temporary execution capture only, never retained candidate work.
type LifecycleTransaction interface {
	Preview() LifecycleResult
	Apply(context.Context, LifecycleConsent) (LifecycleResult, error)
	Close() error
}
