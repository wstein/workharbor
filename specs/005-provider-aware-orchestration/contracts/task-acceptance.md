# T001 acceptance inventory: provider-aware development workflows

**Prepared**: 2026-10-13
**Guard/oracle baseline**: `cdc47c4a6878b541b4a9425775c98561da157bdd` (owner-merged PR #62).
**Status**: Proposed documentation preparation for owner review. T001 remains unchecked; no implementation,
profile grant, task dispatch, runtime activation or live-provider verification is authorized by this file.

This contract freezes the inputs and expected outcomes for the [29 pending tasks](../tasks.md), using the accepted
[model](../data-model.md) and [action table](cli-and-host.md#canonical-actions-and-capability-mapping).
The 93 future methods below do not exist yet. Baseline test collection is an inventory observation, not a test pass.
All fixture identities, authority references and timestamps in this document are synthetic; they confer no permission.

## Bounded preparation scope and approval boundary

This preparation writes only specs/005-provider-aware-orchestration/contracts/task-acceptance.md. It changes no task
checkbox, implementation, existing test/CI policy or Features 001–004. Owner review of this contract and an explicit
source/context/check binding are required before implementing T002/T003 or treating T001 as accepted. A merge is not
a profile grant or live-dispatch authorization. Refresh affected approvals when the reviewed semantics change.

## Canonical identity and fixture inputs

Canonical bytes are UTF-8 JSON with sorted object keys, separators (',', ':'), ensure_ascii=False, allow_nan=False,
no trailing newline and no Unicode normalization. SHA-256 digest fields use exactly 64 lowercase hex characters;
SHA-256 evidence references may use an explicit sha256: prefix, but never compare a reference string to a digest.
No unknown typed-record fields, duplicate keys, floats/non-finite values or unpaired surrogates are valid.

V01–V03 test the generic canonical primitive, not complete profile records. V04/V06 are structurally illustrative
synthetic profiles with placeholder evidence digests: their existence establishes no permission/capability/grant.
V05 is the identity basis shared by policy.profile_digest_basis and capability.profile_identity_basis: hash the
ExecutionProfile object after removing only policy_digest/capability_digest. This avoids a circular evidence digest.
V07 tests only the existing guard context digest primitive; its minimal object is deliberately not a valid guard
execution context. Full valid guard scenarios must be built from the accepted SingleTaskFixture and actual fixture
Git identities, never from these mathematical examples or copied template placeholders.

Each vector below gives the exact canonical text and expected digest. Decode the text as UTF-8 bytes without LF.

### V01

```json
{"a":1,"b":2}
```

SHA-256: `43258cff783fe7036d8a43033f830adfc60ec037382473548ac742b888292777`

### V02

```json
{"interpretation":"é"}
```

SHA-256: `32e999749698333a82a710b36fd89c999f9148f16950ea88fd57e220fc3f6ef2`

### V03

```json
{"interpretation":"é"}
```

SHA-256: `c0cdd7625ee80ae15fbcc75fdf05238d4898e47cb096f406774dcb9883fc1654`

### V04

```json
{"adapter_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","agent":"codex","agent_version":"fixture-1","authentication_kind":"api-key","backend":"fixture-backend","capability_digest":"2222222222222222222222222222222222222222222222222222222222222222","execution_mode":"direct-headless","policy_digest":"1111111111111111111111111111111111111111111111111111111111111111","profile_id":"fixture-codex","project_id":"fixture-project","provider":"fixture-provider","runtime_revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","schema_version":1,"secret_ref":null}
```

SHA-256: `2c319dc47a1f73160cfc6b1480c3c58c2f328066905cdaede4ab3a78fe94777f`

### V05

```json
{"adapter_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","agent":"codex","agent_version":"fixture-1","authentication_kind":"api-key","backend":"fixture-backend","execution_mode":"direct-headless","profile_id":"fixture-codex","project_id":"fixture-project","provider":"fixture-provider","runtime_revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","schema_version":1,"secret_ref":null}
```

SHA-256: `1589042af1ebfec862a563a3187296c19f4645d9e3a4a73f25e35d102b197fa7`

### V06

```json
{"adapter_revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","agent":"codex","agent_version":"fixture-1","authentication_kind":"subscription-native","backend":"fixture-backend","capability_digest":"2222222222222222222222222222222222222222222222222222222222222222","execution_mode":"direct-headless","policy_digest":"1111111111111111111111111111111111111111111111111111111111111111","profile_id":"fixture-codex","project_id":"fixture-project","provider":"fixture-provider","runtime_revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","schema_version":1,"secret_ref":null}
```

SHA-256: `1e8411870beeeb2d0e1c3dc10cf73f26c228571d5fe7e35b30eb66cdd2cbefae`

### V07

```json
{"approval":{"context_digest":"8e3eb00deb18881552770d4ca5c11f9120e97ed069dcdc940980f623e1cadcf9","evidence_ref":"fixture-review","owner_ref":"fixture-owner"},"feature":"synthetic","mode":"implement","schema_version":1,"task_id":"T001"}
```

SHA-256: `0f151429306c8594b7a5f1e7e4ab378005542e87aa8e961610110b54b1be7b21`

V02 and V03 are visually similar but byte-distinct: UTF-8 `c3a9` versus `65cc81` inside interpretation.
For V07, the approval context digest (remove only approval.context_digest before hashing) is `8e3eb00deb18881552770d4ca5c11f9120e97ed069dcdc940980f623e1cadcf9`;
the full canonical context-copy hash is `0f151429306c8594b7a5f1e7e4ab378005542e87aa8e961610110b54b1be7b21`. Do not interchange them.
Metadata such as display names/assessment time stays outside the profile object; it is not an unknown field silently
ignored inside a strict ExecutionProfile. Reordered object keys keep the same digest; adding LF changes the byte hash.

## Finite input/authority/state vector catalogue

The named families below define finite required variants; a range means every listed vector, not a sample.
For every schema-required field, apply one deletion, one unknown key and one wrong-primitive mutation individually
against the valid synthetic control. Boolean values are not integer versions/timestamps/generations. Use fixed
synthetic now=2000, policy interval [1000,3000) and grant interval [1500,2500); these are not real dated permission.
All malformed inputs reject safely; all semantically invalid but well-formed protected requests block before action.

| Vector | Frozen input/condition | Required observation |
| --- | --- | --- |
| J01 | Valid UTF-8 object and reordered keys | Canonical bytes/digest match. |
| J02 | Duplicate keys, including nested records | Reject; never last-key-wins. |
| J03 | One unknown typed-record field | Reject, not ignore. |
| J04 | One missing required field at a time | Reject; no implicit default. |
| J05 | Wrong primitive per field; bool in integer fields | Reject. |
| J06 | Float, NaN, Infinity and -Infinity | Reject. |
| J07 | Invalid UTF-8 byte sequence | Reject without echoing input. |
| J08 | Unpaired surrogate after JSON decoding | Reject safely before hashing. |
| L01 | Valid JSON padded with whitespace to 1,048,576 / 1,048,577 bytes | Accept inclusive bound / reject overflow without truncation. |
| L02 | 100 / 101 profiles | Accept bound / reject overflow. |
| L03 | 100 / 101 evidence references in one record | Accept bound / reject overflow. |
| L04 | 1,000 / 1,001 events; 16 MiB DB bound | Preserve existing records; block an over-limit append; no implicit archival. |
| L05 | ID lengths 1 and 128 / 0 and 129, ASCII allowed set / non-ASCII or slash | Accept valid IDs / reject invalid; never use raw IDs as paths. |
| L06 | Relative artifact path / absolute, traversal, alias, symlink and nonregular paths | Accept approved root-contained regular path / reject unsafe inputs. |
| P01 | Codex, Claude Code, Antigravity synthetic candidates | Each starts unknown/unverified/absent, both flags false. |
| P02 | Change backend/auth/mode/agent/version/source/runtime/secret/evidence individually | New binding; no neighboring profile support or grant reuse. |
| P03 | native-interactive, direct-headless, orchestrated versus task modes | Keep route/profile modes distinct from implement/handoff-only/rework. |
| P04 | Wildcard/range version, unlisted tuple, proposed API/cloud route | Reject invalid tuple or leave proposed route ineligible; no adapter call. |
| P05 | secret_ref null or opaque host reference / input-selected file, env or command | No credentials are read; candidate cannot choose a credential source. |
| E01 | Exact / mismatched service, agreement, action, mode and reviewer provenance | Only matching trusted evidence can support permission. |
| E02 | now=999,1000,2999,3000 against policy interval | Unknown, potentially valid, potentially valid, expired/unknown respectively. |
| E03 | Equal, reversed, missing or non-finite review interval | Reject invalid record; never continuing permission. |
| E04 | Conflicting sources or inaccessible source at renewal | Unknown; retain old evidence for audit only. |
| E05 | Missing trusted time or rollback below last trusted observation | Block; candidate timestamp cannot replace authority. |
| E06 | Primary-source text/reference copied into untrusted JSON | Does not authenticate reviewer or source applicability. |
| E07 | Change policy evidence digest after assessment | Invalidate old grant; zero protected callbacks. |
| E08 | Requested / sibling capability action, missing required capability | Only exact action/all required capabilities can verify. |
| E09 | Exact / changed implementation, runtime, agent and profile revision | Changed or unavailable proof blocks. |
| E10 | Offline/local evidence used for actual-provider execution | Unverified/ineligible, never live. |
| E11 | Successful login or renamed executable without adapter proof | Unverified/ineligible. |
| E12 | Proposed adapter with permission only / fixture success only | Remains proposed/unverified for real provider actions. |
| G01 | Missing authority or forged labels/owner_ref/install/agent declaration | Unknown/absent; no callback. |
| G02 | Exact / one mismatched project/profile/policy/capability/mode/action/owner target | Only exact current attributable target can authorize. |
| G03 | Valid profile grant with missing task/source/rework/review/publication/live approval | Affected execution action blocks. |
| G04 | now=1499,1500,2499,2500 against grant interval | Inactive, potentially active, potentially active, expired respectively. |
| G05 | Equal/reversed/non-finite grant validity or rollback | Reject/block; no authority resurrection. |
| G06 | Revoked execution grant without separate inspection authority | Observation blocked; records retained. |
| G07 | Current scoped inspection decision with revoked execution grant | Read only; both execution flags false; no checks/provider/publication. |
| G08 | Scoped safety decision / missing or uncertain stop capability | Reference executor remains unavailable; uncertainty is not cessation. |
| B01 | permitted + verified + active for exact execution profile/action | profile_eligible=true only; pure assess task_eligible=false. |
| B02 | Vary each axis independently: all other policy/capability/grant states | profile_eligible=false, task_eligible=false. |
| B03 | Positive profile and missing/failed task/source/prerequisite/scope/review/claim gate | profile_eligible may remain true; task_eligible=false, action blocks. |
| B04 | Positive profile plus all current task gates in synthetic protected action | Both true only for that exact task/action, not future actions. |
| B05 | eligible alias, omitted/null/nonboolean flag, or task=true/profile=false | Invalid envelope; never execution success. |
| B06 | Authorized status/inspect/lookup | Both execution flags false even on observation success. |
| B07 | Separately scoped safety request | Both execution flags false; no real stop adapter supplied. |
| A01 | Every exact action/class/operation/capability row in accepted action table | Exact dispatch and requirements, no aliases or substitutions. |
| A02 | entry, completion, wildcard, unknown, duplicated or case-changed token | Reject; internal boundary names are not new grant aliases. |
| A03 | status/inspect/lookup/safe-stop inside execution-grant allowed_actions | Reject; separate authority required. |
| A04 | Remove each mapped capability one at a time | Requested action blocks; siblings do not satisfy it. |
| A05 | assess/exercise or future action requested as executable grant/CLI operation | Preview/fixture boundary only; no new live operation. |
| A06 | guard operation -> exact action, task mode -> unchanged G(a,m) set | Preserve per-operation classification and actual retained guard checks. |
| F01 | New disposable Git/state roots with isolated HOME/environment | No mutation outside fixture. |
| F02 | Real repository, real state root or path alias supplied to exercise | Reject before action. |
| F03 | Inject live port, credential discovery, network or account rotation | Reject; instrument callback/call count. |
| F04 | Fixture authority reused for ordinary guarded/real request | Reject; no grant escape. |
| F05 | Both synthetic routes with provider/tracker/Symphony unavailable | Local permitted fixture work remains possible; no live claim. |
| F06 | Protected feature/code/test oracle mutation | Block/report drift; never rewrite baseline to match. |
| S01 | Host-owned private outside-repo state root / candidate override | Accept authorized root / reject override. |
| S02 | Symlink component, hardlink, FIFO/socket/directory file or unexpected owner | Reject before outside writes or blocking reads. |
| S03 | Non-private mode or unsupported/shared filesystem | Block, never approximate isolation/locking. |
| S04 | Canonical stable lock inode, competing holders, holder termination | One effective holder; no unlink/recreate lock race. |
| S05 | SQLite intent commit failure | No protected invocation; retain predecessor/state. |
| S06 | File/directory flush, rename or result-persistence failure | Incomplete/reconcile-required; do not claim durable success. |
| S07 | Coordinator -> guard/handoff lock order; no SQL write transaction across call | No nested-lock deadlock or unbounded DB wait. |
| S08 | Wrong expected generation or stale executor | Conflict, no loser action. |
| S09 | Restart after each intent/result boundary | Reconcile existing identities; no implicit steal/replay. |
| D01 | Literal approved argv/cwd/pins/interpreter versus injected shell/import/plugin | Only trusted literal invocation; no shell. |
| D02 | Known raw guard exit versus missing/malformed/lost response after invocation | Preserve raw failure; unknown effect -> reconciliation, not retry. |
| D03 | Token canaries in malformed values, arguments, output and exceptions | No canary in returned/persisted diagnostics. |
| D04 | Timeout or successful parent leaves descendants | Own group stops/drains before cleanup; uncertainty blocks. |
| D05 | Actual new-test discovery missing/extra/duplicate/empty/skip/xfail/error/unexpected result | Acceptance fails; no test-authoring exemption. |
| O01 | Same approved fixture task, normal completion through both routes | Same guard outcomes, one implementation/publication identity. |
| O02 | Complete then interrupt before commit/handoff | Resume handoff-only; fresh checks/review; no reimplementation. |
| O03 | Authorized corrective successor through both routes | Same corrective approvals; retained predecessor and new immutable binding. |
| O04 | Missing runtime/tracker/guard capability | Block affected action; preserve local evidence. |
| O05 | Unknown publication write or missing conditional safe-label support | Incomplete; reconcile stable identity, preserve unrelated labels. |
| O06 | Final label operation after durable commits/PR/review/evidence | No separate ready-label removal or required later remote action. |

## Lost-response and transfer vectors

Every variant starts with a fully valid synthetic profile/task control so an earlier missing gate cannot masquerade
as the intended rejection. Use distinct operation IDs and guard-attempt IDs; a retained predecessor ID is never a
claimed response ID for a new creation operation. Digests and baseline values must be independently varied.

| Vector | Injection / observation | Required result |
| --- | --- | --- |
| R01 | First begin, absent journal and otherwise empty namespace, exclusive creator under canonical lock | Empty pre-call set permitted; lock metadata only, no journal/context/attempt yet. |
| R02 | First begin with legacy JSON, sidecar, orphan context/publication or journal appearing during snapshot | Block, preserve state; no empty snapshot or guard call. |
| R03 | begin commits; response lost before coordinator records ID | Unique new full intent/event match adopted once; begin invocation count stays one. |
| R04 | rework commits; response lost before coordinator records ID | Unique linked successor adopted once; predecessor/binding/events retained. |
| R05 | Only pre-call matching-context record or no new record | Block as reconcile-required; no blind creation. |
| R06 | Two matching new records or unrelated conflicting new records | Block; never first-row selection. |
| R07 | Wrong full-copy digest, approval digest, task, baseline, mode or predecessor (one mutation each) | Block adoption independently. |
| R08 | Missing/corrupt/source- or schema-incompatible recovery journal | Block; lookup creates no metadata or journal. |
| R09 | Same operation already retained a different created_attempt_id | Block substitution; do not confuse predecessor with response ID. |
| R10 | Unique confirmed successor/current predecessor match | Advance current binding and generation atomically once; preserve prior results. |
| R11 | Interrupted binding/reference/generation persistence | Reconcile by operation identity; no double advancement or lost history. |
| R12 | Verified same-owner route change | Preserve guard/commit/handoff binding, record old/new route and fresh profile authority. |
| R13 | Changed downstream Feature 004 owner / unsupported cross-host transfer | Block; never edit fixed owner or reset journal. |
| R14 | Unproven creator exclusivity, uncertain old-executor cessation or stale generation | Block lookup/transfer; no fencing claim based on lock release alone. |
| R15 | Change/revoke profile/grant/evidence at each protected boundary | Next action blocks; old evidence retained. |
| R16 | Two synchronized executors compete for claim/creation/transfer | One effective owner; loser performs zero protected effects. |
| R17 | Completed evidence + changed review/check/source after interruption | Fresh validation blocks unauthorized rework or handoff; retain identity. |

## Exact future method inventory

All 93 IDs below are mandatory PASS outcomes once implemented; no failures, skips, xfails, empty suite, import/setup/
cleanup errors or omitted variants qualify. Task-family vectors are required where their inputs apply to that method;
every vector must be exercised by at least one named method in its assigned task, with per-subcase evidence. Record
case ID, vector/subcase, route, reached gate, actual raw exit/outcome and retained-effect assertions. A blocked earlier
gate is not evidence that the intended guard rejection or crash boundary was exercised.

The table is the exact method-ID set for planned Feature 005 methods, not the whole repository discovery set. Helper
imports may not duplicate IDs. Additional new green regressions require reviewed inventory addition, not suppression
or a historical-file CI exception; existing unrelated baseline tests remain required by repository acceptance.

| Task | Exact future unittest ID | Required assertion | Required task-family vectors |
| --- | --- | --- | --- |
| T002 | `test_provider_profiles.T002Tests.test_strict_json` | Accept valid UTF-8 JSON; reject duplicate/unknown keys, floats/non-finite values, invalid UTF-8 and unpaired surrogates. | J01–J08, V01–V07, L01–L06, A01–A06 |
| T002 | `test_provider_profiles.T002Tests.test_canonical_identity` | Match all V-vector bytes/digests; key order is immaterial, Unicode normalization and trailing LF change identity. | J01–J08, V01–V07, L01–L06, A01–A06 |
| T002 | `test_provider_profiles.T002Tests.test_input_limits` | Accept each inclusive L limit and reject its adjacent overflow without truncation. | J01–J08, V01–V07, L01–L06, A01–A06 |
| T002 | `test_provider_profiles.T002Tests.test_canonical_actions` | Match the closed table; refuse aliases/unknown/duplicate/wildcard tokens; keep observation/safety separate. | J01–J08, V01–V07, L01–L06, A01–A06 |
| T003 | `test_profile_fixtures.T003Tests.test_isolated_fixture` | Create only private disposable Git/state roots with no real repository or host-state mutation. | F01–F06 |
| T003 | `test_profile_fixtures.T003Tests.test_fixture_authority` | Only host-injected synthetic target-bound decisions authorize fixture actions; submitted JSON never does. | F01–F06 |
| T003 | `test_profile_fixtures.T003Tests.test_no_live_ports` | Refuse live ports, transport, credential discovery, account/agent fallback and real fixture roots before action. | F01–F06 |
| T004 | `test_provider_profiles.T004Tests.test_profile_fields` | Check every required field, exact primitive type and tuple binding; only secret_ref may be null. | P01–P05, L05–L06 |
| T004 | `test_provider_profiles.T004Tests.test_distinct_tuples` | One-field changes in backend/auth/mode/agent/version/revisions/secret/evidence give distinct bindings and no inherited eligibility. | P01–P05, L05–L06 |
| T004 | `test_provider_profiles.T004Tests.test_candidate_families` | Codex, Claude Code and Antigravity candidates all start unknown/unverified/disabled with no provider claim. | P01–P05, L05–L06 |
| T005 | `test_provider_profiles.T005Tests.test_policy_applicability` | Exact provider/backend/auth/mode/agreement/action/reviewer binding required; neighboring profile evidence blocks. | E01–E07 |
| T005 | `test_provider_profiles.T005Tests.test_policy_expiry_conflict` | Review start inclusive and due exclusive; future, stale, conflicting or inaccessible-at-renewal evidence is unknown. | E01–E07 |
| T005 | `test_provider_profiles.T005Tests.test_policy_provenance` | Primary-source/revision/reviewer provenance must be trusted host evidence, never candidate self-attestation. | E01–E07 |
| T006 | `test_provider_profiles.T006Tests.test_capability_actions` | Require the requested action and every mapped capability, not a sibling action success. | E08–E12 |
| T006 | `test_provider_profiles.T006Tests.test_offline_not_live` | Offline local proof may verify local actions; never provider execution or real runtime/auth/cancellation. | E08–E12 |
| T006 | `test_provider_profiles.T006Tests.test_capability_revision` | Changed implementation/runtime/agent/profile identity or mismatched observation digest invalidates capability proof. | E08–E12 |
| T007 | `test_provider_profiles.T007Tests.test_three_axes` | Independently vary policy/capability/grant; only matching permitted+verified+active can enable the profile action. | B01–B07, A01–A06 |
| T007 | `test_provider_profiles.T007Tests.test_unlisted_blocked` | Unlisted tuples and unsupported requested actions block without substitution. | B01–B07, A01–A06 |
| T007 | `test_provider_profiles.T007Tests.test_assessment_no_calls` | Pure assessment performs zero credential reads, provider/tracker calls or mutations. | B01–B07, A01–A06 |
| T007 | `test_provider_profiles.T007Tests.test_eligibility_field_invariants` | Require both booleans, forbid eligible alias/null/omission and task=true with profile=false. | B01–B07, A01–A06 |
| T008 | `test_profile_execution.T008Tests.test_assess_cli` | Assess success means evaluated only; default authorities yield both flags false, never task authorization. | B01–B07, D01–D05 |
| T008 | `test_profile_execution.T008Tests.test_assess_untrusted_input` | Malformed input fails safely; config cannot select imports/executables/credentials or mint authority. | B01–B07, D01–D05 |
| T008 | `test_profile_execution.T008Tests.test_assess_envelope` | One sanitized JSON response with stable fields/exit; no raw argument, token, source text or exception leakage. | B01–B07, D01–D05 |
| T008 | `test_profile_execution.T008Tests.test_preview_not_task_eligibility` | Even a fixture-authorized positive profile preview reports task_eligible=false. | B01–B07, D01–D05 |
| T009 | `test_profile_authority.T009Tests.test_default_unavailable` | Absent authority returns unknown/absent and never executes a protected callback. | G01–G08, A01–A06 |
| T009 | `test_profile_authority.T009Tests.test_authenticated_target` | Accept only exact authenticated project/profile/evidence/action/validity/reviewer/owner target. | G01–G08, A01–A06 |
| T009 | `test_profile_authority.T009Tests.test_forged_owner` | Labels, owner_ref strings, agent output, installation and fake local grants cannot authenticate owner. | G01–G08, A01–A06 |
| T009 | `test_profile_authority.T009Tests.test_action_vocabulary` | Authority uses exactly the canonical action table and refuses entry/completion aliases. | G01–G08, A01–A06 |
| T010 | `test_profile_authority.T010Tests.test_clock_unavailable` | Missing or invalid trusted clock blocks instead of using caller time. | E02–E05, G04–G05 |
| T010 | `test_profile_authority.T010Tests.test_clock_rollback` | A rollback observation blocks even if the old timestamp would make a grant appear valid again. | E02–E05, G04–G05 |
| T010 | `test_profile_authority.T010Tests.test_review_validity` | Enforce finite owner-reviewed interval, start/due boundaries, actual reviewer and renewal failure. | E02–E05, G04–G05 |
| T011 | `test_profile_authority.T011Tests.test_grant_exact_binding` | Each project/profile/policy/capability/mode/action/owner mismatch blocks independently. | G01–G08, A01–A06 |
| T011 | `test_profile_authority.T011Tests.test_grant_expiry_revoke` | Before-issued, expired and revoked grants never execute; preserve retained records. | G01–G08, A01–A06 |
| T011 | `test_profile_authority.T011Tests.test_grant_not_task_approval` | An active profile grant cannot supply any missing task/rework/review/publication/merge/live approval. | G01–G08, A01–A06 |
| T011 | `test_profile_authority.T011Tests.test_execution_only_actions` | Execution grants reject status/inspect/lookup/safe-stop and unknown/alias actions. | G01–G08, A01–A06 |
| T012 | `test_profile_authority.T012Tests.test_revoked_inspection` | Revoked execution grant alone cannot inspect; separately scoped current inspection decision can read with both flags false. | G06–G08, B06–B07 |
| T012 | `test_profile_authority.T012Tests.test_safe_stop_authority` | A distinct bounded safety decision is required; reference executor never becomes a real stop adapter. | G06–G08, B06–B07 |
| T012 | `test_profile_authority.T012Tests.test_cancellation_unknown` | Unconfirmed cessation remains blocked/incomplete, never successful termination. | G06–G08, B06–B07 |
| T012 | `test_profile_authority.T012Tests.test_observation_flags_and_authority` | Observation/safety success retains false execution flags and cannot enable implementation/publication. | G06–G08, B06–B07 |
| T013 | `test_profile_authority.T013Tests.test_boundary_revalidation` | Repeat authority/time/evidence/claim checks immediately before each mapped protected invocation. | G01–G08, R14–R17, A01–A06 |
| T013 | `test_profile_authority.T013Tests.test_evidence_drift` | Change any bound profile/evidence revision between assessment and invocation; callback count stays zero. | G01–G08, R14–R17, A01–A06 |
| T013 | `test_profile_authority.T013Tests.test_cross_project_denied` | An otherwise valid grant for another project/task target cannot authorize this request. | G01–G08, R14–R17, A01–A06 |
| T013 | `test_profile_authority.T013Tests.test_operation_action_capabilities` | Every table row maps exact operation, boundary and capability set; remove one requirement and block. | G01–G08, R14–R17, A01–A06 |
| T014 | `test_profile_state.T014Tests.test_private_state_root` | Only host-owned outside-repository roots; reject candidate-controlled aliases and real-root fixture reuse. | S01–S09 |
| T014 | `test_profile_state.T014Tests.test_storage_safety` | Reject unsafe filesystem/type/link/owner/mode paths without outside writes or state overwrite. | S01–S09 |
| T014 | `test_profile_state.T014Tests.test_durable_transaction` | Confirmed results survive reopen; failed writes remain incomplete and do not permit action. | S01–S09 |
| T015 | `test_profile_state.T015Tests.test_single_claim` | Synchronized competing processes leave exactly one effective executor and no loser side effect. | S07–S09, R10–R17 |
| T015 | `test_profile_state.T015Tests.test_generation_cas` | Only expected generation advances; stale/mismatched claims conflict and never steal on expiry/death. | S07–S09, R10–R17 |
| T015 | `test_profile_state.T015Tests.test_lock_order` | Coordinator lock precedes guard/handoff lock; no DB write transaction spans subprocess work. | S07–S09, R10–R17 |
| T015 | `test_profile_state.T015Tests.test_rework_binding_history` | Preserve immutable predecessor/result; unique successor advancement is atomic with generation increment; transfer preserves binding. | S07–S09, R10–R17 |
| T016 | `test_profile_state.T016Tests.test_intent_before_action` | Durable source/context/claim-bound intent precedes invocation; failure to persist gives zero invocations. | R01–R17, S05–S09 |
| T016 | `test_profile_state.T016Tests.test_reconcile_existing_attempt` | Read authoritative existing stores before confirming a recovered result; never repeat ambiguous begin/publication. | R01–R17, S05–S09 |
| T016 | `test_profile_state.T016Tests.test_persistence_uncertainty` | Fail before/after each durable boundary and retain intent/evidence with no claimed success or blind replay. | R01–R17, S05–S09 |
| T016 | `test_profile_state.T016Tests.test_creation_intent_binding` | Full-copy and approval digests are distinct; context/baseline/mode/predecessor/claim/source mismatches block adoption. | R01–R17, S05–S09 |
| T016 | `test_profile_state.T016Tests.test_first_entry_empty_snapshot` | Empty snapshot only under canonical lock and proved empty namespace/exclusive creator; orphan/legacy/unexpected state blocks. | R01–R17, S05–S09 |
| T017 | `test_profile_guard.T017Tests.test_pinned_guard_invocation` | Literal approved argv/cwd, accepted source/module closure, supported interpreter and secret-free environment only. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_guard_exit_mapping` | Preserve native raw exit/diagnostic meaning; no known failure becomes success; uncertain response stays recoverable. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_guard_timeout_cleanup` | Honor approved inner deadlines, terminate/reap owned children before fixture cleanup and retain uncertainty. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_creation_lookup_unique` | Exactly one new complete intent/event match may bind; pre-call IDs and unrelated records never count. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_creation_lookup_ambiguous` | Two matches, conflicting records, wrong predecessor or unproven creator exclusivity block without creation. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_creation_lookup_unavailable` | Missing/corrupt/incompatible journal/source/schema blocks and preserves intent. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_first_entry_lock_bootstrap` | Authorized initial lock_only preparation creates canonical metadata only, releases guard lock before CLI, creates no journal/context/attempt. | D01–D05, R01–R17, S01–S06 |
| T017 | `test_profile_guard.T017Tests.test_lookup_never_bootstraps` | Lookup of missing state creates no directory/lock/journal/context/attempt. | D01–D05, R01–R17, S01–S06 |
| T018 | `test_profile_execution.T018Tests.test_manual_local_guard` | One approved manual fixture task reaches actual guard under all profile/task/claim gates. | B01–B07, G01–G08, A01–A06 |
| T018 | `test_profile_execution.T018Tests.test_manual_gate_failure` | Remove each gate independently; intended protected callback is not invoked. | B01–B07, G01–G08, A01–A06 |
| T018 | `test_profile_execution.T018Tests.test_manual_no_tracker` | Local completion succeeds with no Symphony/tracker/network/credentials; external handoff remains incomplete. | B01–B07, G01–G08, A01–A06 |
| T018 | `test_profile_execution.T018Tests.test_cli_action_mapping` | guard --operation maps one-to-one; assess does not execute; inspect/status/lookup require separate authority. | B01–B07, G01–G08, A01–A06 |
| T019 | `test_profile_execution.T019Tests.test_orchestration_delegation` | Delegate to unchanged Feature 004 ports and preserve source/owner/mode requirements without synthetic live claims. | O01–O06, A01–A06 |
| T019 | `test_profile_execution.T019Tests.test_unavailable_capability` | Remove guard/runtime/tracker capability individually; affected action blocks rather than substitutes. | O01–O06, A01–A06 |
| T019 | `test_profile_execution.T019Tests.test_safe_label_blocker` | Absent conditional replacement/all-writer serialization blocks final labels; plain read/PUT/read is insufficient. | O01–O06, A01–A06 |
| T020 | `test_profile_execution.T020Tests.test_same_owner_transfer` | Verified same-owner transfer preserves guard/commit/handoff identity and advances only the authorized generation. | R10–R17 |
| T020 | `test_profile_execution.T020Tests.test_stale_transfer` | Wrong expected generation, new-profile mismatch or uncertain old cessation blocks with retained evidence. | R10–R17 |
| T020 | `test_profile_execution.T020Tests.test_downstream_owner_blocked` | Changed Feature 004 owner or unsupported cross-host authority blocks without journal editing/reset. | R10–R17 |
| T021 | `test_profile_execution.T021Tests.test_handoff_only_resume` | Matching completed evidence selects handoff-only and revalidates, never reimplements. | R01–R17, O04–O06 |
| T021 | `test_profile_execution.T021Tests.test_authorized_rework` | Only exact renewed corrective authorization creates a linked successor; predecessor context/events/marker remain retained. | R01–R17, O04–O06 |
| T021 | `test_profile_execution.T021Tests.test_uncertain_publication` | Unknown write results reconcile by stable identity; no blind retry or duplicate commit/PR/evidence. | R01–R17, O04–O06 |
| T021 | `test_profile_execution.T021Tests.test_lost_begin_response` | After guard commit but before coordinator ID persistence, recover one matching new attempt and never call begin twice. | R01–R17, O04–O06 |
| T021 | `test_profile_execution.T021Tests.test_lost_rework_response` | Recover one linked successor after lost response, retain prior binding and advance current reference/generation once. | R01–R17, O04–O06 |
| T022 | `test_profile_execution.T022Tests.test_status_authorized` | Read-only task summary requires current scoped inspection authority, not active execution grant; no mutation success implied. | D01–D05, F01–F06, B06–B07 |
| T022 | `test_profile_execution.T022Tests.test_exercise_confined` | Both route fixtures reject real repository/state roots, credentials and live ports; outputs remain offline. | D01–D05, F01–F06, B06–B07 |
| T022 | `test_profile_execution.T022Tests.test_cli_failure_mapping` | Stable safe envelope/exit classes preserve raw guard failures and both flags false on observation success. | D01–D05, F01–F06, B06–B07 |
| T023 | `test_profile_execution.T023Tests.test_normal_parity` | Manual/orchestrated normal scenarios have equal task decisions and one eventual fixture publication identity. | O01–O06, R01–R17 |
| T023 | `test_profile_execution.T023Tests.test_interrupted_parity` | Interrupt both routes around completion/commit/publication; retain same implementation identity and revalidate before handoff. | O01–O06, R01–R17 |
| T023 | `test_profile_execution.T023Tests.test_rework_parity` | Both routes require same corrective source/scope/review and preserve linked predecessor/successor evidence. | O01–O06, R01–R17 |
| T024 | `test_profile_execution.T024Tests.test_inherited_guard_negatives` | Replay every applicable inherited scenario through both routes; reach the intended guard gate and preserve all source assertions. | R10–R17, G01–G08, inherited route-replay corpus |
| T024 | `test_profile_execution.T024Tests.test_competing_executor` | Synchronized claim/entry/transfer races never produce two effective executors or loser writes. | R10–R17, G01–G08, inherited route-replay corpus |
| T024 | `test_profile_execution.T024Tests.test_revocation_race` | Revoke/change bindings at each mapped action boundary; next protected side effect is blocked and evidence retained. | R10–R17, G01–G08, inherited route-replay corpus |
| T025 | `test_provider_profiles.T025Tests.test_proposed_adapter_blocked` | Permission-only API/cloud proposals stay unverified/ineligible with named missing capabilities. | P01–P05, E08–E12 |
| T025 | `test_provider_profiles.T025Tests.test_offline_adapter_blocked` | Fixture success never promotes an adapter to real-provider/live support. | P01–P05, E08–E12 |
| T025 | `test_provider_profiles.T025Tests.test_no_adapter_loading` | No candidate-selected import/command/credential broker or implicit fallback is loaded. | P01–P05, E08–E12 |
| T028 | `test_profile_execution.T028Tests.test_protected_trees` | Match protected spec trees and existing code/test oracle closure, including physical/index modes; no silent historical-pin waiver. | D03–D05, F01–F06, protected source manifest |
| T028 | `test_profile_execution.T028Tests.test_secret_redaction` | Seed malformed input, arguments, provider output and exceptions with canaries; none reach returned/persisted diagnostics. | D03–D05, F01–F06, protected source manifest |
| T028 | `test_profile_execution.T028Tests.test_generic_discovery` | All 93 methods and the full inherited native corpus are collected exactly once; no missing/empty/skipped/xfail/error or exemptions. | D03–D05, F01–F06, protected source manifest |

## Exact commands and finite check envelopes

Commands below are argv, not shell strings supplied by issue text. Select an absolute supported interpreter once
(e.g. /opt/homebrew/bin/python3.14 on this host); it is an operator-selected tool pin, not a universal installation
path. Never silently substitute Apple Python 3.9 or a different tool revision. The source/context approval must bind
the actual interpreter identity and command argv before execution. No command here authorizes invocation now.

| Check | Exact argv shape | Finite envelope / expected outcome |
| --- | --- | --- |
| T001 document validation | PYTHON -B scripts/check_repo.py | 30 s, exit 0; repository docs only. |
| T001 whitespace | git diff --check | 10 s, exit 0. |
| T001 inventory/vector verification | PYTHON -I -B - reading the exact verifier block below on stdin | 60 s, exit 0; collection/hash checks only, no test execution. |
| Each future code task Tnnn | PYTHON -I -B scripts/TEST_FILE.py TnnnTests -v (file/class from the exact method table) | 180 s per focused class; all collected named methods/variants PASS, exit 0. |
| T024 inherited replay | PYTHON -I -B scripts/test_profile_execution.py T024Tests -v | Proposed 1,800 s focused envelope; all required paired observations PASS. |
| T029 full profile suites | PYTHON -I -B scripts/test_PROVIDER_FILE.py -v for each of the six files listed below | 1,800 s per script ceiling; no new outcomes exempted. |
| Canonical repository acceptance | make check PYTHON=ABSOLUTE_PYTHON | 2,100 s outer envelope; existing 600 s raw / 1,200 s qualified limits unchanged; exit 0. |

The six T029 scripts are test_provider_profiles.py, test_profile_authority.py, test_profile_state.py,
test_profile_guard.py, test_profile_execution.py and test_profile_fixtures.py, all under scripts/.
TEST_FILE is the literal file for each class in the exact table, not an input-dependent executable selection.
Finite envelopes must be approved in the bounded execution context. Nested existing check/subprocess deadlines remain
unchanged. Focused ceilings are not permission to exceed canonical aggregate budgets: generic CI collects the replay
methods too. Measure/optimize isolated fixture setup or diagnose a failure; do not skip scenarios, weaken assertions
or increase budgets silently. Batch setup only when every scenario still has independent state and revalidation.

Known native guard exits retain their assertions: raw 0 success; raw 2 context/approval/prerequisite blocker maps to
profile blocker 3, except an actual malformed interface/invalid_arguments maps to 2; raw 3 scope/evidence -> 3;
raw 4 acceptance -> 4; raw 5 conflict -> 5; raw 6 persistence -> 6. Before-invocation malformed profile/interface
input is 2. Missing/malformed/lost response after invocation means uncertain effect -> 6 and retained intent/lookup,
not success or a blind retry. Always retain actual raw_exit when known; independent profile/grant gates still apply.

## Validation and handoff evidence

For this preparation, evidence distinguishes (1) pure vector calculation/test-ID collection, (2) documentation/diff
checks, (3) actual repository CI and (4) future profile/route execution. Only observations actually performed may be
reported passed. Current absence of all six new test scripts is expected pending implementation, never a passing
future suite. Owner approval, exact source/context/check pins and prerequisite review precede any T002/T003 execution.

T001 remains unchecked until separately accepted under its bounded contract. A reviewed/green documentation PR does
not implement profiles or prove live dispatch. No provider/tracker credentials, source fetching, live service or
Symphony installation is required by the collection/vector verifier. Source inventory below scopes existing oracle
inputs only; it adds no CI policy, adoption setting, task-specific exemption or historical-hash acceptance gate.

## Inherited source and collection inventory

Collection at the guard/oracle baseline observed **811 unique existing repository tests**, including **426 guard-related
IDs** (402 under tests/test_single_task*.py and 24 direct verification cases). No test body was run by collection.
The 385 other repository cases stay in normal acceptance; they are outside the inherited guard replay scope, not waived.
All 426 native guard cases remain required PASS outcomes. Eight fixture/metadata audits below are native-only with
explicit counterpart tests; the remaining **418 scenarios** require semantic replay through both guarded routes,
yielding **836 paired scenario observations**, which may be grouped under T024 rather than added as 836 test methods.

A native case ID is the complete prefix key concatenated with each method name in route_replay. Each paired ID is
NATIVE_ID::manual or NATIVE_ID::orchestrated. Full ID lists, counts and canonical sorted-ID digest must match exactly.
Every native-only exclusion is listed individually with its reason; no other exclusion is permitted. Positive guard
controls are deliberately included in replay so a universally-blocking stub cannot satisfy a negative matrix.
The synthetic-protected-tree mutation case remains replay-required; only the real-repository identity audit is native-only.

Replay means the same source-pinned scenario/safety property and retained-state assertions through the coordinator,
not merely calling an existing unit method outside it. Prime valid profile/task authority so missing unrelated gates
cannot pass a negative case. Reuse approved source fixtures/fault conditions; instrumentation must prove the mapped
production guard/bridge gate was reached. Internal unit exceptions map to their documented CLI/envelope category;
retain the native unit test unchanged as the independent internal oracle. A positive native primitive/control must
also yield the corresponding guarded positive observation. No mock-only success may count as an actual guard replay.
All parameterized variants inside the pinned source case remain required, not a chosen representative.

| Native class family | Guarded replay entry/safety property |
| --- | --- |
| ContextEntry / ContextRework | begin/rework and context/prerequisite/artifact entry rejection/retention. |
| SingleTaskTests / SingleTaskAcceptanceTests / ScopeTests | begin/inspect/verify/complete; same source/index/history/mode/review/acceptance and marker assertions. |
| StatePrimitiveTests / SingleTaskStateTests | begin/status/resume and owned store locking/schema/immutability/rollback; source fault conditions remain native controls as well. |
| AcceptanceExecutionTests | verify/complete with the same check argv/output/mutation/timeout/process-cleanup condition. |
| CompletionPublicationTests / CompletionRecoveryTests / TaskRecoveryTests | complete/resume/record-commit under the same intent/check/rename/fsync/crash/state conditions. |
| ImplementationBindingTests / HandoffFreshnessTests | record-commit/resume with exact source/content/mode/commit/freshness rejection and retained identities. |
| AuthorizedReworkTests | rework/resume with exact predecessor/corrective scope/check/review and retained history. |
| CompetingTaskProcessTests / CliBoundaryTests | Synchronized competing command/claim transitions and stable sanitized failures, not sleep-based assertions. |
| OfflineCommandMatrixTests / remaining MigrationAudit / synthetic ProtectedFeatureIdentity case | All task modes/no-provider behavior, versioned command lifecycle and protected-output rejection. |

For a low-level scenario, replay must exercise its corresponding real guard-backed safety property, not invent a
new public mutation API or change existing code to accept a test shim. Source-pinned fault instrumentation is allowed
only in disposable fixtures with explicit offline provenance; it may inject the failure, never replace the owning
validation/assertion with a manufactured result. Runtime collection/evidence must demonstrate every paired ID and
all of its original applicable assertions. An unimplemented replay or inability to expose its property is a blocking
acceptance gap requiring owner-reviewed refinement, not an automatic native-only reclassification.

```json
{
  "kind": "inherited-guard-inventory",
  "source_revision": "cdc47c4a6878b541b4a9425775c98561da157bdd",
  "native_guard_count": 426,
  "route_replay_count": 418,
  "paired_observation_count": 836,
  "guard_ids_sha256": "26f3a7ac47f4dfaf7f9b227dc312be4ac69c0805ef93f44af4ec9da1fd314ffb",
  "native_only": {
    "test_single_task.SingleTaskFixtureTests.test_fixture_creation_rejects_network": "Fixture-construction negative control, not a guard request; retain native proof and T003.test_no_live_ports counterpart.",
    "test_single_task.SingleTaskFixtureTests.test_git_configuration_hooks_and_signing_are_isolated": "Fixture Git configuration audit; retain native proof and T003.test_isolated_fixture counterpart.",
    "test_single_task.SingleTaskFixtureTests.test_protected_features_remain_unchanged": "Fixture/repository identity audit; retain native proof and T028.test_protected_trees counterpart.",
    "test_single_task.SingleTaskFixtureTests.test_runtime_context_prerequisite_review": "Fixture data/approval preparation audit; retain native proof and T003.test_fixture_authority counterpart.",
    "test_single_task.SingleTaskFixtureTests.test_tracker_credentials_are_removed_without_logging": "Fixture environment/redaction audit; retain native proof plus T003.test_no_live_ports and T028.test_secret_redaction counterparts.",
    "test_single_task.SingleTaskFixtureTests.test_two_task_feature": "Fixture shape audit; retain native proof and T003.test_isolated_fixture counterpart.",
    "test_single_task.SingleTaskMigrationAuditTests.test_original_cases_remain_required_and_discoverable": "Collection/retention audit, not guard execution; retain native proof and T028.test_generic_discovery counterpart.",
    "test_single_task_scope.ProtectedFeatureIdentityTests.test_source_tree_ids_index_and_bytes_match_recorded_baseline": "Real repository physical/index/tree identity audit; retain native proof and T028.test_protected_trees counterpart."
  },
  "route_replay": {
    "test_single_task.SingleTaskAcceptanceTests.": [
      "test_acceptance_does_not_reuse_old_success",
      "test_acceptance_literal_metacharacters",
      "test_acceptance_local_only_envelope",
      "test_acceptance_missing_executable",
      "test_acceptance_missing_required_check",
      "test_acceptance_nonzero_exit",
      "test_acceptance_ordered_argv",
      "test_acceptance_safe_failure_evidence",
      "test_acceptance_timeout",
      "test_acceptance_validated_cwd",
      "test_check_mutates_allowed_content",
      "test_check_mutates_allowed_mode",
      "test_check_mutates_outside_scope",
      "test_check_mutates_task_content",
      "test_check_removes_allowed_content",
      "test_complete_failed_check_preserves_marker",
      "test_complete_idempotent_same_attempt",
      "test_complete_preserves_immutable_entry_mode",
      "test_complete_rejects_manual_marker",
      "test_complete_reruns_after_verify",
      "test_complete_selected_checkbox_only",
      "test_inspect_after_completion_is_read_only",
      "test_inspect_canonical_manifest",
      "test_inspect_does_not_execute_checks",
      "test_inspect_identities",
      "test_inspect_preserves_content_and_events",
      "test_inspect_reflects_current_content",
      "test_inspect_rejects_invalid_scope",
      "test_inspect_respects_worktree_lock",
      "test_rejected_review_retains_safe_blocker",
      "test_review_duplicate_keys_rejected",
      "test_review_invalid_utf8_rejected",
      "test_review_missing",
      "test_review_nonfinite_json_rejected",
      "test_review_not_approved",
      "test_review_stale_absence",
      "test_review_stale_bytes",
      "test_review_stale_mode",
      "test_review_wrong_attempt",
      "test_review_wrong_context_digest",
      "test_review_wrong_manifest_digest",
      "test_review_wrong_task",
      "test_verify_preserves_pending_marker"
    ],
    "test_single_task.SingleTaskMigrationAuditTests.": [
      "test_all_contract_commands_have_local_versioned_outcomes",
      "test_legacy_and_missing_commands_cannot_bypass_context"
    ],
    "test_single_task.SingleTaskTests.": [
      "test_absent_file_differs_from_sentinel_contents",
      "test_acceptance_cannot_mutate_scope_or_complete",
      "test_committed_violation_hidden_by_revert",
      "test_complete_and_retry_preserve_other_task",
      "test_dirty_checkout_and_unknown_task_rejected",
      "test_early_completion_without_evidence_rejected",
      "test_failed_acceptance_does_not_complete",
      "test_historical_other_task_hidden_by_revert",
      "test_interruption_after_checkbox_before_phase_update",
      "test_mode_change_requires_rework_even_without_git_filemode",
      "test_reject_other_checkbox_and_selected_description",
      "test_reject_outside_scope_committed_and_untracked",
      "test_staged_other_task_hidden_by_worktree_restore",
      "test_staged_symlink_hidden_by_worktree_restore",
      "test_staged_violation_hidden_by_worktree_restore",
      "test_traversal_and_symlink_rejected",
      "test_valid_context_cannot_replace_attempt_awaiting_handoff"
    ],
    "test_single_task_acceptance.AcceptanceExecutionTests.": [
      "test_event_payload_survives_reopen_and_rejects_tampering",
      "test_existing_write_adapter_preserves_read_only_default",
      "test_missing_executable",
      "test_mutation_of_allowed_content",
      "test_mutation_of_outside_scope",
      "test_mutation_of_tasks",
      "test_nonzero_exit_and_discarded_output",
      "test_ordered_literal_argv_and_cwd_with_durable_success",
      "test_signal_permission_requires_observed_group_state",
      "test_stale_review_blocks_before_execution",
      "test_successful_parent_cannot_leave_background_descendant",
      "test_timeout_kills_descendant_group"
    ],
    "test_single_task_binding.ImplementationBindingTests.": [
      "test_candidate_content_mismatch_rejected",
      "test_candidate_mode_mismatch_rejected",
      "test_checked_rework_binding_preserves_uppercase_marker",
      "test_commit_without_completed_marker_rejected",
      "test_matching_binding_is_immutable_and_separate_from_verification",
      "test_physical_mode_drift_rejected",
      "test_physical_sealed_content_drift_rejected",
      "test_reverted_other_task_history_rejected",
      "test_staged_hidden_allowed_drift_rejected",
      "test_staged_hidden_outside_change_rejected",
      "test_unavailable_commit_rejected",
      "test_unreachable_existing_commit_rejected"
    ],
    "test_single_task_completion.CompletionPublicationTests.": [
      "test_changed_content_after_acceptance_cannot_publish",
      "test_corrupt_acceptance_binding_cannot_publish",
      "test_corrupt_manifest_type_cannot_publish",
      "test_directory_flush_failure_keeps_exact_post_bytes_and_intent",
      "test_intent_failure_keeps_marker_pending",
      "test_intent_is_durable_before_atomic_replace",
      "test_later_failed_acceptance_invalidates_prior_success",
      "test_manual_marker_cannot_become_completion_intent",
      "test_missing_acceptance_cannot_publish",
      "test_replace_failure_retains_durable_intent"
    ],
    "test_single_task_context.OfflineCommandMatrixTests.": [
      "test_all_three_modes_run_offline_without_credentials_or_services",
      "test_network_attempt_in_entry_code_is_detected_before_attempt_creation",
      "test_protected_feature_paths_cannot_be_approved_as_outputs"
    ],
    "test_single_task_context.TaskContextEntryTests.": [
      "test_artifact_digest_mismatch",
      "test_artifacts_append_existing_footer",
      "test_artifacts_binding_fields",
      "test_artifacts_crlf_bytes",
      "test_artifacts_fenced_checkbox",
      "test_artifacts_fenced_footer",
      "test_artifacts_footer_records",
      "test_artifacts_rework_binding",
      "test_artifacts_spec_plan_footer",
      "test_canonical_vector",
      "test_changed_approval_binding",
      "test_changed_existing_evidence",
      "test_context_table_mismatch",
      "test_contradictory_dependency_rows",
      "test_dependency_bare_cr",
      "test_dependency_cycle",
      "test_dependency_sorted_lists",
      "test_dirty_index",
      "test_dirty_tracked",
      "test_dirty_untracked",
      "test_duplicate_dependency_ids",
      "test_duplicate_dependency_row",
      "test_duplicate_dependency_table",
      "test_duplicate_json_key",
      "test_duplicate_prerequisite_evidence",
      "test_duplicate_task_id",
      "test_entry_event_rollback",
      "test_entry_legacy_rejected",
      "test_entry_rejections_preserve_evidence",
      "test_entry_status_readonly",
      "test_extra_dependency_row",
      "test_extra_field",
      "test_extra_prerequisite_evidence",
      "test_failed_prerequisite_check",
      "test_fenced_duplicate_ignored",
      "test_fenced_pseudo_task",
      "test_invalid_evidence_footer",
      "test_malformed_dependency_table",
      "test_malformed_json",
      "test_missing_approval",
      "test_missing_artifact",
      "test_missing_dependency_table",
      "test_missing_field",
      "test_missing_prerequisite_evidence",
      "test_multiple_task_ids",
      "test_nonancestor_artifact_commit",
      "test_omitted_dependency_row",
      "test_prerequisite_manifest_types",
      "test_prerequisites_fenced_crlf",
      "test_prerequisites_valid",
      "test_progress_checkbox_only",
      "test_progress_evidence_footer",
      "test_removed_existing_evidence",
      "test_schema_allowed_paths",
      "test_schema_checks",
      "test_schema_nested_fields",
      "test_schema_primitive_types",
      "test_schema_rework_fields",
      "test_schema_unicode_feature",
      "test_schema_valid",
      "test_schema_valid_rework",
      "test_scope_alias_paths",
      "test_scope_allowed_directory",
      "test_scope_allowed_symlink_component",
      "test_scope_changed_gitlink",
      "test_scope_check_cwd_symlink",
      "test_scope_checked_implement",
      "test_scope_clean_gitlink",
      "test_scope_executable_filter",
      "test_scope_feature_symlink",
      "test_scope_fence_lengths",
      "test_scope_git_hooks",
      "test_scope_gitlink_output",
      "test_scope_hidden_mode",
      "test_scope_hidden_tracked",
      "test_scope_nonregular_artifact",
      "test_scope_specs_symlink",
      "test_scope_symlink_artifact",
      "test_scope_traversal",
      "test_scope_unicode_feature",
      "test_self_dependency",
      "test_semantic_plan_drift",
      "test_semantic_spec_drift",
      "test_semantic_task_description",
      "test_semantic_task_order",
      "test_source_digest_mismatch",
      "test_source_excerpt_mismatch",
      "test_stale_prerequisite_check",
      "test_stale_prerequisite_commit",
      "test_stale_prerequisite_digest",
      "test_unchecked_prerequisite",
      "test_unknown_artifact_commit",
      "test_unknown_dependency_id",
      "test_unknown_feature",
      "test_unknown_task_id",
      "test_unsorted_dependency_rows",
      "test_unsupported_schema",
      "test_valid_empty_dependencies",
      "test_valid_satisfied_dependency"
    ],
    "test_single_task_context.TaskContextReworkTests.": [
      "test_absent_retained_predecessor",
      "test_authorized_entry_retains_marker_and_prior_evidence",
      "test_corrective_completion_retains_checked_bytes_and_old_history",
      "test_dirty_rework_baseline_preserves_work",
      "test_missing_prior_attempt",
      "test_missing_prior_implementation",
      "test_missing_rework_approval",
      "test_ready_assertion_cannot_authorize_rework",
      "test_renewed_corrective_scope_and_checks_are_bound",
      "test_retry_counter_cannot_authorize_rework",
      "test_semantic_artifact_drift_requires_renewal",
      "test_unavailable_prior_implementation_commit",
      "test_unbound_prior_implementation_cannot_authorize_rework",
      "test_unrenewed_corrective_checks",
      "test_unrenewed_corrective_scope",
      "test_wrong_predecessor_task",
      "test_wrong_prior_implementation_commit"
    ],
    "test_single_task_handoff.HandoffFreshnessTests.": [
      "test_argument_failure_after_completion_stays_safe_and_stable",
      "test_head_change_after_checks_and_binding_cannot_mark_ready",
      "test_ready_record_binds_exact_fresh_acceptance"
    ],
    "test_single_task_recovery.CompletionRecoveryTests.": [
      "test_active_reruns_checks_and_completes_same_attempt",
      "test_changed_sealed_pre_intent_blocks",
      "test_claimed_applied_phase_with_pre_bytes_blocks",
      "test_corrupt_checkbox_phase_blocks_before_checks_and_keeps_evidence",
      "test_manual_marker_without_intent_blocks_before_checks",
      "test_missing_later_intent_is_corruption",
      "test_prepared_after_replace_reruns_and_finishes_original_intent",
      "test_prepared_before_replace_reruns_and_finishes_original_intent",
      "test_prepared_failed_check_does_not_publish",
      "test_prepared_stale_review_blocks_without_publication",
      "test_prepared_unexpected_private_temporary_is_retained",
      "test_publication_file_swapped_to_fifo_rejects_without_blocking",
      "test_real_crash_after_temporary_fsync_recovers_without_scope_orphan",
      "test_repeated_recovery_reruns_without_duplicate_transition",
      "test_sealed_pre_intent_reruns_checks"
    ],
    "test_single_task_rework.AuthorizedReworkTests.": [
      "test_absent_predecessor_rejected",
      "test_actual_corrective_completion_keeps_checked_marker_and_old_events",
      "test_authorized_entry_links_new_immutable_attempt",
      "test_clean_later_reviewed_baseline_keeps_original_binding",
      "test_corrupt_predecessor_completion_phase_is_rejected",
      "test_missing_authorization_rejected",
      "test_retained_schema_rejects_boolean_versions_and_sequence",
      "test_unbound_predecessor_rejected",
      "test_unrenewed_corrective_checks_rejected",
      "test_unrenewed_corrective_scope_rejected",
      "test_wrong_bound_commit_rejected"
    ],
    "test_single_task_scope.ProtectedFeatureIdentityTests.": [
      "test_synthetic_protected_tree_mutation_is_rejected_by_scope"
    ],
    "test_single_task_scope.SingleTaskScopeTests.": [
      "test_absent_is_distinct_from_sentinel_file",
      "test_allowed_committed_edits",
      "test_allowed_staged_edits",
      "test_allowed_worktree_edits",
      "test_canonical_unicode_binary_mode_and_absence_vector",
      "test_fixed_checkbox_normalization_vector",
      "test_fixed_normalization_preserves_other_checked_marker",
      "test_inspection_without_checks_task_or_acceptance_mutation",
      "test_manifest_entry_missing_mode",
      "test_manifest_entry_missing_path",
      "test_manifest_entry_missing_sha256",
      "test_manifest_entry_missing_type",
      "test_manifest_missing_entries",
      "test_manifest_missing_schema_version",
      "test_manifest_reject_absent_nonnull_digest",
      "test_manifest_reject_absent_nonnull_mode",
      "test_manifest_reject_boolean_version",
      "test_manifest_reject_duplicate_path",
      "test_manifest_reject_entries_wrong_type",
      "test_manifest_reject_entry_unknown_field",
      "test_manifest_reject_unknown_field",
      "test_manifest_reject_unpaired_surrogate_path",
      "test_manifest_reject_unsafe_path",
      "test_manifest_reject_unsorted_paths",
      "test_manifest_reject_unsupported_version",
      "test_manifest_reject_wrong_digest",
      "test_manifest_reject_wrong_mode",
      "test_manifest_reject_wrong_type",
      "test_normalize_only_selected_checkbox_with_crlf",
      "test_physical_executable_mode_in_manifest",
      "test_reject_baseline_submodule_content_drift",
      "test_reject_committed_change_hidden_by_revert",
      "test_reject_directory_output",
      "test_reject_file_swapped_to_fifo_at_open",
      "test_reject_hidden_physical_outside_bytes_and_mode",
      "test_reject_historical_gitlink_hidden_by_revert",
      "test_reject_historical_symlink_hidden_by_revert",
      "test_reject_historical_task_mutation_hidden_by_revert",
      "test_reject_index_gitlink",
      "test_reject_missing_semantic_review",
      "test_reject_negative_semantic_review",
      "test_reject_nonignored_untracked_change",
      "test_reject_other_task_marker",
      "test_reject_review_stale_after_absent_output_creation",
      "test_reject_review_stale_after_output_bytes_change",
      "test_reject_review_stale_after_output_mode_change",
      "test_reject_selected_task_text",
      "test_reject_staged_change_hidden_by_worktree_restore",
      "test_reject_staged_symlink_hidden_by_worktree_restore",
      "test_reject_staged_task_mutation_hidden_by_worktree_restore",
      "test_reject_symlink_worktree",
      "test_reject_task_order",
      "test_reject_unauthorized_worktree_change",
      "test_review_mismatched_attempt_id",
      "test_review_mismatched_content_manifest_digest",
      "test_review_mismatched_context_digest",
      "test_review_mismatched_task_id",
      "test_review_missing_approved",
      "test_review_missing_attempt_id",
      "test_review_missing_content_manifest_digest",
      "test_review_missing_context_digest",
      "test_review_missing_evidence_ref",
      "test_review_missing_reviewer_ref",
      "test_review_missing_schema_version",
      "test_review_missing_task_id",
      "test_review_reject_boolean_version",
      "test_review_reject_empty_evidence_ref",
      "test_review_reject_empty_reviewer_ref",
      "test_review_reject_unknown_field",
      "test_review_reject_unsupported_version",
      "test_review_wrong_type_approved",
      "test_review_wrong_type_attempt_id",
      "test_review_wrong_type_content_manifest_digest",
      "test_review_wrong_type_context_digest",
      "test_review_wrong_type_evidence_ref",
      "test_review_wrong_type_reviewer_ref",
      "test_review_wrong_type_schema_version",
      "test_review_wrong_type_task_id",
      "test_unchanged_baseline_gitlink_and_symlink",
      "test_valid_manifest_schema",
      "test_valid_semantic_review"
    ],
    "test_single_task_state.CompetingTaskProcessTests.": [
      "test_begin_has_one_owner_with_all_competitors_blocked",
      "test_complete_has_one_owner_and_conflicting_commands_do_not_write",
      "test_killed_owner_releases_lock_and_same_attempt_resumes",
      "test_marker_directory_flush_failure_retains_intent_and_recovers",
      "test_publication_flush_failure_retains_intent_and_recovers",
      "test_repeated_retained_file_flush_failure_blocks_publication",
      "test_rework_has_one_owner_and_preserves_predecessor_history",
      "test_sqlite_intent_failure_retains_acceptance_and_recovers"
    ],
    "test_single_task_state.SingleTaskStateTests.": [
      "test_append_only_events",
      "test_corrupt_state_retained",
      "test_immutable_context",
      "test_invalid_entry_preserves_tasks",
      "test_legacy_state_retained",
      "test_lock_released_after_exit",
      "test_one_lock_owner",
      "test_sanitized_structured_error",
      "test_transaction_rollback",
      "test_unsupported_state_retained"
    ],
    "test_single_task_state.TaskRecoveryTests.": [
      "test_block_after_acceptance_check",
      "test_block_after_acceptance_content",
      "test_block_after_acceptance_missing_review",
      "test_block_after_acceptance_review",
      "test_block_after_acceptance_task_bytes",
      "test_block_after_intent_check",
      "test_block_after_intent_content",
      "test_block_after_intent_missing_review",
      "test_block_after_intent_review",
      "test_block_after_intent_task_bytes",
      "test_block_after_replace_check",
      "test_block_after_replace_content",
      "test_block_after_replace_missing_review",
      "test_block_after_replace_review",
      "test_block_after_replace_task_bytes",
      "test_block_before_acceptance_check",
      "test_block_before_acceptance_missing_review",
      "test_block_before_acceptance_review",
      "test_block_before_acceptance_task_bytes",
      "test_checked_rework_after_acceptance",
      "test_checked_rework_before_acceptance",
      "test_checked_rework_block_check",
      "test_checked_rework_block_content",
      "test_checked_rework_block_missing_intent",
      "test_checked_rework_block_review",
      "test_contradictory_intent_blocks_recovery",
      "test_corrupt_context_copy_is_retained",
      "test_corrupt_journal_is_retained",
      "test_corrupt_latest_acceptance_blocks_recovery",
      "test_failed_handoff_rerun_preserves_bound_evidence",
      "test_later_progress_without_intent_is_corruption",
      "test_missing_journal_is_not_reconstructed",
      "test_record_commit_rejects_changed_mode",
      "test_record_commit_rejects_reverted_history_violation",
      "test_record_commit_rejects_staged_hidden_violation",
      "test_record_commit_with_mismatched_sealed_output",
      "test_record_commit_without_completed_marker",
      "test_record_matching_commit_and_fresh_handoff",
      "test_record_unavailable_commit_retains_evidence",
      "test_record_unreachable_existing_commit",
      "test_resume_after_acceptance",
      "test_resume_after_check_result",
      "test_resume_after_checkbox",
      "test_resume_after_completion",
      "test_resume_after_directory_flush",
      "test_resume_after_intent",
      "test_resume_after_replace",
      "test_resume_before_acceptance",
      "test_resume_before_intent",
      "test_verification_retains_original_implementation_identity"
    ],
    "verify_t003_state.StatePrimitiveTests.": [
      "test_append_only_events_and_progress",
      "test_corrupt_journal_retained",
      "test_cross_process_lock_and_release",
      "test_durability_failure_blocks",
      "test_durable_immutable_attempt",
      "test_legacy_evidence_retained",
      "test_lock_released_after_holder_exit",
      "test_one_active_attempt",
      "test_transaction_failure_rolls_back",
      "test_unsafe_context_copy_rejected",
      "test_unsafe_state_path_rejected",
      "test_unsupported_version_retained"
    ],
    "verify_t004_cli.CliBoundaryTests.": [
      "test_acceptance_exit_four_does_not_echo_output_or_argv",
      "test_all_commands_share_nonblocking_store_lock",
      "test_argument_errors_are_safe_and_stable",
      "test_deferred_commands_do_not_enter_or_execute",
      "test_legacy_acceptance_holds_lock_against_all_commands",
      "test_legacy_success_envelope",
      "test_lock_released_after_cli_error",
      "test_malformed_context_is_not_echoed",
      "test_mixed_legacy_and_sqlite_is_never_adopted",
      "test_persistence_exit_six_is_safe",
      "test_scope_blocker_exit_three",
      "test_unsafe_lock_paths_rejected_without_external_writes"
    ]
  }
}
```

## Protected oracle identities

The following reference records preserve the existing guard/automation/test oracle for this bounded feature. Existing
files keep bytes and modes, including physical executable bits and index entries. Additive Feature 005 script/test
paths are allowed only by their reviewed task scope; do not compare the whole scripts tree against the old tree and
thereby reject legitimate additions. Protected tests and source-data trees stay unchanged. New Feature 005 contract
and progress updates are outside these protected trees; their separate artifact approvals still apply.
The runnable verifier below is frozen for T001 preparation and rejects every additive script path before import.
Later tasks need a separately reviewed source closure for their approved additions before using an updated verifier;
this T001 snapshot does not authorize loading future code.

These identities are task-scope/provenance records, not a new global CI historical-hash whitelist. The generic checker,
acceptance manifest, source-pin semantics and deadlines are unchanged. A changed oracle requires reviewed inventory
and renewed affected task/source authorization; do not update a pin solely to make a failing comparison pass.

```json
{
  "kind": "protected-oracle-source",
  "source_revision": "cdc47c4a6878b541b4a9425775c98561da157bdd",
  "trees": {
    "specs/001-workspace-lifecycle": "0fda80d6f62c7576394495969187f349f7d0a81a",
    "specs/002-symphony-doc-smoke": "3b2dce04b696b247e21535c741759cc5633b514a",
    "specs/003-single-task-entry": "214442b5fbccde328af7188e84bf80f3a72a5ff1",
    "specs/004-speckit-symphony-automation": "51710b73b581efada60fa8318fa7c71959cef794",
    "tests": "777cd5843b3f7b39ce1b1ea52871792cd86dadf7",
    "docs/development": "6a161c73175f0618d776054bad244d63f39a898d"
  },
  "existing_files": {
    "scripts/approval_binding.py": {
      "mode": "100644",
      "blob": "8edafae1d55b0c2d9d76874bfc20a3f9de8967b0",
      "sha256": "89653f8f0ffabe42ee8ff04dfb94365caa4f63838cff7b27571fdfb76e9bda06"
    },
    "scripts/check_ci.py": {
      "mode": "100644",
      "blob": "ea7a191140a4d5645e488d93e3959ded74719d5c",
      "sha256": "65952398778133fd57dc980a066f41d64d03244fbbd740ce124eac571f7e525c"
    },
    "scripts/check_repo.py": {
      "mode": "100644",
      "blob": "44b6735b9164963ec64b5bbd0a066fa9ad87a6ad",
      "sha256": "8f8d7077a58586b0cf0b5d345a5caafee4570be3e61188873a2649bd7dd07c6b"
    },
    "scripts/prepare_publication.py": {
      "mode": "100644",
      "blob": "3a16da54dcbeac431955efc879d88912e2bed9d5",
      "sha256": "f9b2556b54b61cd7294b2f1a5ff12d6d9962661fdc4c2dd9c547d38ab91b9745"
    },
    "scripts/prepare_symphony.py": {
      "mode": "100644",
      "blob": "452d9c07307ce2f99e6c75a7c027409411097737",
      "sha256": "debc98e90f5c9a90c023e116bcac3371e2921ce7dd3a99d0c5f28f778e5bcbf1"
    },
    "scripts/select_feature.py": {
      "mode": "100644",
      "blob": "13bf8590701e9194d10a9b450181c08906009e4d",
      "sha256": "336f026f4d5c78b60f19d2e41aad0a751dfd247a393ed7bb974d2b7bfca6acf5"
    },
    "scripts/single_task.py": {
      "mode": "100644",
      "blob": "bae3d398418aa86086f26bd7e9e07bd9479ca4f3",
      "sha256": "a95edc02a1803df7f6491a90ea53bbd790ddb34c68e5252ac1574e39ab7af87b"
    },
    "scripts/single_task_acceptance.py": {
      "mode": "100644",
      "blob": "8511387407f5d0b1becf7beabc3adb4c306ed286",
      "sha256": "af1157b5981fb7d5d0f0c3ac27bae8b9be61095e289410048b4749445819ef16"
    },
    "scripts/single_task_context.py": {
      "mode": "100644",
      "blob": "d510d9d0b7fd72b0dd18699a590917e2d45d8b47",
      "sha256": "87acb4351f4b51e2585228315a36832b4a08ba6ff26bed80698a878dd60c9164"
    },
    "scripts/single_task_scope.py": {
      "mode": "100644",
      "blob": "fe2f67b72d7b1a438b31bb91764977b4cd0c2767",
      "sha256": "9e5f3f05e6f970128beb84274ad584ccae16a66bf21cf048ded731a0128a52b5"
    },
    "scripts/single_task_state.py": {
      "mode": "100644",
      "blob": "d31bdaf3ceba4dd9396e79faf17e04409d8d82ba",
      "sha256": "b0029bb1a5fe47e367dee737f4d45ce3e2229c3a5a6f7c3afa0062984f97b270"
    },
    "scripts/symphony-before-run.sh": {
      "mode": "100755",
      "blob": "2578480dfe54fbdf2b03d0de026727ffb78c1cd4",
      "sha256": "09b6abea6ca623e3424c284f15364ced2069626f73092cf9cffdb0c766f2af41"
    },
    "scripts/task_automation.py": {
      "mode": "100644",
      "blob": "0017771ded18e23d7dcc67dc109819345398c17b",
      "sha256": "5e42c61d99092528e008467ad09ea41e6040a52023f43dbe0d77a727d292b895"
    },
    "scripts/task_handoff.py": {
      "mode": "100644",
      "blob": "e2fbda2eafa4c6ef50973b249e8ae169e822eafd",
      "sha256": "5fc7d3640c4638218e6295b76f9408fe55ed55b4baca6357b97ee595153fe014"
    },
    "scripts/task_outcomes.py": {
      "mode": "100644",
      "blob": "9d17ec2b3a4df4cc368a04dd8b2532e523b490a9",
      "sha256": "3683aa2283e219e8e253b619cc3a41ec51c5bc2ba29c161305ee1e3e2c4938cd"
    },
    "scripts/task_package.py": {
      "mode": "100644",
      "blob": "1b0034cce68b6fd43e0a7fde0bc699101a676d4d",
      "sha256": "1a2fdde7bbd58126cb2b12f686b2913aa3ae3f918e25bd269e1a6a611b2724d0"
    },
    "scripts/task_source.py": {
      "mode": "100644",
      "blob": "12bcca0bc7102a2ec9ba5cda64a174747b501101",
      "sha256": "6179fa6176762f9fb436d6d474f2a09656d8d72bcb73ea65d2ac2f1c729183a3"
    },
    "scripts/task_tracker.py": {
      "mode": "100644",
      "blob": "1a4fa12b81a93f1fe56ebe7a7f41eb5df2349d49",
      "sha256": "42780c26a2f6cb2a55e16ec5ea72507f9292d1950db4f39083dafc5a19ecc679"
    },
    "scripts/test_approval_binding.py": {
      "mode": "100644",
      "blob": "91701e759e445359b8f2bc7bfd027bd1cb1c7a0f",
      "sha256": "337cb855da95e31685acc12b8c9422ac2f6de83bf52bf74ce7dcc94db8f161f6"
    },
    "scripts/test_check_ci.py": {
      "mode": "100644",
      "blob": "740eefbeeb02351ed26b1e594ce9fbec650c70b4",
      "sha256": "f598e61a09cf8388dd1a49adf622b8e7a72c64b3fd48229beb7def4660b2841b"
    },
    "scripts/test_prepare_publication.py": {
      "mode": "100644",
      "blob": "97de74f93d1e3d1319db37156316f7b6faa7de98",
      "sha256": "1669e81085d58f1f23fba6c7f48a3e8ef8304370891d8f86d39ae1322d6b4f65"
    },
    "scripts/test_task_automation.py": {
      "mode": "100644",
      "blob": "a05af078adb24d0b7e29ccbc21daea8f8b044faf",
      "sha256": "2a625f7cdc3f29f27ab7b234754a58f25ddde1fbe96fa7d7d4e907dd529aeee1"
    },
    "scripts/test_task_fixtures.py": {
      "mode": "100644",
      "blob": "51c8156a11fe7ff4079bc7a494ea15a4a0a4cdfa",
      "sha256": "b34cbd55f1355689abe4ef28c22065d17da70ff43324bf236d6034f3f93c43d6"
    },
    "scripts/test_task_handoff.py": {
      "mode": "100644",
      "blob": "1ce7865323281bbd091f6437c5a9d9d8d6983c23",
      "sha256": "85a5a42c3c2ce786f899d611e709398d31adb0c759c89b871885699863b5e26e"
    },
    "scripts/test_task_measurement.py": {
      "mode": "100644",
      "blob": "3578c8e5ba6da4b67e33d2fae53f86a360dd8a19",
      "sha256": "92168ad3a7eeed8334670c3b923ed4d1f4be5d45a23170b4df6605d26e52049b"
    },
    "scripts/test_task_outcomes.py": {
      "mode": "100644",
      "blob": "7e41b4b03c882d07714ca5d795e97b685308753c",
      "sha256": "7c4fe29c30c96dd9fc030d9e96a04282f7edfd854b8b3acce02ce05f7933f50c"
    },
    "scripts/test_task_package.py": {
      "mode": "100644",
      "blob": "18bf9eaae0cdf783e6f949d71033b052db5ac676",
      "sha256": "aafb6877882b1db1554af759306d60b9ea76b0acc0aa033cf4d652d7aef7159f"
    },
    "scripts/test_task_readiness.py": {
      "mode": "100644",
      "blob": "389b2530625bf358659006f4fd5778048238aba0",
      "sha256": "e4887b65ee770db2ed001d58c6bc56760681180937fcaf47ac1250df072857c0"
    },
    "scripts/test_task_runtime.py": {
      "mode": "100644",
      "blob": "10b83903f06f13f4fb298f8f81f0b9cc891f16ff",
      "sha256": "9b8d298681e55cda7a0b7e626cd63f502a81feaf84cecde27b20107ad3fefc71"
    },
    "scripts/test_task_source.py": {
      "mode": "100644",
      "blob": "2b3e680868f6deb40a4a254fdb892962b529b7b4",
      "sha256": "9214ec195dcef695cfc064f0c3d47fffbedad6276a53dad7192703952c94a0a1"
    },
    "scripts/test_task_tracker.py": {
      "mode": "100644",
      "blob": "321bffa267840fafd67b7dd32a6bc24a9b46a9ae",
      "sha256": "22864e5fb663924bb5eb5e96f247d5b373694f856ccf3aa9ca03c4930239c4e2"
    },
    "scripts/verify_t002_red.py": {
      "mode": "100644",
      "blob": "6084b17b12799f147314da2db18305c6931f2db2",
      "sha256": "c863cb1f16f5c0eda3c57631e24401612fd56abf49708dd294b389974a9cfffb"
    },
    "scripts/verify_t003_state.py": {
      "mode": "100644",
      "blob": "aa0d2830ba348b4221819cf2a2b9308e1ee92a05",
      "sha256": "3bba2eeff6d6e1abe14ead36ce98c5f4e2a749fbb71f7d4c03012af9e6e78b0f"
    },
    "scripts/verify_t004_cli.py": {
      "mode": "100755",
      "blob": "21fc038e19af533bf54be8da7b1f546f5bd993b1",
      "sha256": "ec739102e3e120df2106c32af3ef1d301f66b27bd4b7c0746269bcfe2b4c24e6"
    },
    ".github/workflows/check.yml": {
      "mode": "100644",
      "blob": "fbb0ceb1ff7ec3d2fc25e855832952eff49e922b",
      "sha256": "b5e9d214366cb1f3310b2c2aaef4a4108bc07b3e57fc0142356025357cf2c520"
    },
    "Makefile": {
      "mode": "100644",
      "blob": "b2259ff17d0ab8b4e3b56ba55147a1d5013c9f89",
      "sha256": "73dccd78abe703fcf3e9aa75dbbb477b1ff3d3dd36234fe8a6fa7ae59c24a4be"
    }
  }
}
```

## Runnable collection/vector verification (does not execute tests)

Run from a checkout containing the accepted baseline and this contract, using the selected absolute Python 3.11+
interpreter with -I -B and the exact code below on stdin. The 60-second envelope is supplied by the approved check
runner, not a shell-specific timeout utility. This verifier imports accepted local test definitions only after source
checks, collects IDs without executing test bodies, and writes no repository file. No network or credentials required.
A passing verifier validates the document's identities/inventory/math only; it is not future implementation acceptance.

```python
import hashlib, json, os, re, runpy, stat, subprocess
from pathlib import Path

root = Path.cwd()
source = 'cdc47c4a6878b541b4a9425775c98561da157bdd'
contract = root / 'specs/005-provider-aware-orchestration/contracts/task-acceptance.md'
text = contract.read_text(encoding='utf-8')
blocks = [json.loads(x) for x in re.findall(r'```json\n(.*?)\n```', text, re.S)]
inventory = next(x for x in blocks if x.get('kind') == 'inherited-guard-inventory')
protected = next(x for x in blocks if x.get('kind') == 'protected-oracle-source')
assert inventory['source_revision'] == protected['source_revision'] == source

# Inspect local objects without executing repository hooks or external configuration.
git_env = {key: os.environ[key] for key in ('PATH', 'LANG', 'LC_ALL', 'TMPDIR') if key in os.environ}
git_env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull, GIT_OPTIONAL_LOCKS='0',
               GIT_NO_REPLACE_OBJECTS='1', GIT_NO_LAZY_FETCH='1',
               GIT_TERMINAL_PROMPT='0', GIT_ALLOW_PROTOCOL='')
git_command = ['git', '--no-replace-objects', '--no-lazy-fetch',
               '-c', 'core.fsmonitor=false', '-c', 'core.hooksPath=' + os.devnull,
               '-c', 'core.attributesFile=' + os.devnull, '-c', 'submodule.recurse=false']
def git(*argv):
    return subprocess.check_output(git_command + list(argv), cwd=root, env=git_env, timeout=10)
config = git('config', '--null', '--list', '--no-includes')
for entry in filter(None, config.split(b'\0')):
    key = entry.split(b'\n', 1)[0].lower()
    assert not (key.startswith((b'filter.', b'include.', b'includeif.'))
                or (key.startswith(b'merge.') and key.endswith(b'.driver'))
                or (key.startswith(b'diff.') and key.endswith((b'.command', b'.textconv')))
                or key == b'diff.external'), 'Executable/external Git configuration is unsupported'
common = Path(os.fsdecode(git('rev-parse', '--path-format=absolute', '--git-common-dir').strip()))
info = common / 'info'
assert not info.is_symlink() and not (info / 'grafts').exists() and not (info / 'grafts').is_symlink()
assert not git('for-each-ref', '--format=%(refname)', 'refs/replace/')
def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      ensure_ascii=False, allow_nan=False).encode('utf-8')
def sha(value):
    return hashlib.sha256(canonical(value)).hexdigest()

for path, expected in protected['trees'].items():
    assert git('rev-parse', source + ':' + path).decode().strip() == expected
    assert git('rev-parse', 'HEAD:' + path).decode().strip() == expected
    # Check current bytes and index too; never trust assume-unchanged metadata.
    entries = git('ls-tree', '-rz', source, '--', path).split(b'\0')
    expected_paths = set()
    for entry in filter(None, entries):
        metadata, raw_name = entry.split(b'\t', 1)
        mode, kind, blob = metadata.split()
        name = raw_name.decode('utf-8'); expected_paths.add(name)
        assert kind == b'blob' and mode in (b'100644', b'100755')
        file = root / name
        assert stat.S_ISREG(file.stat().st_mode) and file.stat().st_nlink == 1 and not file.is_symlink()
        assert all(not parent.is_symlink() for parent in file.parents)
        assert bool(file.stat().st_mode & 0o111) == (mode == b'100755')
        assert file.read_bytes() == git('show', source + ':' + name)
        assert git('ls-files', '--stage', '--', name).split()[:3] == [mode, blob, b'0']
    # Include ignored paths: new/untracked files in protected trees are not approved additions.
    descendants = list((root / path).rglob('*'))
    assert all(not x.is_symlink() for x in descendants)
    actual_paths = {str(x.relative_to(root)) for x in descendants if not x.is_dir()}
    assert actual_paths == expected_paths
for path, record in protected['existing_files'].items():
    content = git('show', source + ':' + path)
    assert hashlib.sha256(content).hexdigest() == record['sha256']
    assert git('ls-tree', source, '--', path).split()[:3] == [record['mode'].encode(), b'blob', record['blob'].encode()]
    file = root / path
    assert stat.S_ISREG(file.stat().st_mode) and file.stat().st_nlink == 1 and not file.is_symlink()
    assert all(not parent.is_symlink() for parent in file.parents)
    assert file.read_bytes() == content
    assert bool(file.stat().st_mode & 0o111) == (record['mode'] == '100755')
    assert git('ls-files', '--stage', '--', path).split()[:3] == [record['mode'].encode(), record['blob'].encode(), b'0']

vectors = re.findall(r'### (V[0-9]+)\n\n```json\n(.*?)\n```\n\nSHA-256: `([0-9a-f]{64})`', text, re.S)
assert len(vectors) == 7
values = {}
for name, raw, expected in vectors:
    value = json.loads(raw); values[name] = value
    assert canonical(value) == raw.encode('utf-8')
    assert sha(value) == expected
assert sha(values['V02']) != sha(values['V03'])
basis = {k: v for k, v in values['V04'].items() if k not in ('policy_digest', 'capability_digest')}
assert basis == values['V05']
context = json.loads(json.dumps(values['V07']))
context['approval'].pop('context_digest')
assert sha(context) == values['V07']['approval']['context_digest']
assert sha(values['V07']) != sha(context)

expected_ids = set(inventory['native_only'])
for prefix, methods in inventory['route_replay'].items():
    assert len(methods) == len(set(methods))
    expected_ids.update(prefix + method for method in methods)
assert len(expected_ids) == inventory['native_guard_count'] == 426
route_ids = {p + m for p, methods in inventory['route_replay'].items() for m in methods}
assert not route_ids.intersection(inventory['native_only'])
assert len(route_ids) == inventory['route_replay_count'] == 418
assert 2 * len(route_ids) == inventory['paired_observation_count'] == 836
assert sha(sorted(expected_ids)) == inventory['guard_ids_sha256']
# Collection imports Python modules. Reject additions BEFORE any module import,
# including ignored/untracked files, packages and symlinks that introduce no test IDs.
expected_scripts = {p for p in protected['existing_files'] if p.startswith('scripts/')}
script_entries = list((root / 'scripts').rglob('*'))
assert all(not p.is_symlink() for p in script_entries)
actual_scripts = {str(p.relative_to(root)) for p in script_entries if not p.is_dir()}
assert actual_scripts == expected_scripts, 'T001 requires the exact approved script source closure'
for revision in (source, 'HEAD'):
    tracked_scripts = {p.decode('utf-8') for p in git('ls-tree', '-rz', '--name-only', revision, '--', 'scripts').split(b'\0') if p}
    assert tracked_scripts == expected_scripts
indexed_scripts = {p.decode('utf-8') for p in git('ls-files', '-z', '--', 'scripts').split(b'\0') if p}
assert indexed_scripts == expected_scripts
ci = runpy.run_path(str(root / 'scripts/check_ci.py'))
all_ids = [case.id() for case in ci['flatten'](ci['collect'](root))]
assert len(all_ids) == len(set(all_ids))
assert not any('_FailedTest.' in x for x in all_ids)
ci['validate_collection'](all_ids, ci['load_manifest'](root)['required_test_ids'])
guard_ids = {x for x in all_ids if x.startswith(('test_single_task', 'verify_t003_state.', 'verify_t004_cli.'))}
assert guard_ids == expected_ids
future_ids = re.findall(r'^\| T[0-9]{3} \| `(test_[^.]+\.T[0-9]{3}Tests\.test_[^`]+)` \|', text, re.M)
assert len(future_ids) == len(set(future_ids)) == 93
# Absence is allowed only while corresponding tasks are pending, never a future test pass.
tasks = (root / 'specs/005-provider-aware-orchestration/tasks.md').read_text()
declared_ids = set()
completed = set(re.findall(r'^- \[[xX]\] (T[0-9]{3}) ', tasks, re.M))
for task, filename, tail in re.findall(r'^\| (T[0-9]{3}) \| [^|]+ \| `scripts/([^`]+)`:(.*?) \|$', tasks, re.M):
    for method in re.findall(r'`(test_[a-z_]+)`', tail):
        ident = filename[:-3] + '.' + task + 'Tests.' + method
        declared_ids.add(ident)
        if task in completed:
            assert ident in all_ids
assert declared_ids == set(future_ids)
profile_modules = {x.split('.', 1)[0] for x in future_ids}
assert {x for x in all_ids if x.split('.', 1)[0] in profile_modules} <= declared_ids
print('Verified: 7 mathematical vectors; 426 native IDs; 418 paired scenarios; 93 future method IDs. No tests executed.')
```
