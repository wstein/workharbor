# Feature 007 tasks

Status: proposed bounded direct-request scope, not approved Symphony dispatch.

## US1: Deterministic safe shell initialization plan

- [ ] T001 [US1] Specify the pure launch-policy contract, author failing table tests, implement `apps/host_agent/internal/workspaceshell/`, and record focused Go/repository checks plus independent review. Validate fixed-layout attachment descriptions; force Starship off; preserve PATH priority/XDG defaults; implement complete shortcuts opt-out, absence/removal, input immutability and idempotence (FR-001–FR-007; SC-001–SC-003).

## Dependencies and future adoption

T001 uses the existing host-agent module and policy source. The policy
last-modification commit `b2b725eedfad22bc50e2d69563b38694becc2e25` is an ancestor
of the reviewed `ce3cfc83f766aa4f41be092f6b55dee1d98b2754` baseline. Original
pilot `832dd63000257bad01656154b639251d643f2a2d` remains historical provenance,
not a current prerequisite; its object is unavailable in this checkout. T001 does
not depend on a working resolver, attacher or launcher because it executes none. Those missing prerequisites block
full issue-81 integration and any runtime adoption, not pure policy unit tests.
No additional tasks are invented for other owners. Feature006 T013/T014 will choose
and review the actual launch adapter after their own prerequisites are satisfied.
