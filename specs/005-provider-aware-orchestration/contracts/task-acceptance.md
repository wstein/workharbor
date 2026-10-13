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
