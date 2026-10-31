# Feature Specification: VM provider and cross-platform test evidence

**Feature identifier**: `011-vm-provider`
**Feature Branch**: `docs/spec011-vm-provider`
**Created**: 2026-10-31
**Input**: Owner request 2026-10-31: evidence for the failing Mac tests, and disposable macOS (and later other)
guests driven through the same sandbox lifecycle as containers, vendor neutral.
**Status**: Draft, proposed for independent review and owner approval; nothing is implemented. Approval of this text
needs an independent review CLEAR for its exact head and explicit owner approval of that head, evidenced by a tracker
or PR link (WORKFLOW.md).
**Related**: [Feature 008](../008-workspace-sandbox/spec.md) (RuntimeProvider contract, Feature 008 FR-012 to FR-018, FR-029, FR-034),
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
4. conformance cases and refusal codes for the above;
5. a first increment that is library-level and offline, using a fake driver; and
6. a macOS host surface with the same experience for the container and the VM backend (FR-035): a VM readiness summary
   and optional install and image offers in `whr setup` (FR-031, FR-034), a space report in `whr doctor` (FR-032), and
   `whr doctor --fix` that removes owned leftovers (FR-033) and may rebuild a missing or unusable image (FR-037).

### Terms

- **Guest**: the operating system inside a VM (`linux`, `darwin`, `windows`) and its architecture (`arm64`, `amd64`).
- **Image**: a container image or a VM base image. A **container image** is the image the container provider runs
  instances from, built locally from a user-owned container build file and carrying `whr`'s own label. It is not a
  `base_image` in the sense of FR-006: the container provider keeps `base_image=none` (FR-003) because a request never
  names a clone source for it, and how the container provider names its image is part of the Feature 009 amendment (Feature
  008 defines no such field).
- **Base image**: a VM image that belongs to the user and stays local. `whr` names it; the setup wizard can build it
  locally from a user-owned definition after an explicit prompt (FR-034).
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
  use of macOS in a VM is the owner's to check against that license; it was not checked here. Superseded in part by
  decisions 10, 11 and 15 below: the setup wizard can build the image locally from a user-owned definition, and the license
  is the user's responsibility.
- **2026-10-31 (OD-5, drivers; resolves NC-2)**: Tart is the first driver. The user installs the VM backend and holds
  its license; `whr` only drives it and neither installs nor ships it (FR-017), the same pattern as the Docker runtime.
  Superseded in part by decisions 12 and 17 below: the setup wizard can install the VM tool from a catalog entry on
  explicit selection.
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
  and widened refusal codes. A1 reaches more than the contract: Feature 008 FR-012, Feature 008 FR-034, the data model, the
  conformance suite and the admission paths for the new refusals (see "Changes to existing documents"). Every Feature 008
  admission decision is unchanged (FR-003).
- A1 is the single exception to the sentence of Feature 008 FR-034 that adding a provider changes no contract: the
  contract had no platform dimension. A1 is generic. After it, adding a driver changes no contract text.
- `whr` does not publish, upload, sign, redistribute or pull prebuilt base images, and builds one only locally and only
  after an explicit prompt (FR-007, FR-034, FR-037).
- The provider and `whr doctor` do not install a VM backend, and `whr` changes no host virtualization, network, firewall
  or security setting (FR-017). Only the setup wizard installs the VM tool (from a package source of the closed list of FR-038), from a catalog entry that is not preselected
  and only on explicit selection (FR-031). `whr doctor` reports (FR-032, FR-039), removes owned leftovers (FR-033) and may rebuild
  an image (FR-037); `whr doctor --fix` does not install the VM tool, and the rest of Feature 009 FR-016 is unchanged.
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
- The image definition is missing, or was edited so that it no longer builds, when `whr doctor --fix` would rebuild the image (FR-037).
- The guest has no execution agent, so `exec` is unavailable and capability `exec_argv` is `unavailable`.
- An argument is empty, contains a newline, or is 64 KiB long.
- Disk is full during clone.
- A test is skipped on a platform without a recorded reason.

## Requirements *(mandatory)*

### Functional Requirements

**Amendment A1 to the Feature 008 contract**

A1 amends Feature 008 as listed under "Changes to existing documents": Feature 008 FR-012, Feature 008 FR-034, the data model, the contract
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
- **FR-007**: `whr` MUST NOT publish, upload, sign, redistribute or pass on images, MUST NOT pull a prebuilt, finished image
  from a registry, and MUST NOT store or relay guest login credentials. The image stays with the user and local (OD-4).
  Base layers that the container runtime's own build command fetches from a registry while building the image from the
  user-owned definition are not a pull of a prebuilt image (FR-034, decision 16). The only commands that build an image
  are `whr setup` and `whr doctor --fix`, locally, from the user-owned definition, after an explicit prompt or per-item
  confirmation (FR-034, FR-037); the provider never builds. The guest account the driver uses is part of the user's image
  and appears in configuration only as a reference. The documentation MUST also describe a reproducible recipe that starts
  from the operating system vendor's official download and keeps the VM image local, so the user can build it without `whr`.
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
  `refused.interactive_unavailable`. The command is the same one for every backend (FR-035; proposed syntax
  `whr sandbox shell <id>` in the draft Feature 012, pull request 291, not merged).

**Host boundary**

- **FR-017**: The provider MUST change only instances it owns. It MUST NOT change host virtualization, network, firewall,
  security or system settings, and MUST NOT install or ship a VM backend; `whr doctor --fix` does not install the VM tool either. Only the
  setup wizard installs the VM tool, on explicit selection (FR-031, FR-038). The user holds its license (FR-036).
- **FR-018**: `whr doctor` MUST gain read-only checks `vm.backend` (present, version, supported host), `vm.base_image`
  (the named image exists) and `vm.limit` (reports the declared concurrency limit, or that no limit is stated or enforced and therefore none is
  applied, the number of owned instances and pending reservations from the local records, stale reservations, and
  whether a `Create` would be refused now for the concurrency limit, which is never the case when no limit applies). They read local records and use only the driver's
  `probe` and `list`; they never start a VM. A fourth read-only check, `vm.space`, is added by FR-032. On Linux, and when no VM driver is chosen, every `vm.*` check
  reports `skipped` (reason `unsupported_platform` or `no_vm_driver`), never `fail`, so a container-only user's `whr doctor`
  is not failed by them; a chosen driver that is missing or unsupported is reported by `vm.backend` as `fail` (FR-023).

**Test run**

- **FR-019**: A test run MUST be expressed through the sandbox lifecycle of Feature 008: admit, create, copy in, exec,
  export, remove. No VM-specific command family and no wrapper command are added here.
- **FR-020**: Results (exit status, logs, declared artifacts) that are in the result directory MUST survive removal, and no `Remove` or cleanup deletes them. Results still only inside a clone are exported before removal (FR-011) or are deleted only on the explicit confirmation of FR-033. After an interrupted run and `Remove`,
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
    (`docs/drivers/`), the host catalog asset (`apps/whr/internal/hostsetup/catalogue/assets/catalogue.json`, whose VM tool
    entry names the tool and its source) with its templates, and text built at run time from catalog data or driver data (the
    setup and doctor preview, the `vm.backend` evidence), which is data and not gated command text, and regions of this specification between the HTML comment markers `informative:begin` and
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

- **FR-031**: On a macOS host, `whr setup` MUST end with a "VM readiness" summary built from the same read-only checks as
  `whr doctor` (`vm.backend`, `vm.base_image`, `vm.limit`, and the report of FR-032), using the same fixed read-only `probe`
  and `list` invocations of the chosen driver within the shared 5 s budget of the doctor checks, and starting no VM. For each
  check that is not `pass` it MUST print the exact next steps: the offers below, or, when an offer is declined or not
  selected, how the user does it themselves (the documented steps of the driver, FR-024, and the documented recipe, FR-007).
  The summary is not a plan step and writes nothing, so it also appears when the answer is `no` and under `--dry-run`.
  The wizard MAY offer, each as its own explicit step: (a) installing the VM tool, through a Feature 009 catalog entry
  that is not preselected (FR-040 gives the entry count), through the package-manager adapter, from the package source named by its entry under the closed list of FR-038, as a
  modify-class step with its own confirmation (FR-038), and only on explicit selection, never replacing or removing a tool from another source (Feature 009 FR-008); and (b) creating the
  image (FR-034). Nothing is installed, built, downloaded or elevated without that selection. `whr doctor` reports the tool
  entry like any catalog item, but `whr doctor --fix` does not install the VM tool (the rest of Feature 009 FR-016 is
  unchanged); it points to `whr setup`. Linux `whr setup` is
  unchanged.
- **FR-032**: `whr doctor` MUST gain a read-only check `vm.space` and report in it: the disk space taken by owned leftover
  instances, and the free disk space on the volume that holds the VM storage. It MUST report `fail` when that free space
  is below the size of the base image, and MUST say "size unknown" and report `unknown` rather than guess when the driver
  cannot state a size or the storage location. Sizes and the storage location are optional fields in the results of the
  existing driver `probe` and `list` (no new port operation, FR-021); the free space is measured by the host with a file
  system statistic. The check stays inside the shared 5 s budget and starts no VM.
- **FR-033**: On a macOS host, `whr doctor --fix` MUST remove only owned leftovers of the VM and the container backend
  (the rest of Feature 009 FR-016 is unchanged), for both alike: VM or container instances left by a killed or canceled run, and stale VM reservations (FR-009), through the
  provider's `Remove` with the ownership check on every item (FR-008 for a VM instance; the label ownership of Feature 008 for
  a container). A
  foreign or unlabeled object MUST be refused with `refused.foreign_object` and never touched, an instance of a run that
  is still active is not a leftover, and no other VM or container object is repaired except the image rebuild of FR-037. It uses the mechanics of Feature 009 FR-016, FR-007 and
  FR-017: it requires a host profile and otherwise refuses with `host_setup.profile_absent` (Feature 009 FR-016); the repair
  is a modify-class step (it removes something present) with the item id `vm-instance:<instance id>`,
  `vm-reservation:<reservation id>` (VM, released by the step kind `vm_reservation_release`) or `container-instance:<instance id>` (container). Without `--yes` it prints the preview (name, id and size if known, per item) and exits
  70, and with `--yes` each item is removed only with its own `--confirm <id>`. The preview MUST show, for each leftover,
  whether its results were exported; a stopped or failed clone whose results were not exported is marked "results not
  exported". Confirming such an item with `--confirm <id>` IS the results-deletion confirmation of Feature 008 FR-008:
  without it the item is not removed, and `--yes` alone never removes an item. Files already in the result directory are
  never deleted (FR-020). For such an item the preview names the export of Feature 008 FR-007 as the step to run first;
  while no export command is registered (FR-019 adds none, and `whr sandbox export` is proposed syntax under Feature 008
  FR-028), the preview says so instead of naming a command. Linux `whr doctor --fix` still refuses (Feature 009 FR-001).
- **FR-034**: `whr setup` on a macOS host MAY offer "create the image now?" for the container backend and for the VM backend
  alike (the limit to a macOS host holds for both). The offer is its own step, with these rules. The wizard generates the
  image definition (a container build file or a VM recipe) as a readable file owned by the user, shows it, shows a size
  preview (download and disk size, "size unknown" when not stated) and then asks an explicit question with the default
  `no`. The definition is written to disk, and the build starts, only after `yes` to the offer (a `no` writes nothing,
  Feature 009 FR-004); until then the wizard shows the generated content from memory. It builds locally only from that file. The container image is built with the container
  runtime's normal build command: the build changes no VM setting of the container runtime, writes only images that carry
  `whr`'s own label, and may fetch base layers from a registry as part of the build; that fetch is not a pull of a prebuilt
  image (FR-007). A build never starts silently, `--yes` alone approves neither the definition write nor the build (both belong to the offer and are modify-class steps
  with the per-item `--confirm image:<backend>`), and an unattended run without confirmation exits 70. Any download the build needs is named in the preview and happens only after the confirmation. An
  existing definition file is never overwritten without asking: the diff, the confirmation `--confirm image:<backend>` (which then also covers the overwrite, an author proposal) and a
  verified backup, as for a
  dotfile in Feature 009 FR-013. The generated definition is not a catalog dotfile: its content comes from a template of
  the release that the wizard shows, and its target path is chosen by the wizard and shown; the catalog-template and SHA-256
  rule of Feature 009 FR-013 applies to catalog dotfiles only (Feature 009 amendment below). The wizard records the
  definition path (home-relative) and the image name and label it uses in a record of the host profile, so that doctor can
  find them (FR-039; Feature 009 amendment below). The built image stays local and is never distributed (FR-007). Feature 009 and ADR 0009
  define no image creation today, so this is new for both backends and is delivered through the Feature 009 amendment below.
  Real image builds are owner gates (Feature 008 FR-032, plan OG-3) and owner-run evidence; FR-034 is tested offline with
  fake runners (SC-029). Linux `whr setup` is unchanged.
- **FR-035**: The container and the VM backend MUST share one set of core commands (`whr setup`, `whr doctor`,
  `whr sandbox ...` and the interactive shell command). Differences show only in the capability report and in a refusal
  that names its reason. A backend-specific extra command is allowed but MUST NOT replace a core command.
- **FR-036**: `whr` neither checks nor enforces license terms of host software: the VM tool, the host operating system
  running in a VM, concurrency caps that come from licenses, and third-party package sources. They are the user's
  responsibility, as for all user-installed host software. Documentation and the driver support matrix (FR-024) state this.
- **FR-037**: On a macOS host, `whr doctor --fix` MAY rebuild a missing or unusable image, for the container and the VM
  backend alike, locally, from the user-owned definition of FR-034. The repair is a modify-class step with the item id
  `image:container` or `image:vm`. Without `--yes` it prints the preview and exits 70; with `--yes` it runs only with the
  per-item `--confirm <id>`. The preview shows the definition file, the size and effort (as in FR-034, "size unknown"
  when not stated), and that an existing unusable image is replaced. A build never starts silently, nothing is downloaded
  before the confirmation, and nothing is distributed. If the definition is missing, or was edited so that it no longer
  builds, doctor reports it (a missing file in the check, a failed build as a failed step) and points to `whr setup`; it
  never regenerates the definition. The build rules of FR-034 apply (normal build command, own label only, no VM setting changed). `whr doctor --fix` does not
  install the VM tool (FR-031). Real builds are owner-run evidence and FR-037 is tested with fake runners (SC-032). Linux
  `whr doctor --fix` still refuses.
- **FR-038**: The catalog definition MUST carry a closed, named list of allowed third-party package sources (taps), exactly
  two: the source that carries the VM tool and the owner's own source (decision 17). The list is the `source.tap` enum of the
  catalog schema, which every entry names in the required `tap` field: either one of the two core sources of Feature 009 or
  one source of this list. Wildcards, a source supplied by the user and a source outside the list are rejected, and every
  other third-party source stays forbidden (Feature 009 FR-025). The preview shows the source per item and states that
  installing from it makes Homebrew add that source permanently to its configuration. A tool from a listed third-party
  source is installed by the modify-class step `brew_install_source` and upgraded by the modify-class step
  `brew_upgrade_source` (`brew install <tap>/<name>` and `brew upgrade <tap>/<name>`, for the listed sources only, decisions
  19 and 20). The wizard asks for each one by one with default `no`, `--yes` alone never approves either, and an unattended
  run needs `--confirm <item id>`, so `--yes --only <tool>` cannot install or upgrade it. `doctor --fix` may install such an item
  when it is selected and missing (other than the VM tool, FR-031) or upgrade it when outdated, with the same preview and
  per-item `--confirm`. The concrete source names
  are in the informative notes, the catalog and the driver documentation, not in this specification's normative text.
- **FR-039**: `whr doctor` MUST gain read-only checks for the container backend: `container.image` (the image recorded by FR-034 exists and
  carries `whr`'s label: `pass`; missing or unusable: `fail`; no recorded image definition: `skipped`, reason
  `no_image_defined`) and `container.leftovers` (owned
  container instances, with their size when the runtime states it and "size unknown" otherwise; `pass` with the leftovers as
  evidence when they are measurable, `unknown` when not, never `fail` for leftovers alone, so leftovers do not fail `whr
  doctor`; they are listed for `doctor --fix`, FR-033). The free space of the
  container storage is the existing `disk.container_storage` check, which is `unknown` whenever the storage location cannot be
  found without the runtime API (the normal case on macOS), so container free space is in practice not measured, while
  `vm.space` can fail on low space. This is a stated limit, not changed here (Open Questions). They use only read-only list and inspect calls of the
  container runtime on the discovered socket within the shared 5 s budget, start no container and change nothing. The VM
  space report of FR-032 has no further container counterpart in this increment: container space is the leftover sizes
  and `disk.container_storage`. On Linux both checks report `skipped` (reason `unsupported_platform`: the image offer and the
  cleanup are macOS-only), and when no container socket was discovered they report `skipped` (reason `no_runtime`;
  `container.socket` already reports that case), never `fail` for those reasons.
- **FR-040**: The owner's own package source (FR-038) carries two catalog entries, both with `selected_by_default=false`, so
  the catalog has 16 entries: the 13 existing ones, the VM tool and these two. Each entry carries the data of the existing
  entries: id, kind `tool`, title, platforms, source (type formula, name, `tap` from the closed list), the version detection
  fields (command, version arguments, version pattern, minimum version), `network` (the hosts, including that of the source,
  and the purpose, as Feature 009 FR-010 requires) and `selected_by_default=false`. They are installed and upgraded only
  after explicit selection and the per-item confirmation of FR-038, with the same rules as the VM tool entry (never
  replacing or removing a tool from another source, Feature 009 FR-008). (a) A tool for OCI tool bundles, platforms macOS on
  arm64 only (it is not offered elsewhere). It has no hard dependency on a container runtime in the catalog; its entry
  carries a note that the preview shows: its volume subcommand needs one. (b) A tool for read-only overview and stateless
  issue and pull request work on GitHub workspaces, which needs Git 2.46 or newer; its entry carries a host requirement for
  Git. Before the install, setup detects Git with the existing detection (Feature 009 FR-006) and shows the result in the
  preview. If Git is absent, is only the Command Line Tools shim (counted as absent by Feature 009 FR-006) or is older than
  2.46 and the catalog `git` item is not in the plan, the preview says so, the step is `blocked` with the reason `host_setup.host_requirement_unmet`, and Git is neither
  installed nor upgraded silently for this entry. If the catalog `git` item is installed or upgraded in the same plan, the
  entry's step depends on it (Feature 009 `requires` and `depends_on`) and Git is detected again after that step, and the step is `blocked` only if Git is still too old. The
  `blocked` is an existing Feature 009 StepResult status; only its use for an unmet host requirement and the reason code are
  new and are author proposals; the reason code is appended to the Feature 009 `refusal-codes.json` (changes list).

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
DriverSupportMatrix, TestRunResult, PlatformVerdict, SkipRecord, ConcurrencyReservation, VMReadinessSummary, LeftoverCleanupPlan, ImageDefinition, ImageBuildOffer, PackageSource, ThirdPartyCatalogEntry.

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
- **SC-006**: After a killed run and `Remove`, zero owned instances remain, leftovers are listed and results already in the result directory are retained
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
  not be refused for concurrency. On Linux and with no VM driver chosen every `vm.*` check reports `skipped`, never `fail`; a chosen but missing driver is `fail` in `vm.backend` (FR-018, FR-023).
- **SC-011**: `make check` passes offline with no VM backend installed (FR-026).
- **SC-012**: The skip-record check fails a skip that lacks platform, reason or tracking reference, and passes a complete
  one (FR-028).
- **SC-013**: With an `enforced` declared limit of one, two simultaneous `Create` requests yield exactly one instance and one
  `refused.concurrency_limit_reached`; the refused one made zero driver calls; the reservation is released on failed
  create and on `Remove`; a reservation left by a killed `Create` is reclaimed and the next `Create` succeeds; a stale
  reservation with a matching driver instance keeps counting and the next `Create` is refused until `Remove` clears it.
  With the value `unknown`, or an integer limit whose status is not `enforced`, no limit applies: the second `Create` is
  not refused for concurrency, no `refused.concurrency_limit_reached` is ever produced, and a reservation record is still
  written, and a stale one is reclaimed when no matching driver instance exists, or is a leftover for `Remove` when one does. A limit of 0 with status `enforced` refuses every `Create`
  (FR-009).
- **SC-014**: A request for an interactive shell against `interactive_shell=none`, or against `pty` that is not `enforced`, is
  refused with `refused.interactive_unavailable`; against an `enforced` `pty` it follows the terminal-safety rules (FR-003, FR-016).
- **SC-015**: Plans that share the host home directory, a secret-pattern path, the primary `.git` for untrusted work or
  a host socket as a mount are refused; a secret-pattern path in the private copy is refused unless named and confirmed
  (Feature 008 FR-009); a read-write shared mount is not planned by default (FR-014).
- **SC-016**: No code path or command of `whr` publishes, uploads, signs, redistributes or registry-pulls a base image, the
  provider never builds one, only `whr setup` and `whr doctor --fix` build, locally, after confirmation, and no
  configuration field holds a guest credential, only a reference (FR-007). No host virtualization, network, firewall or
  security setting is changed and neither the provider nor `whr doctor` (including `--fix`) installs a VM backend; the recorded mutating calls (start, write, stop, delete) target owned instances only, and the clone call reads the
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
  (no driver chosen, backend missing, base image missing, all ready): the next steps appear for every check that is not
  `pass`; the summary makes only `probe` and `list` calls, starts no VM and changes nothing; it also appears for the answer
  `no` and for `--dry-run`. The VM tool entry is not preselected, the tool is installed
  through the fake adapter only after explicit selection and its per-item confirmation (SC-033), a tool from another source is not replaced or removed, and
  nothing is installed or elevated without selection. Linux `whr setup` output is unchanged (FR-031).
- **SC-026**: With the fake driver, `vm.space` reports the space of owned leftovers and the free space of the storage volume;
  it reports `fail` when free space is below the base image size, `unknown` with "size unknown" when the driver states no
  size or location, and passes otherwise. The recorded driver calls are only `probe` and `list`, no VM is started, and the
  run stays inside the shared 5 s budget (FR-032).
- **SC-027**: On macOS with the fake driver and injected leftovers (a killed run, a canceled run, a stale reservation, an
  active run, a foreign object with the reserved prefix): `doctor --fix --dry-run` writes nothing; `doctor --fix`
  prints the preview and exits 70; `--yes` alone removes nothing; `--yes --confirm <id>` removes exactly the confirmed owned
  leftover through `Remove`; the foreign object is refused with `refused.foreign_object` and untouched; the active run is
  not listed as a leftover; without a host profile `doctor --fix` refuses with `host_setup.profile_absent`; `doctor --fix` makes no install call for the VM tool; Linux `doctor --fix` is refused (FR-033).
- **SC-028**: The `doctor --fix` preview marks every leftover whose results were not exported as "results not exported" with
  the Export step, for a stopped and for a failed clone, and marks exported ones as exported; the marking is present in
  the preview that precedes any removal. An item marked "results not exported" is not removed without its `--confirm <id>`, and after a confirmed removal the files already in the result directory are byte-identical to before (FR-033, FR-011, FR-020).
- **SC-029**: With fake container and VM runners, an image offer shows the generated definition and the size preview before
  any build; no build or download call happens before `yes`; `--yes` alone does not build; an unattended run without
  confirmation exits 70 with no build call; `no` leaves no build call; an existing definition file stays byte-identical
  unless the diff is confirmed, and a verified backup precedes its replacement; the container build uses only the runtime's
  normal build invocation, writes only labeled images, makes no VM setting call, and a base-layer fetch during the build
  is allowed while a call that pulls a prebuilt finished image, and any call that uploads, signs or publishes, fail the test
  (FR-034, FR-007). Real builds are owner-run evidence.
- **SC-030**: The command registry has one entry per core command for both backends, no backend-specific command replaces a
  core command, and the same command-level conformance cases pass for the fake container provider and the fake VM provider
  with differences only in the capability report and in refusal codes that name their reason (FR-035, FR-016).
- **SC-031**: No code path of `whr` reads, validates or enforces a license term of host software, and the documentation and
  the driver support matrix state that these are the user's responsibility (FR-036).
- **SC-032**: With a fake runner, `doctor --fix` without `--yes` previews the rebuild (definition file, size and effort,
  replacement of an unusable image) and exits 70; `--yes` alone builds nothing; `--yes --confirm <id>` builds locally from
  the definition with no download before confirmation; a missing definition is reported and not regenerated, and a failed
  build is a failed step that points to `whr setup`; the same holds for the container and the VM backend, and the VM
  tool is not installed (FR-037).
- **SC-033**: The catalog loader accepts an entry whose `tap` is one of the two core sources or one of the two listed
  third-party sources, and shows it in the preview with the statement that the source is added permanently; it rejects a
  `tap` outside the closed list, a wildcard, a user-supplied source and a list of sources. For every entry from a listed
  third-party source, the fake adapter records an install call (`brew install <tap>/<name>`) or an upgrade call
  (`brew upgrade <tap>/<name>`) only after explicit selection and the per-item confirmation: `--yes` alone, and `--yes` with
  only a selection of the tool, record neither, an unattended run without `--confirm` exits 70, and `doctor --fix` installs a missing
  selected item (other than the VM tool) or upgrades an outdated one only with its `--confirm <item id>`. A call with a source outside the list is refused by the argv table (FR-038).
- **SC-034**: With a fake container runtime, `container.image` reports `pass`, `fail` and `skipped` for a labeled image, a
  missing or unusable image and no defined image; `container.leftovers` lists owned instances only with size or "size
  unknown"; with leftovers present it is still `pass` (leftovers are evidence only); the recorded runtime calls are read-only list and inspect calls, none starts or changes anything; and
  `doctor --fix` removes an owned container leftover only with its `--confirm container-instance:<id>`, refuses an
  unlabeled container with `refused.foreign_object` and leaves it untouched; on Linux and with no runtime discovered the
  two checks report `skipped` (FR-039, FR-033).
- **SC-035**: The shipped catalog has 16 entries; the VM tool entry and the two entries of the owner's own source carry
  id, kind, title, platforms, source, version detection fields, `network` (including the source host) and
  `selected_by_default=false`; the tool for OCI tool bundles is not offered on a
  platform other than macOS on arm64 and its preview states the container runtime note; for the other tool, with a fake
  Git older than 2.46, absent or only the Command Line Tools shim, and the catalog `git` item not in the plan, the preview
  says so, the step is `blocked`, and no Git install or upgrade call is recorded; with Git 2.46 or newer the step is
  offered; when the catalog `git` item is in the same plan the step depends on it and Git is detected again after it, and
  the step is `blocked` only if Git is still too old (FR-040).

## Owner decisions (confirmed 2026-10-31)

The owner decided the six choices that the author had proposed and then added decisions 7 to 20. The author proposals that remain
open for the owner are marked below. Decisions 10, 11 and 15 supersede parts of OD-4 and decisions 12 and 17 parts of OD-5, as noted there; none alters OD-1 to OD-3 or OD-6.

1. **Interactive shell for the container provider (confirmed, follow-up)**: the owner wants an interactive shell for the
   container provider in the future. Until a separate feature builds it, the capability report stays honest (FR-025): the
   container provider reports `interactive_shell=none` (FR-003 unchanged). When that feature lands, the value becomes
   `pty` with status `enforced`. Follow-up: a separate feature "interactive shell for the container provider", out of
   scope here (the draft Feature 012, pull request 291, not merged).
2. **Concurrency limit (CHANGED)**: when the driver states no limit (`unknown`), no limit is applied (unlimited).
   Before, `unknown` was treated as a limit of one. A declared integer limit with status `enforced` still applies, with
   atomic reservations and reclaimed stale reservations (FR-009, SC-013). Author proposal, owner may change: a limit whose
   status is not `enforced` is treated like `unknown` (not applied). License caps on concurrent VMs are the
   user's responsibility and are not checked by `whr` (decision 15).
3. **`guest_arch=unknown` when the architecture cannot be probed (confirmed, unchanged)**.
4. **The name of the Linux-on-Windows layer stays on the gated vendor-name list (confirmed, unchanged)**, although Windows
   is out of scope.
5. **Owner decision text OD-1 to OD-6, which names products, stays inside the informative markers (confirmed, unchanged)**.
   This section names no product and is outside the markers, as the vendor-name gate (FR-022) requires of gated text.
6. **A missing or unsupported driver is refused with the new `refused.driver_unavailable` (confirmed, unchanged)**, eight new
   codes in total. ADR 0013 has no configuration-error category.
7. **`whr setup` on macOS (NEW, extended by decisions 10 to 12)**: `whr setup` ends with a read-only VM readiness summary
   and the exact next steps (FR-031, SC-025). The first form "check and guide only" is superseded where decisions 10 to 12
   conflict: the wizard may also install the VM tool and build the image, each on explicit selection. Linux is unchanged.
8. **Scope of `whr doctor` VM support (NEW)**: A, B and C. A is read-only diagnosis with `vm.backend`, `vm.base_image` and
   `vm.limit` (FR-018). B is the read-only space report (FR-032), which warns below the base image size by reporting `fail`
   (the check statuses have no warning). C is `whr doctor --fix` removing owned leftovers only
   (FR-033). D (`doctor --deep`, a real clone-start-exec-stop-delete smoke) is future work only. E (`doctor --fix` installs
   the VM tool or builds the image) was rejected; by decision 13 the image-build half is now accepted (FR-037) and the
   VM-tool-install half stays rejected: only the wizard installs the VM tool.
9. **One experience for every backend (NEW)**: one shared set of core commands (setup, doctor, sandbox, shell) for the
   container and the VM backend. Differences show only in the capability report and in a refusal that names its reason. A
   backend-specific extra command is allowed but never replaces a core command (FR-035, SC-030).
10. **Image creation (NEW)**: on a macOS host, `whr setup` may offer "create the image now?" for both backends (Linux is unchanged). It builds locally, after an
    explicit prompt and a size preview. No silent build or download. The image stays with the owner and is never
    distributed; NC-1 stays valid. Registry-pulled prebuilt images stay out of scope (FR-034, FR-007, SC-029).
11. **Image definition (NEW)**: `whr` generates the definition (container build file or VM recipe) as a readable file owned
    by the user, shows it before building, builds from it, and never overwrites it without asking (FR-034).
12. **VM tool installation (NEW)**: the VM tool becomes a catalog entry that is not preselected (decision 18 brings the catalog to 16 entries),
    installed through the existing adapter only on explicit selection; a tool from another source is never replaced or
    removed (Feature 009 FR-008). This changes decision 7 (FR-031, SC-025).
13. **Doctor and cleanup (NEW, corrected)**: on a macOS host, the same rules for both backends (container checks FR-039, container leftovers FR-033; the space report is the VM check FR-032 plus, for containers, leftover sizes and `disk.container_storage`, which is usually `unknown` on macOS; see Open Questions): read-only checks, the space report, and
    `whr doctor --fix` removing only owned leftovers (FR-033) with preview and per-item `--confirm`. `doctor --fix` may also
    rebuild a missing or unusable image locally from the user-owned definition, after a preview with size and effort and a
    per-item `--confirm`; no silent build and no silent download (downloads only after the confirmed preview), nothing distributed (FR-037, SC-032). `doctor --fix` does not install
    the VM tool; only the wizard does. The rest of Feature 009 FR-016 (repairing other selected catalog items) is unchanged.
14. **One interactive shell command (NEW)**: one command with the same behavior for both backends (proposed syntax
    `whr sandbox shell <id>`, draft Feature 012, pull request 291, not merged). A backend without an enforced `pty`
    refuses with `refused.interactive_unavailable` (FR-016).
15. **Licensing (NEW)**: license terms of host software (the VM tool, the host operating system in a VM, concurrency caps from
    licenses, third-party package sources) are the user's responsibility, as for all user-installed host software; `whr`
    does not check or enforce them (FR-036, SC-031). The concurrency decision (unknown means unlimited) is unchanged.
16. **Container image build, a narrow exception (NEW)**: on a macOS host, the wizard builds the container image with the
    container runtime's normal build command. It changes no VM setting of the container runtime and writes only images
    that carry `whr`'s own label. Base layers fetched from a registry as part of the build are allowed; pulling a prebuilt
    finished image stays forbidden (FR-007, FR-034, SC-029).
17. **Package source exception (NEW; superseded in part by decision 18)**: the catalog definition carries a closed, named
    list of two allowed third-party package sources: the one that carries the VM tool, and the owner's own. There is no
    wildcard and no user-supplied source. The preview shows the source per item, installation needs explicit selection and
    its own confirmation, and every other source stays forbidden (FR-038, SC-033). The earlier sentence that no catalog entry
    is added for the owner's own source is superseded by decision 18.
18. **Entries from the owner's own source (NEW, owner decision of the third review round)**: the catalog gets two entries from
    it, a tool for OCI tool bundles (macOS on arm64 only, no hard dependency on a container runtime, a note that its volume
    subcommand needs one) and a tool for read-only overview and stateless issue and pull request work on GitHub workspaces
    (needs Git 2.46 or newer). Both are not preselected and installed only after explicit selection and the per-item
    confirmation of decision 17. For the second, setup checks the installed Git version before the install, shows the result
    in the preview, and does not silently install or upgrade Git (FR-040, SC-035).
19. **Four former author proposals confirmed (NEW)**: (a) `vm.space` reports `fail`, not a warning, when space is
    insufficient, because the check statuses are only pass, fail, skipped and unknown (a failed check makes `whr doctor`
    exit 68); the owner's original wording was "warn" (FR-032). (b) Confirming a cleanup item marked "results not exported"
    is the results-deletion confirmation of Feature 008 FR-008, and files already in the result directory are never deleted
    (FR-033, FR-020). (c) Installing from an allowed third-party source asks each item one by one with default `no`,
    unattended only with `--confirm <item id>`, and `--yes` alone never installs it (`brew_install_source`, FR-038).
    (d) The VM image build may download the platform vendor's operating system image, only after the confirmed preview
    (the Feature 009 FR-012 amendment; OD-4 and decision 10).
20. **Upgrade from allowed third-party sources (NEW)**: `brew upgrade <tap>/<name>` is allowed for the listed sources only,
    as its own modify-class step (`brew_upgrade_source`) with a preview and the per-item `--confirm <item id>`, exactly like
    the install. `doctor --fix` may do it; `--yes` alone never does (FR-038, SC-033).

Author proposals made while recording these decisions (the owner may change them):

- A limit with status not `enforced` is treated like `unknown` (decision 2).
- Command names other than those the owner named are not settled. `whr sandbox shell <id>` is proposed syntax of the draft
  Feature 012; the item ids `image:container`, `image:vm` and `container-instance:<id>` are author proposals (FR-037, FR-033). The check ids
  `container.image` and `container.leftovers` are specified in FR-039 and need the doctor contract amendment.
- Applying decision 16 and the OS image download of OD-4 to `doctor --fix` (rebuild with base-layer fetch or official-source
  download after the confirmed preview) goes beyond the recorded wording of decision 13 and is an author proposal.
- Image naming: the wizard records the definition path (home-relative) and the image name and label in a record of the host
  profile; the form of that record is part of the Feature 009 amendment.
- `container.leftovers` is `pass` with the leftovers as evidence (never `fail` for leftovers alone), so leftovers do not make
  `whr doctor` exit 68 (FR-039).
- The names and classes of the new step kinds (`image_definition_write`, `image_build`, `vm_instance_remove`,
  `container_instance_remove`, `vm_reservation_release`, `brew_install_source`, `brew_upgrade_source`) are author proposals.
  The behavior of the last two (per-item confirmation, `doctor --fix` may install a missing or upgrade an outdated tap item)
  is owner decisions 19(c) and 20.
- The reason code `host_setup.host_requirement_unmet`, and the use of the existing Feature 009 status `blocked` for an unmet
  host requirement, are author proposals (FR-040).
- Decisions 12 and 13 read together: the VM tool entry is checked by `whr doctor` like any item, but `whr doctor --fix` does
  not install the VM tool (an explicit exception to Feature 009 FR-016; every other selected catalog item is repaired as
  before, including per-item confirmation for tap-sourced items), although Feature 009 FR-016 repairs other selected catalog
  items by installing them.
- If the definition is missing or no longer builds, doctor reports it and points to `whr setup` and never regenerates it
  (FR-037). An unusable existing image is replaced by the rebuild after the per-item confirmation.
- The default location of the definition file is a user-owned path shown by the wizard; the choice is not fixed here.
- Sizes and the storage location are optional fields of the existing `probe` and `list` results, not a new port operation
  (FR-032).
- The cleanup keeps the Feature 009 requirement of a host profile, as a modify-class step with the item ids
  `vm-instance:<instance id>` and `vm-reservation:<reservation id>` (FR-033).

## Future work (not started; each needs its own specification and is re-decided after the first real VM run)

- An interactive shell for the container provider (decision 1, draft Feature 012).
- `whr doctor --deep`, a real clone-start-exec-stop-delete smoke (decision 8, D).
- A separate `whr vm setup` command is not planned; any backend-specific command must not replace a core command (decision 9).
- Further entries from the owner's own package source beyond the two of decision 18 (which other tools it carries is unknown here).
- The owner may later configure an explicit concurrency limit for a driver that states none.

## Assumptions

- Admit, TestAdmitTable, the conformance suite and `apps/whr/internal/runtime/` do not exist yet (Feature 008 tasks
  T001 and T002 are open), so the before-and-after regression check of FR-003 depends on Feature 008 landing first.
- The Feature 008 trust profiles and plan model are reused. A test run uses a job profile with a private copy and a
  scratch home. This feature does not redefine them.
- License terms of host software are the user's responsibility and are not checked by `whr` (decision 15).
- A driver that states no concurrency limit means no limit is applied (decision 2). The host or the driver can still
  refuse a `Create` for its own reasons; that is a failed `Create` (FR-010).
- Real-VM evidence comes from the owner's Mac. Hosted CI runners are not assumed to support nested virtualization.
- Upstream facts about any VM backend were read from public documentation and secondary sources on 2026-10-31. They are
  not acceptance evidence, and each driver proves its own claims through its tests.

## Open Questions

Two questions for the owner. (1) Should not-applicable skips (the six new checks on Linux, on a host without a VM driver or
runtime) be left out of the doctor verdict? Today, with Feature 006 FR-060, such hosts always get the verdict `incomplete`
instead of `ok` (the exit status stays 0). (2) Should container free space be measured through the runtime API, which Feature 006 FR-060 and the
`doctor-checks.json` note currently forbid, or stay `unknown` (FR-039)? No rule is invented for either. NC-1 to NC-3 were decided on 2026-10-31 and are recorded
as OD-4 to OD-6.

### Verification before the plan (checks, not decisions)

- The full text of the first backend's license and its maintenance status after the reported change of ownership. Only
  search excerpts and a summary were read so far. The license is the user's concern (decision 15), so this check informs
  the support matrix (FR-024) and the maintenance risk, not the design.
- Licenses of host software are not an open owner check: they are the user's responsibility (decision 15, FR-036). A concurrency
  cap that comes from a license shows up, if at all, as a driver or host refusal of a `Create` (FR-010).

### Follow-up outside this feature (owner)

- BundleDock and Anchorage are separate repositories. Their cross-platform CI, and the classification of their failing
  Python tests, need their own change in those repositories (FR-028 applies only here).

## Changes to existing documents (on approval; each lands through the plan and tasks)

- `specs/008-workspace-sandbox/spec.md`: Feature 008 FR-012 (capability list gains the fields of FR-001 and FR-004), Feature 008 FR-034 (the
  exception of A1 and the new refusal paths) and, for the new codes, the error mapping of Feature 008 FR-035 (capability refusals
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
  `vm.base_image`, `vm.limit`, `vm.space`, `container.image` and `container.leftovers` (FR-018, FR-032, FR-039) with their
  `skipped` reasons (`unsupported_platform`, `no_vm_driver`, `no_image_defined`, `no_runtime`), and amend the rule "start no
  process other than the fixed `git --version` ..." to allow the driver's fixed read-only `probe` and `list` invocations,
  and the socket-discovery note "no Docker API call is made" to allow the read-only list and inspect calls of the two
  container checks on the discovered socket, within the shared 5 s budget (owner-gated like other contract changes).
- `specs/006-whr-cli/spec.md` (Feature 009 FR-024 gates Feature 006 text, so these are named amendments): FR-060 gains the six new check ids above
  (the four `vm.*` checks and the two container checks); its wording that `container.socket` "connects and sends nothing" stays for
  that check, while `container.image` and `container.leftovers` make read-only list and inspect calls on the discovered
  socket; acceptance scenario 7 (plain `doctor` "started no process other than the fixed `git --version`" and the FR-055
  invocations) gains "and, when a VM driver is chosen, the driver's fixed read-only `probe` and `list` invocations of
  Feature 011 FR-018". Consequence, stated plainly: because Feature 006 FR-060 gives the verdict `ok` only when all checks pass
  and the six new checks report `skipped` for good on Linux and on hosts without a VM driver or runtime, such hosts get the
  verdict `incomplete` (exit status 0) instead of `ok`; whether not-applicable skips should be left out is an open question.
  The 5 s budget lists of FR-060 and of the `doctor-checks.json` rules ("each socket dial, `git --version` and
  `daemon.json` read draws on it") also name the driver `probe` and `list` invocations and the runtime list and inspect calls.
- `specs/009-host-setup-wizard/` (spec, `contracts/wizard-flow.md`, `data-model.md`, the catalog schema
  `host-tool-catalogue.schema.json`, its loader tests, `refusal-codes.json` and the protocol contract; amendment for FR-031,
  FR-033, FR-034, FR-037, FR-038 and FR-040, owner-gated like AP-1 to AP-9; Feature 009 FR-024 applies and the frozen contracts
  and their tests are amended when this is implemented):
  - **Catalog**: three new tap-sourced entries (the VM tool and the two of FR-040), none preselected, so Feature 009 FR-010
    ("exactly 13 entries", "all 13 entries preselected", now 13 of 16), the `x-initial-items` list and the
    `x-initial-defaults.selected_by_default_false` list of the schema, and the loader tests become 16 (Feature 009 SC-012
    says only that the shipped catalog has the initial items and a minimum version per tool, with no count). The adapter
    installs and upgrades from a listed source only on explicit selection with the per-item confirmation of FR-038, and
    Feature 009 FR-008 (a tool from another source is never replaced or removed) applies; Feature 009 FR-003 gives the
    container runtime as the precedent. The wizard-flow sentence "git-config ... is not a fourteenth catalogue entry" becomes
    "seventeenth". Each new entry carries id, kind, title, platforms, source, the version detection fields, `network` (hosts
    including the source host, and purpose) and `selected_by_default=false`.
  - **Schema (decision 17)**: `source.tap` is a required enum; it grows from `homebrew/core` and `homebrew/cask` by the two
    listed sources (the closed list, no wildcard, no pattern). The loader rule "tap matches type (formula: homebrew/core, cask:
    homebrew/cask)" is amended: an entry from a listed source is a formula entry. The `name` pattern has no `/`; the adapter
    composes `<tap>/<name>` from the two validated fields. Because the schema has `additionalProperties: false`, two optional
    entry properties are added: `note` (text the preview shows, for the container runtime note) and `host_requirement` (a
    tool id and a minimum version checked before the install, for Git 2.46, FR-040).
  - **Terms (Create and Modify class)**: the definition "Create class: adds something absent" is amended: a step that adds
    something absent but needs its own per-item confirmation because of its source or cost is Modify class
    (`brew_install_source`, `image_definition_write`, `image_build`).
  - **Wizard flow and `host.setup` output**: after detection the wizard may ask the image offer of FR-034 (definition shown from
    memory, size preview, default `no`; the file is written only after `yes`) and shows the read-only `vm_readiness` part
    (checks `vm.backend`, `vm.base_image`, `vm.limit`, `vm.space` with status and evidence, and the next steps) in every mode,
    not part of the plan or the protocol. The closed answer sets and the accepted confirmation ids (catalog item ids plus
    `git-config`; unknown ids exit 64) are widened by `image:container`, `image:vm`, `vm-instance:<id>`,
    `vm-reservation:<id>` and `container-instance:<id>`, and the `refusal-codes.json` text of `host_setup.unknown_item` ("neither
    a catalogue id nor the reserved git-config item") is widened the same way.
  - **Refusal codes**: `host_setup.host_requirement_unmet` (a step is `blocked` because a declared host requirement is unmet; 68
    when nothing was applied) is appended to Feature 009 `refusal-codes.json` (author proposal).
  - **Host profile**: the data-model line "No paths or values" is amended: the HostProfile gains a record per image offer with
    the definition path (home-relative), the image name and the label (author proposal).
  - **Plan steps (closed set of kinds)**: new kinds `image_definition_write` and `image_build` (modify class, written and run
    only after `yes` to the offer or the per-item `--confirm`), `vm_instance_remove`, `vm_reservation_release` and
    `container_instance_remove` (modify), `brew_install_source` and `brew_upgrade_source` (modify, FR-038), each with a
    protocol record. The data-model line "There is no kind for ... a tap" is amended: the only tap-related kinds are those two,
    for the closed list.
  - **`doctor --fix` and Feature 009 FR-016**: the sentence "It does not repair non-host findings" (Feature 009 FR-016) is
    amended: three repair classes beyond catalog items are added: removal of owned VM leftovers and reservations, removal of
    owned container leftovers (FR-033) and image rebuild (FR-037), with the preview fields of those FRs, `--confirm <id>` per
    item, exit 70 without confirmation and 68 for a refusal such as `host_setup.profile_absent` or `refused.foreign_object`; a
    failed build is a failed step (73 when something was applied, 68 when nothing was). The VM tool item is checked by `doctor`
    but not installed by `--fix` (author proposal, an exception to Feature 009 FR-016 and to `wizard-flow.md`, where `doctor
    --fix --yes` installs other missing selected items); tap-sourced items other than the VM tool are repaired like other
    selected items, with install and upgrade needing the per-item `--confirm` of FR-038. `wizard-flow.md` ("never touches the
    Docker VM") stays true.
  - **Socket and VM statements (Feature 009 FR-002, D-3, SC-006, Boundaries)**: "the only interaction is connecting to a
    discovered socket and closing it" (Feature 009 FR-002), "it only connects to verify it" (Boundaries), "it only reads and ..."
    (D-3) and "the socket is only connected to" (Feature 009 SC-006) are amended: the container checks of FR-039 make read-only
    list and inspect calls on the discovered socket, and the image build and the removal of owned containers touch only
    containers and images that carry `whr`'s own label and change no VM setting of the container runtime. The socket is still
    never mounted into an environment. `vm_instance_remove`, `vm_reservation_release` and `image_build` touch only owned VM
    instances, records and images.
  - **Feature 009 FR-012 (decisions 16 and 19(d))**: "downloads of software are done by Homebrew" also allows (a) the
    base-layer fetch that the container runtime's own build command performs during an image build and (b) the download of the
    platform vendor's official operating system image during the VM image build (names only in the driver documentation), both
    only after the confirmation of FR-034 or FR-037 and never silently.
  - **Feature 009 FR-013**: the catalog-template and SHA-256 rule applies to catalog dotfiles; the generated image definition
    follows the preview, diff, verified backup and no-silent-overwrite rules but not the catalog-template rule (FR-034).
  - **Feature 009 FR-009, FR-025 and the brew forms**: FR-009 ("only taps homebrew/core and homebrew/cask"), FR-025 ("adds no
    taps") and the brew invocation forms in `wizard-flow.md` ("tap" not used; `brew install <name>`, `brew upgrade <name>`)
    admit `brew install <tap>/<name>` and `brew upgrade <tap>/<name>` for the listed sources, and nothing else. Because Homebrew
    adds the named source permanently on such an install, FR-025 is amended for the listed sources only and the preview says so.
  - **Feature 009 FR-023**: the allowed-argv table admits the driver's read-only `probe` and `list` (FR-031), the runtime's
    read-only list and inspect calls, the fixed image build invocation of each backend, the removal calls of owned instances,
    and the tap-sourced install and upgrade.
  - **Other places that still say 13 or allow only core sources** (to be changed with the catalog; none is edited by this
    spec-only pull request): in Feature 009, `spec.md` D-9 (line 66, "13 initial entries"), `plan.md` OD-2 (line 56),
    `tasks.md` T001 (line 22, done; `TestInitialCatalogueEntries` requires exactly 13 ids), `quickstart.md` (line 15),
    `HANDOFF.md` (line 52) and `checklists/requirements.md` (line 21); in code,
    `apps/whr/internal/hostsetup/catalogue/production_test.go` (line 50, expects 13 items) and
    `apps/whr/internal/hostsetup/pkgmgr/pkgmgr.go` (lines 410 and 519, only `homebrew/core`). T001 and this code are reopened
    by a new task in the implementation plan.
  - **Step deadline**: the deadline of `image_build` is a plan-time value, not the 900 s of the package-manager steps (DP-2).
  - **Boundaries**: "installing anything on Linux" and the Linux enrollment stay unchanged; the image offer is a macOS host
    feature.
- `docs/container-runtime.md`: add a VM backend prerequisite section; the Windows host statement stays unchanged.
- `.github/workflows/check.yml` and `scripts/check_ci.py`: Python and Go suites in a macOS and Linux matrix for this
  repository, aggregate check, skip-record check.
- Documentation of the base image recipe (FR-007), outside the product commands.
- `scripts/check_repo.py`: vendor-name gate of FR-022.
- `apps/whr/internal/runtime/` (ADR 0010 layout): VM provider, driver port, fake driver and the added conformance cases.

## Informative notes

<!-- informative:begin -->
These notes name products. They are not requirements and the vendor-name gate of FR-022 skips this marked region.

- The first driver targets Tart (OD-5), and the catalog entry of decision 12 would be for its VM tool, which that vendor distributes through its own third-party
  tap (decision 17; the exact tap name is verified at implementation). The owner's own source is the repository
  wstein/homebrew-tap, named `wstein/tap` by Homebrew convention; its contents are not known here, except for the two tools of decision 18: `bdock` (a CLI for OCI tool bundles, formula offered for macOS arm64 only; Docker is needed only for its `tools volume` subcommand) and `stw` (read-only overview and stateless issue and pull request tooling for GitHub workspaces; needs Git 2.46 or newer), as described by the owner. A driver for a Linux host would likely use KVM. A driver for Hyper-V is only a
  possibility.
- Initial list for the gate of FR-022: tart, lima, utm, qemu, parallels, vmware, virtualbox, hyper-v, kvm, libvirt,
  vagrant, multipass, firecracker, wsl.
- Docker is the container runtime of Feature 008 and is outside the VM backend list of FR-022.
- Running Linux tests inside WSL 2 on a Windows host is possible and is not a target (FR-029).
<!-- informative:end -->
