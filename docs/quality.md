# Quality register

Reviewed: 2026-10-11. Owner: project maintainer.

| Area | Evidence | Status / next gate |
| --- | --- | --- |
| Spec Kit | Upstream CLI-generated assets, pinned provenance | Scaffold ready |
| Domain and ownership | Constitution, ADRs, contracts, feature requirements | Design only |
| Repository harness | Links/JSON/boundary checks plus negative regression tests | Executable |
| Product runtime | No Phoenix/Go applications yet | T001–T010 pending |
| Docker / persistence / PTY | Acceptance scenarios only | Real-host tests required |
| Symphony | Pinned workflow and operator runbook | Not installed or activated |
| Resource footprint | No comparable runtime workloads | Measure after T001 |
| License | Upstream MIT notice retained | Owner chooses project license |

Do not upgrade a design status to implemented without evidence. Review this register
with every release; convert recurring drift into small, targeted fixes.
