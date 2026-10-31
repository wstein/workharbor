# Feature Specification: VM provider and cross-platform test evidence

**Feature identifier**: `011-vm-provider`
**Feature Branch**: `docs/spec011-vm-provider`
**Created**: 2026-10-31
**Input**: Owner request 2026-10-31: evidence for the failing Mac tests, and disposable macOS (and later other)
guests driven through the same sandbox lifecycle as containers, vendor neutral.
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md).
**Related**: [Feature 008](../008-workspace-sandbox/spec.md) (RuntimeProvider contract, FR-012 to FR-018, FR-029, FR-034),
[Feature 009](../009-host-setup-wizard/spec.md) (host setup), [Feature 006](../006-whr-cli/spec.md),
[container runtime prerequisite](../../docs/container-runtime.md).

## Context and scope

Feature 008 defines a RuntimeProvider contract and one container provider. Two needs are not covered:

1. **Evidence on other operating systems.** Part of the Python test suites of BundleDock and Anchorage fails on the
   owner's Mac. Those are separate repositories (see [references](../../docs/references.md)); their CI is outside this
   repository and this feature (owner follow-up). This repository has the same blind spot: its jobs run on an
   ubuntu-based Linux runner image, and its macOS jobs run only selected Go tests, so its Python suite has no macOS
   evidence.
2. **Disposable guests with their own kernel and their own operating system**, driven through the same sandbox lifecycle
   as containers, first for macOS on the owner's Mac.

This feature defines:

1. a **cross-platform CI evidence path** for this repository on macOS and Linux hosted runners, independent of any
   provider;
2. a **one-time, generic amendment (A1) to the Feature 008 contract** that adds the guest operating system and the
   capabilities a VM needs;
3. a **VM provider** built on a vendor-neutral **driver port**, with disposable clones of an owner-supplied base image;
4. conformance cases and refusal codes for the above; and
5. a first increment that is library-level and offline, using a fake driver.

### Terms

- **Guest**: the operating system inside a VM (`linux`, `darwin`, `windows`) and its architecture (`arm64`, `amd64`).
- **Base image**: a VM image supplied and prepared by the owner. `whr` only names it.
- **Disposable clone**: an instance created from a base image for one use and discarded afterward.
- **VM backend**: a local virtualization tool or API. **Driver**: the adapter from the driver port to one VM backend.
- **VM provider**: the RuntimeProvider implementation that composes a driver with admission, plans, ownership and cleanup.
- **Test run**: create a disposable clone, copy sources in, execute declared argv, export results, remove the clone.

### Owner decisions

<!-- informative:begin -->

- **2026-10-31 (OD-1, CI first)**: repository CI is the authoritative cross-platform evidence path: macOS and Linux
  (OD-6). It does not depend on a provider, and a provider does not depend on it.
- **2026-10-31 (OD-2, interactive macOS)**: the interactive local VM provider targets macOS guests first. Informative
  only: Tart is the first driver for it (OD-5).
- **2026-10-31 (OD-3, vendor neutrality)**: the contract and every normative requirement below are vendor-neutral
  (FR-022). Other drivers can be added without changing them.
- **2026-10-31 (OD-4, base image; resolves NC-1)**: the owner builds the macOS base image on their own Mac from the
  operating system vendor's official download and keeps it local. It is never uploaded to a registry or passed on. The
  build recipe is documented so the image is reproducible (FR-007). Whether the vendor's license permits the intended
  use of macOS in a VM is the owner's to check against that license; it was not checked here.
- **2026-10-31 (OD-5, drivers; resolves NC-2)**: Tart is the first driver. The user installs the VM backend and holds
  its license; `whr` only drives it and neither installs nor ships it (FR-017), the same pattern as the Docker runtime.
  A KVM driver on a Linux host is the likely second wave. A Hyper-V driver is only a possibility, with no commitment.
  Driver selection is explicit (FR-023), and the support matrix lists host and guest separately (FR-024). The
  maintenance status of the first backend after a reported change of ownership is unverified. The swappable driver port
  is the mitigation, so nothing depends on one vendor.
- **2026-10-31 (OD-6, Windows; resolves NC-3)**: Windows is out of scope. The CI matrix is macOS and Linux, with no
  Windows runner. `windows` stays a reserved `guest_os` value in the contract so that a later tool or a Windows guest
  needs no further contract change (FR-030). Linux tests can also run under WSL 2, which is not a stated target.
<!-- informative:end -->

### Boundaries

- Feature 008 stays in force. This feature changes it only through amendment A1 (FR-001 to FR-005) and the appended
  and widened refusal codes. A1 reaches more than the contract: Feature 008 FR-012, FR-034, the data model, the
  conformance suite and the admission paths for the new refusals (see "Changes to existing documents"). Every Feature 008
  admission decision is unchanged (FR-003).
- A1 is the single exception to the sentence of Feature 008 FR-034 that adding a provider changes no contract: the
  contract had no platform dimension. A1 is generic. After it, adding a driver changes no contract text.
- `whr` does not create, download, build, publish, sign or redistribute base images (FR-007).
- `whr` does not install a VM backend and changes no host virtualization, network, firewall or security setting
  (FR-017). Installing a backend on macOS would be a separate Feature 009 decision.
- Not in this increment (each needs its own specification): a Windows guest provider, Windows as a product target of
  `whr` (a different tool would be a different specification), a driver for a Linux host, remote VM hosts, snapshots
  and rollback, egress enforcement, credential mediation, a wrapper command for test runs (needs an ADR 0014
  amendment), and CI for the separate BundleDock and Anchorage repositories.
- Windows is out of scope (FR-029). The [container runtime](../../docs/container-runtime.md) statement on Windows hosts
  is unchanged.
- No claim is made of universal containment, performance or portability. Every protection is stated with its scope.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Cross-platform CI evidence for this repository (Priority: P1)

A maintainer opens a pull request against this repository. Its Python and Go suites run on macOS and Linux hosted
runners. Each job reports a verdict for the exact head under test, and one aggregate check requires both.

**Why this priority**: it gives this repository platform evidence for its own suites at the lowest cost and needs no
VM. It does not answer the question about the failing BundleDock and Anchorage suites: those are separate repositories
that need their own CI change (owner follow-up outside this feature).

**Independent Test**: a pull request that injects a failure which exists on one platform only turns that platform's job
and the aggregate check red, and leaves the other jobs green.

**Acceptance Scenarios**:

1. **Given** a test that fails only on macOS, **When** CI runs, **Then** the macOS job fails with the test named and the
   aggregate check fails.
2. **Given** a test skipped on one platform, **When** the repository check runs, **Then** the skip must name platform,
   reason and tracking reference, or the check fails.
3. **Given** all jobs green on the exact head, **Then** the aggregate check is green and records the head SHA.

### User Story 2 - Disposable macOS test run on the owner's Mac (Priority: P1)

The owner runs a test run against a base image they prepared. A clone is created, sources are copied in, the declared
command runs, results are exported, and the clone is removed. The host is not modified.

**Independent Test**: a fake driver records every driver call. The sequence is clone, start, ready, copy in, exec, copy
out, stop, delete, in that order. After the run no owned instance exists, results are present, and the base image content
id is unchanged. Real-VM evidence is produced by the owner on their Mac (FR-026).

**Acceptance Scenarios**:

1. **Given** a named base image and a declared argv, **When** the run completes, **Then** exit status, logs and declared
   artifacts are in the result directory and the clone is gone.
2. **Given** the test command fails, **Then** results are still exported and the clone is still removed.
3. **Given** the base image does not exist, **Then** the request is refused with `refused.base_image_unavailable` and
   nothing is created.

### User Story 3 - Refusal of what the provider cannot do (Priority: P2)

A request names a guest, a limit or a mount the provider cannot satisfy. It is refused with a stable code before any
side effect.

**Independent Test**: admission table of guest, architecture, tool mount and interactive shell against provider capabilities,
with the expected code and zero driver calls per refusal row. The concurrency limit is not an admission row; it is
tested by the provider check of FR-009 (SC-013).

**Acceptance Scenarios**:

1. **Given** `guest_os=windows` and a provider reporting `darwin`, **Then** `refused.guest_os_unavailable`.
2. **Given** a provider at its concurrency limit, **Then** `refused.concurrency_limit_reached` and nothing is created.
3. **Given** a request for `ro_bdock` tool mounts and a guest that cannot mount them, **Then**
   `refused.tool_mounts_unavailable`.

### User Story 4 - Swapping the driver changes no contract (Priority: P2)

A second driver for another VM backend is added.

**Independent Test**: a second fake driver with different declared capabilities is added in a test only. The conformance
suite passes unchanged, and no production contract, data model or admission file changes.

### User Story 5 - Interrupted run leaves nothing behind (Priority: P3)

A run is canceled or the host process is killed mid-run. Cleanup removes owned instances only and reports leftovers.

**Independent Test**: fake driver with injected failures at each step. After `Remove`, no owned instance remains, results
are retained, and leftovers are listed by name and id.

### Edge Cases

- The base image is missing, or it is running or locked by another process: refused with
  `refused.base_image_unavailable`, nothing is created.
- Clone succeeds and start fails; the guest never becomes reachable within the deadline.
- The host sleeps or the VM backend restarts during a run; the driver's state differs from the provider's record.
- A foreign VM has the reserved name prefix but no provider record.
- Two runs are requested at once with a limit of one (FR-009: exactly one reservation succeeds).
- The guest has no execution agent, so `exec` is unavailable and capability `exec_argv` is `unavailable`.
- An argument is empty, contains a newline, or is 64 KiB long.
- Disk is full during clone.
- A test is skipped on a platform without a recorded reason.

## Requirements *(mandatory)*

### Functional Requirements

**Amendment A1 to the Feature 008 contract**

A1 amends Feature 008 as listed under "Changes to existing documents": FR-012, FR-034, the data model, the contract
(fields, re-verification sentence, conformance cases 1, 4, 6 and the new ones; case 6 "argv as data, no shell" is amended
so that shell transport is allowed only under `exec_argv=quoted_verified` with the FR-012 corpus round-tripping byte-exact) and the admission paths for the new
refusals. Every amendment is generic and additive.

- **FR-001**: The capability report MUST gain `guest_os` (`linux`, `darwin`, `windows`) and `guest_arch` (`arm64`,
  `amd64`), each with value status and scope under Feature 008 FR-015. The container provider reports `linux` and the
  architecture of the guest its runtime reports (FR-003). A request naming a `guest_os` or `guest_arch` that is not
  `enforced` MUST be refused with `refused.guest_os_unavailable` or `refused.guest_arch_unavailable` before any side
  effect. There is no fallback.
- **FR-002**: `kernel_isolation=microvm` means a dedicated guest kernel and says nothing about the guest operating
  system. A request that needs a specific guest MUST name `guest_os`. A provider MUST NOT satisfy a `guest_os` request with
  another operating system, and Feature 008 FR-014 applies unchanged.
- **FR-003**: Amendment A1 MUST be additive and name no vendor. The container provider reports these values, chosen so
  that no existing row can change: `guest_os=linux` and `guest_arch` equal to the architecture the container runtime
  reports (`unknown` if it cannot be probed; only a request naming `guest_arch` is then affected); `base_image=none`;
  `concurrency_limit=unknown` (FR-009 is a provider check of the VM provider, not an admission row, so `unknown` cannot
  fail any existing row); `exec_argv=native` (Feature 008 FR-029 and conformance case 6 already require argv as data);
  `ownership_mechanism=native_labels`; `interactive_shell=none` (the Feature 008 contract defines no interactive
  shell; conservative). `Admit` reads a new field only when the request names its requirement (`guest_os`,
  `guest_arch` or an interactive shell); every `Exec` reads `exec_argv`, and the container value `native` admits all
  existing rows; a request that names none of them is not affected by the new
  fields. Every row of the Feature 008 admission table (`TestAdmitTable`) MUST yield the same decision and code before
  and after A1, checked by a regression row set that runs the table against the container values above.
- **FR-004**: The capability report MUST gain these fields, each with status and scope:
  `base_image` (`read_only_clone_source`, `none`); `concurrency_limit` (a non-negative integer or `unknown`);
  `exec_argv` (`native`, `quoted_verified`, `unavailable`); `ownership_mechanism` (`native_labels`,
  `name_and_record`); `interactive_shell` (`pty`, `none`). With `guest_os` and `guest_arch` the report has eighteen
  fields (eleven in Feature 008).
- **FR-005**: A provider whose guest cannot mount the tool bundles reports `tool_mounts=none`. A request for `ro_bdock`
  against it MUST be refused with `refused.tool_mounts_unavailable`.

**VM lifecycle**

- **FR-006**: An instance MUST be created only as a disposable clone of a base image the owner named. The Plan MUST
  record the base image name and the identifier the driver reports for it. The base image MUST NOT be started for a
  job, written or modified, and removal MUST discard the clone. The offline suite proves this with the fake driver
  (no start, write or delete call targets the base image). For a real image the claim is owner-run evidence: the owner
  compares a host-computed digest of the image files before and after a run. The driver-reported identifier alone is
  not proof. A base image that does not exist, is running or locked, or is not usable as a clone source MUST be refused
  with `refused.base_image_unavailable` before any clone call.
- **FR-007**: `whr` MUST NOT create, download, build, publish, sign or redistribute base images, and MUST NOT store or
  relay guest login credentials. The guest account the driver uses is part of the owner's base image and appears in
  configuration only as a reference. The documentation MUST describe a reproducible recipe that starts from the operating
  system vendor's official download and keeps the image local. The recipe is documentation, not a `whr` command (OD-4).
- **FR-008**: Every instance name MUST be a reserved prefix plus the instance id. For `name_and_record`, ownership is
  proven only when the name matches and a provider-owned local record exists and agrees. The record MUST carry the full
  ownership label set of the Feature 008 data model (the sandbox marker, id, workspace, profile, provider, retention,
  created, schema, gitprofile and shared_git_metadata), and ownership is not proven if any of them is missing or
  disagrees. `List` MUST return only owned instances. `Create`, `Exec`, `Cancel` and `Remove` MUST re-verify ownership
  before acting, as in the Feature 008 contract, and `Export` resolves its instance only through the owned record. A
  foreign VM, or one that lacks the labels or the record, MUST be refused with `refused.foreign_object` and zero
  mutations. The "when" text of `refused.foreign_object` becomes exactly: "a target object lacks the ownership labels, or, for
  `name_and_record`, its name or provider record is missing or disagrees". No new code is needed.
- **FR-009**: The provider MUST declare a concurrency limit derived from the driver and the host. Concurrency is runtime
  state, so it is not an `Admit` input: `Admit` stays a pure function of request and capabilities (Feature 008 FR-013).
  The limit is an explicit provider check immediately before the driver is touched in `Create`. It MUST be atomic: under
  an exclusive lock on the local record store the provider counts owned instances that hold resources, and if the count
  is below the limit it writes a reservation record in the same locked step; otherwise it refuses with
  `refused.concurrency_limit_reached` and makes no driver call. A reservation counts toward the limit while pending and records the owner process id, start time and a deadline. Under
  the same lock, a reservation whose process is gone or whose deadline passed is reclaimed if no matching driver
  instance exists; if one exists it becomes a leftover for `Remove` and keeps counting toward the limit until `Remove` clears it. The reservation is released when `Create` fails and
  on `Remove`. Only owned instances count; foreign running VMs are reported as information. A driver that declares
  `unknown` is treated as a limit of one.
- **FR-010**: An instance is `ready` only after guest reachability is confirmed within a bounded deadline. A failed
  `Create` or start MUST remove only what it created and report leftovers by name and id.
- **FR-011**: `Cancel` and `Remove` MUST be bounded, idempotent, stop gracefully and then force within the deadline,
  allow `Export` from a stopped or failed clone before removal, and report leftovers.

**Execution and files**

- **FR-012**: Arguments MUST reach the guest as data (Feature 008 FR-029). `exec_argv` is `native`, or `quoted_verified`
  only when the conformance corpus round-trips byte-exact through the guest shell the provider declares. The corpus is:
  empty argument, spaces, single and double quotes, newline, tab, `$`, backtick, backslash, `;`, `&&`, glob characters,
  leading dash, non-ASCII text, and a 64 KiB argument. A provider without proof MUST report `unavailable` and MUST
  refuse every `Exec`, under every profile, with `refused.exec_argv_unavailable`, because Feature 008 FR-029 applies to
  all of them.
- **FR-013**: Environment filtering, identity variables and git-control variable handling are those of Feature 008
  FR-023. No host credential enters the guest.
- **FR-014**: The default filesystem mode is `private_copy` through the driver's copy facility. A host directory may be
  shared with the guest only as a declared Plan mount, read-only by default. Forbidden in every plan: the host home
  directory, the secret-pattern paths of Feature 008, the primary `.git` for untrusted work, and any host socket.
- **FR-015**: Egress enforcement is reported `none` unless the provider's tests prove otherwise. A default guest network
  is not a claim.
- **FR-016**: An interactive shell is offered only where `interactive_shell=pty`, with the terminal-safety rules of the
  local shell of Feature 006. Otherwise a request for one is refused with `refused.interactive_unavailable`.

**Host boundary**

- **FR-017**: The provider MUST change only instances it owns. It MUST NOT change host virtualization, network, firewall,
  security or system settings, and MUST NOT install or ship a VM backend. The user installs the backend and holds its
  license, as with the container runtime prerequisite (OD-5).
- **FR-018**: `whr doctor` MUST gain read-only checks `vm.backend` (present, version, supported host), `vm.base_image`
  (the named image exists) and `vm.limit` (reports the declared concurrency limit, the number of owned instances and pending reservations from
  the local records, stale reservations, and whether a `Create` would be refused now). They read local records and use only the driver's
  `probe` and `list`; they never start a VM.

**Test run**

- **FR-019**: A test run MUST be expressed through the sandbox lifecycle of Feature 008: admit, create, copy in, exec,
  export, remove. No VM-specific command family and no wrapper command are added here.
- **FR-020**: Results (exit status, logs, declared artifacts) MUST survive removal. After an interrupted run and `Remove`,
  no owned instance may remain, and leftovers MUST be reported.

**Driver port and vendor neutrality**

- **FR-021**: A driver port is the only seam to a VM backend: probe, list, clone, start (headless, resources, declared
  shares), address and readiness, exec, copy in and out, stop, delete. Drivers hold no policy. Admission, plans,
  ownership and refusals live in the provider.
- **FR-022**: VM backend names MUST NOT appear in the contract, data model, admission function, refusal codes,
  conformance suite or user-facing command text. A repository check (`scripts/check_repo.py`) enforces it:
  - Listed names are a reviewed constant in the check script, matched case-insensitively as whole words, so a name
    never matches inside a longer word. The initial list is in the informative notes. It grows when a driver is added.
  - Docker is the container runtime named throughout Feature 008 and is not a VM backend; it is not on the list.
  - Gated files: the Feature 008 contract files and data model, `apps/whr/internal/runtime/` except driver packages,
    the conformance suite, the command text under `apps/whr/` (the registry, help, output and error strings in non-test files; test files are
    not gated), and the normative sections of this specification.
  - Allowed places: driver packages (`apps/whr/internal/runtime/drivers/<name>/`), their tests, adapter documentation
    (`docs/drivers/`), and regions of this specification between the HTML comment markers `informative:begin` and
    `informative:end` (the check recognizes a marker only when the comment stands alone on its line), which hold the owner decisions and the informative notes. Marked regions hold no MUST, SHOULD or MAY; the check
    looks for these uppercase words, matched case-sensitively, and fails if one occurs.
- **FR-023**: Exactly one driver MUST be chosen explicitly, by configuration or provider id. There MUST be no
  auto-detection and no fallback or substitution. A missing or unsupported driver is reported by `vm.backend` and the
  request MUST be refused with `refused.driver_unavailable` before any side effect. Swapping the driver changes admission only through declared capability values.
- **FR-024**: Each driver MUST publish a support matrix per release that lists the supported host (OS and architecture)
  and the supported guest (OS and architecture) separately, plus limits and a license note that names the backend's
  license as the user's concern. It states what the driver's tests prove, not portability.

**Conformance and evidence**

- **FR-025**: The conformance suite of Feature 008 MUST gain cases for: guest and architecture mismatch with zero driver
  calls; concurrency limit; ownership with a prefix-collision foreign VM; base image content id unchanged after a run;
  the hostile argv corpus; cleanup after a killed run; `unknown` never satisfying a requirement; and the honesty rule that
  a declared `enforced` field needs a passing probe.
- **FR-026**: The default offline `make check` MUST run the suite against a fake driver with injected failures: no VM,
  no network, no hypervisor. Real-VM conformance is run by the owner on their Mac under explicit approval and recorded as
  evidence. It is not a required CI check.

**Cross-platform CI evidence**

- **FR-027**: This repository's CI MUST run its Python and Go suites on macOS and Linux hosted runners. Today its jobs
  run on an ubuntu-based Linux runner image and its macOS jobs run only selected Go tests, so the Python suite has no
  macOS evidence here. Each job MUST bind its verdict to the exact tested head, and the aggregate `check` MUST require
  both. The exact runner images are pinned in the plan. This does not cover BundleDock or Anchorage, which are separate
  repositories needing their own CI change (owner follow-up, outside this feature).
- **FR-028**: In this repository, a test that fails on a platform MUST NOT be silently skipped. A skip MUST name
  platform, reason and tracking reference, and a repository script MUST check it. Each failure is classified
  `product_defect`, `test_portability_defect` or `host_environment`, with evidence.
- **FR-029**: Windows is out of scope (OD-6). CI has no Windows runner, `whr` declares no Windows host support, and no
  Windows guest provider is specified. Running Linux tests in another environment on a Windows host is not a stated
  target and carries no commitment.

**Reserved guest value**

- **FR-030**: `guest_os=windows` MUST remain a reserved, representable value in the contract. No provider declares it,
  and a request for it MUST be refused with `refused.guest_os_unavailable` until one does.

### Appended refusal codes

| Code | When |
| --- | --- |
| `refused.guest_os_unavailable` | the provider does not declare the requested guest operating system as `enforced` |
| `refused.guest_arch_unavailable` | the provider does not declare the requested guest architecture as `enforced` |
| `refused.concurrency_limit_reached` | creating the instance would exceed the declared concurrency limit |
| `refused.tool_mounts_unavailable` | `ro_bdock` is requested and the guest cannot mount tool bundles |
| `refused.base_image_unavailable` | the named base image does not exist, is running or locked, or is not usable as a clone source |
| `refused.exec_argv_unavailable` | `exec_argv` is `unavailable`; every `Exec` under every profile is refused |
| `refused.driver_unavailable` | no driver is chosen, or the chosen driver is missing or unsupported on this host |
| `refused.interactive_unavailable` | an interactive shell is requested and `interactive_shell` is `none` |

Codes are appended to `refusal-codes.json` of Feature 008 and never reused or renamed.

### Key Entities *(include if feature involves data)*

BaseImageRef, DisposableClone, OwnershipRecord, GuestPlatform (`guest_os`, `guest_arch`), VMDriver (port),
DriverSupportMatrix, TestRunResult, PlatformVerdict, SkipRecord.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The admission table (guest, architecture, tool mount, interactive shell by provider capability)
  yields exactly the expected decision and code for every row, with zero driver calls on refusal (FR-001, FR-004,
  FR-002, FR-005, FR-016). The concurrency limit is not an admission row (FR-009, SC-013).
- **SC-002**: Every Feature 008 admission row, run with the container provider report of FR-003, yields its previous
  decision and code, checked by a regression row set against the unamended `Admit` (FR-003).
- **SC-003**: The conformance suite passes unchanged for the fake provider, the container provider and the fake VM
  provider, and a second fake provider (a second fake driver) is added in a test only, with no change to production
  contract code (FR-021 to FR-023, FR-025; terms of Feature 008 SC-010).
- **SC-004**: The hostile argv corpus round-trips byte-exact for every guest shell model a provider declares, and a
  provider reporting `exec_argv=unavailable` refuses every `Exec` with `refused.exec_argv_unavailable` (FR-012).
- **SC-005**: A foreign VM with the reserved prefix and no record, and an instance whose record lacks any label of the
  Feature 008 ownership set, are refused with `refused.foreign_object` and zero mutations (FR-008).
- **SC-006**: After a killed run and `Remove`, zero owned instances remain, leftovers are listed and results are retained
  (FR-011, FR-020).
- **SC-007**: With the fake driver, no start, write or delete call ever targets the base image, and the driver-reported
  base image identifier in the Plan equals the identifier after every run, including failed and canceled ones (FR-006).
  For a real image this is owner-run evidence only: the owner's real-VM run compares a host-computed digest of the image
  files before and after. The offline suite proves nothing about a real image.
- **SC-008**: A failure injected on one platform turns exactly that platform's job and the aggregate check red; all
  platforms green yields a green aggregate bound to the head SHA (FR-027).
- **SC-009**: The vendor-name gate (FR-022) fails on fixtures that put a listed name in a gated file outside the marked
  informative region, passes on the same name inside a driver location or the marked region, and does not fire on
  "start" or "restart" (whole-word matching). It also fails on a fixture where a marker comment does not stand alone on
  its line (the marker is not recognized, so the names after it are gated), and on a fixture with an uppercase MUST,
  SHOULD or MAY inside a marked region (FR-022).
- **SC-010**: The `vm.*` doctor checks start no VM and no process other than the driver's declared read-only `probe`
  and `list`: the recorded driver calls are only those, no `exec` or `start` is recorded, and the file tree is
  unchanged (FR-018).
- **SC-011**: `make check` passes offline with no VM backend installed (FR-026).
- **SC-012**: The skip-record check fails a skip that lacks platform, reason or tracking reference, and passes a complete
  one (FR-028).
- **SC-013**: Two simultaneous `Create` requests with a limit of one yield exactly one instance and one
  `refused.concurrency_limit_reached`; the refused one made zero driver calls; the reservation is released on failed
  create and on `Remove`; a reservation left by a killed `Create` is reclaimed and the next `Create` succeeds; a stale reservation with a matching driver instance keeps counting and the next `Create` is
  refused until `Remove` clears it (FR-009).
- **SC-014**: A request for an interactive shell against `interactive_shell=none` is refused with
  `refused.interactive_unavailable`; against `pty` it follows the terminal-safety rules (FR-003, FR-016).
- **SC-015**: Plans containing the host home directory, a secret-pattern path, the primary `.git` for untrusted work or
  a host socket are refused; a read-write shared mount is not planned by default (FR-014).
- **SC-016**: No code path or command of `whr` creates, downloads, builds, publishes or signs a base image, and no
  configuration field holds a guest credential, only a reference (FR-007). No host virtualization, network, firewall or
  security setting is changed and no backend is installed; the recorded calls touch owned instances only (FR-017).
- **SC-017**: A start failure leaves no owned instance and lists leftovers by name and id; readiness is reported only
  after the bounded reachability check (FR-010).
- **SC-018**: The guest environment equals the Feature 008 FR-023 result and contains no host credential variable
  (FR-013); egress is reported `none` for a provider without a passing probe (FR-015).
- **SC-019**: A test run is expressed with the lifecycle operations of Feature 008 only; no command is added to the
  command registry (FR-019).
- **SC-020**: Each driver release ships a support matrix with host and guest listed separately and a license note; a
  repository check fails on a missing matrix (FR-024).
- **SC-021**: A request for `guest_os=windows` is refused with `refused.guest_os_unavailable` by every provider that
  does not declare it, and the value stays representable in the contract (FR-030).
- **SC-022**: With two fake drivers and none chosen, the request is refused with `refused.driver_unavailable` and zero
  driver calls. With a chosen driver that is missing or unsupported on this host, it is refused with the same code and
  the only driver call is that driver's read-only `probe`. With one supported driver chosen, only that driver records
  calls (FR-023).
- **SC-023**: A missing, running, locked or unusable base image is refused with `refused.base_image_unavailable` and
  zero clone calls (FR-006).
- **SC-024**: No CI job, check or document of this repository declares Windows host support or a Windows runner
  (FR-029).

## Author proposals (owner may change)

These choices were made by the author to stay conservative. The owner may change any of them; none alters OD-1 to OD-6.

- The container provider reports `interactive_shell=none`, because the Feature 008 contract defines no interactive shell.
- The container provider reports `concurrency_limit=unknown`, and a driver declaring `unknown` is treated as a limit of one.
- `guest_arch` is `unknown` when the runtime architecture cannot be probed.
- The initial gated name list (FR-022) includes the name of a Linux-on-Windows layer, although Windows is out of scope.
- The owner decision text is kept inside the informative markers.
- A missing or unsupported driver is refused with the new `refused.driver_unavailable` (eight new codes in total). ADR
  0013 has no configuration-error category, so a configuration error exit was not used.

## Assumptions

- The Feature 008 trust profiles and plan model are reused. A test run uses a job profile with a private copy and a
  scratch home. This feature does not redefine them.
- Real-VM evidence comes from the owner's Mac. Hosted CI runners are not assumed to support nested virtualization.
- Upstream facts about any VM backend were read from public documentation and secondary sources on 2026-10-31. They are
  not acceptance evidence, and each driver proves its own claims through its tests.

## Open Questions

None. NC-1 to NC-3 were decided on 2026-10-31 and are recorded as OD-4 to OD-6.

### Verification before the plan (checks, not decisions)

- The full text of the first backend's license and its maintenance status after the reported change of ownership. Only
  search excerpts and a summary were read so far. Under OD-5 the license is the user's concern, so this check informs
  the support matrix (FR-024) and the maintenance risk, not the design.
- The operating system vendor's license terms for running macOS in a VM, including any limit on concurrent VMs. The
  owner checks this (OD-4). It was not checked here, and the concurrency limit of FR-009 is declared by the driver.

### Follow-up outside this feature (owner)

- BundleDock and Anchorage are separate repositories. Their cross-platform CI, and the classification of their failing
  Python tests, need their own change in those repositories (FR-028 applies only here).

## Changes to existing documents (on approval; each lands through the plan and tasks)

- `specs/008-workspace-sandbox/spec.md`: FR-012 (capability list gains the fields of FR-001 and FR-004), FR-034 (the
  exception of A1 and the new refusal paths) and, for the new codes, the error mapping of FR-035 (capability refusals
  and `refused.driver_unavailable` map to 68 like the existing refusals; ADR 0013 has no configuration-error code).
- `specs/008-workspace-sandbox/data-model.md`: "the eleven fields" becomes eighteen; the ownership description
  covers `name_and_record` (FR-008).
- `specs/008-workspace-sandbox/contracts/runtime-provider.md`: the new fields with the container provider values of
  FR-003; the re-verification sentence; conformance cases 1 ("all eleven fields"), 4 (label-based ownership, now per
  `ownership_mechanism`), 6 ("argv as data, no shell": shell transport allowed only under `exec_argv=quoted_verified`
  with the FR-012 corpus round-tripping byte-exact) and the new cases of FR-025.
- `specs/008-workspace-sandbox/contracts/refusal-codes.json`: append the eight codes above and widen the "when" text of
  `refused.foreign_object` generically (FR-008). Codes are not renamed.
- The Feature 008 conformance suite and `Admit` (`apps/whr/internal/runtime/`): new cases and the admission paths for
  the new refusals. The existing table rows stay unchanged (FR-003).
- `contracts/cli/doctor-checks.json` and its freeze test `TestDoctorChecksMatchContract`: add `vm.backend`,
  `vm.base_image` and `vm.limit`, and amend the process rule to allow the driver's fixed read-only `probe` and `list`
  invocations within the shared 5 s budget (owner-gated like other contract changes).
- `docs/container-runtime.md`: add a VM backend prerequisite section; the Windows host statement stays unchanged.
- `.github/workflows/check.yml` and `scripts/check_ci.py`: Python and Go suites in a macOS and Linux matrix for this
  repository, aggregate check, skip-record check.
- Documentation of the base image recipe (FR-007), outside the product commands.
- `scripts/check_repo.py`: vendor-name gate of FR-022.
- `apps/whr/internal/runtime/` (ADR 0010 layout): VM provider, driver port, fake driver and the added conformance cases.

## Informative notes

<!-- informative:begin -->
These notes name products. They are not requirements and the vendor-name gate of FR-022 skips this marked region.

- The first driver targets Tart (OD-5). A driver for a Linux host would likely use KVM. A driver for Hyper-V is only a
  possibility.
- Initial list for the gate of FR-022: tart, lima, utm, qemu, parallels, vmware, virtualbox, hyper-v, kvm, libvirt,
  vagrant, multipass, firecracker, wsl.
- Docker is the container runtime of Feature 008 and is outside the VM backend list of FR-022.
- Running Linux tests inside WSL 2 on a Windows host is possible and is not a target (FR-029).
<!-- informative:end -->
