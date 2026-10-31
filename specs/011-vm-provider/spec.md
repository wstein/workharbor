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
5. a first increment that is library-level and offline, using a fake driver; and
6. a minimal macOS host surface that stays read-only or removes owned leftovers only: a VM readiness summary at the end of
   `whr setup` (FR-031), a space report in `whr doctor` (FR-032) and a cleanup of owned leftovers in `whr doctor --fix`
   (FR-033).

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
  (FR-017). Installing a backend on macOS would be a separate Feature 009 decision. `whr setup` and `whr doctor` only
  check and guide (FR-031, FR-032); `whr doctor --fix` removes owned leftovers only (FR-033).
- Not in this increment (each needs its own specification): a Windows guest provider, Windows as a product target of
  `whr` (a different tool would be a different specification), a driver for a Linux host, remote VM hosts, snapshots
  and rollback, egress enforcement, credential mediation, a wrapper command for test runs (needs an ADR 0014
  amendment), CI for the separate BundleDock and Anchorage repositories, and the items listed under "Future work (not
  started)".
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

**Independent Test**: a fake driver records every driver call. Apart from the read-only `probe` and `list`, the calls
are clone, start, ready, copy in, exec, copy out, stop, delete, in that order. The clone call names the base image only as its read-only source, and no start, write or delete call
targets it. After the run no owned instance exists, results are present, and the base image content id is unchanged. Real-VM
evidence is produced by the owner on their Mac (FR-026).

**Acceptance Scenarios**:

1. **Given** a named base image and a declared argv, **When** the run completes, **Then** exit status, logs and declared
   artifacts are in the result directory and the clone is gone.
2. **Given** the test command fails, **Then** results are still exported and the clone is still removed.
3. **Given** the base image does not exist, **Then** the request is refused with `refused.base_image_unavailable` and
   nothing is created.

### User Story 3 - Refusal of what the provider cannot do (Priority: P2)

A request names a guest, a limit or a mount the provider cannot satisfy. It is refused with a stable code before any
side effect.

**Independent Test**: admission table of guest, architecture, base image, tool mount and interactive shell against provider capabilities,
with the expected code and zero driver calls per refusal row. The concurrency limit is not an admission row; it is
tested by the provider check of FR-009 (SC-013).

**Acceptance Scenarios**:

1. **Given** `guest_os=windows` and a provider reporting `darwin`, **Then** `refused.guest_os_unavailable`.
2. **Given** a provider whose declared concurrency limit has status `enforced` and is already reached, **Then**
   `refused.concurrency_limit_reached` and nothing is created. A provider that states no limit (`unknown`) or whose limit
   is not `enforced` applies none, so no concurrency refusal occurs (FR-009).
3. **Given** a request for `ro_bdock` tool mounts and a guest that cannot mount them, **Then**
   `refused.tool_mounts_unavailable`.

### User Story 4 - Swapping the driver changes no contract (Priority: P2)

A second driver for another VM backend is added.

**Independent Test**: a second fake driver with different declared capabilities is added in a test only. The conformance
suite passes unchanged, and no production contract, data model or admission file changes.

### User Story 5 - Interrupted run leaves nothing behind (Priority: P3)

A run is canceled or the host process is killed mid-run. Cleanup removes owned instances only and reports leftovers.
On the owner's Mac, `whr doctor --fix` can remove those owned leftovers after a preview (FR-033).

**Independent Test**: fake driver with injected failures at each step. After `Remove`, no owned instance remains, results
are retained, and leftovers are listed by name and id.

### Edge Cases

- The base image does not exist, is running or locked (for example by another process), or is not usable as a clone
  source: refused with
  `refused.base_image_unavailable`, nothing is created.
- Clone succeeds and start fails; the guest never becomes reachable within the deadline.
- The host sleeps or the VM backend restarts during a run; the driver's state differs from the provider's record.
- A foreign VM has the reserved name prefix but no provider record.
- Two runs are requested at once with an enforced declared limit of one (FR-009: exactly one reservation succeeds). With
  no limit stated both are created, and if the driver or host then refuses the second (for example a license cap of the
  operating system vendor), that is a failed `Create` under FR-010, not `refused.concurrency_limit_reached`.
- A leftover clone holds results that were never exported when `whr doctor --fix` is run (FR-033).
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
  `concurrency_limit=unknown` (FR-009 is a provider check of the VM provider, not an admission row, and for `unknown` no
  limit applies, so it cannot fail any existing row); `exec_argv=native` with status `enforced`, proven by conformance case 6 (Feature 008 FR-029 and case 6 already require
  argv as data);
  `ownership_mechanism=native_labels`; `interactive_shell=none` (the Feature 008 contract defines no interactive
  shell; conservative). Each of these values has status `enforced`, except `guest_arch` when it is `unknown`
  and `concurrency_limit=unknown`, in line with the container report of the Feature 008 contract. `Admit` reads `guest_os`, `guest_arch` and
  `interactive_shell` only when the request names that requirement, and reads `base_image` for every request (FR-006): a
  request that names a base image needs `read_only_clone_source`, and a request that names none needs `none`, which the
  container value admits. Every `Exec` reads `exec_argv`, and the container value `native` admits all existing rows. So
  a request that names none of these requirements gets the same decision from the container provider as before A1.
  Whether the named image exists, is running or locked, or is usable as a clone source is runtime state and is checked
  by the provider before any clone call, not by `Admit` (Feature 008 FR-013). Every row of the Feature 008 admission table (`TestAdmitTable`) MUST yield the same decision and code before
  and after A1, checked by a regression row set that runs the table against the container values above.
- **FR-004**: The capability report MUST gain these fields, each with status and scope:
  `base_image` (`read_only_clone_source`, `none`); `concurrency_limit` (a non-negative integer or `unknown`);
  `exec_argv` (`native`, `quoted_verified`, `unavailable`); `ownership_mechanism` (`native_labels`,
  `name_and_record`); `interactive_shell` (`pty`, `none`). With `guest_os` and `guest_arch` the report has eighteen
  fields (eleven in Feature 008).
- **FR-005**: A provider whose guest cannot mount the tool bundles reports `tool_mounts=none`. A request for `ro_bdock`
  against it, or against a provider whose `tool_mounts` is not `enforced`, MUST be refused with `refused.tool_mounts_unavailable`.

**VM lifecycle**

- **FR-006**: An instance MUST be created only as a disposable clone of a base image the owner named. The Plan MUST
  record the base image name and the identifier the driver reports for it. The base image MUST NOT be started for a
  job, written or modified, and removal MUST discard the clone. The offline suite proves this with the fake driver
  (no start, write or delete call targets the base image). For a real image the claim is owner-run evidence: the owner
  compares a host-computed digest of the image files before and after a run. The driver-reported identifier alone is
  not proof. Each of the following MUST be refused with `refused.base_image_unavailable` before any clone call: a base
  image that does not exist, is running or locked, or is not usable as a clone source; a request that names a base
  image against a provider whose `base_image` is not `read_only_clone_source` with status `enforced`; and a request
  that names no base image against a provider whose `base_image` is not `none`. The container provider reports
  `base_image=none` (FR-003), so nothing changes for it.
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
  mutations. `ownership_mechanism` is not a request requirement: a provider whose `ownership_mechanism` is not `enforced`
  proves no ownership, so it refuses every `Create`, `Exec`, `Export`, `Cancel` and `Remove` with
  `refused.foreign_object` before any side effect, and its `List` returns no instance. The "when" text of
  `refused.foreign_object` becomes exactly: "a target object lacks the ownership labels, or, for `name_and_record`, its
  name or provider record is missing or disagrees, or the provider's `ownership_mechanism` is not `enforced`". For
  the container provider, whose `ownership_mechanism` is `native_labels` with status `enforced` (FR-003), its meaning
  is unchanged. No new code is needed.
- **FR-009**: The provider MUST declare a concurrency limit derived from the driver and the host: a non-negative integer,
  or `unknown` when the driver does not state one. A limit applies only when it is an integer with status `enforced`.
  When the value is `unknown`, or its status is not `enforced`, no limit is applied (unlimited) and the provider never
  refuses with `refused.concurrency_limit_reached`. Concurrency is runtime state, so it is not an `Admit` input: `Admit`
  stays a pure function of request and capabilities (Feature 008 FR-013). The provider takes an exclusive lock on the local
  record store immediately before the driver is touched in `Create`. Under that lock, when a limit applies, the provider
  counts owned instances that hold resources; if the count is below the limit it writes a reservation record in the same
  locked step, otherwise it refuses with `refused.concurrency_limit_reached` and makes no driver call (the check is
  atomic). A limit of 0 refuses every `Create`. When no limit applies, the provider still writes the reservation record in
  the locked step, without a count check, so that a killed `Create` leaves a traceable record. A reservation records the
  owner process id, start time and a deadline; while pending it counts toward an applicable limit. Under the same lock, a
  reservation whose process is gone or whose deadline passed is reclaimed if no matching driver instance exists; if one
  exists it becomes a leftover for `Remove` and, when a limit applies, keeps counting toward it until `Remove` clears it.
  The reservation is released when `Create` fails and on `Remove`. Only owned instances count; foreign running VMs are
  reported as information. If the driver or host refuses a `Create` for a reason of its own, it is a failed `Create`
  (FR-010).
- **FR-010**: An instance is `ready` only after guest reachability is confirmed within a bounded deadline. A failed
  `Create` or start MUST remove only what it created and report leftovers by name and id.
- **FR-011**: `Cancel` and `Remove` MUST be bounded, idempotent, stop gracefully and then force within the deadline,
  allow `Export` from a stopped or failed clone before removal, and report leftovers.

**Execution and files**

- **FR-012**: Arguments MUST reach the guest as data (Feature 008 FR-029). `exec_argv` is `native`, or `quoted_verified`
  only when the conformance corpus round-trips byte-exact through the guest shell the provider declares. The corpus is:
  empty argument, spaces, single and double quotes, newline, tab, `$`, backtick, backslash, `;`, `&&`, glob characters,
  leading dash, non-ASCII text, and a 64 KiB argument. A provider without proof MUST report `unavailable` and MUST
  refuse every `Exec`, under every profile, also when the `exec_argv` status is not `enforced`, with `refused.exec_argv_unavailable`, because Feature 008 FR-029 applies to
  all of them.
- **FR-013**: Environment filtering, identity variables and git-control variable handling are those of Feature 008
  FR-023. No host credential enters the guest.
- **FR-014**: The default filesystem mode is `private_copy` through the driver's copy facility. A host directory may be
  shared with the guest only as a declared Plan mount, read-only by default. Forbidden as a shared mount in every
  plan: the host home directory, any secret-pattern path of Feature 008, the primary `.git` for untrusted work, and any
  host socket. The private copy follows the Feature 008 copy policy (FR-009) unchanged: a secret-pattern path is copied
  only when named and confirmed, otherwise it is refused with `refused.copy_policy_secret_path`.
- **FR-015**: Egress enforcement is reported `none` unless the provider's tests prove otherwise. A default guest network
  is not a claim.
- **FR-016**: An interactive shell is offered only where `interactive_shell=pty` with status `enforced`, with the terminal-safety rules of
  the local shell of Feature 006. Otherwise (value `none`, or `pty` not `enforced`) a request for one is refused with
  `refused.interactive_unavailable`.

**Host boundary**

- **FR-017**: The provider MUST change only instances it owns. It MUST NOT change host virtualization, network, firewall,
  security or system settings, and MUST NOT install or ship a VM backend. The user installs the backend and holds its
  license, as with the container runtime prerequisite (OD-5).
- **FR-018**: `whr doctor` MUST gain read-only checks `vm.backend` (present, version, supported host), `vm.base_image`
  (the named image exists) and `vm.limit` (reports the declared concurrency limit, or that no limit is stated or enforced and therefore none is
  applied, the number of owned instances and pending reservations from the local records, stale reservations, and
  whether a `Create` would be refused now for the concurrency limit, which is never the case when no limit applies). They read local records and use only the driver's
  `probe` and `list`; they never start a VM. A fourth read-only check, `vm.space`, is added by FR-032.

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
- **FR-029**: Windows is out of scope (OD-6). CI has no Windows runner, `whr` declares no Windows host support in any CI job, check or
  current document (the verbatim historical copies under `docs/archive/` excepted), and no
  Windows guest provider is specified. Running Linux tests in another environment on a Windows host is not a stated
  target and carries no commitment.

**Reserved guest value**

- **FR-030**: `guest_os=windows` MUST remain a reserved, representable value in the contract. No provider declares it,
  and a request for it MUST be refused with `refused.guest_os_unavailable` until one does.

**Host surface on macOS (check, guide, clean up owned leftovers)**

These requirements amend Feature 009 and `whr doctor` as listed under "Changes to existing documents". The container
provider and Linux hosts are unchanged.

- **FR-031**: On a macOS host, `whr setup` MUST end with a read-only "VM readiness" summary built from the same read-only
  checks as `whr doctor` (`vm.backend`, `vm.base_image`, `vm.limit`, and the report of FR-032), using the same fixed
  read-only `probe` and `list` invocations of the chosen driver within the shared 5 s budget of the doctor checks, and
  starting no VM. For each check that is not `pass` it MUST print the exact next steps the user performs: how to install and
  license the chosen VM tool (the user's own task, NC-2/OD-5, the documented steps of the driver, FR-024) and the documented
  recipe for building the base image locally (FR-007, OD-4). `whr setup` MUST NOT install a VM tool, build or download a base
  image, run sudo for this purpose, or add a catalogue entry: the Feature 009 catalogue stays at 13 entries. The summary is
  not a plan step and writes nothing, so it also appears when the answer is `no` and under `--dry-run`. Linux `whr setup`
  is unchanged.
- **FR-032**: `whr doctor` MUST gain a read-only check `vm.space` and report in it: the disk space taken by owned leftover
  instances, and the free disk space on the volume that holds the VM storage. It MUST report `fail` when that free space
  is below the size of the base image, and MUST say "size unknown" and report `unknown` rather than guess when the driver
  cannot state a size or the storage location. Sizes and the storage location are optional fields in the results of the
  existing driver `probe` and `list` (no new port operation, FR-021); the free space is measured by the host with a file
  system statistic. The check stays inside the shared 5 s budget and starts no VM.
- **FR-033**: On a macOS host, `whr doctor --fix` MUST remove owned leftovers only: instances left by a killed or canceled
  run, and stale reservations (FR-009), through the provider's `Remove` with the ownership check of FR-008 on every item. A
  foreign or unlabeled object MUST be refused with `refused.foreign_object` and never touched, an instance of a run that
  is still active is not a leftover, and nothing else is repaired. It uses the mechanics of Feature 009 FR-016, FR-007 and
  FR-017: the repair is a modify-class step (it removes something present), so without `--yes` it prints the preview
  (name, id and size if known, per item) and exits 70, and with `--yes` each item is removed only with its own
  `--confirm <id>`. The preview MUST show, for each leftover, whether its results were exported; a stopped or failed clone
  whose results were not exported is marked "results not exported" with the Export step to run first (FR-011). Because
  nothing is removed before the preview and a missing confirmation exits 70, the owner always sees this before any
  removal, and `--yes` alone never removes an item. Linux `whr doctor --fix` still refuses (Feature 009 FR-001).

### Appended refusal codes

| Code | When |
| --- | --- |
| `refused.guest_os_unavailable` | the provider does not declare the requested guest operating system as `enforced` |
| `refused.guest_arch_unavailable` | the provider does not declare the requested guest architecture as `enforced` |
| `refused.concurrency_limit_reached` | creating the instance would exceed the declared integer concurrency limit of FR-009 whose status is `enforced`; it never fires for `unknown` or for a limit that is not `enforced` |
| `refused.tool_mounts_unavailable` | `ro_bdock` is requested and `tool_mounts` is not `ro_bdock` with status `enforced` |
| `refused.base_image_unavailable` | no base image is named for a provider whose `base_image` is not `none`, or the named base image does not exist, is running or locked, is not usable as a clone source, or `base_image` is not `read_only_clone_source` with status `enforced` |
| `refused.exec_argv_unavailable` | `exec_argv` is `unavailable` or not `enforced`; every `Exec` under every profile is refused |
| `refused.driver_unavailable` | no driver is chosen, or the chosen driver is missing or unsupported on this host |
| `refused.interactive_unavailable` | an interactive shell is requested and `interactive_shell` is not `pty` with status `enforced` |

Codes are appended to `refusal-codes.json` of Feature 008 and never reused or renamed.

### Key Entities *(include if feature involves data)*

BaseImageRef, DisposableClone, OwnershipRecord, GuestPlatform (`guest_os`, `guest_arch`), VMDriver (port),
DriverSupportMatrix, TestRunResult, PlatformVerdict, SkipRecord, ConcurrencyReservation, VMReadinessSummary, LeftoverCleanupPlan.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The admission table (guest, architecture, base image, tool mount, interactive shell by provider capability)
  yields exactly the expected decision and code for every row, with zero driver calls on refusal (FR-001, FR-004,
  FR-002, FR-005, FR-006, FR-016). The concurrency limit is not an admission row (FR-009, SC-013).
- **SC-002**: Every Feature 008 admission row, run with the container provider report of FR-003, yields its previous
  decision and code, checked by a regression row set against the unamended `Admit` (FR-003).
- **SC-003**: The conformance suite passes unchanged for the fake provider, the container provider and the fake VM
  provider, and a second fake provider (a second fake driver) is added in a test only, with no change to production
  contract code (FR-021 to FR-023, FR-025; terms of Feature 008 SC-010).
- **SC-004**: The hostile argv corpus round-trips byte-exact for every guest shell model a provider declares, and a
  provider reporting `exec_argv=unavailable`, or `native` or `quoted_verified` with a status other than `enforced`,
  refuses every `Exec` with `refused.exec_argv_unavailable` (FR-012).
- **SC-005**: A foreign VM with the reserved prefix and no record, and an instance whose record lacks any label of the
  Feature 008 ownership set, are refused with `refused.foreign_object` and zero mutations. A provider whose `ownership_mechanism` is not `enforced` refuses every `Create`, `Exec`, `Export`,
  `Cancel` and `Remove` with `refused.foreign_object` before any side effect, and its `List` returns no instance
  (FR-008).
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
  unchanged. With `unknown` or a non-`enforced` limit, `vm.limit` reports that no limit applies and that a `Create` would
  not be refused for concurrency (FR-018).
- **SC-011**: `make check` passes offline with no VM backend installed (FR-026).
- **SC-012**: The skip-record check fails a skip that lacks platform, reason or tracking reference, and passes a complete
  one (FR-028).
- **SC-013**: With an `enforced` declared limit of one, two simultaneous `Create` requests yield exactly one instance and one
  `refused.concurrency_limit_reached`; the refused one made zero driver calls; the reservation is released on failed
  create and on `Remove`; a reservation left by a killed `Create` is reclaimed and the next `Create` succeeds; a stale
  reservation with a matching driver instance keeps counting and the next `Create` is refused until `Remove` clears it.
  With the value `unknown`, or an integer limit whose status is not `enforced`, no limit applies: the second `Create` is
  not refused for concurrency, no `refused.concurrency_limit_reached` is ever produced, and a reservation record is still
  written and a stale one is still handled as a leftover. A limit of 0 with status `enforced` refuses every `Create`
  (FR-009).
- **SC-014**: A request for an interactive shell against `interactive_shell=none`, or against `pty` that is not `enforced`, is
  refused with `refused.interactive_unavailable`; against an `enforced` `pty` it follows the terminal-safety rules (FR-003, FR-016).
- **SC-015**: Plans that share the host home directory, a secret-pattern path, the primary `.git` for untrusted work or
  a host socket as a mount are refused; a secret-pattern path in the private copy is refused unless named and confirmed
  (Feature 008 FR-009); a read-write shared mount is not planned by default (FR-014).
- **SC-016**: No code path or command of `whr` creates, downloads, builds, publishes or signs a base image, and no
  configuration field holds a guest credential, only a reference (FR-007). No host virtualization, network, firewall or
  security setting is changed and no backend is installed; the recorded mutating calls (start, write, stop, delete) target owned instances only, and the clone call reads the
  base image only as its source (FR-017).
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
- **SC-023**: A base image that does not exist, is running or locked, or is not usable as a clone source, a named base image against a provider whose
  `base_image` is not `read_only_clone_source` with status `enforced`, and a request naming no base image against a
  provider whose `base_image` is not `none`, are refused with `refused.base_image_unavailable`
  and zero clone calls (FR-006).
- **SC-024**: No CI job, check or current document of this repository (the verbatim historical copies under
  `docs/archive/` excepted) declares Windows host support or a Windows runner (FR-029).
- **SC-025**: On a macOS host, the end of `whr setup` shows the VM readiness summary with the fake driver in each state
  (no driver chosen, backend missing, base image missing, all ready): the exact next steps appear for every check that
  is not `pass`; the recorded driver calls are only `probe` and `list`; no VM is started, nothing is installed, built,
  downloaded or elevated, the catalogue still has 13 entries, and the file tree is unchanged by the summary. It also
  appears for the answer `no` and for `--dry-run`. Linux `whr setup` output is unchanged (FR-031).
- **SC-026**: With the fake driver, `vm.space` reports the space of owned leftovers and the free space of the storage volume;
  it reports `fail` when free space is below the base image size, `unknown` with "size unknown" when the driver states no
  size or location, and passes otherwise. The recorded driver calls are only `probe` and `list`, no VM is started, and the
  run stays inside the shared 5 s budget (FR-032).
- **SC-027**: On macOS with the fake driver and injected leftovers (a killed run, a canceled run, a stale reservation, an
  active run, a foreign object with the reserved prefix): `doctor --fix --dry-run` writes nothing; `doctor --fix`
  prints the preview and exits 70; `--yes` alone removes nothing; `--yes --confirm <id>` removes exactly the confirmed owned
  leftover through `Remove`; the foreign object is refused with `refused.foreign_object` and untouched; the active run is
  not listed as a leftover; Linux `doctor --fix` is refused (FR-033).
- **SC-028**: The `doctor --fix` preview marks every leftover whose results were not exported as "results not exported" with
  the Export step, for a stopped and for a failed clone, and marks exported ones as exported; the marking is present in
  the preview that precedes any removal (FR-033, FR-011).

## Owner decisions (confirmed 2026-10-31)

The owner decided the six choices that the author had proposed and added two points. The author proposals that remain
open for the owner are marked below. None alters OD-1 to OD-6.

1. **Interactive shell for the container provider (confirmed, follow-up)**: the owner wants an interactive shell for the
   container provider in the future. Until a separate feature builds it, the capability report stays honest (FR-025): the
   container provider reports `interactive_shell=none` (FR-003 unchanged). When that feature lands, the value becomes
   `pty` with status `enforced`. Follow-up: a separate feature "interactive shell for the container provider", out of
   scope here and not started.
2. **Concurrency limit (CHANGED)**: when the driver states no limit (`unknown`), no limit is applied (unlimited).
   Before, `unknown` was treated as a limit of one. A declared integer limit with status `enforced` still applies, with
   atomic reservations and reclaimed stale reservations (FR-009, SC-013). Author proposal, owner may change: a limit whose
   status is not `enforced` is treated like `unknown` (not applied). The operating system vendor's license may cap
   concurrent macOS VMs; this stays an owner check, unverified and unchanged (see "Verification before the plan").
3. **`guest_arch=unknown` when the architecture cannot be probed (confirmed, unchanged)**.
4. **The name of the Linux-on-Windows layer stays on the gated vendor-name list (confirmed, unchanged)**, although Windows
   is out of scope.
5. **Owner decision text stays inside the informative markers (confirmed, unchanged)**.
6. **A missing or unsupported driver is refused with the new `refused.driver_unavailable` (confirmed, unchanged)**, eight new
   codes in total. ADR 0013 has no configuration-error category.
7. **`whr setup` on macOS (NEW)**: minimal form "check and guide only". `whr setup` ends with a read-only VM readiness
   summary and the exact next steps, and installs, builds, downloads and elevates nothing (FR-031, SC-025). Later options
   (an opt-in catalogue entry for the VM tool, a separate `whr vm setup` command, a guided base image build) are future
   work only. Linux is unchanged.
8. **Scope of `whr doctor` VM support (NEW)**: A, B and C. A is read-only diagnosis with `vm.backend`, `vm.base_image` and
   `vm.limit` (FR-018). B is the read-only space report (FR-032). C is `whr doctor --fix` removing owned leftovers only
   (FR-033). D (`doctor --deep`, a real clone-start-exec-stop-delete smoke) is future work only. E (`doctor --fix` installs
   the VM tool or builds the base image) is rejected because it contradicts NC-1 and NC-2.

Author proposals made while recording these decisions (the owner may change them):

- A limit with status not `enforced` is treated like `unknown` (decision 2).
- `vm.space` reports `fail` below the base image size, like the existing disk checks, so a failing space check makes
  `whr doctor` exit 68 as for any failed check; the alternative is evidence only (FR-032).
- Sizes and the storage location are optional fields of the existing `probe` and `list` results, not a new port operation
  (FR-032).
- The cleanup keeps the Feature 009 requirement of a host profile (`host_setup.profile_absent`) and treats the removal as
  a modify-class step with one `--confirm <id>` per item, with the item id `vm-instance:<instance id>` or
  `vm-reservation:<reservation id>` (FR-033).
- Results not exported are shown and flagged in the preview, and removed once the owner confirms that item; the cleanup
  does not add a block or a new flag (FR-033).

## Future work (not started; each needs its own specification and is re-decided after the first real VM run)

- An interactive shell for the container provider (decision 1).
- For `whr setup`: an opt-in catalogue entry for the VM tool, a separate `whr vm setup` command, a guided base image build
  (decision 7).
- `whr doctor --deep`, a real clone-start-exec-stop-delete smoke (decision 8, D).
- The owner may later configure an explicit concurrency limit for a driver that states none.

## Assumptions

- Admit, TestAdmitTable, the conformance suite and `apps/whr/internal/runtime/` do not exist yet (Feature 008 tasks
  T001 and T002 are open), so the before-and-after regression check of FR-003 depends on Feature 008 landing first.
- The Feature 008 trust profiles and plan model are reused. A test run uses a job profile with a private copy and a
  scratch home. This feature does not redefine them.
- A driver that states no concurrency limit means no limit is applied (decision 2). The host or the driver can still
  refuse a `Create` for its own reasons; that is a failed `Create` (FR-010).
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
  owner checks this (OD-4). It was not checked here (unverified, unchanged). Because an unstated limit is not applied
  (FR-009), the provider does not enforce any such cap; the cap, if any, shows up as a driver or host refusal of a `Create`
  (FR-010).

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
  FR-003 and, per field, the code that refuses a request when the field is not `enforced`: `guest_os` and `guest_arch`
  (FR-001), `tool_mounts` (FR-005), `base_image` (FR-006, read for every request: a named image needs `read_only_clone_source`, no named image needs `none`),
  `exec_argv` (FR-012), `interactive_shell` (FR-016),
  `ownership_mechanism` (`refused.foreign_object`, FR-008), and `concurrency_limit` (the provider check of FR-009, not
  an admission row); the re-verification sentence; conformance cases 1 ("all eleven fields"), 4 (label-based ownership, now per
  `ownership_mechanism`), 6 ("argv as data, no shell": shell transport allowed only under `exec_argv=quoted_verified`
  with the FR-012 corpus round-tripping byte-exact) and the new cases of FR-025.
- `specs/008-workspace-sandbox/contracts/refusal-codes.json`: append the eight codes above and widen the "when" text of
  `refused.foreign_object` generically (FR-008). Codes are not renamed.
- The Feature 008 conformance suite and `Admit` (`apps/whr/internal/runtime/`): new cases and the admission paths for
  the new refusals. The existing table rows stay unchanged (FR-003).
- `contracts/cli/doctor-checks.json` and its freeze test `TestDoctorChecksMatchContract`: add `vm.backend`,
  `vm.base_image`, `vm.limit` and `vm.space` (FR-018, FR-032), and amend the process rule to allow the driver's fixed read-only `probe` and `list`
  invocations within the shared 5 s budget (owner-gated like other contract changes). `whr doctor --fix` for the VM
  leftovers is covered by the Feature 009 entry below, since `doctor-checks.json` lists only checks.
- `specs/009-host-setup-wizard/spec.md`, `contracts/wizard-flow.md` and `data-model.md` (amendment for FR-031 and FR-033, owner-gated
  like AP-1 to AP-9; Feature 009 FR-024 applies): the `host.setup` output on a macOS host gains a read-only
  `vm_readiness` part (the checks `vm.backend`, `vm.base_image`, `vm.limit`, `vm.space` with status and evidence, and the
  next steps), shown in every mode and not part of the plan or the protocol; the catalogue stays at 13 entries and
  FR-003 and FR-008 are untouched. The scope of `doctor --fix` (FR-016, wizard-flow) gains one repair class beyond
  catalogue items: removal of owned VM leftovers, as a new modify-class step kind in the closed set of step kinds
  (`vm_instance_remove`, with its protocol record), with the item ids of FR-033, `--confirm <id>` per item, the preview
  of FR-033 and the existing exit codes. The sentence that a tool from another source is never replaced or removed is
  unaffected: leftovers are owned instances, not tools. The frozen Feature 009 contracts (`protocol.schema.json` and its
  fixtures, `wizard-flow.md`, the data model step kinds, the AP-6 result shape) and their tests are amended when this is
  implemented, and a foreign object is refused with `refused.foreign_object` of Feature 008.
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
