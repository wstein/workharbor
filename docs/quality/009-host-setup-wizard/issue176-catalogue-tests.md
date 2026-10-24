# Issue 176: isolated catalogue loader tests

Measured 2026-10-24 against main `c71890d27c3d415e50fe15610a6f8f044b313142`.
Test commit: `8f45fd58ee9cdf952172f478e4433ef5e32fbf6e`.
Scope is the remaining loader-test gaps from [issue 176](https://github.com/wstein/workharbor/issues/176).
The baseline `TestInitialCatalogueEntries` already checks the exact production
13 IDs, defaults, four config entries, source types and measured minima. Those
assertions and production assets are unchanged; no duplicate version list added.

`TestCatalogueIsolatedGuards` first checks each valid control against the
schema and loader, then changes only the field or constraint under test and
requires loader rejection with exit 74:

| Subtest | Valid control → isolated invalid fixture |
| --- | --- |
| `agent_id` | `sample` ID → `codex`; command/source stay `sample` |
| `agent_command` | `sample` command → `codex`; ID/source stay `sample` |
| `cask_core_mismatch` | cask with `homebrew/cask` → `homebrew/core` |
| `privileged_limit` | Four valid privileged steps → five |
| `network_hosts_limit` | Eight valid distinct hosts → nine; lookup host stays declared |
| `duplicate_requires` | `sample` requires existing `second` once → twice; second has no dependencies |

The first three invalid fixtures still satisfy the schema, isolating loader
semantics. The last three fail the authoritative schema as expected. The old
self-cyclic duplicate-requires fixture is replaced by the acyclic two-item case.
The existing version-suffixed agent-source test remains unchanged.

## Mutation red evidence

After adding tests, each temporary production mutation was tested separately
with `go test -count=1 -timeout 90s ./internal/hostsetup/catalogue -run
'^TestCatalogueIsolatedGuards/<subtest>$'` from `apps/whr`:

| Subtest | Temporary mutation |
| --- | --- |
| `agent_id` | Remove `agentIdentity(item.ID)` disjunct |
| `agent_command` | Remove `agentIdentity(item.Tool.Command)` disjunct |
| `cask_core_mismatch` | Remove cask/tap mismatch disjunct |
| `privileged_limit` | Change `len(steps) > 4` to `> 5` |
| `network_hosts_limit` | Change hosts maximum argument from 8 to 9 |
| `duplicate_requires` | Change requires uniqueness argument from true to false |

All six runs exited 1 at their named subtest with
`isolated invalid catalogue accepted`. Each mutant passed the valid control;
none failed from compilation, fixture shape or another guard. The probe restored
`catalogue.go` in a `finally` block. Its final diff is empty. No production
behavior change was needed; this is test-quality work, with mutation failures
providing the measured red evidence.

## Verification and limits

- PASS: focused six-case test on unmodified production guards.
- PASS: `go test -count=1 -timeout 90s ./internal/hostsetup/catalogue` (full package once after restoration).
- PASS: `go vet ./internal/hostsetup/catalogue`.
- PASS: `gofmt` applied; `git diff --check`.
- PASS: `python3 -I -B scripts/check_repo.py` (bootstrap only; runtime not evaluated).
- NOT PASS: coordinator attempted `make check` at pristine head
  `f91036ed49f506b40ec717321f1789e278821dd0`, bounded to five seconds; it
  timed out with exit 124 (`/tmp/whr-d176-make-check.log`). No complete
  `make check` result is claimed.

No race run, standalone build, install, Docker/runtime operation, real-host
measurement or task/feature acceptance is claimed. Independent exact-head review
and PR publication remain coordinator work. This evidence does not close the
issue before owner merge or change task checkboxes, specs or owner gates.
