<!--
Copy to docs/releases/<tag>.md (for example v0.1.0-alpha.4.md) in a commit
before the tag; release.yml prepends the file to the generated notes. Keep the
summary to 4-6 lines. The comments are not rendered.
-->
**WorkHarbor <tag>** (pre-release or release): one sentence on what it is.

- **Highlights:** the changes a user notices, with issue numbers.
- **You can now:** what a user can do that they could not before.
- **Known limits:** what is known broken or unverified; say "unverified" when it is.
- **Verify provenance:** `shasum -a 256 -c checksums.txt`; provenance is `whr_<tag>.intoto.jsonl`, checked with `gh attestation verify`.

## Install

<!-- install-block: filled by #459 (install instructions for this tag); keep this heading. -->
