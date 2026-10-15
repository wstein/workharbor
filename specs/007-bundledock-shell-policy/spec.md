# Feature 007: BundleDock workspace shell policy

Status: proposed owner-review scope for issue #81. The direct implementation request
covers a bounded executable policy port with Starship disabled; it does not approve
prompt trust, artifact execution, attachment or activation. Feature 002 remains the
Symphony smoke feature. Full workspace integration is not implemented by this slice.

## User scenario

US1: a future trusted workspace launcher obtains a deterministic shell environment
and fixed bash/zsh system-rc fragments for a previously authorized, immutable,
read-only toolbox. Image binaries retain priority. Agent-writable prompt inputs
never cause this policy to enable BundleDock Starship.

## Requirements

- FR-001: Supply a pure host-agent policy port. It performs no filesystem, network,
  subprocess, mount or provider effects and grants no attachment or execution trust.
  Caller authorization and observed artifact/mount verification remain mandatory.
- FR-002: Admit only the fixed toolbox layout at `/opt/bundledock/toolbox`, Linux
  amd64/arm64, immutable SHA-256 artifact and exact source revision. Reject missing
  required shell entry points or unsupported observations. Absence is explicit.
- FR-003: Force `BDOCK_STARSHIP=0` in every returned environment, including absent
  attachment and inherited enablement. No prompt enablement API exists.
- FR-004: Remove every exact toolbox bin/share entry, append bin last and prepend
  share first once when attached and shortcuts enabled. Preserve other entries in
  order. Unset/empty XDG data directories and lists emptied by exact toolbox removal
  retain `/usr/local/share:/usr/share`.
  Empty, relative or malformed search entries refuse; PATH must remain nonempty.
- FR-005: `BDOCK_SHORTCUTS=0` omits toolbox share and both rc fragments, providing
  complete Nushell opt-out while preserving toolbox binaries. Other opt-outs pass
  through; they cannot override FR-003. Return fixed readable-file and interactive guarded bash/zsh fragments
  only for their interactive system rc files; never source them from POSIX sh.
- FR-006: Do not mutate caller data. Repeated application is idempotent. Attachment
  removal strips stale exact paths and returns no startup fragments.
- FR-007: Future adoption applies the result before a new shell or console can
  autoload; reconnect to an existing persistent session must not claim environment
  changes. Same-user code can still change its own startup files and environment;
  this is initialization policy, not hostile-process isolation.

## Success criteria and limits

SC-001: independent failure-first table tests demonstrate descriptor/environment
refusals, order, defaults, inherited enablement denial, opt-out, immutability,
absence/removal and idempotence. SC-002: format, focused Go tests/vet and repository
checks pass on the reviewed head. SC-003: source contracts identify the later
resolver, attacher and new-session launcher responsibilities without widening T009.

A unit-tested plan does not prove a mount exists, digest integrity, execution trust,
actual vendor autoload, shell startup ordering or prompt rendering. Native Docker
and all-four-shell acceptance are future integration gates, recorded as unverified.
This slice does not establish full issue-81 integration or its remaining evidence.
