# Codex pin admission and public evidence

The [settled verification boundary](https://github.com/wstein/workharbor/issues/164#issuecomment-5969043794)
requires provenance verification **when admitting or updating a pin**, using
an external, version-pinned verifier run by the human or CI. The supervisor
never runs that verifier, fetches a signature bundle or trusts its certificate.
At runtime it requires the reviewed archive SHA-256, bounded safe extraction,
and the extracted/copied binary SHA-256 from `../pins.json`.

## Admission procedure

1. Select an exact release and artifact from the official
    [Codex releases](https://github.com/openai/codex/releases). Record the release
    commit, archive URL, binary name, binary bundle URL and expected workflow
    identity with its exact tag. Do not use a moving latest-version URL or a
    wildcard identity. Download the archive and legacy `.sigstore` bundle over
    HTTPS into a private temporary directory, with size and time limits.
2. Hash the archive and compare it with the published release digest. Extract
    only the named regular binary into that temporary directory, without running
    it. Use bounded extraction, refuse links/special files/path escape, and hash
    the extracted bytes. The optional runtime archive test below additionally
    exercises these controls for this pin.
3. Run the pinned external verifier from outside the project module. For
    upstream's legacy bundle, first use cosign's own conversion command; it
    performs a **public Rekor network lookup** to recover inclusion evidence.
    Then verify the converted evidence with the independently pinned public
    trust root, exact expected identity and issuer. No key, subscription login,
    credential helper or vendor binary execution is needed.

    ```sh
    go run github.com/sigstore/cosign/v3/cmd/cosign@v3.1.3 bundle create \
      --artifact "$binary" --bundle "$legacy_bundle" \
      --out "$standard_bundle" --timeout 45s
    go run github.com/sigstore/cosign/v3/cmd/cosign@v3.1.3 verify-blob \
      --bundle "$standard_bundle" --trusted-root "$trusted_root" \
      --certificate-identity "$expected_identity" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      "$binary" --timeout 45s
    ```

    Here each path names the public file in the private admission directory.
    Check the trust-root SHA-256 against the independent source below before
    using it. Require both commands to exit zero; do not use flags that skip
    certificate, certificate-transparency or transparency-log checks. cosign
    `v3.1.3` fixes the
    [legacy-bundle identity bypass](https://github.com/sigstore/cosign/security/advisories/GHSA-fx35-mq7g-6g98).
    A conversion failure or unknown trust key blocks admission; it is not an
    excuse to disable verification. A future key rotation requires a separately
    reviewed official trust-root update.
4. Check that wrong identity, wrong issuer and changed binary bytes each fail.
    Record verifier/root versions, hashes, verified identity/issuer and results
    with the pin update. Commit the verified archive and binary hashes together
    with that evidence; preserve both original and converted public bundles.
    Remove the private admission files after completing the check.

## Admitted 0.159.2 arm64 musl artifact

The [release](https://github.com/openai/codex/releases/tag/rust-v0.159.2) points
at commit `ff6aec96948b70d94983af2641a6b67c94faeff5`.
Its [signing action](https://github.com/openai/codex/blob/ff6aec96948b70d94983af2641a6b67c94faeff5/.github/actions/linux-code-sign/action.yml)
signs **the binary**, not its standalone tarball.
The archive contains `codex-aarch64-unknown-linux-musl` as one regular file.
The published archive SHA-256 and measured extracted binary SHA-256 are
recorded in `../pins.json`.

Verified identity:
`https://github.com/openai/codex/.github/workflows/rust-release.yml@refs/tags/rust-v0.159.2`.
Verified issuer: `https://token.actions.githubusercontent.com`.
The external verifier was `github.com/sigstore/cosign/v3/cmd/cosign@v3.1.3`.
These admission checks ran on 2026-10-05 against the actual extracted bytes:

| Check | Exit | Result |
| --- | --- | --- |
| Convert original legacy bundle with cosign | 0 | Standard v0.3 bundle with Rekor inclusion proof and promise |
| Exact identity, issuer and binary | 0 | `Verified OK` |
| Identity changed to tag `rust-v0.159.3` | 1 | Expected SAN did not match |
| Issuer changed to `https://accounts.google.com` | 1 | Expected issuer did not match |
| Changed binary bytes | 1 | Signature verification failed |

The public evidence files are:

| File | SHA-256 | Provenance |
| --- | --- | --- |
| Original upstream legacy bundle, before final newline | `177267105badf9912855e6933a46e531dc2083feaed86d37fdcec58cfd87f7da` | Release asset `codex-aarch64-unknown-linux-musl.sigstore` |
| `codex-0.159.2.sigstore` | `963a36a5aca61086587d572a7a8c0f1637ed386a553f5745f4181c18122d9a2c` | Original bundle with only a final newline added for repository text conventions |
| `codex-0.159.2-standard.sigstore.json` | `723538b1684790c754dfe26c225c95f6fb3423c941cbd0415b6f649f44eaf54b` | External cosign conversion with public Rekor lookup; final newline added |
| `sigstore-trusted-root.json` | `6494e21ea73fa7ee769f85f57d5a3e6a08725eae1e38c755fc3517c9e6bc0b66` | Unmodified independent [sigstore/root-signing at 5888f358fc4ab58874447259edf83790261fc616](https://github.com/sigstore/root-signing/blob/5888f358fc4ab58874447259edf83790261fc616/targets/trusted_root.json) |

These files are admission fixtures, **not embedded supervisor trust material**.
The standardized fixture can be independently rechecked with the second
command above and the actual downloaded/extracted binary. Conversion alone
is not provenance verification.

## Remaining limits and runtime check

No arm64 GNU Codex asset is published by this release. The glibc pin remains
absent pending an upstream GNU artifact or authoritative evidence for
portability of this exact musl binary. The complete dual-libc pin criterion
remains unmet. Fixtures, admission signatures and digest checks establish no
target-container, native protocol, login or agent-conformance support; those
measurements remain issues #34 and #241. The 0.159.2 artifact is separate from
the unverified 0.160.0 schema foundation.

The optional complete runtime check runs with
`WHR_CODEX_RELEASE_ARCHIVE=<downloaded-public-tarball> go test ./internal/toolstore -run TestCodexReleaseArchive -count=1`.
It serves the pinned archive over a local test server, checks its hash, safely
extracts and installs the binary, and rechecks the immutable store entry.
It does not fetch provenance evidence, execute an external verifier or run Codex.
