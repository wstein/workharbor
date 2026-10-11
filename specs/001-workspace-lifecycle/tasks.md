# Feature 001 tasks

All tasks are pending. Each task ends with focused commits and verification evidence.
No parallel implementation is assumed; isolate independent work only when authorized.

- [ ] T001 Pin toolchain/dependencies; scaffold Phoenix and Go apps; add format/build/test CI.
- [ ] T002 Implement Workspace, Host, Operation, Storage, Session migrations and owner login (FR-001, FR-006).
- [ ] T003 Define validated versioned protocol and negative contract fixtures (FR-003, FR-011).
- [ ] T004 Implement enrollment, mTLS identity, rotation/revocation and scoped commands (FR-002, FR-003).
- [ ] T005 Resolve trusted config revisions/submodules and reject unsupported devcontainers (FR-004, FR-005).
- [ ] T006 Implement Docker provision/inspect/stop/start with persistent storage (FR-007).
- [ ] T007 Implement durable idempotency, recovery/reconciliation, redacted events (FR-006, FR-009, FR-010, FR-011).
- [ ] T008 Implement authorized PTY relay and one-time session grants (FR-008).
- [ ] T009 Implement browser dashboard and operation diagnostics (FR-009).
- [ ] T010 Run US1–US4 on real host, security negative tests, browser evidence and runbooks (FR-001–FR-012).

Dependencies: T002 after T001; T003 after T002; T004/T005 after T003;
T006 after T004/T005; T007 after T006; T008/T009 after T007; T010 after T008/T009.
Use `$speckit-analyze` before implementation and `$speckit-converge` after each slice.
