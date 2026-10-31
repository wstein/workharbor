# Feature Specification: VM provider and cross-platform test evidence

**Feature identifier**: `011-vm-provider`
**Feature Branch**: `docs/spec011-vm-provider`
**Created**: 2026-10-31
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md).
**Related**: [Feature 008](../008-workspace-sandbox/spec.md) (RuntimeProvider contract, FR-012 to FR-018, FR-029, FR-034),
[Feature 009](../009-host-setup-wizard/spec.md) (host setup), [Feature 006](../006-whr-cli/spec.md),
[container runtime prerequisite](../../docs/container-runtime.md).

## Context and scope

Feature 008 defines a RuntimeProvider contract and one container provider. Two needs are not covered:

1. **Evidence on other operating systems.** Part of the Python test suites of BundleDock and Anchorage fails on the
   owner's Mac. Whether each failure is a product defect, a test portability defect or a host environment effect is
   unknown, because the repository CI runs the Python suite on Linux only.
2. **Disposable guests with their own kernel and their own operating system**, driven through the same sandbox lifecycle
   as containers, first for macOS on the owner's Mac.

This feature defines:

1. a **cross-platform CI evidence path** on macOS and Linux hosted runners, independent of any provider;
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
  needs no further contract change (FR-030). Linux tests may also run under WSL 2, which is not a stated target.

### Boundaries

- Feature 008 stays in force. This feature changes it only through amendment A1 (FR-001 to FR-005) and appended
  refusal codes. Every Feature 008 admission decision is unchanged (FR-003).
- A1 is the single exception to the sentence of Feature 008 FR-034 that adding a provider changes no contract: the
  contract had no platform dimension. A1 is generic. After it, adding a driver changes no contract text.
- `whr` does not create, download, build, publish, sign or redistribute base images (FR-007).
- `whr` does not install a VM backend and changes no host virtualization, network, firewall or security setting
  (FR-017). Installing a backend on macOS would be a separate Feature 009 decision.
- Not in this increment (each needs its own specification): a Windows guest provider, Windows as a product target of
  `whr` (a different tool would be a different specification), a driver for a Linux host (KVM is the likely next one),
  remote VM hosts, snapshots and rollback, egress enforcement, credential mediation, a wrapper command for test runs
  (needs an ADR 0014 amendment).
- Windows is out of scope (FR-029). The [container runtime](../../docs/container-runtime.md) statement on Windows hosts
  is unchanged.
- No claim is made of universal containment, performance or portability. Every protection is stated with its scope.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Test results on macOS and Linux (Priority: P1)

A maintainer opens a pull request. The Python and Go suites run on macOS and Linux hosted runners. Each job reports a
verdict for the exact head under test, and one aggregate check requires both.

**Why this priority**: it answers the open question about the failing tests at the lowest cost and needs no VM.

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

**Independent Test**: admission table of guest, architecture, concurrency and tool mount against provider capabilities,
with the expected code and zero driver calls per refusal row.

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

- The base image is missing, or it is running or locked by another process.
- Clone succeeds and start fails; the guest never becomes reachable within the deadline.
- The host sleeps or the VM backend restarts during a run; the driver's state differs from the provider's record.
- A foreign VM has the reserved name prefix but no provider record.
- Two runs are requested at once with a limit of one.
- The guest has no execution agent, so `exec` is unavailable and capability `exec_argv` is `unavailable`.
- An argument is empty, contains a newline, or is 64 KiB long.
- Disk is full during clone.
- A test is skipped on a platform without a recorded reason.

## Requirements *(mandatory)*

### Functional Requirements

**Amendment A1 to the Feature 008 contract**

- **FR-001**: The capability report MUST gain `guest_os` (`linux`, `darwin`, `windows`) and `guest_arch` (`arm64`,
  `amd64`), each with value status and scope under Feature 008 FR-015. The container provider reports `linux` and the
  architecture of its Docker VM. A request naming a `guest_os` or `guest_arch` that is not `enforced` MUST be refused with
  `refused.guest_os_unavailable` or `refused.guest_arch_unavailable` before any side effect. There is no fallback.
- **FR-002**: `kernel_isolation=microvm` means a dedicated guest kernel and says nothing about the guest operating
  system. A request that needs a specific guest MUST name `guest_os`. A provider MUST NOT satisfy a `guest_os` request with
  another operating system, and Feature 008 FR-014 applies unchanged.
- **FR-003**: Amendment A1 MUST be additive and name no vendor. Every row of the Feature 008 admission table MUST yield
  the same decision and code as before, checked by a regression row set.
- **FR-004**: The capability report MUST gain these fields, each with status and scope:
  `base_image` (`read_only_clone_source`, `none`); `concurrency_limit` (a non-negative integer or `unknown`);
  `exec_argv` (`native`, `quoted_verified`, `unavailable`); `ownership_mechanism` (`native_labels`,
  `name_and_record`); `interactive_shell` (`pty`, `none`).
- **FR-005**: A provider whose guest cannot mount the tool bundles reports `tool_mounts=none`. A request for `ro_bdock`
  against it MUST be refused with `refused.tool_mounts_unavailable`.

**VM lifecycle**

- **FR-006**: An instance MUST be created only as a disposable clone of a base image the owner named. The Plan MUST
  record the base image name and the content or version id the driver reports. The base image MUST NOT be started for a
  job, written or modified, and removal MUST discard the clone.
- **FR-007**: `whr` MUST NOT create, download, build, publish, sign or redistribute base images, and MUST NOT store or
  relay guest login credentials. The guest account the driver uses is part of the owner's base image and appears in
  configuration only as a reference. The documentation MUST describe a reproducible recipe that starts from the operating
  system vendor's official download and keeps the image local. The recipe is documentation, not a `whr` command (OD-4).
- **FR-008**: Every instance name MUST be a reserved prefix plus the instance id. For `name_and_record`, ownership is
  proven only when the name matches and a provider-owned local record (instance id, base image id, creation time, owner
  marker) exists and agrees. `List` MUST return only owned instances. `Create`, `Exec`, `Export`, `Cancel` and `Remove`
  MUST re-verify ownership before acting. A foreign VM with the reserved prefix and no record MUST be refused with zero
  mutations.
- **FR-009**: The provider MUST declare a concurrency limit derived from the driver and the host. A `Create` above the
  limit MUST be refused with `refused.concurrency_limit_reached` before any side effect. Only owned instances count;
  foreign running VMs are reported as information.
- **FR-010**: An instance is `ready` only after guest reachability is confirmed within a bounded deadline. A failed
  `Create` or start MUST remove only what it created and report leftovers by name and id.
- **FR-011**: `Cancel` and `Remove` MUST be bounded, idempotent, stop gracefully and then force within the deadline,
  allow `Export` from a stopped or failed clone before removal, and report leftovers.

**Execution and files**

- **FR-012**: Arguments MUST reach the guest as data (Feature 008 FR-029). `exec_argv` is `native`, or `quoted_verified`
  only when the conformance corpus round-trips byte-exact through the guest shell the provider declares. The corpus is:
  empty argument, spaces, single and double quotes, newline, tab, `$`, backtick, backslash, `;`, `&&`, glob characters,
  leading dash, non-ASCII text, and a 64 KiB argument. A provider without proof MUST report `unavailable` and MUST
  refuse job profiles with `refused.exec_argv_unavailable`.
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
  license, as with the Docker runtime (OD-5).
- **FR-018**: `whr doctor` MUST gain read-only checks `vm.backend` (present, version, supported host), `vm.base_image`
  (the named image exists) and `vm.limit`. They never start a VM.

**Test run**

- **FR-019**: A test run MUST be expressed through the sandbox lifecycle of Feature 008: admit, create, copy in, exec,
  export, remove. No VM-specific command family and no wrapper command are added here.
- **FR-020**: Results (exit status, logs, declared artifacts) MUST survive removal. After an interrupted run and `Remove`,
  no owned instance may remain, and leftovers MUST be reported.

**Driver port and vendor neutrality**

- **FR-021**: A driver port is the only seam to a VM backend: probe, list, clone, start (headless, resources, declared
  shares), address and readiness, exec, copy in and out, stop, delete. Drivers hold no policy. Admission, plans,
  ownership and refusals live in the provider.
- **FR-022**: Vendor names MUST NOT appear in the contract, data model, admission function, refusal codes, conformance
  suite or user-facing command text. They appear only in a driver's package, its tests and adapter documentation, and in
  the informative notes of this specification.
- **FR-023**: Driver selection MUST be explicit (configuration or flag). There is no auto-detection and no substitution.
  Swapping the driver changes admission only through declared capability values.
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

- **FR-027**: Repository CI MUST run the Python and Go suites on macOS and Linux hosted runners. Each job MUST bind its
  verdict to the exact tested head, and the aggregate `check` MUST require both. The exact runner images are pinned in
  the plan.
- **FR-028**: A test that fails on a platform MUST NOT be silently skipped. A skip MUST name platform, reason and tracking
  reference, and a repository script MUST check it. Each failure is classified `product_defect`,
  `test_portability_defect` or `host_environment`, with evidence.
- **FR-029**: Windows is out of scope (OD-6). CI has no Windows runner, `whr` declares no Windows host support, and no
  Windows guest provider is specified. Linux tests may run under WSL 2, which is not a stated target and carries no
  commitment.

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
| `refused.base_image_unavailable` | the named base image does not exist or is not usable as a clone source |
| `refused.exec_argv_unavailable` | a job profile needs argv as data and the provider has no proof (`unavailable`) |
| `refused.interactive_unavailable` | an interactive shell is requested and `interactive_shell` is `none` |

Codes are appended to `refusal-codes.json` of Feature 008 and never reused or renamed.

### Key Entities *(include if feature involves data)*

BaseImageRef, DisposableClone, OwnershipRecord, GuestPlatform (`guest_os`, `guest_arch`), VMDriver (port),
DriverSupportMatrix, TestRunResult, PlatformVerdict, SkipRecord.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The admission table (guest, architecture, concurrency, tool mount by provider capability) yields exactly the
  expected decision and code for every row, with zero driver calls on refusal (FR-001, FR-004, FR-005, FR-009).
- **SC-002**: Every Feature 008 admission row yields its previous decision and code (FR-003).
- **SC-003**: The conformance suite passes unchanged for the fake container provider and the fake VM provider, and a
  second fake driver is added in a test only, with no change to production contract code (FR-021 to FR-023).
- **SC-004**: The hostile argv corpus round-trips byte-exact for every guest shell model a provider declares (FR-012).
- **SC-005**: A foreign VM with the reserved prefix and no record is refused with zero mutations (FR-008).
- **SC-006**: After a killed run and `Remove`, zero owned instances remain, leftovers are listed and results are retained
  (FR-011, FR-020).
- **SC-007**: The base image content id is unchanged after every run, including failed and canceled ones (FR-006).
- **SC-008**: A failure injected on one platform turns exactly that platform's job and the aggregate check red; all
  platforms green yields a green aggregate bound to the head SHA (FR-027).
- **SC-009**: A repository check fails when a vendor name appears outside the paths FR-022 permits (FR-022).
- **SC-010**: The `vm.*` doctor checks leave the file tree and the process list unchanged (FR-018).
- **SC-011**: `make check` passes offline with no VM backend installed (FR-026).

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

## Changes to existing documents (on approval; each lands through the plan and tasks)

- `specs/008-workspace-sandbox/contracts/runtime-provider.md`: add the fields of FR-001 and FR-004.
- `specs/008-workspace-sandbox/contracts/refusal-codes.json`: append the seven codes above.
- `docs/container-runtime.md`: add a VM backend prerequisite section; the Windows host statement stays unchanged.
- `.github/workflows/check.yml` and `scripts/check_ci.py`: Python and Go suites in a macOS and Linux matrix, aggregate
  check, skip-record check.
- Documentation of the base image recipe (FR-007), outside the product commands.
- `scripts/check_repo.py`: vendor-name gate of FR-022.
- `apps/whr/internal/runtime/` (ADR 0010 layout): VM provider, driver port, fake driver and the added conformance cases.
