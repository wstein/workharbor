# Feature 001 tasks

Status: revised proposal awaiting human spec/plan review. All tasks are pending.
Existing IDs T001–T010 are retained with narrower scope; T011–T020 separate work
previously bundled into them. Execute in phase/dependency order, not numeric order.
Every task ends with focused commits and recorded validation. No parallel execution
is authorized by this document; examples below identify only possible independence.

## Phase 1: Setup
- [ ] T001 Pin the Elixir/Erlang toolchain and scaffold `apps/control_plane/mix.exs`, `apps/control_plane/mix.lock` and `.tool-versions`; verify format, compile and `mix test` from `apps/control_plane/`.
- [ ] T011 Pin Go and scaffold `apps/host_agent/go.mod` and `apps/host_agent/cmd/workharbor-agent/main.go`; verify `gofmt -l .` is empty, `go build ./...` and `go test ./...` from `apps/host_agent/`.
- [ ] T012 Add both application format/build/test gates to `.github/workflows/check.yml` and root `Makefile`; local and CI commands must match, fail on missing tools and retain `make check` (SC-006).

## Phase 2: Foundations
- [ ] T002 Implement Workspace, Host, Operation, PersistentStorage, RuntimeInstance and Session migrations in `apps/control_plane/priv/repo/migrations/` and contexts in `apps/control_plane/lib/workharbor/`; enforce foreign-key ownership and unique `(owner_id, workspace_id, idempotency_key)`, canonical request hash/outcome, atomic `consumed_at`, independent storage lifecycle and generation checks; verify with `mix test` (FR-001, FR-006; data-model contract).
- [ ] T013 Implement local owner login and scoped authorization in `apps/control_plane/lib/workharbor/accounts.ex` and `apps/control_plane/lib/workharbor_web/plugs/require_owner.ex`; add wrong-owner and unauthenticated rejection tests in `apps/control_plane/test/workharbor/accounts_test.exs`; verify with `mix test` (FR-001, FR-003).
- [ ] T003 Define versioned messages and invalid-version/scope/replay fixtures in `contracts/host-protocol/`; implement validators in `apps/control_plane/lib/workharbor/host_protocol.ex` and `apps/host_agent/internal/protocol/`; verify both application suites reject invalid fixtures before provider calls and redact secrets (FR-003, FR-011).

## Phase 3: US1 — Register a host
Goal: authenticated enrollment and visible host identity/capabilities.
Independent acceptance: valid enrollment succeeds; expired/reused codes and revoked
identities fail, including concurrent attempts to consume a code.

- [ ] T004 [US1] Implement five-minute single-use enrollment and mTLS/scoped commands in `apps/control_plane/lib/workharbor/hosts.ex` and `apps/host_agent/internal/identity/`; verify expiry boundaries, atomic consumption, wrong-host commands and encrypted authenticated transport with both application suites and a real-host handshake (FR-002, FR-003; SC-004).
- [ ] T014 [US1] Implement rotation, revocation and reconnect in `apps/control_plane/lib/workharbor/host_connections.ex` and `apps/host_agent/internal/connection/`; test revoked reconnect rejection, ten-second heartbeats and unknown observations after 30 seconds with controlled clocks; record a real reconnect in `docs/quality.md` (FR-002, FR-009; SC-003).
- [ ] T015 [US1] Implement host enrollment/status UI in `apps/control_plane/lib/workharbor_web/live/hosts_live.ex`; verify enrollment success/error states, capabilities and stale display within five seconds of stale marking with LiveView tests and browser evidence in `docs/quality.md` (FR-009, FR-012; SC-003).

## Phase 4: US2 — Provision a configuration revision
Goal: explicitly trusted revisions produce one persistent workspace with diagnostics.
Independent acceptance: image/Dockerfile fixtures provision on Docker; unsupported
config and unapproved revisions produce no hooks or provider mutation.

- [ ] T005 [US2] Implement config/submodule resolution and capability validation in `apps/host_agent/internal/config/`; add traversal, unsupported-config and pinned-revision fixtures in `examples/workspace/`; verify with `go test ./...` that unsupported capabilities fail before side effects and effective commits are recorded (FR-004, FR-005).
- [ ] T016 [US2] Persist owner/repository/config-commit/submodule-commit/timestamp trust approvals in `apps/control_plane/lib/workharbor/config_trust.ex` and migrations; add approval UI in `apps/control_plane/lib/workharbor_web/live/config_trust_live.ex` and agent enforcement in `apps/host_agent/internal/config/trust.go`; test refusal and changed config/submodule commits require new approval before hooks, with both application suites and browser evidence (FR-004, FR-005, FR-011; SC-004).
- [ ] T006 [US2] Implement Docker provision/inspect and persistent storage through the provider port in `apps/host_agent/internal/ports/provider.go` and `apps/host_agent/internal/providers/docker/`; pin the official Dev Container CLI and document the tested matrix in `docs/development/devcontainer-matrix.md`; verify image/Dockerfile provisioning on real Docker and port-isolated unit tests (FR-005, FR-007).
- [ ] T017 [US2] Implement workspace creation and correlated redacted progress UI in `apps/control_plane/lib/workharbor_web/live/workspaces_live.ex`; test clone/build errors and effective revision display with LiveView tests, real provisioning and browser evidence in `docs/quality.md` (FR-004, FR-009, FR-011, FR-012).

## Phase 5: US3 — Access terminal
Goal: browser terminal confined to the authorized container with finite resources.
Independent acceptance: owner can type and resize; other-owner, expired/reused grants,
quota overflow and buffer overflow fail; transport loss ends only the connection and releases its relay.

- [ ] T008 [US3] Implement atomic 60-second grants in `apps/control_plane/lib/workharbor/sessions.ex`, relay in `apps/control_plane/lib/workharbor_web/channels/terminal_channel.ex` and PTY service in `apps/host_agent/internal/terminal/`; enforce eight-hour maximum, 15-minute input idle expiry, two connections/workspace, eight/owner, 1 MiB/direction, 1–500 columns and 1–200 rows, all per connection; disconnect, grant expiry and idle timeout end only the connection with the relay released within five seconds, while stop, rebuild, runtime deletion or an explicit close end the console session; verify boundary/concurrent-use tests in both suites plus a real container PTY (FR-008, FR-011; SC-004).
- [ ] T018 [US3] Implement browser terminal UI in `apps/control_plane/lib/workharbor_web/live/terminal_live.ex` and `apps/control_plane/assets/js/terminal.js`; test input, resize, fresh-grant reconnect, limit diagnostics and absence of host-shell/socket access; record browser walkthrough in `docs/quality.md` (FR-008, FR-012).

## Phase 6: US4 — Stop, restart and recover
Goal: retained files and no duplicate runtime after retries, interruption or reconnect.
Independent acceptance: stop/start retains uncommitted files; duplicate keys return
one outcome, changed payload conflicts; interrupted provision reconciles safely.

- [ ] T020 [US4] Implement idempotent start/stop with retained volumes in `apps/host_agent/internal/providers/docker/lifecycle.go` and `apps/control_plane/lib/workharbor/workspaces.ex`; verify repeated stop/start and identical uncommitted file contents on real Docker, plus both application suites (FR-006, FR-007; SC-002, SC-005).
- [ ] T007 [US4] Implement durable recovery/reconciliation in `apps/control_plane/lib/workharbor/operations.ex` and `apps/host_agent/internal/service/reconcile.go`; enforce changed-payload conflict, generation checks, three retries with 1/2/4-second delays, 120-second mutation deadlines and 20-minute provision deadline; inspect before retry with no overlapping mutation; verify interruption, exhausted retry, unavailable provider and responsive-provider reconnect within 30 seconds (FR-006, FR-009, FR-010, FR-011; SC-002, SC-003).
- [ ] T009 [US4] Add lifecycle controls and unknown/failed/retry diagnostics to `apps/control_plane/lib/workharbor_web/live/workspaces_live.ex`; test explicit retry after exhaustion, preserved evidence and stale-state display, with LiveView tests and real-host browser walkthrough in `docs/quality.md` (FR-009, FR-012; SC-003).

## Phase 7: Release evidence and recovery
- [ ] T019 Implement quiesced backup and clean-install restore in `scripts/backup-workharbor.sh`, `scripts/restore-workharbor.sh` and `docs/development/backup-restore.md`; verify retained owner/host/workspace associations, operation outcomes and exact uncommitted file contents, unknown restored observations, revoked-identity rejection and no automatic runtime start on a clean host; abort backup if quiescence fails (plan: backup/restore gate; SC-005).
- [ ] T010 Run US1–US4 and all SC-001–SC-006 at the specified five-workspace/eight-connection load on a real host; record exact startup/test/browser commands, versions, timings, idle/load measurements and failure evidence in `specs/001-workspace-lifecycle/quickstart.md` and `docs/quality.md`; include tested rotation and backup/restore runbooks and rerun `make check` plus both application gates; missing tools/hosts block release (FR-001–FR-012).

## Dependencies
T001/T011 → T012 → T002 → T013 → T003 → T004 → T014 → T015.
T005 follows T003; T016 follows T005/T013; T006 follows T004/T016;
T017 follows T006/T015. T008 follows T017; T018 follows T008.
T020 follows T006; T007 follows T020/T014; T009 follows T007/T017.
T019 follows T007; T010 follows T018/T009/T019 and all earlier acceptance gates.

## Potential parallel work
After authorization, T001/T011 use separate applications. T005 and T014 can proceed
after their prerequisites without sharing files. Terminal UI T018 and recovery T007
can proceed independently after their respective prerequisites. Shared-file tasks
T017/T009 remain sequential. No task is marked [P] because execution is sequential
by default.

## Implementation strategy and issue contract
Deliver US1 after setup/foundations as the first independently reviewable increment;
it is not the full product MVP. The usable MVP requires US1–US4 and release gates.
Each implementation issue must name Feature, Task, spec/plan/tasks paths, approved
scope, satisfied prerequisite IDs, concrete acceptance commands and human spec/plan
review evidence. Select the feature before Spec Kit commands. Run analyze before
implementation and converge only after an implementation pass. Do not regenerate
planning artifacts or execute unrelated tasks inside a bounded implementation issue.
