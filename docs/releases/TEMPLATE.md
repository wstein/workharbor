<!--
Copy to docs/releases/<tag>.md (for example v0.1.0-alpha.4.md) in a commit
before the tag; release.yml prepends the file to the generated notes. Keep the
summary to 4-6 lines. The comments are not rendered.
-->
**WorkHarbor <tag>** (pre-release or release): one sentence on what it is.

- **Highlights:** the changes a user notices, with issue numbers.
- **You can now:** what a user can do that they could not before.
- **Known limits:** what is known broken or unverified; say "unverified" when it is.
- **Verify provenance:** `grep ' install-release.sh$' checksums.txt | shasum -a 256 -c -` (see Install); provenance is `whr_<tag>.intoto.jsonl`, checked with `gh attestation verify`.

## Install

macOS on Apple silicon; needs only `curl`, `shasum`, `tar` and `install`. Replace `<tag>` and run as the administrator; the prefix is `/opt/whr` (add another path as a second argument). The last command of the first block prints `install-release.sh: OK`; any other output is a failure, do not go on.

```bash
cd "$(mktemp -d)"
tag=<tag>; base=https://github.com/wstein/workharbor/releases/download/$tag
curl -fsSLO "$base/install-release.sh" -O "$base/checksums.txt" &&
grep ' install-release.sh$' checksums.txt | shasum -a 256 -c -
```

With the GitHub CLI (`gh`) installed and signed in (optional; skip this step otherwise), verify the script itself next, because the installer runs as root and its own attestation check proves nothing if the script was swapped:

```bash
commit=$(gh api repos/wstein/workharbor/commits/refs/tags/$tag --jq .sha) &&
gh attestation verify install-release.sh --repo wstein/workharbor \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
```

Then run the installer as the administrator:

```bash
sudo bash install-release.sh "$tag"
```

Under `sudo` the script usually has no `gh` login, so it usually checks the checksums only. The `gh attestation verify install-release.sh` step above proves only the script, not the archives or `checksums.txt`. The script verifies the archives' attestation only when it can run `gh` itself; without `gh`, or with a `gh` that cannot read the tag (not signed in, or no login under `sudo`), only the checksums are checked and the script says so; that proves the download is intact, not who built it. The `workharbor` user must not be able to write the prefix. Full guide: [Install, upgrade and release](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/).
