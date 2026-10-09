<!--
Copy to docs/releases/<tag>.md (for example v0.1.0-alpha.4.md) in a commit
before the tag; release.yml prepends the file to the generated notes. Keep the
summary to 4-6 lines. The comments are not rendered.
-->
**WorkHarbor <tag>** (pre-release or release): one sentence on what it is.

- **Highlights:** the changes a user notices, with issue numbers.
- **You can now:** what a user can do that they could not before.
- **Known limits:** what is known broken or unverified; say "unverified" when it is (the first release with a new pipeline step lists it here; see the manual's release profile).
- **Verify provenance:** `shasum -a 256 -c` on the archive (see Install); provenance is `whr_<tag>.intoto.jsonl`, checked with `gh attestation verify`.

## Install

macOS on Apple silicon; needs only `curl`, `shasum`, `tar`, `install` and `sudo`. One archive holds `bin/whr`, the Linux guest binaries in `guest/` (payload, never run on the Mac) and `install.sh`. Replace `<tag>`. The first block must print `whr_<version>_darwin_arm64.tar.gz: OK`; any other output is a failure, do not go on.

```bash
cd "$(mktemp -d)"
tag=<tag>; base=https://github.com/wstein/workharbor/releases/download/$tag
f=whr_${tag#v}_darwin_arm64.tar.gz
curl -fsSLO "$base/$f" -O "$base/checksums.txt" &&
grep " $f\$" checksums.txt | shasum -a 256 -c -
```

Then unpack and run the installer as the administrator; the prefix is `/opt/whr` (add another path as a second argument). Nothing is downloaded and no `gh` is needed:

```bash
tar -xzf "$f" &&
sudo ./install.sh "$tag"
```

Optional, once `gh` is installed (setup installs it) and signed in: verify who built the archive.

```bash
commit=$(gh api repos/wstein/workharbor/commits/refs/tags/$tag --jq .sha) &&
gh attestation verify "$f" --repo wstein/workharbor \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
```

`install.sh` checks no attestation and does not tie the tag to the archive. The `workharbor` user must not be able to write the prefix. Full guide: [Install, upgrade and release](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/).
