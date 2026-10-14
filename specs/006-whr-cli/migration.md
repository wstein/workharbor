# Selective Python-to-Go migration

Status: proposed inventory and acceptance; ports not implemented.
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
