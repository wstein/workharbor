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

Optional, and before the installer: verify who built the archive with `gh`. Skip it on a first install, when `gh` is not installed yet (setup installs it); on an upgrade `gh` is there, so run it. It needs `gh` signed in.

```bash
commit=$(gh api repos/wstein/workharbor/commits/refs/tags/$tag --jq .sha) &&
gh attestation verify "$f" --repo wstein/workharbor \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
```

Then unpack and run the installer as the administrator; the prefix is `/opt/whr` (add another path as a second argument). Nothing is downloaded and no `gh` is needed:

```bash
tar -xzf "$f" &&
sudo ./install.sh "$tag"
```

With `checksums.txt` next to it, the tag must name that archive and match its checksum, or `install.sh` installs nothing; the files come from the checked archive, and its entries should be `root:wheel` (unverified on the release runner, see Known limits). `install.sh` itself checks no attestation (that is the `gh` step above). The `workharbor` user must not be able to write the prefix.

Next, as the administrator: `/opt/whr/bin/whr setup` (`/opt/whr/bin` is not on the administrator's `PATH`; `/opt/whr/bin/whr version` shows the tag). It now also initializes the `workharbor` account's base configuration (#529; this changes the first-install order of alpha.4). The word `host` is gone from the command; `whr setup host` still works as an alias. Then, as `workharbor` in its desktop session, not over SSH: `export PATH=/opt/whr/bin:$PATH` in `~/.zprofile`, and `whr setup` again for the steps that need that session (API token, container system, GitHub App keys, tool store, service).

In a later shell, `cd` back to the unpacked directory and set `tag` and `f` again; the blocks use them.

Full guide: [Install, upgrade and release](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/).
