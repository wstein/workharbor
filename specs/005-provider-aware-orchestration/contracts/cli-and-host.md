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
An assess exit 0 means assessment completed, not eligibility: report eligible=false for unavailable authorities.
Fixture exercise must refuse a real repository/state root or any injected live port; synthetic grants cannot escape
into ordinary guard requests. Fixtures use isolated HOME/environment and may execute only their reviewed checks.

Output: schema_version, profile_digest, task identity (when known), action, separate permission/capability/authorization,
profile_eligible, task_eligible, attempt_id, generation, guard_result (sanitized), provenance, raw_exit, blockers,
next_action. Do not log credentials, raw argv, malformed values, provider output or private policy text. An offline
success never sets live_verified=true. External handoff incomplete remains explicit.

Exit 0: requested step succeeded; 2: invalid input/interface; 3: permission/capability/authorization/task blocker;
4: acceptance failure; 5: ownership/generation conflict; 6: persistence or external uncertainty.
Preserve the original guard exit in raw_exit and map it explicitly; never turn an existing failure into success.
Pure assessment blockers can coexist with exit 0; protected actions cannot.

## Trusted host ports

Profiles and configuration JSON cannot instantiate ports, authenticate owners or select executable/import paths.
Trusted host construction pins repository, tool/interpreter versions, state root and implementations.

- **ProfileAuthority**: independently authenticate owner decisions against exact project/profile/evidence/action/validity
  targets; observe revocation and trusted time; authenticate policy/capability reviewer attestations. Missing authority
  yields unknown/absent, not local file-based trust. A future production authority is separately approved; offline
  reference authority is synthetic only. Existing task approval ports remain independently required.
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
- **ClaimAuthority**: obtain a task claim, compare-and-swap generation, prove old executor cessation/fencing and observe
  current ownership. Local reference implementation supports only demonstrated single-host ownership. Multi-host or
  changed downstream owners require proven compatible authority; otherwise unavailable.
- **OrchestrationPorts**: delegate applicable Feature 004 assessment/handoff through its unchanged guard/runtime/tracker
  ports. A new local GuardPort bridge can expose only tested capabilities; no live runtime or tracker capability is
  inferred. Feature 004 owner identity and safe-label constraints remain mandatory.
- **FutureProviderPort**: assessment-only boundary documenting identity, policy applicability, allowed actions,
  credential handling, cancellation/deadlines, provenance, uncertainty and recovery. No implementation or credential
  broker is included. A real provider call requires new reviewed adapter work and separate activation authorization.

## Protected action protocol

1. Resolve the host-pinned task/profile/tool identities and acquire the coordinator task lock.
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
