#!/usr/bin/env bash
# Install whr, whr-shim and whr-proxy from a release of this repository, a
# draft included (design D24, D34, §13 Releases). Run it as the administrator:
# a draft is downloadable only by a repository writer, and PREFIX should be one
# the whr user cannot write.
#
#   scripts/install-release.sh <tag> [prefix]
#
# It downloads the macOS archive, the guest archive and checksums.txt with gh,
# checks both archives against checksums.txt and against the build-provenance
# attestation of this repository's release workflow for exactly this tag (the
# attestation must come from the release workflow run on refs/tags/<tag>, at the
# tag's commit, on a GitHub-hosted runner), and installs nothing unless every check
# passes. It refuses an older tag than the one installed unless --allow-downgrade
# is given: the attested archives of an old, vulnerable release are still validly
# attested. WHR_RELEASE_DIR names a folder that already holds the three files, to
# skip the download; the checks still run. WHR_RELEASE_REPO changes the repository
# whose attestations are trusted (a fork, say): it must be owner/name and is refused
# unless --trust-release-repo confirms it; the script says which one it trusts.
# The installed version is read from $prefix/libexec/whr/VERSION, which this script
# writes after a verified install: the installed whr is never run before the checks
# (an install from before that file existed needs --allow-downgrade once).
set -euo pipefail

die() {
  echo "install-release: $*" >&2
  exit 1
}

# semver_lt A B succeeds when version A (vX.Y.Z[-pre]) is older than B: the numbers
# compare as numbers, a pre-release is older than its release, and two pre-releases
# compare by version sort.
semver_lt() {
  local a="${1#v}" b="${2#v}" an bn ap bp
  an="${a%%-*}"
  bn="${b%%-*}"
  ap=""
  bp=""
  [[ "$a" == *-* ]] && ap="${a#*-}"
  [[ "$b" == *-* ]] && bp="${b#*-}"
  if [ "$an" != "$bn" ]; then
    [ "$(printf '%s\n%s\n' "$an" "$bn" | sort -t. -k1,1n -k2,2n -k3,3n | head -1)" = "$an" ]
    return
  fi
  [ "$ap" = "$bp" ] && return 1
  [ -z "$bp" ] && return 0 # a pre-release is older than the release
  [ -z "$ap" ] && return 1
  [ "$(printf '%s\n%s\n' "$ap" "$bp" | sort -V | head -1)" = "$ap" ]
}

main() {
repo="${WHR_RELEASE_REPO:-wstein/workharbor}"
allow_downgrade=0
trust_repo=0
args=()
for a in "$@"; do
  case "$a" in
    --allow-downgrade) allow_downgrade=1 ;;
    --trust-release-repo) trust_repo=1 ;;
    *) args+=("$a") ;;
  esac
done
[[ "$repo" =~ ^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+$ ]] || die "WHR_RELEASE_REPO must be owner/name (got '$repo')"
[[ "${repo%%/*}" != . && "${repo%%/*}" != .. && "${repo##*/}" != . && "${repo##*/}" != .. ]] || die "WHR_RELEASE_REPO must be owner/name (got '$repo')"
if [ -n "${WHR_RELEASE_REPO:-}" ] && [ "$repo" != wstein/workharbor ] && [ "$trust_repo" -ne 1 ]; then
  die "WHR_RELEASE_REPO=$repo is not the default repository: pass --trust-release-repo to trust its attestations"
fi
tag="${args[0]:-}"
prefix="${args[1]:-/opt/whr}"

[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "usage: $0 vX.Y.Z[-pre] [prefix] (got '$tag')"
[[ "$prefix" = /* ]] || die "the prefix must be an absolute path (got '$prefix')"
[ "$(uname -s)/$(uname -m)" = Darwin/arm64 ] || die "whr releases are for macOS on Apple silicon"
command -v gh >/dev/null || die "needs gh (brew install gh), signed in as a writer of $repo"
echo "install-release: trusting attestations of $repo for $tag (release workflow on refs/tags/$tag)" >&2

# Never go back to an older release silently.
# The version comes from the file the installer wrote, never from running the binary.
if [ -e "$prefix/bin/whr" ]; then
  installed=""
  if [ -f "$prefix/libexec/whr/VERSION" ] && [ ! -L "$prefix/libexec/whr/VERSION" ]; then
    installed="$(head -c 64 "$prefix/libexec/whr/VERSION" | awk '{print $1; exit}')" || installed=""
  fi
  if [[ "$installed" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
    if semver_lt "$tag" "$installed" && [ "$allow_downgrade" -ne 1 ]; then
      die "$tag is older than the installed $installed: pass --allow-downgrade to go back"
    fi
  elif [ "$allow_downgrade" -ne 1 ]; then
    die "cannot read the installed version ('$installed'): pass --allow-downgrade to install $tag anyway"
  fi
fi

version="${tag#v}"
mac="whr_${version}_darwin_arm64.tar.gz"
guest="whr-guest_${version}_linux_arm64.tar.gz"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ -n "${WHR_RELEASE_DIR:-}" ]; then
  for f in "$mac" "$guest" checksums.txt; do
    cp "$WHR_RELEASE_DIR/$f" "$work/" || die "$f is not in $WHR_RELEASE_DIR"
  done
else
  gh release download "$tag" --repo "$repo" --dir "$work" \
    --pattern "$mac" --pattern "$guest" --pattern checksums.txt ||
    die "cannot download $tag from $repo (a draft needs a writer's gh login)"
fi

# Exactly one line per archive, checked with the file names fixed above.
(
  cd "$work"
  for f in "$mac" "$guest"; do
    [ "$(awk -v f="$f" '$2 == f' checksums.txt | wc -l)" -eq 1 ] || die "checksums.txt has no single line for $f"
    awk -v f="$f" '$2 == f' checksums.txt | shasum -a 256 -c - >/dev/null || die "$f does not match checksums.txt"
  done
)

# The tag's commit, so an attestation made for another commit does not pass.
commit="$(gh api "repos/$repo/commits/refs/tags/$tag" --jq .sha 2>/dev/null)" || commit=""
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || die "cannot read the commit of tag $tag in $repo"
for f in "$mac" "$guest"; do
  gh attestation verify "$work/$f" --repo "$repo" \
    --signer-workflow "$repo/.github/workflows/release.yml" \
    --source-ref "refs/tags/$tag" --source-digest "$commit" \
    --deny-self-hosted-runners >/dev/null ||
    die "$f has no valid build-provenance attestation from $repo's release workflow run on tag $tag at $commit"
done

mkdir -p "$work/mac" "$work/guest"
tar -xzf "$work/$mac" -C "$work/mac" whr
tar -xzf "$work/$guest" -C "$work/guest" whr-shim whr-proxy

install -d -m 0755 "$prefix/bin" "$prefix/libexec/whr"
install -m 0755 "$work/mac/whr" "$prefix/bin/whr"
install -m 0755 "$work/guest/whr-shim" "$prefix/libexec/whr/whr-shim-linux-arm64"
install -m 0755 "$work/guest/whr-proxy" "$prefix/libexec/whr/whr-proxy-linux-arm64"
printf '%s\n' "$tag" >"$work/VERSION"
install -m 0644 "$work/VERSION" "$prefix/libexec/whr/VERSION"

echo "installed whr $("$prefix/bin/whr" version), whr-shim and whr-proxy (linux-arm64) under $prefix" >&2
echo "next, as whr: $prefix/bin/whr tools build -store <tool store> -shim $prefix/libexec/whr/whr-shim-linux-arm64" >&2
}

# Run main unless the file is sourced (the tests source it for semver_lt).
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
