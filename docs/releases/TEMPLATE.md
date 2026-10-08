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

macOS on Apple silicon; needs only `curl`, `shasum`, `tar` and `install`. Replace `<tag>` and run as the administrator; the prefix is `/opt/whr` (add another path as a second argument).

```bash
tag=<tag>; base=https://github.com/wstein/workharbor/releases/download/$tag
curl -fsSLO "$base/install-release.sh" -O "$base/checksums.txt"
shasum -a 256 -c checksums.txt --ignore-missing   # install-release.sh: OK
sudo bash install-release.sh "$tag"
```

With the GitHub CLI (`gh`) installed the script also verifies the build-provenance attestation; without it only the checksums are checked, which proves the download is intact, not who built it. The `workharbor` user must not be able to write the prefix. Full guide: [Install, upgrade and release](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/).
