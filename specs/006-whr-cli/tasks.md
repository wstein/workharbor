# Feature 006 pilot work items

Status: execution in progress. T001 scaffold independently Sol CLEAR at
`d3d84c8361a1389292fad96d02a828ac9fabc2e3`; T002 is independently CLEAR at
`bc18034164597aa841bea561cf6a882e9c0715e2`; T003 independently CLEAR at
`191c62baf227f64122770244d62d289900bb4d8d`; T004 and later work items remain open.
No product runtime acceptance is claimed.
Branch/worktree: `feat/installable-pilot`, retained through final pilot acceptance.
Input: [spec](spec.md), [plan](plan.md), [research](research.md),
[data model](data-model.md), [migration](migration.md), [owner API](contracts/owner-api.md).

Each Tnnn below is one WI. Execute in dependency order, write failure-path tests
before behavior, run actual acceptance, then obtain independent review and fix all
findings before its successor. Each WI records its exact candidate and reviewed
revision. Existing Feature 001 task IDs remain authoritative and are completed only
with their own required evidence. This list maps the combined delivery; it does not
silently weaken Feature 001 or mark its tasks complete.

## Phase 1: Setup

Goal: locked, reproducible buildable applications, not a usable product.
Independent check: clean-checkout format/build/tests with missing-tool failures.

- [x] T001 Scaffold and pin Phoenix/Ecto/SQLite in `apps/control_plane/`, Go modules `apps/host_agent/go.mod`, `apps/whr/go.mod`, `apps/api_client/go.mod`, root `go.work` and `.tool-versions`; lock dependencies and pass each application format/build/test gate (Feature 001 T001/T011).
- [x] T002 Add explicit application checks and meaningful required test inventory to `scripts/check_ci.py`, `docs/development/ci-acceptance.json`, `Makefile` and `.github/workflows/check.yml`, with fixed gate/event-validation helper `scripts/application_checks.py`, negative fixtures `scripts/test_application_checks.py`, complete real-checker fixture copy lists in `scripts/test_task_readiness.py` and `scripts/test_task_source.py`, and test-only ExUnit reporter `apps/control_plane/test/support/acceptance_formatter.ex`; the helpers keep application argv out of manifest data and report actual ExUnit outcomes without adding product behavior. Prove local/CI equivalence, fail on missing tools and preserve all inherited tests (001 T012; ADR 0007/0010).

## Phase 2: Foundations and migration

Goal: product boundary, durable owner state and reviewed Python-to-Go parity.
Independent check: negative vectors, cross-language bytes and no runtime Python.

- [x] T003 Freeze the selected Python behavior inventory, source/tool digests and independently inspected vectors in `contracts/migration/`, with offline generator `scripts/generate_go_vectors.py`, strict fixture schema, meaningful negative tests in `tests/test_go_vectors.py` and their required IDs in `docs/development/ci-acceptance.json`; cover nested duplicate keys, malformed UTF-8, integer bounds, float/non-finite rejection, non-ASCII ordering and paths. Retain development-only rows in `specs/006-whr-cli/migration.md` (ADR 0010).
- [x] T004 Port only accepted pure JSON/path behavior into `apps/api_client/wirejson/` and `apps/host_agent/internal/config/`; write Go negative/golden tests first and reproduce frozen vectors independently in Python; no caller switches before parity and independent review (T003).
- [ ] T005 Implement ownership, separate storage/runtime records, durable operations/canonical request hash, generation checks and audit in `apps/control_plane/lib/workharbor/` and migrations/tests in `apps/control_plane/`; prove owner/workspace key uniqueness, changed-payload conflicts and crash-safe intent-before-side-effect (001 T002).
- [ ] T006 Implement owner bootstrap/login, revocable scoped credentials and browser request protections in `apps/control_plane/lib/workharbor/accounts.ex`, `apps/control_plane/lib/workharbor_web/` and tests; prove TLS-first auth, wrong owner, expired/revoked token, read-only dashboard scope, Host/Origin/anti-forgery rejection and secret redaction (001 T013; FR-035–038).
- [ ] T007 Define/validate versioned host protocol in `contracts/host-protocol/`, `apps/control_plane/lib/workharbor/host_protocol.ex`, `apps/host_agent/internal/protocol/`; reject invalid version/scope/replay before provider calls and redact diagnostics in both suites (001 T003).

## Phase 3: US1 — Authenticate and manage workspaces (P1)

Goal: lifecycle and operation recovery shared by browser and CLI.
Independent check: trusted image/Dockerfile create, start/stop/rebuild/removal,
retained files and exactly one operation after lost responses.

- [ ] T008 [US1] Implement one-time enrollment, mTLS identity/rotation/revocation and outbound heartbeat/reconnect in `apps/control_plane/lib/workharbor/hosts.ex`, `apps/control_plane/lib/workharbor/host_connections.ex`, `apps/host_agent/internal/identity/`, `apps/host_agent/internal/connection/`; exercise concurrency/expiry/scope and real handshake/reconnect; stale after 30 seconds (001 T004/T014).
- [ ] T009 [US1] Implement trusted config/submodule resolution, owner revision approval, official pinned Dev Container CLI and Docker storage/provider operations in `apps/host_agent/internal/config/`, `apps/host_agent/internal/providers/docker/`, `apps/control_plane/lib/workharbor/config_trust.ex`, `examples/workspace/` and `docs/development/devcontainer-matrix.md`; prove refusal/unsupported config has no hooks/provider mutation and real Docker retains files (001 T005/T016/T006).
- [ ] T010 [US1] Implement lifecycle reconciliation, bounded nonoverlapping retries and durable start/stop/rebuild/runtime-versus-data deletion in `apps/control_plane/lib/workharbor/operations.ex`, `apps/control_plane/lib/workharbor/workspaces.ex`, `apps/host_agent/internal/service/`, `apps/host_agent/internal/providers/docker/`; test interrupted provisioning, generations, failed rebuild, exhausted retries and exact uncommitted hashes on Docker (001 T007/T020; FR-003–005a).
- [ ] T011 [US1] Implement typed TLS owner client and `auth`, `ws`, `op` commands in `apps/api_client/` and `apps/whr/internal/commands/`, with shared human-text escaping in `apps/whr/internal/terminal/` before displaying any untrusted names, errors or remote messages; replace every FR-022 control class with visible placeholders in human output and escape them in JSON. Write targeted hostile-text tests before behavior, alongside first login, secret refusal, profile permissions, server identity, stable preallocated workspace/key, lost-response lookup and wait timeout/detach tests (FR-002–005a/022/035).
- [ ] T012 [US1] Implement enrollment/trust/workspace lifecycle browser views in `apps/control_plane/lib/workharbor_web/live/`; test and record real browser success/error/stale/retry/progress journeys with corresponding audit (001 T015/T017/T009).

## Phase 4: US2 — Shell and persistent console (P1)

Goal: scoped bounded access with session lifetime distinct from attachment lifetime.
Independent check: type/resize, detach/reattach, retained tail and no host access.

- [ ] T013 [US2] Implement scoped atomic grants, bounded relay, fresh shell and one persistent console per workspace in `apps/control_plane/lib/workharbor/sessions.ex`, `apps/control_plane/lib/workharbor_web/channels/`, `apps/host_agent/internal/terminal/`; test all Feature 001 bounds, 64 KiB tail/drop reporting, concurrent single use, endpoint restart replay and <=5s cleanup; stop/rebuild/delete/close end session (001 T008; FR-006–007a).
- [ ] T014 [US2] Implement `open`, `shell`, `console` and browser terminal in `apps/whr/internal/commands/`, `apps/control_plane/lib/workharbor_web/live/terminal_live.ex`, `apps/control_plane/assets/js/terminal.js`; implement the complete baseline FR-022a bounded stateful stream filter in `apps/whr/internal/terminal/` before accepting any CLI attachment. Filter OSC/DCS/APC/PM/SOS by default, including split-frame and unterminated strings; require explicit per-session raw opt-in displayed at session start and never persisted, and never emit raw terminal bytes in JSON. Write targeted hostile-stream, split-frame, unterminated-string, bounded-state and raw-opt-in/reset tests before behavior; verify input/resize, fresh-grant reconnect, status/close, foreign scope, remote 0–255 and real browser/PTY evidence (001 T018; FR-022/022a).

## Phase 5: US3 — Host operations (P2)

Goal: resumable local administration with truthful state and revocation.
Independent check: dry-run no writes, interrupted setup/offboard resumes safely.

- [ ] T015 [US3] Implement typed Linux/systemd change plans and atomic checkpoints for `setup`, `service`, `offboard host` in `apps/whr/internal/host/`; test unsupported OS/privilege, enrollment failure, each interrupted step, confirmed revocation before deletion, local-only nonzero and default data retention; exercise a disposable real Linux host (FR-009/011–013).
- [ ] T016 [US3] Implement `doctor` and shareable redacted report in `apps/whr/internal/doctor/`; test pass/fail/skipped/unknown, absent tools, private-file permissions and secret redaction; verify real host findings are measured (FR-010/021).

## Phase 6: US4 — Stable output and scripting (P2)

Goal: every command has frozen output/exit semantics and safe terminal rendering.
Independent check: success/failure schema validation and hostile bytes/corpus.

- [ ] T017 [US4] Implement output envelopes/typed events, fresh exit table, color and framework error routing in `apps/whr/internal/output/`, `apps/whr/internal/exitcode/`, `contracts/cli/output.schema.json`; cover every command/exit path and stream event, stderr/stdout purity, 0/1/255 remote passthrough and unsupported JSON refusal (FR-014–019/027; SC-003/012).
- [ ] T018 [US4] Complete exhaustive hostile-corpus, fuzz and command-integration acceptance of the human-text escaping and bounded stateful stream filter already implemented in T011/T014 in `apps/whr/internal/terminal/`; fuzz split/unterminated OSC/DCS/APC/PM/SOS, C1, bidi, arbitrary binary and raw per-session opt-in, cover every existing output/attachment path and prove JSON never emits raw bytes. Later agent/SSH WIs must reuse the accepted shared filter and verify their own integration before acceptance (FR-020–022a; SC-004).

## Phase 7: US5 — Agent interoperation and read-only dashboard (P2)

Goal: conforming in-container sessions without native-tool launch or dispatch.
Independent check: all five actions, lost send response and direct API scope negatives.

- [ ] T019 [US5] Finalize/review provider-reachable session transport and grant verification; implement contract validators, independent conformance fixture and scoped forwarding in `apps/host_agent/internal/agentsessions/`, `apps/control_plane/lib/workharbor/agent_sessions.ex`, `apps/whr/internal/commands/agent.go`, `tests/test_agent_session_contract.py`; test every contract shape/bound/grant/version and unknown delivery without resend, plus real in-container conformance without native-agent configuration (FR-029–031).
- [ ] T020 [US5] Implement immutable Git artifact projection and read-only pages in `apps/control_plane/lib/workharbor/spec_kit.ex`, `apps/control_plane/lib/workharbor_web/live/spec_kit_live.ex`; cover unavailable/unknown layouts, revision/evidence source, safe Markdown, every mutation endpoint called with read credential and hostile browser origins; record browser walkthrough (FR-032/033; SC-008/009).

## Phase 8: US6 — Isolated SSH (P3)

Goal: pinned workspace SSH without personal state or host shell access.
Independent check: key/cert failures, remote statuses and external config disclosure.

- [ ] T021 [US6] Finalize/review SSH-capable fixture, key/certificate provisioning and owner-authorized tunnel; implement `apps/host_agent/internal/ssh/`, `apps/control_plane/lib/workharbor/ssh_access.ex`, `apps/whr/internal/commands/ssh.go`; test pinned keys/expiry/scope, no personal config/agent, generated isolated config, filtering boundary and 0/1/255/lost-transport on real supported fixture (FR-008/008a/019).

## Phase 9: Pilot install and final acceptance

Goal: installed recoverable product and exact-head reviewable handoff.
Independent check: clean client/host install, real loaded journey, restore and local package/release-policy checks.

- [ ] T022 Implement quiesced backup/clean restore in `scripts/backup-workharbor.sh`, `scripts/restore-workharbor.sh`, `docs/development/backup-restore.md`; prove exact uncommitted hashes, owner/storage/operation associations, revoked identity rejection, unknown restored observations, no auto-start and failure to quiesce aborts (001 T019).
- [ ] T023 Implement local archive/install/uninstall, pinned toolchain instructions, checksums, SBOM, recorded build inputs/provenance and draft-only release machinery in `scripts/pilot/`, `.github/workflows/`, `docs/development/installable-pilot.md`; test clean installation/uninstallation, archive contents and release policy, including signed-tag/allowlist/main/check requirements and refusal to publish automatically. The owner-authorized allowlisted human-signed release exercise and SC-006 are a separate follow-up outside internal-pilot acceptance; unsigned local artifacts never satisfy SC-006 (FR-023–027).
- [ ] T024 Run every applicable internal-pilot story/criterion, including SC-001–005 and SC-007–012, at five-workspace/eight-attachment load on the authorized real host, record exact commands/versions/timings/failures in `specs/006-whr-cli/quickstart.md`, `specs/001-workspace-lifecycle/quickstart.md`, `docs/quality.md`; fix findings, run exact-head canonical/application gates, obtain independent Sol final review and open the combined PR to main for owner review (001 T010). SC-006 signed-release verification is excluded only from this internal-pilot handoff and remains a separately authorized follow-up; full Feature 006 completion remains open until SC-006 passes.

## Dependencies and execution

T001 -> T002 -> T003 -> T004 -> T005 -> T006 -> T007 -> T008 -> T009 ->
T010 -> T011 -> T012 -> T013 -> T014 -> T015 -> T016 -> T017 -> T018 ->
T019 -> T020 -> T021 -> T022 -> T023 -> T024.

This deliberately follows the owner's sequential WI/review route in one worktree;
file-disjoint parallelism is not used. Output/error plumbing needed by an earlier
WI is implemented minimally there and receives full matrix acceptance in T017,
not a passing claim before T017. FR-022 text escaping is mandatory from T011,
and the full baseline FR-022a stream filter is mandatory from T014; T018 expands
corpus/fuzz/integration evidence rather than introducing either safety behavior.
Feature 001 mappings above retain all original
acceptance and task completion rules; review evidence is appended, never fabricated.

## Evidence log

T001 and T002 are independently accepted at the revisions above. T003 is independently accepted at
`191c62baf227f64122770244d62d289900bb4d8d`. T004 is being implemented and
remains unchecked pending its independent review; no product runtime acceptance
is claimed. Durable check and review
evidence, including earlier failed runs, is retained in
[the pilot execution record](../../docs/exec-plans/active/installable-pilot.md).
