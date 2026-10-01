// Package runtime defines the runtime adapter contract. Backends report
// capabilities explicitly and do not pretend to share Docker semantics.
// See docs/content/docs/design.md §5.1.
package runtime

import (
	"context"
	"errors"

	"github.com/wstein/workharbor/internal/domain"
)

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

// Errors every adapter returns for the same situations, so callers and the
// conformance suite can tell them apart.
var (
	ErrNotFound   = errors.New("environment not found")
	ErrNotRunning = errors.New("environment is not running")
	ErrNotOwned   = errors.New("environment is not owned by this supervisor")
)

// Info describes one environment as the runtime reports it.
type Info struct {
	ID     string
	Owner  string // the OwnerLabel value
	Labels map[string]string
	Image  string
	State  domain.EnvState
	// Addr is the address the environment has now. It is empty unless the
	// environment is running, and it changes across a restart and a recreate,
	// so it is read again every time and never stored.
	Addr string
}

// ExecRequest is a command to run in a running environment.
type ExecRequest struct {
	Cmd []string
	Env []string
	Dir string
}

// Stream names a chunk's source.
type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// Chunk is output produced by a command.
type Chunk struct {
	Stream Stream
	Data   []byte
}

// ExecStream is a running command. Chunks delivers output as it is produced
// and is closed when the command has ended and everything was delivered; then
// Wait returns the exit code. A caller that stops reading cancels the context
// it passed to Exec.
type ExecStream interface {
	Chunks() <-chan Chunk
	Wait() (exitCode int, err error)
}

// Collect reads a stream to the end and returns its output and exit code.
func Collect(st ExecStream) (stdout, stderr []byte, exitCode int, err error) {
	for c := range st.Chunks() {
		switch c.Stream {
		case Stdout:
			stdout = append(stdout, c.Data...)
		case Stderr:
			stderr = append(stderr, c.Data...)
		}
	}
	exitCode, err = st.Wait()
	return stdout, stderr, exitCode, err
}

// Endpoint is a temporary access point into an environment.
type Endpoint struct {
	Kind string // "ssh" | "browser"
	Addr string
}

// Adapter manages execution environments on one backend. Start, Stop and
// Delete are idempotent, so a reconciler can retry them (design §5.3), and
// every method that takes an ID refuses an environment the adapter does not
// own with ErrNotOwned.
type Adapter interface {
	Name() string
	Capabilities() Capabilities

	// Provision creates an environment, stopped. It validates the spec and its
	// bind mounts first (Spec.Validate and Spec.CheckMounts) and creates
	// nothing if either fails.
	Provision(ctx context.Context, spec Spec) (envID string, err error)
	Start(ctx context.Context, envID string) error
	Stop(ctx context.Context, envID string) error
	// Delete removes one environment by its exact ID, never by pattern.
	Delete(ctx context.Context, envID string) error

	Inspect(ctx context.Context, envID string) (Info, error)
	// List returns the environments with this owner label and no others.
	List(ctx context.Context, owner string) ([]Info, error)

	// Exec runs a command in a running environment (ErrNotRunning otherwise).
	Exec(ctx context.Context, envID string, req ExecRequest) (ExecStream, error)
	Logs(ctx context.Context, envID string) ([]byte, error)
	Endpoints(ctx context.Context, envID string) ([]Endpoint, error)
}
