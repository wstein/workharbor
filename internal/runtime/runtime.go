// Package runtime defines the runtime adapter contract. Backends report
// capabilities explicitly and do not pretend to share Docker semantics.
// See docs/content/docs/design.md §5.1.
package runtime

import "context"

// Isolation describes the isolation boundary of a backend.
type Isolation string

const (
	SharedKernel Isolation = "shared-kernel"
	GuestKernel  Isolation = "guest-kernel"
	FullVM       Isolation = "full-vm"
)

// Capabilities are reported by the backend, never assumed.
type Capabilities struct {
	Isolation         Isolation
	Arch              string
	PersistentStorage []string // what survives stop, rebuild, delete
	NetworkIsolation  bool
	Suspend           bool
	SSH               bool
	Browser           bool
}

// Spec describes an environment to provision.
type Spec struct {
	Image    string
	CPUs     int
	MemoryMB int
	Mounts   []Mount
}

// Mount is an explicit project mount; host-home mounts are rejected.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// Endpoint is a temporary access point into an environment.
type Endpoint struct {
	Kind string // "ssh" | "browser"
	Addr string
}

// Adapter manages execution environments on one backend.
type Adapter interface {
	Name() string
	Capabilities() Capabilities
	Provision(ctx context.Context, spec Spec) (envID string, err error)
	Start(ctx context.Context, envID string) error
	Stop(ctx context.Context, envID string) error
	Delete(ctx context.Context, envID string) error
	Inspect(ctx context.Context, envID string) (state string, err error)
	Exec(ctx context.Context, envID string, cmd []string) (exitCode int, err error)
	Logs(ctx context.Context, envID string) ([]byte, error)
	Endpoints(ctx context.Context, envID string) ([]Endpoint, error)
}
