---
title: General agent utilities
description: "Required and preferred immutable utility profiles without repository installation authority."
weight: 3
toc: true
---

## 5.6.1 General utility profiles (D19, D44; #284)

{{< status decided >}} Werner requests general utilities through workharbor's
verified read-only tool store, independently of external skill sets. This
contract specializes §5.6: it selects no mandatory catalogue or new default,
grants no installation permission, and changes neither D44's git-in-image rule
nor the current native Codex priority. Optional utilities must not delay first
Codex end-to-end evidence or the next alpha. Implementation and compatibility
of utility artifacts remain {{< status unverified >}}.

### Selection and admission

Only explicit supervisor/operator configuration selects a versioned utility
profile. Its entries identify a utility and exact admitted artifact reference,
with requirement `required` or `preferred`. A missing selection means no added
utilities, preserving existing agent CLI/helper selection. Repository, issue,
package or model suggestions are requests only: they cannot select a profile,
install/download executables, change PATH, widen mounts or egress, or turn a
preferred tool into a required launch dependency. Supervisor configuration is
validated strictly; reject unknown fields, duplicate utilities or command names,
conflicting requirements, invalid platform tuples and unadmitted references.

Required means every selected entry must be available and verified for the
exact environment before launch; any unavailable required entry refuses start
or resume with its reason. Preferred means its absence is recorded and exposed
as unavailable while the otherwise valid run may start. A corrupt or writable
artifact/profile is a verification failure, never an optional-tool exception:
exclude it from a newly resolved profile; refuse a recorded immutable profile
whose content has changed. Do not silently run an image binary with the same
name, replace a utility with another program or pick a different release.

Availability is separate from requirement. Use typed reasons `not_admitted`,
`missing_cached_artifact`, `unsupported_platform`, `missing_dependency`,
`compatibility_unverified` and `verification_failed`; report the exact utility
and platform tuple without secrets. No unavailable result is represented as
success or an empty version. A preferred tool resolved as unavailable remains
so for that recorded run; a later installation affects new runs only.

Assess fd, jq, ripgrep and ast-grep first, and xsv's archived/unmaintained upstream
without substituting another tool. Further optional candidates include
mikefarah/yq, ShellCheck, actionlint, Gitleaks, difftastic, mlr, hyperfine and
SQLite. These are assessment candidates, not approved pins, compulsory tools,
supported-platform claims or authority to fetch their current releases.
Language-specific compilers, formatters, linters, package managers and language
servers stay in target-project toolchains, devcontainers and dependencies,
outside this catalogue. Git remains an image prerequisite under D44.

An admitted artifact has a reviewed, versioned record: canonical upstream/source
identity, exact upstream release/version, artifact URL/name and format, OS,
architecture and libc tuple, archive digest when applicable, extracted binary
path and full SHA-256, expected executable names, bounded size/entry limits,
license information, declared dynamic dependency requirements, and committed
admission evidence. Evidence records verification method and verifier version,
exact signature/attestation subject and digest, identity/issuer when checked,
and any unsigned or unverified gaps. A checksum establishes matching bytes,
not an upstream signature. Do not invent a signature, infer one from another
artifact, or trust a digest fetched from the same download as independent proof.
A pin update is reviewed source/configuration admission, never a run action.

### Bounded delivery and compatibility

Build/install explicitly outside runs using the existing host-owned store.
Never run an upstream installer, package script or downloaded binary on the
host as part of admission or extraction. Bound HTTP redirects, deadline,
compressed bytes, expanded bytes, archive entry count and each selected binary
before processing. For general utilities this contract sets new ceilings:
1 GiB compressed bytes, 1 GiB total decompressed bytes including metadata and
skipped payloads, 1 GiB per selected binary, 64 archive entries, three HTTP
redirects and a 30-minute total install deadline. Artifact-specific limits must
be positive and no larger; operator configuration may lower them. Invalid
limits refuse installation, and retries/redirects share the total deadline.
Utility enforcement must hold even with a caller-supplied HTTP client.

These are requirements for utility implementation, not existing universal
store guarantees. Currently `Store.MaxBytes` accepts a positive override of
`DefaultMaxBytes` (1 GiB), and a supplied HTTP client replaces the store's
default 30-minute client timeout (`internal/toolstore/toolstore.go`). The
existing tar extractor limits entries to 64, but its expanded-byte allowance
includes extra metadata overhead (`internal/toolstore/extract.go`). Do not
claim those defaults already enforce these utility ceilings, or change existing
agent CLI delivery through this specification.
Only reviewed HTTPS artifact origins are allowed; redirects cannot select a
new unreviewed origin or carry credentials. Store delivery needs no provider
credential and no extra agent egress allowance.

Extract only declared regular binaries into host-selected staging paths;
validate the entire archive. Reject absolute/traversing paths, backslashes,
NUL, duplicate normalized paths, case collisions, links (including hard links),
devices, sockets, FIFOs and other special files even when unselected. Reject
undeclared executable payloads and unsupported archive formats. Bound skipped
payloads and metadata too; compressed bombs and checksum failures publish
nothing. Verify archive and extracted digests before atomic immutable entry
publication. Existing store-owned relative profile links are generated by the
supervisor after validation, never accepted from an archive.

Resolve exact OS/architecture/libc and declared dependency closure from the
reviewed environment configuration. Unknown platform/libc or unavailable
closure is unavailable; musl/static does not imply glibc compatibility, and a
matching executable name is not evidence. An architecture alias, emulator or
replacement tool must not silently satisfy an entry. Native compatibility
requires committed focused evidence for the exact artifact, target image
digest, runtime/version and tested invocations. Cross-compilation, host version
output, fake-runtime checks and historic measurements of other releases do not
establish support. No real target probes are authorized by this specification.

### Immutable profiles and run provenance

A profile's canonical inventory binds contract version 1, operator selection,
resolved platform, required/preferred entries, exact artifact digests and
command names, unavailable preferred entries/reasons and capability evidence
references. Hash its deterministic sorted encoding with full SHA-256; human
profile names and shortened entry hashes are labels only. Capability inventory
reports measured commands/behavior with evidence, never broad tool permission.
Use an immutable digest-addressed profile separate from mutable display aliases;
updates create new entries/profiles rather than rewrite a digest's contents.

Verify selected entries and the profile before every start/resume. Mount only
the verified store/profile read-only through existing runtime preparation and
mount refusals. Reject command collisions with the agent CLI or supervisor
helpers; a utility selection cannot replace them. The supervisor constructs
guest PATH from absolute selected profile directories and fixed system image
directories, with no empty, relative, workspace or agent-home entries. Missing
preferred commands are not satisfied through a same-name image fallback.
For each unavailable selected command, the profile contains a supervisor-built
denial stub that exits 127 with the recorded unavailable reason, so later image
PATH entries cannot silently satisfy it. Stub bytes/digests belong to the profile
inventory, never an upstream artifact or repository script. This does not
replace system git or weaken existing client PATH/configuration protections.

Persist the profile digest and complete resolved inventory before launching,
alongside the existing agent/tool provenance. Every resume/recovery uses that
recorded profile, artifact versions, platform and availability; no current
alias, new default, newly cached preferred tool or implicit `none` substitution.
If the exact recorded content is missing, changed or incompatible, refuse the
resume and retain its original record for diagnosis. Pre-contract runs carry
an explicit legacy marker and retain their protected path, without pretending
a current utility profile was selected. Changing selection requires a new run.

A verified cache supports offline start/resume with no network fallback. Cache
misses are actionable unavailable reasons, never launch-time latest downloads.
An explicit reviewed update/rollback changes future selection to another
retained verified profile. Keep artifacts/profiles referenced by resumable runs;
removal of in-use content is refused. Updating an environment's mount requires
existing idle/ownership/rebuild locks and never happens under a live agent.
No new store root, host-home mount, runtime socket, credential handling or
subscription-login behavior is introduced; D40 remains unchanged.

### Implementation and acceptance boundaries

Implement profile resolution and provenance before admitting optional catalogue
artifacts. Serialize shared toolstore/config/service changes with existing
native/skill work (#35, #242, #283); #164's applicable pin groundwork precedes
overlapping toolstore changes. Assessment can report unavailable artifacts
without a substitute or delay to the Codex end-to-end priority. The utility delivery
ceilings above require explicit enforcement; profile metadata is capped at 64 KiB,
32 utilities and 64 command names, with each name/reference at most 128 bytes.
Reject over-limit inventory rather than truncate required selection.

Focused regressions cover required refusal/preferred absence, unauthorized
selection, collisions, platform/dependency/evidence mismatch, every extraction
refusal and byte limit, offline cache miss, immutable inventory tampering,
recorded resume, retained old profiles and update/rollback affecting new runs
only. They must prove that refusal launches no agent and corrupt artifacts reach
no guest PATH. Exact-SHA independent security review is required before source
clearance. Artifact admission evidence and target compatibility remain separate;
this page implements no utility profile, binary pin or native support.
