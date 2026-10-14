# 0010: Go product code, whr CLI layout

Date: 2026-10-13
Status: Proposed for owner review and merge
Amends: [ADR 0001](0001-stack.md) (does not supersede it)

## Context

ADR 0001 fixes Elixir/Phoenix for the control plane and Go for the host agent, and
notes that no product code exists. The `whr` CLI and the host agent need one
product language and shared API client. The Python harness (`scripts/`, `tests/`)
is development tooling and the only executable oracle for several behaviors today.
Owner decision H10:a/H11:a, 2026-10-13.

## Decision

1. Product code outside the Elixir control plane is Go: the `whr` CLI and the host
   agent. ADR 0001's Elixir/Go split is unchanged; this ADR adds the CLI to Go.
2. The Python harness stays development tooling. It is never a product dependency
   and is not rewritten wholesale.
3. Ports from Python to Go are selective. Each port is gated by frozen behavior
   vectors for the ported behavior and its output semantics, captured from Python as
   the oracle until the vectors are accepted. Go must reproduce them; a vector change
   needs a reviewed decision, not a silent Go-side edit. Vectors do not define `whr`
   exit codes: those come only from the separate exit-code ADR (H15:b, fresh table).
4. Layout: each Go component is its own module under `apps/`, consistent with
   `apps/host_agent`. `whr` is another module, `apps/whr`. The shared API client
   is its own package in a module of its own under `apps/`, outside either
   `internal/` (other modules cannot import it), used by both. Its name and path
   are left to the whr spec or a later ADR. Within a module (paths relative to it):
   - `cmd/whr/` (in `apps/whr`) is the thin CLI entry point; no domain logic.
   - `internal/` holds domain logic, not importable outside its module.
   The host agent keeps its existing location and plans.
5. Exit codes and output schemas are language-neutral contract files under
   `contracts/` (planned: `contracts/cli/exit-codes.*` and
   `contracts/cli/output.schema.json`). Exit-code numbering is fixed by a separate
   owner-reviewed ADR; output schema content is set by the whr feature.

This ADR amends rather than supersedes ADR 0001: the Elixir control plane and Go
host agent decisions stay intact, and only the set of Go components grows, so the
consequences rule's superseding ADR is not needed.

## Go CI acceptance plan (not yet implemented, not claimed passing)

Per [ADR 0007](0007-explicit-ci-acceptance.md) Go checks must be named explicitly in
the reviewed acceptance manifest and run by the same repository-owned runner as
`make check`. The plan, once Go code exists:

- In each Go module, `gofmt -l .` reports no files.
- `go vet ./...`
- `go build ./...`
- `go test ./...` with required test IDs listed in
  `docs/development/ci-acceptance.json` (an extension of its schema) and behavior
  vectors checked against the Python oracle.

Until a Go module exists, none of these checks run and none is reported as passing.
`docs/development/ci-acceptance.json` and `scripts/check_ci.py` are intentionally
unchanged here; the manifest and runner extension is follow-up work requiring owner
PR review and merge under ADR 0007. Real-host/browser evidence remains separate.

## Consequences

- Product dependencies stay free of AI tooling and Python.
- Two languages share behavior only through contracts and frozen vectors.
- Changes to this decision require an owner-reviewed ADR.
