# Confirmation record v1: writers and consumers

`internal/confirm` implements the neutral answer record from #333 and #340.
This guide records the existing v1 contract and the #345 hardening; it does
not grant approval or change the supervisor's policy.

## Canonical bytes and cross-language writers

Use `confirm.Encode` for Go writers. Other writers must reproduce its bytes,
not merely equivalent JSON: bytewise-sorted object keys, no whitespace or
trailing newline, omitted empty optional fields, integers only, no null,
valid UTF-8 strings and lowercase hexadecimal escapes. Printable ASCII stays
literal except quotation marks and backslashes. Every other code point uses
`\uXXXX`; supplementary code points use a UTF-16 surrogate pair. Control
characters use `\u000a`, `\u0009`, `\u0008`, `\u000c`, and `\u000d`, never
JSON's short `\n`, `\t`, `\b`, `\f`, or `\r` escapes.

Python's `json.dumps(ensure_ascii=True)` is not a v1 canonical encoder: its
short control escapes differ even when whitespace and key order are configured.
JCS is also a different encoding. A Python writer needs a v1-specific encoder.
Python's default `json.loads` is lenient about duplicate keys and non-finite
numbers; parsing successfully does not establish v1 validity. A crewbook
validator must explicitly reject duplicate keys at every level, null, floats,
non-finite numbers, invalid UTF-8 and lone surrogates, unknown fields, excessive
nesting and trailing bytes, then check v1 fields and compare the input with its
canonical re-encoding. Do not silently normalize non-canonical inputs.

The golden fixture `internal/confirm/testdata/v1/decision-controls.json` pins
newline, tab, other controls and a surrogate pair ending in `\udfff`. Its
record digest is
`749023a6ce9588491548af6939b40742718ed51e7d0762e549592fcd358ec497`.
The fixture file has a final newline for editor tooling; the record bytes do
not. A **record digest** is SHA-256 of `workharbor-confirm-v1` followed by a
NUL byte and the canonical record bytes, without the file newline. A **file
SHA-256** hashes the complete file, including its newline, without that domain
separator. A pin must explicitly name which it uses; the Go golden tests pin
record digests. Remove exactly the fixture's one final newline when testing,
not arbitrary whitespace from received records. `DigestOf` trusts that its
input is already canonical.

## Constructing a record

`Encode` and `Record.Digest` do not call `Validate`. Build the record, check
`Validate`, then `Encode`; treat every error as failure. `Validate` checks v1
fields but does not traverse extension values. `Encode` checks supported
extension value types, UTF-8 and the decoder's depth limit, so validation alone
cannot guarantee encodability. For acceptance from bytes, use `Decode`, which
checks canonicality and validates the decoded record.

Nesting depth counts edges from the root object at depth zero. Every object
member value or array element adds one; values at depth 16 are accepted and
values deeper than 16 are rejected. The encoder shares this limit and returns
an error for cyclic maps or slices rather than recursing indefinitely.
Extension values can be strings, booleans, `int` or `int64`, arrays of these
values (`[]any`) and objects (`map[string]any`); extension names are namespaced.
Large integers are valid within the supported integer range, but JavaScript
numbers lose precision above 2^53-1 in magnitude. Cross-language writers should
use exact integer parsing and encoding, or agree on namespaced decimal-string
extensions for larger values; do not convert them through floating point.

There is no public evidence-sort helper. Encode each evidence item with the
same canonical object rules (omit empty optional fields), sort the resulting
bytes lexicographically, and reject duplicate items. `Validate` rejects evidence
that is unordered or duplicated; `Encode` preserves the supplied order.

Every action requires a nonempty subject. `land` and `push` additionally require
a full lowercase 40- or 64-digit `subject.commit`. An issue is `owner/repo#n`
with a positive issue number, so writers must retain the owner and repository;
there is no separate repository field. `typed_sha` is a lowercase hexadecimal
prefix of that commit, at least seven characters. v1 does not require
`subject.decision` for `decision`, or `subject.ref` for `change.confirm`.
Consumers must bind the subject to the actual pending operation; a valid
record with an unrelated nonempty subject does not establish that binding.

## Validation, approval and presentation

Successful validation means a structurally valid answer record, not approval.
`deny` and `yn:no` validate and mean refusal. Consumers must check the answer:
`yn:yes` is affirmative, `typed_sha` identifies the validated commit prefix,
and an `option` value needs interpretation against the options of its question.
There is deliberately no generic approval helper that treats every option as
permission. Consumers must also verify the action, exact subject, origin and
applicable approval policy before acting.

`assurance` is self-asserted metadata. v1 accepts `cli` plus `passkey` without
a passkey assertion and does not cross-check channel, assurance or evidence.
A string saying `passkey`, or evidence naming an assertion, is not verified
cryptographic proof. Consumers requiring passkey assurance must verify the
actual assertion and its binding through their trusted authentication flow.
The local CLI record is an honest log, not proof against the same user.

Free-form strings may contain controls, bidirectional markers and visually
confusable characters, including `by`, branch, ref, decision and question.
Never print these raw in approval prompts, terminals or audit summaries. Render
an escaped representation (for example Go `%+q` for ASCII-escaped strings),
using the same representation in the prompt and later inspection. HTML escaping
alone does not expose bidi markers or confusable characters. Show exact trusted
commit IDs and structured subjects alongside text; a visually familiar name is
not an authenticated identity. v1 preserves these strings for byte and digest
compatibility; changing allowed strings, assurance cross-checks or required
subject fields needs a separate design decision and compatibility assessment.
