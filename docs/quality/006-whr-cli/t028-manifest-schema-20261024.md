# T028 manifest-schema acceptance reconciliation

Inspected source: `22f73010218de15a0d92d2921e9026b37bfea50d`, 2026-10-24.
Scope: Feature 006 T028 / FR-058, package C wave 1a. The existing implementation
satisfies this bounded schema/checker/vector scope; no product correction or
replacement implementation is needed. This record changes no task checkbox,
specification, plan, provider input or runtime behavior.

## Existing delivery and current decision

[PR #122](https://github.com/wstein/workharbor/pull/122), “finalize manifest schema
v2 and type-exact checks (T028),” merged on 2026-10-17 at
`3c32f983f0663f8744629d34ad9e2e54fbd032b5`.
[PR #148](https://github.com/wstein/workharbor/pull/148), the subsequent T028
diagnostic/regression correction, merged on 2026-10-20 at
`04d3aebacd05f78d280fac56ce724553fcb2c61c`. These merged states and commit IDs
were read directly with `gh pr view --json state,mergedAt,mergeCommit,commits`.

The inspected history already contains the implementation commits
[a09723eb](https://github.com/wstein/workharbor/commit/a09723ebc9358e60faf5b89e577fbbf6bfccf13f),
[e683eddb](https://github.com/wstein/workharbor/commit/e683eddbdc763607a5af1edfa4647ac7e4447a65)
and [0da6e745](https://github.com/wstein/workharbor/commit/0da6e74552b13b67162994efeba325022baf0286).
`git merge-base --is-ancestor <commit> <inspected-source>` returned 0 for each
and for both merged PR commits above. GitHub's commit-to-pulls API returned
empty arrays for those three implementation SHAs; the PR links establish
historical delivery independently, rather than claiming an API association.
The original a09723eb commit message records failing-first missing-entry-point
evidence; that historical RED was not rerun or represented as fresh evidence.

The current console default is **plain**, with explicit `herdr` retained.
[Feature 006 FR-058](../../../specs/006-whr-cli/spec.md#functional-requirements),
[Feature 010 FR-012/020 and OQ-7](../../../specs/010-host-and-workspace-panes/spec.md),
and merged [P3 #222](https://github.com/wstein/workharbor/pull/222) supersede
T028's historical `herdr` default wording. The current schema and tests agree;
this record preserves that decision. P3's merged commit
`efc0715046196f360674dba3f7789ca09b546fab` and implementation commit
`e0a9afa7b4c7d8b8064fe044530130642331f79e` are ancestors of the inspected source.
The [P3 review record](../010-host-and-workspace-panes/p3.md) already explains
the retained historical task wording.

## Acceptance mapping

| Requirement | Existing source and current proof |
| --- | --- |
| Version 1 has exactly four fields | [Schema](../../../contracts/workspace/workharbor.schema.json), `$defs.v1`: four required properties, `additionalProperties: false`; `test_version1_rejects_version2_fields` rejects all six v2 optional fields on v1. |
| Version 2 has only the reviewed optional settings/project | Same schema, `$defs.v2`: `name`, `start_dir`, `human_shell`, `console_mode`, `shortcuts`, `project`; project is closed with `url` and `path` required. `test_version2_defaults_documented` verifies `.`, fish, plain, on, and no invented name/project default. |
| Python reads the schema and every shared vector | [check_repo.py](../../../scripts/check_repo.py): `_schema`, `_versions`, `manifest_findings`, `manifest_document_findings`, `vector_bytes`, `vector_case_errors`, `vector_errors`, `check`. [Python tests](../../../tests/test_workspace_manifest_vectors.py): `test_workspace_manifest_vectors`, `test_check_follows_the_schema_file`, `test_repository_check_runs_vectors_and_reports_disagreement`. |
| Raw input, size and exact version types | `test_vector_kinds_cover_raw_byte_and_size_cases`, `test_schema_version_is_type_exact`, `test_hostile_bytes`; shared vectors reject `true`, `1.0`, `1e0`, string and v2 float versions, and oversized input. |
| Existing examples stay valid and resolver-compatible | `test_example_manifests_still_valid`, `test_examples_stay_on_the_schema_version_the_resolver_accepts` and symlink/example hardening tests; all three committed `workharbor.json` examples remain version 1. |
| Every case is covered by CI consumers | [CI manifest](../../development/ci-acceptance.json) requires all four task-named Python tests plus raw/size/type/schema hardening IDs: 18 of the module's 21 tests are explicitly pinned. The other three are discovered hardening tests. Go `workspacecfg/TestManifestVectors` is separately required and iterates every case of the same file. |
| Resolver extension remains separately gated | [resolver.go](../../../apps/host_agent/internal/config/resolver.go) still requires exactly `schema_version`, `devcontainer_path`, `projects_path`, `bundles` and `json.Number("1")`. [Plan](../../../specs/006-whr-cli/plan.md#resolver-follow-up-and-rebasing-rules-h77-round-5) retains T035 after T009/T028 and its own owner-approved scope. Version 2 remains fail-closed and not creation-ready. |

The shared [vectors](../../../contracts/workspace/vectors.json) have 145 unique
cases, matching `MIN_VECTOR_CASES = 145`: 115 document, 21 raw-text, seven
hex-byte and two generated-byte cases. `vector_case_errors(cases)` returned no
disagreements. The generated cases are exactly 65,536 bytes (accepted) and
65,537 bytes (`manifest.too_large`). The console expectation cases are
`v1-valid: plain`, `v2-minimal: plain`, `v2-full: herdr`.

## Executed checks and limits

Local tools: Python 3.13.5; Go 1.27.2, linux/arm64. All commands below ran against
the inspected source without modifying implementation files.

| Command / probe | Result |
| --- | --- |
| `python3 -I -B scripts/check_repo.py` | Exit 0; repository bootstrap checks passed, including this record's local links; product runtime not evaluated. |
| `python3 -B -m unittest discover -s tests -p test_workspace_manifest_vectors.py -v` | Exit 0; 21 tests passed, including all four T028 task-named tests. |
| In `apps/whr`: `go test ./internal/workspacecfg -run '^(TestManifestVectors\|TestExampleManifestsValid\|TestManifestMissingConsoleModeDefaultsPlain\|TestManifestExplicitHerdrPreserved)$' -count=1 -json` | Exit 0; 149 passed test identities: 145 vector children and four top-level tests; no skipped or failed identities. |
| In-memory `_is_int` mutation to loose equality; existing `test_schema_version_is_type_exact` | Expected RED: six assertion failures, zero errors. |
| In-memory v1 `additionalProperties` mutation to true; existing `test_version1_rejects_version2_fields` | Expected RED: six assertion failures, zero errors. |
| In-memory `MANIFEST_MAX_BYTES` mutation to 65,537; existing `test_workspace_manifest_vectors` | Expected RED: one assertion failure, zero errors. |

Each mutation was restored immediately; none changed a repository file. Local
raw Go events and mutation logs are retained at
`/tmp/whr-c-t028-proof-20261024/`; that ephemeral location supplements this durable
record and is not required to interpret it.

The full `make check` gate is delegated to the coordinating lane and is **not
claimed passing here**. This focused source evidence does not replace required
CI on the published candidate. No standalone build, installation, host-agent
change, live Docker/browser/SSH test or resolver/provider admission was performed.
T029/T030 and the final package task/progress reconciliation remain separate PRs;
this record establishes no T035 or wave 1b authorization.
