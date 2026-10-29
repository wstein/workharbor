# Feature 006 pilot work items

Delivery priority follows the [owner roadmap](plan.md#owner-delivery-order-2026-10-20):
validator/foundation → usable init → wizard/reuse/generic templates → existing 008
integration. Local lifecycle/drift support integration. Existing task dependencies,
scopes, current numeric schema validation and all approval gates remain unchanged.

Amendment H77 (2026-10-16/17) adds the first-increment WIs T025–T037 below; they are proposed, not approved or started,
and change no checkbox of T001–T024. The owner decided that the series runs on its own branches and pull requests; its
edits to the descriptions and dependencies of T011, T015, T016 and T017 take effect only with the owner's approval of
tasks.md from T009 onward at one exact head with a tracker or PR link (WORKFLOW.md), to be added when the specification pull request exists; Werner's chat answer "5approved" (2026-10-17) is recorded as decision H77 (5) and is not that approval. Until the spec, the plan and the whole `tasks.md` are approved at one exact head, neither T009 nor any of T025 to T037 may start on the strength of this amendment.
Status: execution in progress. T001 scaffold independently Sol CLEAR at
`d3d84c8361a1389292fad96d02a828ac9fabc2e3`; T002 is independently CLEAR at
`bc18034164597aa841bea561cf6a882e9c0715e2`; T003 independently CLEAR at
`191c62baf227f64122770244d62d289900bb4d8d`; T004 independently CLEAR at
`6abb7419c77b3712a6ac276bc798b102503d3144`; T005 and T006 are accepted at
`1189c81` and `ef399f5`. T007 is independently CLEAR at
`e10c6997c24407e0f472a7ea22ba2dd1b42067e3` for its pure protocol scope. S001 is independently CLEAR at
`c88f33959a4c0a767f638afaccc446a8040c55b4`. T008 implementation and bounded
macOS/Linux transport are independently Sol CLEAR at
`64541c1fa8b1f0d9f353272a0ea8ee6888ea5f5c`; the documentation handoff retains that runtime source binding.
Complete lifecycle/provider/browser/installable-pilot acceptance remains pending.
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
- [x] T005 Implement ownership, separate storage/runtime records, durable operations/canonical request hash, generation checks and audit in `apps/control_plane/lib/workharbor/` and migrations/tests in `apps/control_plane/`; prove owner/workspace key uniqueness, changed-payload conflicts and crash-safe intent-before-side-effect (001 T002).
- [x] T006 Implement owner bootstrap/login, revocable scoped credentials and browser request protections in `apps/control_plane/lib/workharbor/accounts.ex`, `apps/control_plane/lib/workharbor_web/` and tests; prove TLS-first auth, wrong owner, expired/revoked token, read-only dashboard scope, Host/Origin/anti-forgery rejection and secret redaction (001 T013; FR-035–038).
- [x] T007 Define/validate versioned host protocol in `contracts/host-protocol/`, `apps/control_plane/lib/workharbor/host_protocol.ex`, `apps/host_agent/internal/protocol/`; reject invalid version/scope/replay before provider calls and redact diagnostics in both suites (001 T003).

- [x] S001 Mitigate locked Bandit 1.12.5 HTTP/2 resource exhaustion advisory GHSA-69m6-wcv4-rwcf before T008: enforce the reviewed HTTP/1-only authenticated listener profile, refuse unsafe overrides, and prove actual ephemeral TLS protocol rejection plus HTTP/1 authentication and WSS success. Follow [the bounded transport contract](contracts/transport-security.md); retain the dependency advisory and require a released fixed version plus new independent review before re-enabling HTTP/2. No dependency update, proxy or T008 behavior belongs to this follow-up.

## Phase 3: US1 — Authenticate and manage workspaces (P1)

Goal: lifecycle and operation recovery shared by browser and CLI.
Independent check: trusted image/Dockerfile create, start/stop/rebuild/removal,
retained files and exactly one operation after lost responses.

- [x] T008 [US1] Implement one-time enrollment, mTLS identity/rotation/revocation and outbound heartbeat/reconnect in `apps/control_plane/lib/workharbor/hosts.ex`, `apps/control_plane/lib/workharbor/host_connections.ex`, `apps/host_agent/internal/identity/`, `apps/host_agent/internal/connection/`; exercise concurrency/expiry/scope and real handshake/reconnect; stale after 30 seconds (001 T004/T014).
- [ ] T009 [US1] Implement trusted config/submodule resolution, owner revision approval, official pinned Dev Container CLI and Docker storage/provider operations in `apps/host_agent/internal/config/`, `apps/host_agent/internal/providers/docker/`, `apps/control_plane/lib/workharbor/config_trust.ex`, `examples/workspace/` and `docs/development/devcontainer-matrix.md`; prove refusal/unsupported config has no hooks/provider mutation and real Docker retains files (001 T005/T016/T006).
- [ ] T010 [US1] Implement lifecycle reconciliation, bounded nonoverlapping retries and durable start/stop/rebuild/runtime-versus-data deletion in `apps/control_plane/lib/workharbor/operations.ex`, `apps/control_plane/lib/workharbor/workspaces.ex`, `apps/host_agent/internal/service/`, `apps/host_agent/internal/providers/docker/`; test interrupted provisioning, generations, failed rebuild, exhausted retries and exact uncommitted hashes on Docker (001 T007/T020; FR-003–005a).
- [ ] T011 [US1] Implement typed TLS owner client and `auth`, `workspace` (alias `ws`; short forms per ADR 0014) and `op` commands on the T025 foundation (command table, envelope, error line, exit table and FR-022 escaping already delivered and frozen; extend them for these commands) in `apps/api_client/` and `apps/whr/internal/commands/`, with shared human-text escaping in `apps/whr/internal/terminal/` before displaying any untrusted names, errors or remote messages; replace every FR-022 control class with visible placeholders in human output and escape them in JSON. Write targeted hostile-text tests before behavior, alongside first login, secret refusal, profile permissions, server identity, stable preallocated workspace/key, lost-response lookup and wait timeout/detach tests (FR-002–005a/022/035).
- [ ] T012 [US1] Implement enrollment/trust/workspace lifecycle browser views in `apps/control_plane/lib/workharbor_web/live/`; test and record real browser success/error/stale/retry/progress journeys with corresponding audit (001 T015/T017/T009).

## Phase 4: US2 — Shell and persistent console (P1)

Goal: scoped bounded access with session lifetime distinct from attachment lifetime.
Independent check: type/resize, detach/reattach, retained tail and no host access.

- [ ] T013 [US2] Implement scoped atomic grants, bounded relay, fresh shell and one persistent console per workspace in `apps/control_plane/lib/workharbor/sessions.ex`, `apps/control_plane/lib/workharbor_web/channels/`, `apps/host_agent/internal/terminal/`; test all Feature 001 bounds, 64 KiB tail/drop reporting, concurrent single use, endpoint restart replay and <=5s cleanup; stop/rebuild/delete/close end session (001 T008; FR-006–007a).
- [ ] T014 [US2] Implement `open`, `shell`, `console` and browser terminal in `apps/whr/internal/commands/`, `apps/control_plane/lib/workharbor_web/live/terminal_live.ex`, `apps/control_plane/assets/js/terminal.js`; implement the complete baseline FR-022a bounded stateful stream filter in `apps/whr/internal/terminal/` before accepting any CLI attachment. Filter OSC/DCS/APC/PM/SOS by default, including split-frame and unterminated strings; require explicit per-session raw opt-in displayed at session start and never persisted, and never emit raw terminal bytes in JSON. Write targeted hostile-stream, split-frame, unterminated-string, bounded-state and raw-opt-in/reset tests before behavior; verify input/resize, fresh-grant reconnect, status/close, foreign scope, remote 0–255 and real browser/PTY evidence (001 T018; FR-022/022a).

## Phase 5: US3 — Host operations (P2)

Goal: resumable local administration with truthful state and revocation.
Independent check: dry-run no writes, interrupted setup/offboard resumes safely.

- [ ] T015 [US3] Implement typed Linux/systemd change plans and atomic checkpoints for `setup` (including `--dry-run`, deferred from the first increment), `service` and `host offboard` (short `offboard`) in `apps/whr/internal/host/`; test unsupported OS/privilege, enrollment failure, each interrupted step, confirmed revocation before deletion, local-only nonzero and default data retention; exercise a disposable real Linux host (FR-009/011–013). T015 stays Linux-only; the macOS route of `setup` is [Feature 009](../009-host-setup-wizard/spec.md).
- [ ] T016 [US3] Extend T027's local `doctor` with the control-plane, host-identity, credential-permission and service checks and the shareable redacted report in `apps/whr/internal/doctor/`; test pass/fail/skipped/unknown, absent tools, private-file permissions and secret redaction; verify real host findings are measured (FR-010/021).

## Phase 6: US4 — Stable output and scripting (P2)

Goal: every command has frozen output/exit semantics and safe terminal rendering.
Independent check: success/failure schema validation and hostile bytes/corpus.

- [ ] T017 [US4] Complete output envelopes/typed events for every command on the T025 foundation (envelope, fresh exit table, color and error routing already exist for the first-increment commands) in `apps/whr/internal/output/`, `apps/whr/internal/exitcode/`, `contracts/cli/output.schema.json`; cover every command/exit path and stream event, stderr/stdout purity, 0/1/255 remote passthrough and unsupported JSON refusal (FR-014–019/027; SC-003/012).
- [ ] T018 [US4] Complete exhaustive hostile-corpus, fuzz and command-integration acceptance of the human-text escaping and bounded stateful stream filter already implemented in T011/T014 in `apps/whr/internal/terminal/`; fuzz split/unterminated OSC/DCS/APC/PM/SOS, C1, bidi, arbitrary binary and raw per-session opt-in, cover every existing output/attachment path and prove JSON never emits raw bytes. Later agent/SSH WIs must reuse the accepted shared filter and verify their own integration before acceptance (FR-020–022a; SC-004).

## Phase 7: US5 — Agent interoperation and read-only dashboard (P2)

Goal: conforming in-container sessions without native-tool launch or dispatch.
Independent check: all five actions, lost send response and direct API scope negatives.

- [ ] T019 [US5] Finalize/review provider-reachable session transport and grant verification; implement contract validators, independent conformance fixture and scoped forwarding in `apps/host_agent/internal/agentsessions/`, `apps/control_plane/lib/workharbor/agent_sessions.ex`, `apps/whr/internal/commands/agent.go`, `tests/test_agent_session_contract.py`; test every contract shape/bound/grant/version and unknown delivery without resend, plus real in-container conformance without native-agent configuration (FR-029–031).
- [ ] T020 [US5] Implement immutable Git artifact projection and read-only pages in `apps/control_plane/lib/workharbor/spec_kit.ex`, `apps/control_plane/lib/workharbor_web/live/spec_kit_live.ex`; cover unavailable/unknown layouts, revision/evidence source, safe Markdown, every mutation endpoint called with read credential and hostile browser origins; record browser walkthrough (FR-032/033; SC-008/009).
- [ ] T041 [US5] Host-agent `spec_kit.read` command (PROPOSED; owner decisions O1/F8 of 2026-10-27; no checkbox, prerequisite or acceptance of T007, T019 or T020 changes): add the reviewed, versioned, read-only host-protocol request/result forms, shared fixtures, both validators and a negotiated capability to `contracts/host-protocol/`, `apps/control_plane/lib/workharbor/host_protocol.ex`, `apps/control_plane/lib/workharbor/host_connections/protocol.ex`, `apps/control_plane/lib/workharbor/host_connections.ex`, `apps/control_plane/lib/workharbor_web/agent_socket.ex`, `apps/control_plane/lib/workharbor_web/agent_ingress.ex`, `apps/control_plane/lib/workharbor/spec_kit.ex`, `apps/host_agent/internal/protocol/`, `apps/host_agent/internal/connection/`; the host reads only immutable commit blobs of the workspace's own clone and returns a bounded, correlated, explicit-complete-or-unavailable response, and `SpecKit.project/2` consumes it. Contract: [read-only precursors](contracts/read-only-web-precursors.md#t041-spec_kitread-host-command-contract). Failing tests first, named in that section (`request_overrides_refused`, `foreign_workspace_not_found`, `wrong_peer_refused`, `bounds_at_n_and_n_plus_one` and the rest). Out of scope: rendering available artifacts (T020), agent sessions (T019), any write, fetch or lifecycle path.

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

## First increment (decisions H77 (1)-(6), 2026-10-16/17): local `whr` commands

Status: proposed with the spec amendment; no work item below is approved for execution until an independent review is
CLEAR for the exact head of the amended spec, plan and tasks and the owner explicitly approves that head with a tracker
or PR link (Werner's chat decisions of 2026-10-16/17 are decision H77, not approval of this text). The initialization
revision also needs the owner's human review of the plan, task scope, configuration schema and launcher contracts; this
amendment is the input to that review. See [spec](spec.md#owner-decisions-2026-10-16-h77) and
[ADR 0014](../../docs/adr/0014-whr-command-structure.md).

Goal: a `whr` binary that needs no control plane, login or Docker daemon and offers, in wave 1a, `version`, `completion`,
`help`, `validate`, local `doctor` and the non-interactive core of `workspace init`, plus a pure resource-parsing library;
in wave 1b the interactive wizard and devcontainer reuse. Wave 2 (not here): `workspace create` wiring and persistence
with the owner API, and the starter catalog after provider admission. Blocked on the owner API and not here: workspace
lifecycle, `auth`, `op`, `shell`/`console`/`ssh`/`open`, `agent`, `host ls|show`, `setup`, `service`, `offboard`
(router verified 2026-10-16: only `/api/v1/hosts` and `/api/v1/auth`; `auth` and `host show` wait for the typed TLS owner client T011, not for endpoints). `setup --dry-run` is excluded (FR-061).
Independent check: each WI's failing-first tests, `make check-apps` and `make check`, the hostile corpus (T026, T030)
and the T034 real-host runs.

Dependency graph (each WI runs in its own worktree, branch and pull request to `main`; the merge order follows the graph):
T025 -> {T026, T027, T028, T031}; {T026, T028} -> T030 (T027 is not a prerequisite); T030 -> {T032, T033} (T032 also needs T031); T029 has no
dependency; T035 needs T009 and T028 and is outside the waves; T037 needs T030 and T036; T036 needs T030 and T032 (T032 owns the per-path confirmation; T036 and T037 are not prerequisites of T034); T034 needs T025-T033. Wave 1a = T025-T030; wave 1b = T031-T033, T036 (after T030 and T032) and T037 (after T030 and T036). The only expected merge points are the
`internal/commands` registration, `docs/development/ci-acceptance.json` and `apps/whr/go.mod`; `contracts/workspace/vectors.json` has the single owner T028, and T026 and T027 touch disjoint packages but share the `internal/commands` registration merge point; later branches rebase.

Common rules: each WI is its own task and commit series (Conventional Commits), written test first with the
absent-implementation RED recorded in the workpad, appends its required test IDs to `docs/development/ci-acceptance.json`
without changing earlier IDs, records its candidate and reviewed revision, and needs independent Sol review CLEAR for its
exact head before a dependent WI starts. One pull request per WI, draft until review is CLEAR for the exact head and checks
are green (WORKFLOW.md). Each WI's issue lists exact regular-file paths (the single-task guard accepts no directory scope
and only `T###` ids, and refuses `spec.md`, `plan.md` and `tasks.md`): the `apps/whr/go.mod` and `go.sum` entries,
`go.work.sum` if Cobra changes it, any dependency notice following the `coder/websocket` precedent, and the new source and
test files; hostile fixtures that need symbolic links or special files are generated in `t.TempDir`, never committed.
Allowed paths stay inside `apps/whr/`, `contracts/`, `docs/`, `scripts/check_repo.py`, `tests/` and this feature directory
minus the three guarded files; no change to `apps/host_agent` (except T035, whose scope needs its own owner approval), `apps/control_plane`, `apps/api_client` or the root
`README.md`. Spec status and reconciliation records go to the workpad or to a separate owner-reviewed docs commit. Builds are
the repository's ordinary `go build`/application checks; no archive, image, release, tag or publication is part of any WI.

- [ ] T025 [US4] Foundation in `apps/whr/internal/{commands,exitcode,output,terminal,version}/`, `apps/whr/cmd/whr/` and `go.mod` (Cobra 1.10.2/pflag, pinned, plus a test-only JSON Schema validator pinned for output conformance; `apps/whr` reaches `apps/api_client` through the root `go.work` only, as `host_agent` does): the command table mirrored from `contracts/cli/commands.json` with generated short forms and the `ws` alias, unavailable-command handler, `help` and `completion` for bash, zsh and fish, `--json` envelope, `whr:` error line, color, the ADR 0013 table and error mapping, FR-022 text escaping and `version`/`--version`. Failing tests first: the five `exit-codes.json` freeze IDs of ADR 0013 (`TestExitTableMatchesContract`, `TestExitTableCodesUniqueAndInRange`, `TestEveryExitPathMapsToCode` for the paths that exist (partial: SC-012 stays open until T017, so a green ID is not SC-012 evidence), `TestRemoteStatusPassthrough0_1_255` as table-level only, `TestCLIFailureAnnouncedOnStderrAndKindCLI`), the seven table IDs of `commands.json` (`TestCommandTableMatchesContract`, `TestCommandTableAliasRules`, `TestAliasInvocationEquivalent`, `TestJSONCommandFieldIsCanonical`, `TestHelpShowsShortFormHint`, `TestCompletionKnowsBothForms`, `TestLaterCommandsRefuseWithoutBackend`), `TestEnvelopeMatchesOutputSchema` (success, error and unsupported-`--json` cases), `TestJSONStdoutPurity`, `TestColorMatrix`, `TestEscapeFR022Corpus` (every control class, human and JSON) and `TestVersionFields`, `TestUnknownCommandHint`, `TestInitFlagsMatchContract` (flags of `workspace.init` in `commands.json`; the command itself is unavailable until T030). Acceptance: a mutation of any table entry, alias or exit code fails a named test; stdout carries only data, one envelope in `--json`; evidence is the test list, the RED log and the exact-head review (FR-014–019, 022, 027, 051–053; SC-003, SC-016; ADR 0013, 0014).
- [ ] T026 [US7] `whr validate` in `contracts/workspace/vectors.json` (authored with the amendment and owned by T028; read here), `contracts/workspace/git-vectors.json` (new, owned here: gitmodules and index cases), `apps/whr/internal/workspacecfg/`, `apps/whr/internal/commands/`: manifest, devcontainer pre-flight (bounded strict JSONC), projects, Git-state and submodule checks, safe no-follow file access and the hardened read-only Git invocation, `--json` result, exit codes of FR-063. Failing tests first: vector conformance (`TestManifestVectors` over all cases of `vectors.json`, version 1 and 2, and the example/fixture manifests), `TestValidateHostileCorpus` (symlink at every path position, FIFO, sparse 10 GiB file, invalid UTF-8, BOM, NUL, duplicate keys, depth bomb, 100,000-entry `.gitmodules` and index, credential URLs, `file:`/`ext::`/relative URLs, `update = !cmd`), `TestValidateExecutesNothing` and `TestValidateIsReadOnly` (real Git against fsmonitor, hook, filter, alias and update-command repositories; marker file absent; before/after tree and timestamp comparison including `.git`), `TestValidateNoNetwork`, `TestValidateExitCodes`, `TestValidateJSONMatchesSchema`, `TestValidateInterrupted`, `TestValidateManifestV2Settings` (project declaration, `project.declared_not_pinned`), `TestValidateDevcontainerWarnings`, `TestValidateSubmoduleRules` (pinned info, unlisted, outside projects, not pinned, not committed, unsupported key, scp-like and credential URLs), `TestValidateGitUnsafePath` (symlinked and gitfile `.git`, and a FIFO, socket or directory as `.git/HEAD`, `.git/index` or `.git/config`, found by `lstat` before any Git call), `TestValidatorGitFreeProfileInMemory` (an exported checker entry point that validates in-memory content in the Git-free profile, consumed by T030), `TestValidateOldGit`, `TestValidateTimeBudget` (injected clock: `findings.time_budget_exhausted` outside Git work, `git.listing_failed` during Git work), `TestValidateGitProtocolOverridesIgnored` (partial clone, `protocol.file.allow=always`, `uploadpack` marker), `TestValidateGitInternalsUnsafe` (`commondir`, `alternates`), `TestValidateGitUnavailableIsError`, `TestFindingCatalogueMatchesContract` and `TestFindingCodesMatchOutputSchemaPattern` (against `contracts/cli/validate-findings.json`), `TestValidateUnbornHeadStagedSubmodule` (a freshly initialised repository with an unborn HEAD and a staged submodule), `TestValidateGitFailureIsError` (a FIFO loose ref or `packed-refs`, unparsable output, Git deadline), `TestGitmodulesResolverParity` (committed-blob vectors: unsupported keys, duplicate fields, nesting, mode). Network denial is proved by an injected dialer that fails the test on any use, plus a real run under `unshare -n` on Linux; device-node cases and the 5 s / 256 MiB bound run on a real host only. Acceptance: every acceptance scenario of US7; the independent reviewer reruns the hostile corpus on a fresh checkout; residual Git risk stated in the evidence (FR-054/055/062/063; SC-017, SC-020). Depends on T025; parallel with T027 and T028.
- [ ] T027 [US8] `whr doctor` local checks in `apps/whr/internal/doctor/` and its registration in `apps/whr/internal/commands/` (consuming a validator port; it does not edit `workspacecfg`), mirroring `contracts/cli/doctor-checks.json`: platform, Docker-compatible socket discovery (connect only), Git version, disk space, `config.valid`, skipped control-plane and host checks, verdict, redaction. Failing tests first: `TestDoctorChecksMatchContract`, `TestDoctorNeverPassesUnmeasured`, `TestDoctorLaterChecksReportSkipped`, `TestDoctorSocketCandidates` (each candidate, tcp `DOCKER_HOST` unknown, denied, refused), `TestDoctorGitVersion` (absent, old, equal, new), `TestDoctorDiskThresholds`, `TestDoctorTimeBudget` (injected clock: `config.valid` uses the remainder and reports `unknown` on exhaustion; until T034 wires the real validator it reports `skipped` with `not_in_increment`), `TestDoctorConfigValidUsesValidatorPort` (the `config.valid` check calls a validator port; `TestDoctorConfigValidEqualsValidate` with the real validator is owned by T034 only), `TestDoctorRedaction` (canary home, user, host and environment values), `TestDoctorReadOnlyNoNetwork` (the checks of FR-060; the host checks of Feature 009 are outside this task), `TestDoctorExitCodes`, `TestDoctorCustomDataRoot`, `TestDoctorMacGitShim`, `TestDoctorPlatformUnknown` (and table-level `TestMutatingHostCommandsRefuseUnsupportedPlatform`, since those commands are unavailable until T015; it treats macOS `setup` as supported by [Feature 009](../009-host-setup-wizard/spec.md) and still refuses macOS `service` and `offboard`), `TestDoctorJSONMatchesSchema`; adds `docs/container-runtime.md` link verification. Acceptance: US8 scenarios on injected ports plus real runs on a Linux Docker host and macOS with OrbStack with exact versions recorded (FR-010/060/062/063; SC-007, SC-019). Depends on T025; parallel with T026 and T028.
- [x] T028 [US0] Manifest-schema extension (wave 1a; decision of 2026-10-17) in `contracts/workspace/workharbor.schema.json` and `contracts/workspace/vectors.json` (already authored with the amendment, extended here), `scripts/check_repo.py`, `tests/test_workspace_manifest_vectors.py` and `docs/development/ci-acceptance.json`: enforce version 1 (exactly four fields) and the proposed version 2 (optional `name`, `start_dir`, `human_shell`, `console_mode`, `shortcuts`, `project`; defaults fish, herdr, on, `.`) in the repository check, reading the shared vectors. Failing tests first: `test_workspace_manifest_vectors` (every case of `vectors.json`, including raw-byte and size cases), `test_example_manifests_still_valid`, `test_version1_rejects_version2_fields`, `test_version2_defaults_documented`. Records, without changing it, the host-agent resolver requirement (work item T035) (`fields(manifest,...)` rejects version 2; a separate reviewed WI sequenced with T009 must accept it; until then version 2 is fail-closed and not creation-ready). No change to `apps/host_agent`. Acceptance: Python checks green with the new vectors, adding only cases `check_repo.py` passes; whichever of T026 and T028 merges second runs every case in CI (type-exact `schema_version`: `true`, `1.0`, `1e0` are rejected); the `check_repo.py` rules and the vectors agree on every case (FR-058; SC-018 inputs). Depends on T025 for ordering only; parallel with T026 and T027.
- [x] T029 [US1a] Resource parsing library (wave 1a) in `apps/whr/internal/resources/` and `contracts/cli/resources-vectors.json` (authored with the amendment, extended here): CPU thousandths and memory bytes parsing, canonicalization, rejection classes and per-field resolution per FR-059. No command, flag, prompt or output wiring. Failing tests first: `TestCPUVectors`, `TestMemoryVectors`, `TestResolutionVectors`, `TestEquivalentSpellingsShareIdentity`, `TestNoUnlimitedOnOmission`, `FuzzParseCPU`, `FuzzParseMemory` (no panic, round-trip of accepted values, no accepted value above 2^63-1). Acceptance: all vector cases pass offline; the vector file carries enough information for an Elixir implementation to be tested against it later (FR-039/040, FR-059; SC-021). Independent of the other WIs (needs no foundation code).
- [ ] T030 [US0] `whr workspace init` core D1 (wave 1a) in `apps/whr/internal/scaffold/` and `apps/whr/internal/commands/`: proposal model (create, overwrite, reuse, skip_existing), preview with contents or diff, `--dry-run`, `--non-interactive`, `--yes`, `--overwrite <path>`, `--name`, `--project-url`, `--start-dir`, `--human-shell`, `--console-mode`, `--shortcuts`, preserved existing settings and defaults, change-since-preview hashes, validation with T026's checker in its Git-free profile (in memory before, on disk after; `git_checks: not_run`), valid existing v1/v2 manifests handled by ordinary file preservation/proposal/edit consent (no edit preserves bytes; requested edits preview the current authoring format v2 and need per-path overwrite consent), mandatory not-creation-ready warning for a v2 proposal or written replacement in preview and result until T035/FR-058, no automatic version bump, special v1 workflow or not-persisted output, exclusive no-follow creation, temp-file-and-rename overwrite, identity-checked restore-or-report-partial rollback, no-op rerun, reuse of an existing devcontainer, `--template` refusal while starters are unsupported, no Git, no network. Failing tests first: `TestInitProposalWizardFlagParity` (flags only in this wave; the proposal model is the shared contract for T032), `TestInitCreatesExpectedManifest`, `TestInitOutputValidates`, `TestInitPreservesExistingSettings`, `TestInitRerunIsNoOp`, `TestInitNeverOverwritesWithoutOverwriteFlag`, `TestInitOverwriteRequiresYesAndPath`, `TestInitFileChangedSincePreview`, `TestInitSymlinkTarget`, `TestInitExistingDirectoriesReused`, `TestInitDryRunWritesAndRunsNothing`, `TestInitFailureInjectionRollback` (failure and SIGINT/SIGTERM after every step, full disk, path appearing after preview), `TestInitRollbackIncompleteExits73`, `TestInitMissingInputNeedsHuman` (flag names listed, no devcontainer), `TestInitProjectURLRules` (userinfo, scheme, derived path), `TestInitNoGitNoNetwork`, `TestInitTemplateRefused`, `TestInitModes` (wave 1a: without `--non-interactive` and without `--dry-run`, on a terminal or not, exit 64 "not in this release"; `--non-interactive` without `--yes` exit 70 naming `--yes`), `TestInitJSONMatchesSchema`, `TestInitExitCodes`, `TestInitProjectDeclaredNotPinnedWarning`, `TestInitExistingManifestPreservedWithoutRequestedEdit`, `TestInitManifestEditsNeedOverwrite`, `TestInitVersion2NotCreationReadyWarning` (preview and result for new v2 and consented replacement until T035/FR-058), `TestInitNonInteractiveWithoutYes`, `TestInitDevcontainerFlagNotInRelease`, `TestInitIdenticalFileSkipExisting`, `TestInitGitkeepUnderProjectsPath`, `TestInitBundlesLeftUnchanged`, `TestInitOverwriteTempFilePrefixDetected`, `TestInitExitCodeExistingFileVsReplaced`. Acceptance: US0 scenarios 4, 5, 8 (the declaration and the warning only; pinning is evidenced at creation-time resolution by later WIs) and 9 and the safety matrix of SC-018 for the non-interactive core (SC-014 partially, without wizard, shell and console runtime parts); the reviewer reruns the failure-injection matrix (FR-043 to FR-046, FR-049 to FR-050, FR-056, FR-057, FR-062, FR-063; SC-014, SC-018). Depends on T026 and T028; T027 is not a prerequisite. Branches of dependents start from the merged `main` containing their prerequisites.

  Status reconciliation (2026-10-24): T028 and T029 are checked after acceptance
  of their [schema evidence](../../docs/quality/006-whr-cli/t028-manifest-schema-20261024.md)
  and [resource evidence](../../docs/quality/006-whr-cli/t029-resources-20261024.md)
  in merged PRs #238/#239. T030 remains open: its [core evidence record](../../docs/quality/006-whr-cli/t030-init-core-20261024.md)
  lists remaining cancellation/signal, physical ENOSPC and network-isolation
  evidence and missing historical RED proof; passing source tests do not close SC-018.

  Proposed PATH-contract boundary (FR-046a, SC-014a): T030 and T033 reuse the devcontainer and all existing startup files byte-for-byte, including custom JSONC, PATH and shell settings. Add failing-first `TestInitExistingDevcontainerPathPreserved` and `TestInitExistingShellSettingsPreserved`; any policy guidance uses existing next-step output and grants no activation. The future generation/editing acceptance matrix of SC-014a is deferred to a separately owner-approved task, not implemented by T030/T033. Do not mark these tasks complete or infer approval from this amendment.
- [ ] T031 [US0] Terminal test harness (wave 1b, its own task) in `apps/whr/internal/ptytest/`: a pseudo-terminal driver for end-to-end tests of interactive commands (raw mode, line editing, window size, scripted keystrokes, interrupt, transcript capture with timeouts, no network), reusable by later interactive commands. Failing tests first: `TestPTYDrivesPromptAndCapturesTranscript`, `TestPTYInterruptDelivered`, `TestPTYTimeoutFailsTest`, `TestPTYNoLeakedProcess`, `TestPTYUnsupportedPlatformSkipsExplicitly` (documented platforms Linux and macOS; skipping is reported, never a pass claim). Acceptance: harness tests green on Linux and macOS with exact versions recorded (SC-007). Depends on T025.
- [ ] T032 [US0] Interactive wizard D2 (wave 1b) in `apps/whr/internal/wizard/`: prompts on stderr for name, optional project URL, starting directory, existing devcontainer, human shell, console mode and shortcuts with back, review (proposal preview) and cancel over the T030 proposal model, including the step that offers an existing devcontainer file found by T030's discovery function; terminal detection; `--non-interactive`, `--json` and non-terminals never prompt; fish, herdr and shortcuts-on defaults only for missing settings; cancel changes nothing and exits 72; it asks per path for every existing file it would edit (FR-046; a declined path stays a cancellation, 72, as FR-063 says) tested by `TestWizardPerPathEditConfirmation`; T036 reuses the step for `AGENTS.md` only, where (FR-064) declining leaves the file unchanged with its warning, an edit without the flag that would be refused is not offered (warning printed) and an edit pre-confirmed by `--overwrite AGENTS.md` that would be refused is 68; `.claude/settings.json` (FR-065) is likewise pre-confirmed by its flag and otherwise not offered. Failing tests first: `TestWizardScriptedAnswersMatchFlags` (byte-identical files), `TestWizardBackAndReview`, `TestWizardCancelChangesNothing`, `TestWizardExistingSettingsOnlyMissingPrompted`, `TestWizardNeverPromptsWhenNonTerminalOrJSON`, `TestWizardPromptsOnStderrOnly`, and PTY tests with T031: `TestWizardPTYHappyPath`, `TestWizardPTYInterruptRollsBack`. Acceptance: US0 scenarios 1 to 3 (starters excluded), real PTY runs on Linux and macOS (FR-044, FR-045; SC-014, SC-018). Depends on T030 and T031.
- [ ] T033 [US0] Devcontainer reuse D3 (wave 1b) in `apps/whr/internal/scaffold/` and `apps/whr/internal/commands/` (the wizard step is in T032): `--devcontainer <relative-path>` selects an existing devcontainer file (validated, never modified, `devcontainer_path` updated in the manifest) and the starter catalog is displayed as unsupported (`contracts/workspace/starters.json`) with `--template` still refused. Failing tests first: `TestInitDevcontainerSelectionValidated`, `TestInitDevcontainerNeverModified`, `TestInitDevcontainerPathTraversalRefused`, `TestInitDevcontainerSymlinkRefused`, `TestStartersMatchContract`, `TestTemplateRefusedWhileUnsupported`. Acceptance: US0 scenario 2 and FR-049 reuse by default; no template is generated or executed (SC-014). Depends on T030; parallel with T031 and T032.
- [ ] T034 [US4, US7, US8, US0] First-increment acceptance and handoff (after T025 to T033): command documentation for the available commands, short-form table and exit codes in `docs/`, completion scripts tested in real bash, zsh and fish, offline run with no profile, credential or Docker daemon (SC-020), unavailable-command run, `TestDoctorConfigValidEqualsValidate` with the real validator (T034 also owns the one-file wiring of doctor's validator port to T026's checker in `apps/whr/internal/commands/`), and real evidence of the built binary on the owner's macOS with OrbStack and on Linux recorded with exact versions and commands in `specs/006-whr-cli/quickstart.md`; updates `docs/quality.md` honestly (no verified-platform claim beyond the runs); spec status and checklist changes are a separate owner-reviewed docs commit. Failing tests first: `TestFirstIncrementOffline` (network denied by an injected dialer and a Linux `unshare -n` run), `TestFirstIncrementEndToEnd` (`validate`, `doctor`, `init` flags then `validate` in one temp tree on the built binary). Acceptance: SC-016 to SC-021 satisfied or exact gaps listed; exact-head `make check` and application gates; independent Sol final review; no release, tag, archive, package-channel or deployment authorization is implied (SC-007).

- [ ] T035 [US0] Resolver acceptance of manifest version 2 (after T009 and T028; its scope needs its own owner approval because it touches accepted provider inputs) in `apps/host_agent/internal/config/resolver.go`, `apps/host_agent/internal/config/resolver_test.go` and a vectors test in `apps/host_agent/internal/config/`: accept `schema_version` 1 with exactly four fields and 2 with the four plus the optional fields of `contracts/workspace/workharbor.schema.json`, type-exact version comparison, reuse of `CanonicalPath` for `start_dir` and `project.path`, and run every case of `contracts/workspace/vectors.json`. Failing tests first: `TestResolverManifestVectors`, `TestResolverVersion1StillExactFields`, `TestResolverVersion2Accepted`, `TestResolverRejectsVersionTrueAndFloat`, `TestResolverIgnoresRuntimeSettings` (the settings are read by no resolver output). Acceptance: all vector cases agree with the whr checker and `check_repo.py`; no provider behavior changes; independent Sol review (FR-058).

- [x] T036 [US0] Agent workflow block (FR-064, SC-022; wave 1b, after T030 and T032): the versioned template in `apps/whr/internal/scaffold/`, marker parsing and the current/different-template/modified classification and action mapping of FR-064 (numeric markers retained; no older/newer preservation promise) with the exit codes of FR-063, `--no-agents-block` (already registered in `contracts/cli/commands.json` and the command table, so the FR-051 freeze tests stay green), the new actions `append_block`, `replace_block`, `left_unchanged` and the `agents_block_warnings` array in `contracts/cli/output.schema.json` (`initResult` is closed), a unit test that the template body contains no marker-like line, and the table test of SC-022. FR-064 is on by default, so it changes the D1 (T030) proposal output and the no-op-rerun baseline: T036 updates the T030 and T032 tests and vectors accordingly. Needs the spec review of the 2026-10-17 revision first; no previous approval covers it. Source and unit tests only: no container, network or Git command.

- [x] T037 [US0] Claude Code settings default (FR-065, SC-023; wave 1b, after T030 and T036): the authored `.claude/settings.json` (`{"attribution": {"sessionUrl": false}}`, 2-space indent, trailing newline) in the `apps/whr/internal/scaffold/` proposal, an existing file left unchanged (`left_unchanged`, exit 0) unless `--overwrite .claude/settings.json` (also for non-regular targets and a non-directory `.claude`, with one `left_unchanged` row, a `next_steps` entry and an output line), the `.claude` create or reuse action, the exit codes of FR-063, no new flag and no schema change (it reuses the actions of T036), the table test of SC-023, and updates of the T030, T032 and T036 tests and vectors for the new default proposal and the no-op-rerun baseline. Needs the spec review first. Source and unit tests only: no container, network or Git command.

- [ ] T038 [US1, US4] Local devcontainer and temporary container lifecycle with volume protection (FR-066, FR-067, FR-068, SC-024; #142) in `apps/whr/internal/localdocker/`, `apps/whr/internal/commands/`, `contracts/cli/commands.json`, and `contracts/cli/output.schema.json`: `--local` and `--label` on `workspace.ls`; `--local`, `--label`, `--role`, `--volumes`, `--force`, `--dry-run`, and `--yes` on `workspace.rm`; bounded 5 s Docker socket inspection; pre-mutation ownership verification (exit 68 on foreign container); default preservation of home volume; deletion of home volume requiring both `--volumes` and `--force` (and `--yes` unattended, exit 70 without it); refusal of staging/temp cleanup resolving to primary home volume (exit 68); refusal of volume deletion if mounted by another container (exit 68); protection of `/opt/bdock/*` and `bundledock-*` tool volumes; side-by-side staging and promotion; `--dry-run` zero mutations; partial failure reporting (exit 73). Failing tests first: `TestLocalLsListsLabelledContainersAndVolumes`, `TestLocalLsLabelFilter`, `TestLocalLsEscapesHostileMetadata`, `TestLocalRmContainerOnlyPreservesHomeVolume`, `TestLocalRmWithVolumesAndForceRemovesHomeVolume`, `TestLocalRmVolumesWithoutForceRefused`, `TestLocalRmNeverRemovesBundleDockOrReadOnlyVolumes`, `TestLocalRmRefusesForeignUnlabelledContainer`, `TestLocalRmRefusesDeletingPrimaryHomeVolumeFromStagingOrTemp`, `TestLocalRmRefusesVolumeMountedByOtherContainer`, `TestLocalRmDryRunExecutesNoMutations`, `TestLocalRmNonInteractiveRequiresYes`, `TestLocalRmPartialVolumeFailureExits73`, `TestLocalCommandsJSONMatchesSchema`, `TestWorkspaceLsAndRmWithoutLocalStillRefuse`. Acceptance: all SC-024 cases pass offline with fake Docker fixtures; `python3 scripts/check_repo.py` and Go tests pass; independent exact-head review is CLEAR. Prerequisite: T025.

T038 disposal amendment (owner, 2026-10-25): new whr-managed candidates are
throwaway. The [separate amendment](contracts/throwaway-staging-amendment-20261025.md)
supersedes old retained-clone and extra candidate-home consent expectations;
other homes and all Anchorage/primary guards stay unchanged. Implement the
[plan's disposal packages](plan.md#throwaway-staging-delivery-amendment-owner-2026-10-25)
with red-first zero-artifact remove/promote, failed-stage and rollback tests,
result refs verified before promote clone deletion, normal confirmation,
dry-run/refusal-reason coverage and exact residual reporting. Keep this task
open until its delivered code and acceptance evidence cover the new scope;
no checkbox is changed by this amendment.

T038 stage/promote amendment (2026-10-24, proposed): follow
[the selected local interaction](contracts/local-stage-promote.md) and
[plan packages S0–S4](plan.md#t038-stagepromote-amendment-2026-10-24). D1/D2 direction
follows [LS-1 and its linked owner decision record](spec.md#ls-1-owner-direction-2026-10-24);
artifact approval is the owner approval/merge record of [PR #229](https://github.com/wstein/workharbor/pull/229),
covering interaction only, with S0 still gated afterwards. Before
stage/promote product code, complete S0's capability/capture/argv/image/receipt/
cleanup and output contract with independent review and owner approval. Additional
red-first IDs and SC-024 stage/promote coverage are enumerated there. Preserve the
existing FR-067 guard and all removal regressions. Current protected Anchorage
staging/temp cleanup remains excluded; a FR-067(e) change is a separate decision
and task. Fresh candidate-home naming is decided in the
[candidate-home decision](contracts/candidate-home-cleanup-decision.md). The
[2026-10-25 throwaway amendment](contracts/throwaway-staging-amendment-20261025.md)
supersedes separate deletion flags for new whr-managed candidates. This note neither
checks T038 nor changes T025/T038/T039 dependencies, other tasks or prior evidence.

The [candidate-home decision](contracts/candidate-home-cleanup-decision.md) is
DECIDED (owner, 2026-10-25). The
[cleanup task proposal](contracts/candidate-home-cleanup-task-proposal.md) tracks
implementation units reconciled with that decision. This documentation update
adds no task or acceptance evidence and changes no dependencies or checkmarks.

- [x] T039 [US1, US4] Bundle mount drift reporting (FR-069, SC-025; #143) in `apps/whr/internal/localdocker/`, `apps/whr/internal/commands/`, and `contracts/cli/output.schema.json`: read-only comparison of pinned bundle references in `.devcontainer/bundle-pins.json` or devcontainer configuration against mounted image/volume references on running container; per-bundle reporting of pinned, mounted, and status (`in_sync`, `drift`, `cannot_determine`); exit code 0 when all in sync, and exit 68 for drift or unreadable/undetermined results, distinguished by report status (owner decision 2026-10-24); `--json` output conforming to `bundleDriftResult`; zero mutations verified. Failing tests first: `TestBundleDriftAllInSync`, `TestBundleDriftDetected`, `TestBundleDriftMissingMount`, `TestBundleDriftUnreadablePins`, `TestBundleDriftJSONMatchesSchema`, `TestBundleDriftZeroMutations`. Acceptance: all SC-025 cases pass offline with fake Docker fixtures; independent review is CLEAR. Prerequisites: T025, T038.

## Dependencies and execution

The first-increment WIs T025 to T034, T036, T037, T038 and T039 follow the dependency graph above and depend only on T001–T004 (accepted) and on no
control-plane WI; T035 additionally needs T009 and T028; T011 additionally depends on T025, T016 on T027 and T017 on T025. T005–T010 are unaffected. By owner decision
(2026-10-16/17) they run on their own branches and pull requests outside the chain below. Werner's chat answer "5approved" (2026-10-17) is recorded as decision H77 (5); it is not approval of any head's text, and the edits take effect only with approval at one exact head with a tracker or PR link; until then the chain below is unchanged and authoritative.

Machine-readable dependency table (the single-task guard requires exactly one, sorted, covering every task; T001 to T024 follow the chain below, T025 to T039 the graph above):

| Task | Prerequisites |
| --- | --- |
| T001 | none |
| T002 | T001 |
| T003 | T002 |
| T004 | T003 |
| T005 | T004 |
| T006 | T005 |
| T007 | T006 |
| T008 | T007 |
| T009 | T008 |
| T010 | T009 |
| T011 | T010, T025 |
| T012 | T011 |
| T013 | T012 |
| T014 | T013 |
| T015 | T014 |
| T016 | T015, T027 |
| T017 | T016, T025 |
| T018 | T017 |
| T019 | T018 |
| T020 | T019 |
| T021 | T020 |
| T022 | T021 |
| T023 | T022 |
| T024 | T023 |
| T025 | T004 |
| T026 | T025 |
| T027 | T025 |
| T028 | T025 |
| T029 | none |
| T030 | T026, T028 |
| T031 | T025 |
| T032 | T030, T031 |
| T033 | T030 |
| T034 | T025, T026, T027, T028, T029, T030, T031, T032, T033 |
| T035 | T009, T028 |
| T036 | T030, T032 |
| T037 | T030, T036 |
| T038 | T025 |
| T039 | T025, T038 |
| T041 | T008 |


(The dependency table above encodes the amended graph, including T011 <- T025, T016 <- T027 and T017 <- T025; those extra edges take effect only with the owner's approval of this file at one exact head with a tracker or PR link. The chain below is the unamended order.)

T001 -> T002 -> T003 -> T004 -> T005 -> T006 -> T007 -> T008 -> T009 ->
T010 -> T011 -> T012 -> T013 -> T014 -> T015 -> T016 -> T017 -> T018 ->
T019 -> T020 -> T021 -> T022 -> T023 -> T024.

The T001–T024 chain deliberately follows the owner's sequential WI/review route in one worktree;
file-disjoint parallelism is not used there, and T025–T034 join it only as the owner decides. Output/error plumbing for the first-increment commands is delivered and frozen by T025; for
other commands it is implemented minimally in the WI that needs it and receives full matrix acceptance in T017,
not a passing claim before T017. FR-022 text escaping is delivered by T025 and mandatory from T011,
and the full baseline FR-022a stream filter is mandatory from T014; T018 expands
corpus/fuzz/integration evidence rather than introducing either safety behavior.
Feature 001 mappings above retain all original
acceptance and task completion rules; review evidence is appended, never fabricated.

## Evidence log

T001 and T002 are independently accepted at the revisions above. T003 is independently accepted at
`191c62baf227f64122770244d62d289900bb4d8d`. T004 is independently accepted at
`6abb7419c77b3712a6ac276bc798b102503d3144`. T005 is independently accepted at
`1189c81ba81b79a47a0485a9d5148fb8286c8eb0`; no product runtime acceptance is claimed.
Durable check and review
evidence, including earlier failed runs, is retained in
[the pilot execution record](../../docs/exec-plans/active/installable-pilot.md).

T005 independently Sol CLEAR at `1189c81ba81b79a47a0485a9d5148fb8286c8eb0`;
its native persistence scope does not establish authentication or raw JSON decoding.
T006 follows [the consolidated authentication contract](contracts/authentication.md).

S001 independently Sol CLEAR at `c88f33959a4c0a767f638afaccc446a8040c55b4`.
Author evidence: `/private/tmp/workharbor-s001-validation-h_fb25q3/evidence.json`;
independent: `/private/tmp/workharbor-s001-independent-leo2snzz/summary.json`.
Actual gates: Phoenix114, whr1, host_agent132, api_client162 green; Python846
collected/520required retained. HTTP1-only mitigation; Bandit remains affected.
No full canonical/live browser/pilot acceptance is claimed. T008 follows
[the consolidated enrollment contract](contracts/host-enrollment.md).

T008 implementation independently Sol CLEAR at
`64541c1fa8b1f0d9f353272a0ea8ee6888ea5f5c` after all scoped source/application
checks and actual macOS/Linux transport. Author pristine evidence:
`/private/tmp/workharbor-t008-validation-dho78ci4/evidence.json`; independent:
`/private/tmp/workharbor-t008-independent-645-kb8o8fqu/summary.json` and
`/private/tmp/workharbor-t008-independent-linux-645.json`. Actual corrected Linux:
`/private/tmp/workharbor-t008-linux-absent-pk-plan/amended-run/transport/assessment.json`.
Phoenix 173, Go host 240/API 162/CLI 1 events passed; all prior required IDs and
Python 846 collection/520 required remain. Genuine Linux transport 12 and security 9
passed with measured bounded resources and owned guest teardown. Earlier predecessor
and blocked pretest guard attempts remain in the execution/quality record. This
later documentation head does not claim a Linux run of its own SHA; exercised
runtime/config/dependency/test-input blobs remain exactly the accepted 64541c1 source.
Only Feature006 T008 is newly checked; Feature001 markers/frozen history are unchanged.
S001 remains mitigation-only; no provider/browser/load/pilot/release acceptance follows.

WP0 source reconciliation at `628bc4ded0f96ed0ffebc501eae833fe4e2d3e5f`: [first-increment evidence and remaining work](../../docs/quality/006-whr-cli/first-increment.md). This reference changes no task text, dependency, approval marker or completion checkbox.

Wave reconciliation (2026-10-25): T036, T037 and T039 source acceptance is
recorded in [the closing evidence record](../../docs/quality/006-whr-cli/t038-wave-acceptance-20261025.md),
including the three merged T038 slices and their exact-head Opus reviews.
T038 integration/host limits and T025-T034 acceptance gaps remain explicit;
T034 is not checked by this reconciliation.

## Proposed read-only web precursor scope (2026-10-27)

The [precursor proposal](contracts/read-only-web-precursors.md) describes isolated T020
read-only artifact/dashboard work after existing T006/T005 and partial T009 trust
persistence, then isolated T012 enrollment/trust reads after existing T005/T008, partial
T009 ConfigTrust records and the owner shell; reuse is limited to those existing T009
components, not full T009 completion. This is **PROPOSED**, pending exact-head
independent review, explicit owner acceptance of this PR's exact revision and merge;
until then existing gates apply. These are partial delivery scopes, not new task IDs or
full task completion. The table's T020-after-T019 and T012-after-T011 edges, downstream
prerequisites, checkboxes and full acceptance remain unchanged; no
lifecycle/browser/host gate is waived. Each implementation PR records its exact source,
scope and evidence.

O1 is DECIDED by the owner, revised 2026-10-27: read repository identity and immutable
commit from the existing owner workspace's `requested_config`. Requests select only
`workspace_id`; refuse foreign ownership and source override fields. No new source
approval/configuration/permission point is introduced. The exact-revision
sequencing/reuse exception remains proposed.

F8 is DECIDED by the owner on 2026-10-27: option B, artifact reads through the
host agent's existing workspace clone and authenticated connection. The proposed
`spec_kit.read` command and its bounded, owner/workspace-scoped read-only response
are a T007/T019 host-protocol dependency, detailed in the
[precursor contract](contracts/read-only-web-precursors.md#f8-decision-and-host-protocol-dependency).
No new approval, configuration, control-plane clone/mirror or fetching is allowed.
Until that command is implemented and supported, dashboard artifacts are unavailable.
This decision does not complete T007/T019 or waive the sequencing/reuse gate.
