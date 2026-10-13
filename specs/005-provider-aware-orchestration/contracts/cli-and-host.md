# Contract: Profile assessment and guarded route coordination

Status: proposed; none of the new commands below exist yet. Development-only interface, no product endpoints.

## CLI

Future entry: `python3 scripts/profile_execution.py COMMAND`. No shell interpretation of profile or issue input.

| Command | Inputs | Effects |
| --- | --- | --- |
| assess | --profiles PATH --profile ID --action ACTION | Pure local matrix/decision preview; no credentials, authority grant, provider call or task mutation. |
| status | --task REPO/FEATURE/TASK | Authorized read-only retained claim/recovery summary through trusted host, or unavailable-host blocker. |
| guard | --profile ID --task REPO/FEATURE/TASK --operation OP with operation-specific approved input refs | Trusted host revalidates profile/task/claim, then delegates one existing guard operation. CLI alone has no mutation authority. |
| transfer | --task REPO/FEATURE/TASK --profile ID --expected-generation N | Trusted host reconciles and transfers only if every ownership/capability gate passes; never overwrites existing owners. |
| exercise | --scenario normal/interrupted-handoff/rework --fixture-root PATH --route manual/orchestrated | Disposable synthetic repositories and injected fixture authorities only; no real-provider or tracker access. |

OP is begin, inspect, verify, complete, record-commit, resume or rework; inputs retain the existing Feature 003 CLI
contract. Status/inspection of revoked work require authorized inspection, not an active execution grant. The default
reference CLI has no real-provider transport or live host ports; protected requests fail closed when those are absent.
An assess exit 0 means assessment completed, not task eligibility. Always report task_eligible=false for assess;
report profile_eligible=false when authorities are unavailable. Use exactly the two required boolean fields defined
in data-model.md; reject the legacy eligible alias. Successful authorized observation does not set either flag true.
Fixture exercise must refuse a real repository/state root or any injected live port; synthetic grants cannot escape
into ordinary guard requests. Fixtures use isolated HOME/environment and may execute only their reviewed checks.

Output: schema_version, profile_digest, task identity (when known), action, separate permission/capability/authorization,
profile_eligible, task_eligible, attempt_id, generation, guard_result (sanitized), provenance, raw_exit, blockers,
next_action. Do not log credentials, raw argv, malformed values, provider output or private policy text. An offline
success never sets live_verified=true. External handoff incomplete remains explicit.

Exit 0: requested step succeeded; 2: invalid input/interface; 3: permission/capability/authorization/task blocker;
4: acceptance failure; 5: ownership/generation conflict; 6: persistence or external uncertainty.
Preserve the original guard exit in raw_exit and map it explicitly; never turn an existing failure into success.
Pure assessment blockers can coexist with exit 0. Successful authorized observations may also return both flags
false; a normal execution action requires both true and cannot return success with an execution blocker. Safe-stop
uses only its separately attributable safety authority; neither false execution flag constitutes its permission.

## Canonical actions and capability mapping

The closed action vocabulary is begin, inspect, verify, complete, record-commit, resume, rework, status, lookup,
transfer, ready, publish, final-labels, provider-execute and safe-stop. Use these exact case-sensitive tokens in
assess --action, PolicyEvidence.applicable_actions, CapabilityEvidence.action_results and authorization targets.
ProfileAuthorization.allowed_actions may contain only execution-class tokens from the table. Unknown tokens,
wildcards, duplicated tokens and aliases such as entry/completion are invalid; do not normalize them.
Entry/completion/resume/eligibility/handoff are internal boundary labels, not additional grant actions.

guard --operation accepts only begin/inspect/verify/complete/record-commit/resume/rework and maps one-to-one to the
same action token; status maps to status, transfer to transfer and internal lookup_creation to lookup. assess previews
the requested table action without performing it. exercise runs the mapped actions with confined synthetic ports;
it is not an authorization action and cannot mint grants. ready/publish/final-labels are host API operations only,
not new CLI subcommands. provider-execute/safe-stop are assessment-only future boundaries with no reference executor.

Let G(a,m) mean the unchanged task_package.required_capabilities(a,m), where m is the separately validated task mode.
It selects existing guard/transport requirements; it never supplies profile or task authorization. Each bridge.*
capability additionally requires source-bound, action-specific bridge test evidence. A capability for one operation
cannot stand in for another. All execution rows require current profile_eligible=true and task_eligible=true.

| Action | Class / guard boundary | Required capability and delegation |
| --- | --- | --- |
| begin | Execution / entry | bridge.begin + G(entry,m); existing guard begin --context. |
| inspect | Observation | bridge.inspect; existing guard inspect --attempt; scoped inspection authority. |
| verify | Execution / completion | bridge.verify + G(completion,m); existing guard verify with current review. |
| complete | Execution / completion | bridge.complete + G(completion,m); existing guard complete with current review. |
| record-commit | Execution / completion | bridge.record-commit + G(completion,m); existing guard binds the exact commit. |
| resume | Execution / resume | bridge.resume + G(resume,m); existing guard resume; actual retained phase/checks remain authoritative. |
| rework | Execution / entry | bridge.rework + G(entry,owner-authorized rework); existing guard rework with separate corrective approval. |
| status | Observation | ledger.read and bridge.status when a guard ID is retained; scoped inspection authority. |
| lookup | Observation | bridge.lookup; validated creation-intent lookup under existing guard lock; scoped inspection authority. |
| transfer | Execution / eligibility | claim.fenced-transfer, new-profile revalidation and every downstream transfer/ownership capability. |
| ready | Execution / eligibility | G(ready,m), existing Feature 004 ready; separate eligibility-write authorization. |
| publish | Execution / handoff | G(handoff,m), existing Feature 004 publication recovery; separate publication authorization/review. |
| final-labels | Execution / handoff | G(handoff,m), existing final-label reconciliation after durable publication/evidence; separate label-write authorization. |
| provider-execute | Execution / provider | Verified live provider.execution and task/claim gates; unavailable in this reference implementation. |
| safe-stop | Safety | Verified runtime.safe-stop, applicable safety policy/capability and separately attributable bounded safety authorization; unavailable in the reference executor. |

Observations require a fresh host-authenticated inspection decision scoped to project, task/worktree, retained attempt
or creation intent, requested action and finite validity. They do not require an active execution grant and never
run checks, implement, publish or call providers. Report both eligibility booleans false for these observation
requests even if they succeed; record their sanitized observation result separately. Creation lookup may append its
correlation result to the coordinator ledger under this authority, but never mutate guard/task state or authorize
subsequent execution. A revoked execution grant cannot substitute for inspection authority.

Safety similarly requires a distinct host-authenticated owner decision bound to project/profile/task/attempt,
claim generation, the safe-stop action, its finite deadline and current cancellation capability. A revoked execution
grant is not that decision. Report both execution eligibility flags false and retain uncertain cancellation as a
blocker; no implicit continuation, credential access or remote call follows. This contract permits no real stop
adapter implementation in Feature 005. Missing inspection/safety authority blocks that requested observation/action.

## Trusted host ports

Profiles and configuration JSON cannot instantiate ports, authenticate owners or select executable/import paths.
Trusted host construction pins repository, tool/interpreter versions, state root and implementations.

- **ProfileAuthority**: independently authenticate owner decisions against exact project/profile/evidence/action/validity
  targets; observe revocation and trusted time; authenticate policy/capability reviewer attestations. Missing authority
  yields unknown/absent, not local file-based trust. A future production authority is separately approved; offline
  reference authority is synthetic only. Expose separate authorize_inspection(target) and authorize_safety(target)
  observations matching the table's targets and validity; execution grants cannot impersonate either decision.
  Existing task approval ports remain independently required.
- **GuardBridge**: invoke the accepted Feature 003 CLI with pinned interpreter, script revision and literal argv,
  approved cwd, isolated secret-free environment and finite approved deadlines. Validate one JSON response and exact
  exit mapping; revalidate source/inventory between boundaries. Never duplicate its scope, acceptance or recovery logic.
  Add an internal lookup_creation(intent) observation, not a new Feature 003 CLI command. The source-pinned reference
  reader uses single_task_state.open_store(root, require_existing=True, writable_existing=False), a narrowly scoped
  read-only schema-v1 SELECT of attempt IDs, and store.read_attempt/read_events to validate records. Enumeration is
  necessary: active_attempt() returns only one row and cannot prove uniqueness. Do not copy lifecycle mutation logic.
  Preflight existing journal/directories/lock before opening the store so an absent journal cannot create state.
  The reader participates in the existing guard lock protocol; validate exact source/schema before reading and fail
  closed on incompatibility. Return absent/unique/ambiguous/unavailable with sanitized IDs and matching digests.
  Only unique + full intent/ownership correspondence permits the coordinator to bind the previously unknown ID;
  absent/ambiguous/unavailable/conflicting results block. No raw SQL or journal path is selected by repository input.
  A separate prepare_creation_snapshot observation is invoked only inside an authorized creation action. For absent
  first-entry state, use the pinned single_task.lock_only helper and prove the empty namespace described in the model;
  it may create canonical lock metadata but no journal/context/attempt. This is not lookup_creation: recovery lookup
  always requires an existing journal and cannot create state. Rework uses an existing predecessor journal and new
  linked CreationBinding; it never overwrites the prior binding or reuses the old operation's response ID.
- **ClaimAuthority**: obtain a task claim, compare-and-swap generation, prove old executor cessation/fencing and observe
  current ownership. Local reference implementation supports only demonstrated single-host ownership. Multi-host or
  changed downstream owners require proven compatible authority; otherwise unavailable.
- **OrchestrationPorts**: delegate applicable Feature 004 assessment/handoff through its unchanged guard/runtime/tracker
  ports. A new local GuardPort bridge can expose only tested capabilities; no live runtime or tracker capability is
  inferred. Feature 004 owner identity and safe-label constraints remain mandatory.
- **FutureProviderPort**: assessment-only boundary documenting identity, policy applicability, allowed actions,
  credential handling, cancellation/deadlines, provenance, uncertainty and recovery. No implementation or credential
  broker is included. A real provider call requires new reviewed adapter work and separate activation authorization.

## Execution action protocol

Observation/safety requests use their separately scoped authority from the action table, not the active execution
grant required below. They still enforce trusted source, task ownership, applicable locks and the exact read/stop scope.

1. Resolve the exact canonical action via the table, then host-pinned task/profile/tool identities, and acquire the coordinator task lock.
2. Reobserve policy validity, capability applicability, current owner grant/revocation/time and claim generation.
3. Validate applicable existing task/source/scope/mode/rework prerequisites. Never require Symphony/tracker for local
   manual actions; never skip their existing gates for orchestrated eligibility or external publication.
4. Persist operation intent bound to attempt/generation and exact reviewed inputs; revalidate immediately before the
   protected invocation. No mutation follows a failed gate. Execute only the named approved action.
5. Reconcile the actual guard/external result, persist evidence, retain the same implementation/publication identity
   and release locks. Uncertain writes remain reconcile-required; no blind transport retry.

Manual and orchestrated routes call the same profile/claim gates and existing local guard bridge. The orchestrated
route adds the existing applicable orchestration gates; parity means equal task safety, not identical requirements
for external capabilities. Task mode remains implement/handoff-only/owner-authorized rework independently of route.
Safe-stop after revocation needs its own reviewed capability and authorization; lack of confirmed cancellation is a
blocker, never proof of a stopped provider. Inspection and local evidence retention cannot authorize external writes.

## Extension acceptance boundary

A future adapter must supply exact service/auth/mode identity, current policy evidence, attributable project opt-in,
verified protocol/source/runtime identity and action-specific credential isolation, deadlines, cancellation, claim
fencing, recovery and publication evidence. Require actual provider observations for real-provider claims. Keep
proposal, offline verification and live verification visibly distinct. There is no wildcard support or automatic fallback.
