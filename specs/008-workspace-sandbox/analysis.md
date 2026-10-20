# Feature 008 consistency analysis

Status: author's read-only analysis of spec.md, plan.md, data-model.md, contracts and tasks.md at the authoring head; not an
independent review and not approval. Re-run `$speckit-analyze` before any task starts.

## Checks performed

| Check | Result |
| --- | --- |
| Every FR (001 to 035) has a task or an explicit non-increment note | FR-026 is covered by the T005 check library and the T010 proposal (command per OD-5); FR-028, FR-029, FR-035 covered by T004 and T016; FR-030 by T007 and T013; all others by T001 to T014; command registration by T016 |
| Every SC (001 to 012) has a named test | yes, see plan evidence table; SC-009 is the check command set |
| Test names in tasks equal names in plan and quickstart | yes (grep-verified at authoring) |
| Exit codes use only ADR 0013 values | yes: 64, 65, 68, 70, 71, 72, 73, 74; refusals travel as an additive optional `error.refusal` field (OD-2, approved; registration is T016) |
| Frozen contracts untouched in Stage 1 and T011 to T015 | yes; per-PR diff check against the merge base; T010 proposes; T016 and T017 are the owner-gated exceptions |
| Backend neutrality (FR-034) vs capability table | no provider-kind field in the data model; T009 tests it |
| Terminology (Mac host, Docker VM, sandbox backend) | consistent in spec, plan, contracts, research |
| Task dependencies acyclic and ordered | yes (machine-readable table) |

## Findings

- **A-1 (medium)**: the agent profile v1 keys and the host-only catalogue extension keys are author proposals; they were
  accepted by the owner as v1 (OD-3, 2026-10-20); the first tests still verify real git behavior.
- **A-2 (medium)**: `sandbox exec` status passthrough would extend ADR 0013's list; needs a separate ADR 0013 amendment (OD-1, decided 2026-10-20); `exec` is outside Stage 1 and T016.
- **A-3 (resolved to OD-5)**: an earlier draft read Feature 006 T015 as a control-plane server and extended the owner's rule to
  it. That reading is withdrawn: FR-009 and T015 define `setup` as Linux/systemd enrollment that mutates the host receiving the
  host agent. The owner statement of 2026-10-20 ("whr setup arbeitet auf dem Mac host, der Linux VM host wird von whr nie
  veraendert") concerns the Docker VM. Which command carries the Mac-host git configuration step is OD-5 (still open; option B recommended); T015 is unchanged.
- **A-4 (low)**: the preflight set-and-unset probe writes to the owner's real configuration (not an
  open decision). An owner who prefers a read-only preflight would change FR-022.
- **A-5 (low)**: T013 depends on 006/T038 being on `main`; if not, work stops (plan, handoff).
- **A-6 (info)**: the 006 convention of `analysis-first-increment.md` is a review record; this file is an author analysis and
  gets review rounds appended when reviews occur.
- **A-7 (info)**: review round 1 of head 747de4c (NOT CLEAR, 3 High, 5 Medium, 6 Low) is addressed in the amended commits: agent
  detection removed from FR-024; writable host-config risk stated; hardened export; per-profile scoping; per-PR diff guard instead
  of a hash test; OD-5; git-control environment scrub; failed-instance export and cleanup capability refusal; numbering; SC for
  export; source citation; paths; ADR rule citation; worktree handling.
- **A-8 (info)**: review round 2 of head 0967ae9 (NOT CLEAR, 2 Medium, 6 Low) addressed: T016 and T017 named as SC-008 and T015
  exceptions with a three-PR split; FR-027 profile-precise; stale Mac host wording; boundary list; MUST NOT; export working
  directory and snapshot hashes; scrub-then-set for `GIT_CONFIG_GLOBAL`; the 008 error mapping renumbered FR-035 so bare FR-030
  is unambiguous (Feature 006 FR-030 is always named with its feature).
