# Quality register

Reviewed: 2026-10-12. Owner: project maintainer.

| Area | Evidence | Status / next gate |
| --- | --- | --- |
| Spec Kit | Upstream CLI-generated assets, pinned provenance | Scaffold ready |
| Domain and ownership | Constitution, ADRs, contracts, feature requirements | Design only |
| Repository harness | Links/JSON/boundary checks plus negative regression tests | Executable |
| Feature 004 local automation | [T017 evidence](../specs/004-speckit-symphony-automation/quickstart.md#t017-combined-local-acceptance): 422 qualified cases at `ebe4833`; raw 41/five failures separately | T001–T017 local tooling/fixture acceptance; T018–T020 pending, live guard/provider/isolation/safe-label gates blocked |
| Product runtime | No Phoenix/Go applications yet | Feature 001 implementation pending |
| Docker / persistence / PTY | Acceptance scenarios only | Real-host tests required |
| Symphony | Pinned workflow and operator runbook | Not installed or activated |
| Resource footprint | No comparable runtime workloads | Measure after T001 |
| License | EUPL-1.2 project license; upstream MIT notice retained | Recorded |

Do not upgrade a design status to implemented without evidence. Review this register
with every release; convert recurring drift into small, targeted fixes.
