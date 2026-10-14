# Selected Python migration contracts

T003 candidate; independent implementation review is pending. T004 Go ports and
caller changes are absent. Authority: [ADR 0010](../../docs/adr/0010-go-whr-cli-layout.md)
and the [migration inventory](../../specs/006-whr-cli/migration.md).

The selected JSON profile can identify normalized integer-only lifecycle request
bodies for stable idempotency hashes. T005 must define the actual body schema and
excluded fields; arbitrary incoming JSON is not automatically a request identity.
The lexical path profile can validate spelling of repository-relative config
references. Neither primitive grants trust or authorization. These are not a
general Dev Container JSON parser or the RFC 8785/JCS canonicalization profile.

## Profiles and deliberate restrictions

JSON preserves the selected provider serializer's strict UTF-8, no BOM, decoded
unique keys at every object depth, integer-only numbers, exact boolean/integer
distinction, sorted Unicode scalar keys, compact UTF-8 bytes, no Unicode
normalization and no trailing LF. SHA-256 covers those exact canonical bytes.
HTML characters and U+2028/U+2029 are emitted literally. U+E000 sorts before
U+10000; UTF-16 key ordering would give a different result.

- Parse input is at most 1,048,576 bytes. Larger bytes input is
  `input-limit-exceeded` before UTF-8/syntax validation; non-bytes input is
  `invalid-json` before size inspection.
- **4300 decimal digits** applies to parsed integers and direct
  `canonical_bytes`/`canonical_digest` integers. The minus sign does not count;
  zero has one digit and `-0` canonicalizes to `0`. No machine-width bound applies.
  The runner fixes CPython's otherwise configurable integer-string limit to 4300.
  Larger integers are `invalid-json`.
- **128 containers** is a deliberate product restriction for parsing,
  canonicalization and digest. Scalars have depth zero; each object/array adds
  one level, including the root. Keys and bracket/brace characters inside strings
  add none. Greater depth is `invalid-json`. The source accepts our depth-129
  cases; its eventual recursion failure depends on interpreter/call-stack state.
- Canonicalization inherits no parse-input byte-size cap. The large native-string
  vector proves this. Tooling expansion limits below are fixture-processing bounds,
  not additional product limits.

Paths preserve the pinned lexical predicate: reject empty/nonstring, NUL,
leading slash, backslash, CR/LF/tab, `*?[]`, empty components and components equal
to `.`, `..` or `.git`. Accepted paths are returned unchanged. Whole-string `.`
is permitted only with `dot=true`, whose default is false.

The product path domain deliberately admits only Unicode scalar strings (valid
UTF-8 in Go), excluding source-accepted lone surrogates. The `dot` option must
be an exact boolean; the source accepts truthy nonbooleans and also ignores their
type for ordinary paths. Both restrictions reject with `context_schema`, including
`dot=1` on `a`. Source successes are preserved beside product rejections.

Spaces, colons/drive-shaped spellings, `.Git`, `~`, percent-encoded text and other
permitted control characters retain their classifications. There is no inherited
path-length bound. Do not normalize, expand, case-fold or percent-decode names.
This POSIX-shaped lexical contract proves no symlink safety, containment, Git-entry
type, provider capability or owner trust. Runtime validation remains separate.

## Offline reproduction and review

From the repository root with the pinned CPython 3.14.8:

```sh
python3.14 -I -B scripts/generate_go_vectors.py
python3.14 -I -B -m unittest discover -s tests -p test_go_vectors.py -v
python3.14 -I -B scripts/generate_go_vectors.py --candidate /private/tmp/proposed-workharbor-vectors.json
```

Default verification reads only and compares the complete reproduced fixture file
byte-for-byte, including its frozen SHA-256. Candidate mode explicitly writes a
new external file, refuses every path within the accepted checkout (including
symlink aliases), and refuses an existing destination. It never updates accepted
fixtures, their manifest hashes or source snapshots. Candidate generation checks
source/tool/schema pins but intentionally does not require the current fixture
file hash: a reviewed proposal may change cases or expectations. Strict schema,
source/product distinction and oracle reproduction still apply. Adoption is a
separate reviewed edit, followed by new exact-revision verification.

The generator neither fetches Git history nor accesses source-linked resources.
It checks complete original snapshot SHA-256 and Git blob identities, then compiles
only the allowlisted AST definitions/imports recorded in [manifest](manifest.json).
JSON's closure imports only `json` and `hashlib`; paths import no modules.
Everything else in the mirrored original sources is inert provenance and never
executed. Full snapshots support offline shallow-checkout reproduction. The
original WorkHarbor sources and snapshots retain [EUPL-1.2](../../LICENSE);
mirrors are byte-identical, so license/provenance is recorded here and in the
manifest rather than inserting new bytes into their headers.

The manifest pins CPython release identity, oracle/tool/schema/vector digests and
settings. It records the observed exact `.tool-versions` release pin, not an
unverified upstream source-archive digest. Each CLI run emits its actual executable
SHA-256, build, platform and path as execution evidence. Those platform-specific
values never enter vectors or act as portable preconditions: macOS and Linux
executables may differ. Both use CPython 3.14.8, digit limit 4300 and recursion
limit 1000. Settings are explicitly established, verified and restored; no exact
source recursion-failure threshold is claimed portable. Cross-platform reproduction
is required evidence when exercised, not claimed from this macOS run.

Fixture processing uses bounded regular-file reads (8 MiB documents, 512 KiB
source snapshots), at most 256 cases, schema depth 64, native constructor depth
32, a 2 MiB aggregate native-construction budget, expanded byte values at most
2 MiB, and a 30-second standalone command deadline. The schema permits compact
nested containers up to 256, decimal spellings up to 5001 characters and powers
up to exponent 5000.
These bounds permit the selected negative cases without arbitrary execution.

## Vector format and independent anchors

[vectors.json](vectors.json) validates against [vectors.schema.json](vectors.schema.json).
The stdlib verifier implements precisely the schema's documented keyword subset;
unknown schema keywords/references and extra fixture fields are rejected.
Each case records operation, tagged input, note/decision, `source_expected` and
`product_expected`. Parity means the two results are equal. Deliberate restrictions
record an actual source success and product rejection with their decision label.

Raw bytes use lowercase hex or exact prefix/unit/repetition-count/suffix expansion;
both representations include expanded length and SHA-256, checked before use.
Strings/path results use integer code points, preserving surrogate observations
without introducing malformed fixture JSON. Native integers use decimal chunks or
bounded arithmetic powers, so a 4301-digit value reaches the selected canonical
operation rather than failing during fixture construction. Unsupported native
float, tuple, bytes and nonstring-key cases reach the source validator as such.

An independent Sol reviewer checked these anchors before initial freezing; tests
hard-code them separately from generator results:

| Input | Canonical hex | SHA-256 |
| --- | --- | --- |
| `-0` | `30` | `5feceb66ffc86f38d952786c6d696c79c2dbc239dd4e91b46729d73a27fb57e9` |
| U+E000=1, U+10000=2 object | `7b22ee8080223a312c22f0908080223a327d` | `8706b5798a29d65739b3d1bbb1d009f87d005c71c8130c5353c4990c15ddfec8` |
| U+2028/U+2029 string | `22e280a8e280a922` | `e3a0e2262ac7790f4df36b522a8fbdd4ee3370d568eb15dc25c5f1775f14ed6c` |
| `<>&/` string | `223c3e262f22` | `396adbc953af00557241e2c859a735e8a11b03279d475359cf45dffed8746492` |
| boolean/integer object | `7b2262223a747275652c2269223a317d` | `1dc08b312fcdfc60ec48be27868b868ea4846bcb42e831b21f5db31162f31c63` |

4300 decimal ones produce 4300 canonical bytes and SHA-256
`5155e7af4f6c5b0853aa65153bf170836c773666b83ae909760596f07154c950`.
Other independently checked negatives include nested/escaped/supplementary duplicate
keys, malformed UTF-8/surrogates, float/nonfinite values and oversize-input precedence.
Fixtures are reviewed evidence, not a substitute for the later independent Go port
review. Go tests must read them directly without runtime Python.
