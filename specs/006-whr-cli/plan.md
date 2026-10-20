# Implementation Plan: WorkHarbor installable pilot

Branch: `feat/installable-pilot` | Date: 2026-10-14
Spec: [spec.md](spec.md)
Baseline: `f02250fc75b1e72a1d949edaee128e31fffbaa53`
Status: execution in progress. T001 scaffold independently Sol CLEAR at
`d3d84c8361a1389292fad96d02a828ac9fabc2e3`; T002–T004 are accepted as recorded in
tasks/evidence. T005 is under implementation; later work remains open.
Scaffold checks do not establish product runtime verification.

## Authorized objective

The owner explicitly requested on 2026-10-14 that work continue in this WorkHarbor
worktree, on this branch, through the installable pilot, including selective
Python-to-Go migration, independent review and fixes after each work item, and
one PR after all internal-pilot work items are accepted, for final human review.
The pilot is internal; release publication is outside this execution scope. The
same worktree is retained.
This is a WorkHarbor task; no SheetMill files or sibling product scope are changed.

## Work-item sequence

1. Complete and review the combined Feature 001 / Feature 006 pilot design and
   acceptance mapping. Resolve retry semantics before product code.
2. Pin and scaffold Phoenix control plane, Go host agent, CLI and shared API client;
   establish explicit application format, build and test gates.
3. Inventory selected Python behavior, freeze reference vectors, port pure product
   primitives to Go and prove parity. Retain the Python development harness.
4. Implement ownership, authentication, durable lifecycle operations and audit.
5. Implement mTLS enrollment, host identity rotation/revocation and reconnect.
6. Implement trusted Git/devcontainer resolution, Docker provider and persistent data.
7. Implement browser and CLI lifecycle paths, bounded terminals and persistent console.
8. Implement agent-session interoperation and read-only Spec Kit dashboard.
9. Implement isolated SSH, resumable host setup/offboarding and diagnostics.
10. Implement backup/restore, local install packaging and real-host/browser acceptance.
11. Resolve all review findings, run exact-head required checks and open the combined PR.

## Migration boundary

ADR 0010 permits selective ports with frozen Python behavior vectors. Python remains
development tooling, not a product dependency. Do not migrate Symphony, tracker,
provider eligibility or task-approval orchestration into product code. Product exit
codes come from ADR 0013 rather than Python development-script exit codes.

## Review and evidence

Every work item records its scope, prerequisites, actual check results and exact
candidate reviewed. Fix actionable findings before successor work. Missing tools,
unknown runtime outcomes and mocks are not passing real-host acceptance. Signed
release, deployment, activation and merge gates remain separate owner decisions.

## Technical design

Phoenix 1.8.15 / Elixir 1.20.4 / OTP 29 control plane with Ecto/SQLite;
Go 1.27.2 host agent, CLI and exported API client in separate modules under
`apps/host_agent`, `apps/whr`, `apps/api_client`, with a root `go.work`.
Pin direct and transitive dependencies in setup and prove clean-checkout builds.
Use Cobra 1.10.2/pflag for interspersed flags and completion, without Viper.
Standard net/http verifies TLS, refuses credential redirects and applies deadlines.
Use coder/websocket 1.8.15 for WSS relay and golang-jwt/jwt 5.3.1 for Ed25519
operation-scoped grant verification. No dependency version is claimed tested yet.

Linux/systemd/Docker is the first host target. Client build targets are Linux and
macOS amd64/arm64; mark only actual exercised combinations verified. Windows is
unsupported until credential ACL and terminal evidence exist. Reuse the official
Dev Container CLI, pin its exact tested version in the provider WI, and reject
untested capabilities before hooks or provider mutation.

The control plane owns owner login, scoped opaque credentials, durable operations,
audit, trust records and the owner API. Host agents connect outbound over mTLS.
The CLI never contacts a provider or host agent directly. Local host commands use
explicit privileged adapters. Dashboard credentials authorize reads only; browser
cookies are Secure, HttpOnly and SameSite=Strict, with Host/Origin and anti-forgery
validation on requests and upgrades. No direct credential in URLs or arguments.

A lifecycle submission allocates its stable workspace identity and idempotency key
before transmission. Retrying preserves both; changed payload conflicts. Retry at
most three times at 1/2/4 seconds on documented transient failures. Operator wait
interruption never cancels provider work. Agent input is not automatically resent.
The console is one process/session per workspace with a 64 KiB byte ring, dropped
byte reporting and attachment-independent lifetime. Use Feature 001 quotas, grant
TTLs, idle/maximum duration and cleanup bounds. Cancel pumps/release accounting
independently of graceful WebSocket close; force close within the five-second bound.

Grant claims bind issuer, owner, workspace, runtime generation, audience, kind,
operations, session when applicable, jti and expiry. Attach issuance and reusable
control-grant lifetimes are 60 seconds; no expiry grace. Use configured public keys,
never token-selected URLs. Atomic attach consumption survives interface restart;
emergency revocation closes active attachments. Validate before session lookup.
Agent-owned sessions remain separate from the platform console. The provider
session transport and SSH server/certificate provisioning contract must be finalized
and reviewed before those WIs; adding a hidden helper or configuring an agent tool
is not an acceptable substitute for an authorized in-container interface.

`whr ssh` uses Go SSH over an owner-authorized byte tunnel with a pinned server
key and fresh certificate, without personal SSH state. External config uses private
isolated files, states expiry and warns that external-client traffic is unfiltered.
Unsupported SSH capability fails before connection. Host setup/offboarding use
private atomic local checkpoints; success requires health or confirmed revocation,
respectively. Dry-run has no writes and incomplete/offline is never success.

Artifact views use repository/feature/task identity, immutable Git commits/content
hashes, parser capability, evidence source/time and availability. Safely render
untrusted Markdown; never execute it or fetch embedded references. Missing objects
or unsupported layouts remain unavailable, not fabricated task completion.

## Existing design reconciliation

Committed constitution v1.1.0 permits ordinary in-container agents; all tracker,
AI orchestration and provider account management remain development tooling.
Feature 001's older plan wording excluding product AI and conflating PTY/session
lifetime is superseded only by the accepted constitution and ADR 0012; its actual
lifecycle, authorization, recovery and release requirements are retained.
The explicit user execution request supplies implementation scope, but does not
supply a live host grant, signed release, merge or evidence that checks passed.

## Installable-pilot acceptance

All Feature 001 tasks and applicable Feature 006 internal-pilot stories remain
required, including SC-001–005 and SC-007–012. Additional command surface
(rebuild/removal, CLI auth/operations, host administration, agent controls,
dashboard and SSH) and local packaging/release-policy checks are not completed
by a scaffold. Each task requires its own meaningful failure-path tests and
independent review. SC-006 is excluded only from internal-pilot acceptance; full
Feature 006 completion remains open until its signed-release verification passes.
Application checks: Go format/vet/build/tests per module, Phoenix format,
warnings-as-errors compile and tests, plus inherited `make check` using Python
3.11+ in a pristine committed checkout. Extend the reviewed acceptance runner and
manifest explicitly; do not weaken discovery or call missing tools passing.

Real acceptance: fresh install, TLS login, enrollment/rotation/revocation, trusted
image/Dockerfile provisioning, console reattachment, stop/start/rebuild with exact
uncommitted file hashes, interrupted-operation recovery without duplicate runtime,
agent interface, dashboard authorization/browser-origin negatives, SSH, resumable
offboarding and quiesced clean restore. Measure Feature 001 five-workspace/eight-
attachment load, timings and idle/load footprint. Stub tests do not prove real use.

T023 produces local unsigned archives labelled development artifacts, checksums,
SBOM and recorded build inputs/provenance, plus clean install/uninstall evidence
and draft-only release machinery/policy tests. T024 requires the real-host,
browser and load acceptance above and exact-head checks/independent Sol review
before the combined PR is handed to the owner for final human review.

The owner-authorized allowlisted human-signed release exercise on `main` and
end-to-end SC-006 verification are a separate follow-up after this internal-pilot
handoff; they cannot be prerequisites for the PR introducing the implementation.
Full Feature 006 remains open until SC-006 passes. No signed-release evidence is
claimed from an unsigned archive. Signed release tags, public distribution,
package channels and deployment need separate owner authorization. Actual
disposable Linux host, TLS hostname/certificate and live-test scope remain
owner-controlled inputs required for pilot acceptance; signer allowlist and
release authorization are inputs to the later release exercise. A configured
Docker context establishes none of them.

## Amendment H77: first local `whr` increment

Status: proposed with [ADR 0014](../../docs/adr/0014-whr-command-structure.md); the amendment text needs an
independent review CLEAR for its exact head and then explicit owner approval of that head (tracker or PR link).
Werner's chat direction and implementation go-ahead of 2026-10-16 are the recorded decision H77, not text approval.

Scope: spec FR-051 to FR-063 together with the owner's initialization and resource revisions (US0, US1a, FR-039 to
FR-050), as work items T025 to T035 in [tasks](tasks.md). Everything needing the owner API (verified 2026-10-16: the
router serves only `/api/v1/hosts` and `/api/v1/auth`) stays with T009 onward. The first-increment series (T025 to
T035) touches `apps/whr`, `contracts/`, `scripts/check_repo.py`, `tests/` and docs, and T035 alone the host agent; T011, T016 and T017 consume its foundation instead of
re-creating it. Owner decisions of 2026-10-16/17 (H77 (5)): the series runs on its own branches and pull requests,
one pull request per work item, outside the T001-T024 chain;
Werner's chat answer "5approved" (2026-10-17) is recorded as decision H77 (5); it is not approval of any head's text, and the edits take effect only with approval at one exact head with a tracker or PR link. After the foundation T025 the work items can proceed in parallel worktrees (see the dependency graph
in tasks.md); shared files (`internal/commands` registration, `ci-acceptance.json`, `go.mod`) are the only expected
merge points, resolved by rebasing in merge order. The initialization revision requires human review of the plan, task
scope, configuration schema and launcher contracts before implementation; this document and its contracts
(`workharbor.schema.json` version 2, `vectors.json`, `starters.json`, `resources-vectors.json`) are the input to that review, not an approval.

### Design

Layout in `apps/whr` (ADR 0010; `cmd/whr` stays a thin entry point):

| Package | Responsibility |
| --- | --- |
| `internal/commands` | command table (the Go mirror of `contracts/cli/commands.json`), Cobra registration generated from it, help hints, completion |
| `internal/exitcode` | ADR 0013 table, error-to-code mapping, freeze tests |
| `internal/output` | envelope (`contracts/cli/output.schema.json`), human renderer, color, `whr:` error line |
| `internal/terminal` | FR-022 text escaping now; the stream filter of FR-022a remains T014 |
| `internal/workspacecfg` | manifest, devcontainer, project and Git-state checks shared by `validate`, `doctor` and `init`; safe file access |
| `internal/doctor` | check catalogue (`contracts/cli/doctor-checks.json`) behind measurement ports |
| `internal/scaffold` | `init` proposal model, preview and diff, writer with rollback, starter catalog (D1, D3) |
| `internal/wizard` | interactive wizard with back, review and cancel on top of the proposal model (D2) |
| `internal/resources` | strict CPU and memory parsing, canonical values, origin summary (E) |
| `internal/version` | build metadata (FR-027) |

Cobra 1.10.2/pflag as already planned. `ws` is a native Cobra `Aliases` entry of the `workspace` group. Cobra aliases
only rename siblings, so the root short forms are generated root commands sharing one handler; they stay visible
in a custom help template (hint form, "not yet available" block for unavailable commands), are offered by a root
`ValidArgsFunction` that reads the table (Hidden commands are excluded from Cobra completion), and flag errors go
through `SetFlagErrorFunc` so messages and the `command` field use the canonical id. Unavailable commands share one
handler that returns the usage error (FR-061, FR-063) before any I/O.

Strict JSON for the manifest uses `apps/api_client/wirejson` (unchanged; this series does not touch `apps/api_client`),
which reports only `invalid-json` or `input-limit-exceeded`. `whr` therefore classifies first (size, UTF-8, BOM,
NUL), runs a whr-local duplicate-key and depth scan to produce `manifest.encoding`, `syntax` and `duplicate_key`,
and then calls wirejson as the final strict gate. The devcontainer file is JSONC: the host agent's parser shells out to
Node (`JSONC{NodePath}`) and is `internal` to its module, so `whr` carries a small bounded strict-JSONC reader
(comments, trailing commas, duplicate keys rejected, size and depth limits). It is a pre-flight, not the authority; the
host agent re-parses at trust time, which is why `provider_admission` is `not_evaluated`. No JSON Schema library is
added to product code; the manifest checker is hand-written and frozen against vectors in
`contracts/workspace/vectors.json`, which the amendment authors and T028 owns (T026 reads it); the schema's lookahead pattern (not RE2)
is shown equivalent to the Go checker by the positive and negative path vectors, so no Python or JavaScript test runner
is added. Divergence between this checker,
the resolver and `check_repo.py` (severity of an entry without a gitlink, scp-like origins, https userinfo, case-folded
`.git`, `1.0`) is closed by those vectors on the whr side; running the same vectors in the host agent test is a
work item T035, sequenced after T009.

Hostile input (FR-055): the target directory itself is opened with a no-follow, directory-only open and its
device and inode are compared with a prior `Lstat`; every further component is opened relative to the previous
descriptor with `O_NOFOLLOW|O_DIRECTORY` (Linux additionally `openat2` with `RESOLVE_NO_SYMLINKS`), the type is
checked by `Lstat` before any `open` so a device or FIFO is never opened, files are opened non-blocking and `fstat`ed
for a regular file, and reads go through a size-capped reader. Ancestors of the given `dir` are followed (macOS `/tmp`
is a symbolic link); only the directory and below are held to no-follow. A user directory is never listed, only named
entries are opened. Git is invoked only for `.git`-bearing directories whose `.git` is a real directory (never a
symbolic link or gitfile), with a freshly built environment (only `PATH` for locating git, `HOME` set to a fixed, never created
path such as `/nonexistent`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_OPTIONAL_LOCKS=0`,
`GIT_TERMINAL_PROMPT=0`, `GIT_NO_LAZY_FETCH=1` (Git 2.44+), `GIT_PAGER=cat`; every other inherited `GIT_*` variable such as
`GIT_TRACE*`, `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_CONFIG_COUNT`,
`GIT_CONFIG_PARAMETERS` and `GIT_EXEC_PATH` is dropped), neutral `cmd.Dir`, empty stdin, `--git-dir` and `--work-tree`
arguments, `-c` flags (not `GIT_CONFIG_COUNT`, which needs Git 2.31) for `core.fsmonitor=false`,
`core.hooksPath=/dev/null`, `protocol.allow=never` and `protocol.<name>.allow=never` for file, git, ssh, http, https and
ext (a per-protocol setting in the hostile `.git/config` would otherwise override `protocol.allow`), `GIT_ALLOW_PROTOCOL` set to a
name matching no protocol, `GIT_NO_LAZY_FETCH=1` (effective from Git 2.44; below it the protocol overrides are what stop a promisor
fetch), one shared 5 second wall-clock budget per run (each call gets the remainder), `lstat` pre-checks that `.git/HEAD`, `.git/index` (if present) and `.git/config` are regular files, a capped output reader, a minimum Git version check (below it: the error
`git.unavailable`), `exec.ErrDot` treated as Git absent, and exactly seven argument vectors: `--version`,
`symbolic-ref -q HEAD`, `rev-parse --verify --quiet HEAD`, `ls-files --stage -z`, `ls-tree -r -z HEAD`,
`cat-file blob HEAD:.gitmodules` (only when the HEAD tree lists it) and `cat-file blob :.gitmodules` (only with an unborn HEAD
and when the index lists it). HEAD is unborn exactly when `rev-parse --verify --quiet HEAD` exits 1 with empty standard error and
`symbolic-ref -q HEAD` names a missing ref (FR-054); the two HEAD vectors are then skipped. A `.git` containing `commondir` or
`objects/info/alternates` is `git.unsafe_path`. The corpus includes a partial-clone repository with `protocol.file.allow=always` and an
`uploadpack` that writes a marker, asserting no fetch and no marker. An explicit `--git-dir` skips discovery
and the ownership check, and `.git/config` (with includes) is still parsed; the `-c` list and the minimum version carry
the protection and `safe.directory` is never set. No `status`, `diff`, `submodule` or checkout command ever runs on
the validated directory. On macOS the Git binary is first checked without executing it: the `/usr/bin/git` shim with
no developer directory (`xcode-select -p` equivalent via the path check) is reported unavailable so the Command Line
Tools installer never opens. `.gitmodules` is parsed in process by a bounded INI subset reader; no `git config` call reads it. A
corpus test runs real Git against repositories carrying fsmonitor, hook, filter, alias, include and update-command payloads
and asserts that no marker appears.

`doctor` uses ports (environment, stat, unix dial, fixed exec of `git --version`, statfs via `syscall`, platform) so unit
tests inject every status; real-host evidence supplements, never replaces, them. The socket check dials and closes. The
redaction function is shared and tested with a canary home directory, user and host name.

The proposed shared installation-PATH contract (FR-046a, SC-014a) is future
generation/editing work, not an expansion of D1 or D3: existing devcontainers,
JSONC and startup files remain byte-preserved. D1/D3 may explain the policy in
existing next-step output. A later owner-approved proposal must share one policy
across the supported shell adapters, preview every affected file and preserve
per-path overwrite consent and unrelated settings. Environment-derived Cargo
roots and pre-lookup PATH sanitization follow FR-046a, with marker-based future
acceptance under SC-014a; no Cargo invocation or project-config parsing is needed.
Do not introduce a new user
configuration filename or per-language mechanism without demonstrated need.
The exact amended spec, plan and task text still require owner approval before
implementation; neither this proposal nor existing init flags authorize generation,
toolchain installation or live activation.

`init` (D1, wave 1a) builds a proposal in memory: the manifest `workharbor.json` (version 2, FR-058, explicit values for
every setting, defaults filling only missing ones), the existing devcontainer file at the manifest's path (reused
unchanged and only validated; wave 1a never generates one) and `<projects_path>/.gitkeep`, each marked create, overwrite, reuse or
skip_existing, with a unified diff for overwrites. The same proposal model feeds the wizard (D2, wave 1b), the unattended
flags and `--dry-run`, which gives wizard and flag parity by construction. The proposal is validated with the `validate`
code (in-memory content checks before the first write, the Git-free checker profile on the written result), preflighted, and
written with `O_EXCL|O_NOFOLLOW` relative to a no-follow-opened target; overwrites go through a same-directory
temporary file and rename, with the old bytes kept until the rename. Each created path is recorded with device and
inode for reverse-order rollback, which restores overwritten files and removes a path only when the identity still
matches; the preview carries a content hash per file, re-checked immediately before writing to detect change since
preview. `init` runs no Git command and makes no network connection (FR-056); it validates with the Git-free profile of the `validate` code (manifest, devcontainer and projects checks, `git_checks: not_run`, FR-057), and valid existing v1/v2 manifests follow ordinary file preservation/proposal/consent rules:
no edit means original bytes retained, and requested edits preview the current authoring format (v2)
with explicit per-path overwrite consent; no automatic version bump or separate migration workflow; `--project-url` is written to the manifest
only. The wizard (D2) uses a prompt port (stderr for prompts, terminal check on stdin and stderr, `--non-interactive`,
`--json` and non-terminals suppress it) so unit tests drive it with scripted answers; a separate work item builds a PTY
test harness for end-to-end terminal behavior (raw mode, line editing, cancel by interrupt), which D2 then uses for its
acceptance. Devcontainer reuse (D3, wave 1b) adds `--devcontainer <path>` and the wizard step; the starter catalog is
listed unsupported and `--template` refuses (wave 2). Failure injection is a test seam on the step runner, not a flag.
SIGINT and SIGTERM trigger rollback; SIGKILL does not, and the next run refuses on the leftover paths. Runtime
enforcement of the shell and console choices is out of scope and reported as `runtime_enforcement: not_in_this_release`.

`workspace create` resources (E, wave 1a) is only a pure parsing and resolution library in `apps/whr/internal/resources`
tested against `contracts/cli/resources-vectors.json`; the host profile is an input to the resolver, there is no
command wiring, flag or prompt until wave 2.

The manifest-schema extension (wave 1a) updates the checker rules, `contracts/workspace/vectors.json` and
`scripts/check_repo.py` (Python reads the same vectors); the host-agent resolver change that accepts version 2 is a separate
reviewed work item sequenced with T009, so version 2 stays fail-closed until then.

### Verification

Per work item: tests first with recorded absent-implementation RED, then `gofmt`, `go vet`, `go build`,
`go test -count=1` in `apps/whr` through `make check-apps` (`apps/whr` reaches `apps/api_client` through the root
`go.work` only, as `host_agent` does, without a `require` line; a standalone build outside the workspace fails the same way), plus `make check`;
required test IDs are appended to `docs/development/ci-acceptance.json` without changing earlier IDs. Real evidence
for T034: the built binary run on the owner's macOS with OrbStack and on Linux (arm64 or amd64), exact versions
and commands recorded in the quickstart; mocks are labelled. Building artifacts for evidence uses ordinary
`go build` into a temporary directory under the repository's application checks; no archive, image, release or
publication is produced or authorized. Independent Sol review of each WI's exact head, with the hostile corpus rerun
by the reviewer for T026 and T030 (and the wizard transcript and PTY tests of T032).

### Risks

- Divergence from the host agent's admitted subset (mitigated: warnings only, `not_evaluated`, vectors).
- Git as an attack surface on hostile repositories (mitigated: fixed read-only argument vectors, isolated environment,
  corpus test; residual risk is a Git bug and is stated in the T026 evidence).
- Cobra alias semantics (mitigated: generated root commands and the equivalence test).
- Windows is unsupported; no path or permission claim is made for it.

## T005 accepted foundation and verification

Independent Sol design CLEAR covers the consolidated V3 contract in the
[owner API](contracts/owner-api.md#accepted-t005-foundation-contract) and
[data model](data-model.md#accepted-t005-persistence-foundation). Implementation
is limited to native normalization/canonical bytes, scoped records/constraints,
atomic intent/reservations/audit, metadata claim/result guards and retained
storage/history. No auth, raw HTTP decoder, trust/fetch, provider, effect consumer,
replacement-start, delete_data acceptance or repeatable observation API is added.

Source lives in apps/control_plane/lib/workharbor/foundation/, migrations in
apps/control_plane/priv/repo/migrations/, tests in apps/control_plane/test/workharbor/,
and isolated database helpers in apps/control_plane/test/support/. Scoped Repo
configuration pins durability settings; migration formatting includes the new files.
Meaningful Phoenix IDs append to the existing acceptance manifest without changing
prior IDs/policy. Write tests before behavior and record actual absent-implementation
RED. Verify native independent canonical anchors and all identity negatives;
direct-SQL constraints; independent writer races; same-generation stale intent;
immutable terminal replay; audit rollback/redaction; process crash and file reopen;
per-writer effective pragmas. Run Phoenix format/compile warnings-as-errors/tests
and pristine source/manifest/repository/whitespace checks. Assets need no change.
Full combined-head canonical and live pilot evidence remain later required gates.

## Open questions for the owner (H77 amendment)

Decided by the owner in chat on 2026-10-16/17: own branches and pull requests for the first increment (Werner's chat answer "5approved" (2026-10-17) is recorded as decision H77 (5); it is not approval of any head's text, and the edits take effect only with approval at one exact head with a tracker or PR link); `setup --dry-run` excluded; a superseding ADR for "remote
unreachable" before owner-API client work; `doctor` exit 0 with verdict `incomplete`; read-only `doctor` on macOS;
thresholds Git >= 2.39.5, 2 GiB, 64/256/64 KiB, 5 s, 256 MiB ("seems to be fine from my side", 2026-10-17).

Still open:

1. (Decided 2026-10-17, H77 (7); kept for the record) Manifest `schema_version` 2 adds `name` and `project` beyond the four fields the owner listed (`start_dir`, `human_shell`,
   `console_mode`, `shortcuts`), because the wizard collects them and no other file is permitted; this was my addition and is confirmed.
   The resolver change to accept version 2 is the work item T035 (after T009).
2. Decided (H77 (7)): `--template` refuses with exit 64 while starters are unsupported.
3. Decided (H77 (7)): wizard cancellation exits 72; an overwrite without permission or a missing input exits 70 in unattended mode only.
4. Exit 68 for an invalid configuration and for a failed doctor check (rather than 64 or a new code).
5. `init` runs no Git command and never adds the project submodule; the owner pins the project with Git after init
   (FR-049). Confirm that a `.gitkeep` and a printed next-step list are sufficient.
6. In wave 1a, unattended init needs an existing devcontainer file; devcontainer relocation and wizard reuse arrive in 1b.

## Resolver follow-up and rebasing rules (H77, round 5)

T035 is the reviewed resolver change: `apps/host_agent/internal/config/resolver.go` accepts manifest version 2 (exact field
set per version, type-exact `schema_version`) and a shared-vectors test in `apps/host_agent` runs
`contracts/workspace/vectors.json`; it starts only after T009 is accepted and needs its own owner-approved scope because it
touches accepted provider inputs. In-flight rebases: a branch with a started single-task state (`.symphony/single-task.json`)
finishes its completion before rebasing onto a sibling merge, or the owner re-baselines it; otherwise merges of siblings are
serialised in graph order. Dependent branches start from the merged `main` that contains their prerequisites.
