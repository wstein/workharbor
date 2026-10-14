# Selective Python-to-Go migration

Status: T003 contracts independently accepted at
`191c62baf227f64122770244d62d289900bb4d8d`; T004 Go candidate awaiting
independent implementation review. No callers are switched.
Source pin: `fd0019b7711438a2dbf7255ce3a43572d5a3a6fd`.
Authority: [ADR 0010](../../docs/adr/0010-go-whr-cli-layout.md).

| Existing Python behavior | Go target / disposition |
| --- | --- |
| `scripts/provider_profiles.py`: strict JSON parser, canonical bytes/digest | Candidate pure wire primitives in `apps/api_client/wirejson`; first prove product relevance and freeze duplicate keys, UTF-8, integer/float rules, non-ASCII ordering and exact digests. |
| `scripts/single_task_context.py`: canonical lexical relative paths | Candidate config validator in `apps/host_agent/internal/config`; freeze traversal/absolute/noncanonical negative cases. Runtime symlink/provider validation remains a separate requirement. |
| Other Python canonical serializers | Inventory differing domains/errors; never silently replace one profile or digest with another. |
| Task state/approval, recovery, tracker publication, Symphony and profile eligibility | Retain Python development tooling. No product orchestration port in wave one. |
| CI runners, verifiers and fixtures | Retain the independent Python harness and add explicit Go acceptance. |
| Python development CLI output/exit codes | Not inherited. Product contract is Feature 006 / ADR 0013. |

## Migration WI acceptance

Freeze selected source pins, input bytes, success/rejection classifications,
canonical output bytes and SHA-256 hashes before the Go port. Generate in a
confined offline environment. Independently review cases including nested duplicate
keys, malformed UTF-8, boolean/integer distinctions, large integers, floats,
non-finite values, supplementary-plane key ordering and path attacks.
Write failing Go tests first. Compare byte-for-byte rather than parsed equality.
Go tests use checked-in vectors, never runtime Python; an independent Python job
reproduces the frozen vectors. Retain the existing Python tests. A changed vector
requires reviewed decision/evidence, not regeneration to fit Go output.
Review parity and product dependency boundaries before switching callers.
Record each retained, ported or deliberately replaced behavior. Do not claim
wholesale Python migration; that would require a superseding ADR and new scope.

## T003 selected profiles and provenance

The [migration contract](../../contracts/migration/README.md),
[strict schema](../../contracts/migration/vectors.schema.json),
[vectors](../../contracts/migration/vectors.json) and
[provenance manifest](../../contracts/migration/manifest.json) capture the selected
accepted T003 candidate. Independent Sol review cleared exact revision
`191c62baf227f64122770244d62d289900bb4d8d`; no caller switch is included.
Intermediate profile decisions were independently reviewed
before authoring under the owner's delegated Sol review route.

The product JSON domain fixes CPython's configurable integer-string limit to
4300 decimal magnitude digits for parsing, direct canonicalization and digest;
the sign does not count. Arbitrary machine-width-independent integers within that
bound remain accepted. Over-limit integers are `invalid-json`. Product depth is
at most 128 object/array containers (root container is one, scalar is zero);
keys and bracket characters inside strings add none. Greater depth is
`invalid-json`. Depth 129 is a deliberate product restriction: the pinned source
accepts the selected cases, and its eventual recursion error is call-stack and
runtime dependent. Canonicalization inherits no parse-input byte-size bound.

The lexical path product domain admits Unicode scalar strings/valid UTF-8 and an
exact boolean `dot` option, default false. The pinned source accepts lone
surrogates and truthy nonbooleans, including `dot=1` for both `.` and `a`.
Product exclusions are `context_schema`. Existing accepted spaces, colons,
`.Git`, percent text and permitted controls remain accepted unchanged; no path
normalization or filesystem security claim is introduced.

Vectors distinguish actual `source_expected` from normative `product_expected`.
Deliberate domain restrictions retain source successes rather than fabricating
oracle failures. Original source snapshots are full-hash/Git-blob checked before
only allowlisted AST closures execute. Portable reproduction requires the pinned
CPython release and explicit settings, not an author's platform-specific binary
hash. Each CLI run emits its actual executable digest/build/platform as evidence.
No source-linked resources or Git objects are fetched.

## Retained serializer and path differences

The following inventory was inspected at the T002 baseline and is not a migration
of those behaviors or their digests:

| Existing primitive | Retained domain/error distinction |
| --- | --- |
| `single_task_context.canonical_bytes` / `context_digest` | Finite floats and JSON-native serializer coercions are not excluded by provider `_json_value`; context digest removes its own approval digest. Errors use `ContextError`, not the selected provider diagnostic. |
| `single_task_state._canonical` | Generic finite-float-capable JSON serialization, with `InvalidState` on listed serialization failures; no provider byte-size predicate. |
| `single_task_scope.canonical_manifest_bytes` | Validates the task manifest first, then uses the context serializer. Its schema and scope diagnostics remain development-only. |
| `task_package.canonical_json` | Integer-only native-value validation plus state serialization and an output-byte limit; this is not the provider serializer's unbounded canonical output domain. |
| `approval_binding._canonical` / `binding_digest` | Generic serializer and approval-binding field selection, without the selected provider error wrapper or domain predicate. |
| `provider_profiles.require_artifact_path` | Different lexical rule: rejects colons and validates UTF-8, but does not inherit the context path's `.git`/glob exclusions. Not substituted for `canonical_path`. |

Task state/approval, recovery, publication, Symphony, provider eligibility and all
filesystem readers remain Python development tooling. Existing Python tests remain
intact. CLI exit/output contracts remain Feature 006 / ADR 0013.

## T004 native Go boundary (candidate)

`apps/api_client/wirejson` exposes `Parse(any)`, `CanonicalBytes(any)` and
`CanonicalDigest(any)`. Parse accepts exact `[]byte`; source-native nonbytes and
bytearray fixtures therefore reach production rejection before size inspection.
It returns `nil`, `bool`, `string`, `*big.Int`, `[]any` and `map[string]any`.
Canonical operations accept those exact types and built-in signed/unsigned integer
types (`int` through `int64`, `uint` through `uint64`; not `uintptr`). Typed nil
slices/maps mean empty containers; a nil `*big.Int` is invalid. Defined types,
arrays, structs, other pointers, bytes, `json.Number`, floats and custom marshalers
are not admitted. No reflection/coercion or custom marshaling runs in product code.
Do not mutate native containers/integers concurrently with these read operations.

`apps/host_agent/internal/config` exposes `CanonicalPath(any)` (dot false) and
`CanonicalPathWithDot(any, any)`. Exact native string/bool validation belongs to
production code; fixture adapters never generate rejection codes. Defined string
or bool types, byte slices, pointers and malformed UTF-8 are `context_schema`.
These are untrusted-value predicates before a future caller's typed schema.

Go has no scalar-string representation for a source surrogate. The vector adapters
preserve surrogate observations as invalid UTF-8 bytes in Go strings, so the real
validator rejects them without replacing them with U+FFFD. Source results remain
in the frozen fixture; only `product_expected` is normative for Go. A distinct
native bytearray type, actual Go arrays for tuples and a non-string native map
preserve unsupported-type cases through production validation. Native oversize
integers are constructed as `big.Int` before canonicalization/digest calls.

Both implementations apply the accepted 4300-digit and 128-container limits;
JSON failures use fixed `invalid-json`, except oversized bytes parse input uses
`input-limit-exceeded` first. Path failures use fixed `context_schema`. The Go
errors expose only these safe codes. No Python, third-party library, filesystem
resolution, provider behavior or caller switch is introduced.

T004's narrow paths are the two Go primitive directories, required Go test IDs in
`docs/development/ci-acceptance.json`, this migration inventory, Feature 006 status
in `tasks.md` and the pilot execution record. Frozen vectors, schema, generator,
source snapshots and manifest pins remain unchanged; Feature 001 task markers
remain unchanged. Independent Python reproduction and Go tests remain separate.
