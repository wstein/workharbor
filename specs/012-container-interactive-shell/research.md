# Feature 012 research

Status: design options for review; nothing is run or claimed. The runtime facts named in the spec's "Verification before the
plan" are unverified and stay so until T002. Nothing below reopens an owner decision (D1 to D8).

<a id="r-1"></a>
- **R-1 Atomic `ready` to `running` (D8c).** Feature 008 defines no instance-record store; Feature 011 FR-009 (draft) describes a
  local record store taken under an exclusive lock. Options: (a) reuse that store and lock, and update under it; (b) one record file
  per instance with an exclusive create/rename compare-and-swap on a version field; (c) a single lock file plus read-check-write.
  The choice is the plan's per D8c; this document proposes (a) if Feature 011 lands first, otherwise (b) inside Feature 008/T003, and
  asks the owner (plan open question 2). T005 tests only the contract: exactly one winner, loser gets `refused.invalid_state`,
  no mechanism named in a test.
- **R-2 Why one client child and no relay.** The spec follows Feature 010: `whr` supervises one container runtime client with the
  real descriptors, so it can run the FR-016 query after the client ends; replacing the process by exec would prevent it, and a
  proxy would put `whr` in the byte stream, contradicting FR-011. Rejected: an engine-API pty relay (would relay and could filter,
  a different feature), a cleanup daemon (not in scope, FR-017).
- **R-3 Detach disabled (D2).** Feature 010 fixes a rarely typed sequence for `shell --local`; here the sequence is disabled so the
  session ends only with the shell or the client. V2 decides whether the runtime can; if not, no `pty`/`enforced`.
- **R-4 Environment route (FR-009).** Values must not appear in argv. Candidates the spec leaves open: container create-time
  environment inherited by the exec, and a route supplied to the client that does not expose values in the process list. Which one is
  V3; T007 asserts through a fake route, T016 uses the verified one.
- **R-5 End-of-exec and reconciliation query (V5).** The command-line client prints no exec id, so a read-only query that tells
  whether the exec process ended is needed; the spec names the engine API as a candidate. If only the engine API works, the adapter
  talks to the already selected `unix://` socket with no SDK (a dependency question for the owner, not decided here). Without a
  query, row 3 of FR-016 cannot be told from row 4 and S12-C7, hence FR-003, cannot be met.
- **R-6 Reuse of Feature 010.** `localdocker.ResolveEndpoint`, the client environment allowlist and 5-second budget, and the
  `localshell` TERM validation and exit classification already exist. Reuse by narrow extraction, not copy (as 010 did for
  `workspacecfg`). Differences stay explicit: detach disabled here; instance records and ownership labels here, container-name
  selection there; confirmation for `untrusted_job` only here.
- **R-7 Not decided.** A `Shell` operation for a second provider (Feature 011 owns its own value); `exec`; reattach; whr-side
  resize handling; terminal-mode restore after a killed client (plan open question 5).
