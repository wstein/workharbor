#!/usr/bin/env bash
# Install whr, whr-shim and whr-proxy from a release of this repository (design
# D24, D34, §13 Releases). Run it as the administrator; PREFIX must be one the whr
# user cannot write.
#
#   install.sh <tag> [prefix]          from the unpacked release archive
#   install-release.sh <tag> [prefix]  from a clone: downloads the archive
#
# A release is one archive, whr_<version>_darwin_arm64.tar.gz: bin/whr, the Linux
# guest binaries in guest/ (payload, never run on the host) and this script as
# install.sh. Next to bin/ and guest/ the script installs them as they are: the
# archive was checked against checksums.txt by the user (shasum -a 256 -c), nothing
# is downloaded and gh is not needed. When checksums.txt sits next to the script, the
# archive of <tag> must sit there too and match its line, so a wrong tag is refused
# before it can reach VERSION; the attestation is the user's to check beforehand. From a clone it downloads the archive and
# checksums.txt with curl (gh is the fallback for a draft, which only a repository
# writer can download), checks the archive against checksums.txt and, with gh
# installed, the build-provenance attestation of this repository's release workflow
# for exactly this tag (run on refs/tags/<tag>, at the tag's commit, on a
# GitHub-hosted runner). Without gh only the checksums are checked: that proves the
# download is intact, not who built it, and the script says so. Nothing is installed
# unless every check passes. It refuses an older tag than the one installed unless
# --allow-downgrade is given: the archives of an old, vulnerable release are still
# validly attested. WHR_RELEASE_DIR names a folder that already holds the archive and
# checksums.txt, to skip the download; the checks still run. WHR_RELEASE_REPO changes
# the repository whose attestations are trusted (a fork, say): it must be owner/name
# and is refused unless --trust-release-repo confirms it. The installed version is
# read from $prefix/libexec/whr/VERSION, which this script writes after a verified
# install: the installed whr is never run before the checks (an install from before
# that file existed needs --allow-downgrade once).
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

# check_archive <dir>: exactly one line for the archive in <dir>/checksums.txt,
# checked with the file name fixed by the tag.
check_archive() {
  (
    cd "$1" || exit 1
    [ "$(awk -v f="$archive" '$2 == f' checksums.txt | wc -l)" -eq 1 ] || die "checksums.txt has no single line for $archive"
    awk -v f="$archive" '$2 == f' checksums.txt | shasum -a 256 -c - >/dev/null || die "$archive does not match checksums.txt"
  )
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
# In the unpacked archive the files are next to the script.
self="${BASH_SOURCE[0]:-$0}"
[[ "$self" == */* ]] || self="./$self"
here="$(cd "${self%/*}" && pwd)"
unpacked=0
[ -f "$here/bin/whr" ] && [ -d "$here/guest" ] && unpacked=1
have_gh=0
command -v gh >/dev/null && have_gh=1
if [ "$unpacked" -eq 1 ]; then
  have_gh=0
elif [ "$have_gh" -ne 1 ]; then
  echo "install-release: gh not found: checking checksums.txt only. That proves the download is intact, not who built it (install gh to verify the attestation of $repo)." >&2
fi

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
archive="whr_${version}_darwin_arm64.tar.gz"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if [ "$unpacked" -eq 1 ]; then
  if [ -f "$here/checksums.txt" ]; then
    # The documented flow unpacks next to the downloaded archive and checksums.txt:
    # the tag must name that archive, or a wrong tag would be written to VERSION
    # and corrupt the downgrade guard.
    [ -f "$here/$archive" ] || die "checksums.txt is next to the script but $archive is not: $tag does not match the downloaded archive"
    check_archive "$here"
    echo "install: the tag $tag matches $archive next to the script (checksum ok)" >&2
  else
    echo "install: no checksums.txt next to the script: the tag $tag is not checked against the archive" >&2
  fi
  echo "install: from the unpacked archive in $here (its checksum is the user's to check; with gh, verify its attestation before running this script)" >&2
  root="$here"
else
if [ -n "${WHR_RELEASE_DIR:-}" ]; then
  for f in "$archive" checksums.txt; do
    cp "$WHR_RELEASE_DIR/$f" "$work/" || die "$f is not in $WHR_RELEASE_DIR"
  done
else
  base="https://github.com/$repo/releases/download/$tag"
  fetch() { # fetch <file>: curl, then gh for a draft
    if command -v curl >/dev/null && curl -fsSL --proto '=https' --tlsv1.2 --max-redirs 5 -o "$work/$1" "$base/$1"; then
      return 0
    fi
    [ "$have_gh" -eq 1 ] && gh release download "$tag" --repo "$repo" --dir "$work" --pattern "$1" 2>/dev/null
  }
  for f in "$archive" checksums.txt; do
    fetch "$f" || die "cannot download $f of $tag from $repo (a draft needs a writer's gh login)"
  done
fi

check_archive "$work"

commit=""
if [ "$have_gh" -eq 1 ]; then
  # The tag's commit, so an attestation made for another commit does not pass.
  commit="$(gh api "repos/$repo/commits/refs/tags/$tag" --jq .sha 2>/dev/null)" || commit=""
  if ! [[ "$commit" =~ ^[0-9a-f]{40}$ ]]; then
    # gh is installed but unusable (not signed in, no login under sudo, no network
    # to the API): same as no gh. A working gh never takes this path, so a failing
    # attestation check below still stops the install.
    echo "install-release: gh cannot read the commit of tag $tag in $repo (not signed in? under sudo the login may be missing): checking checksums.txt only" >&2
    have_gh=0
  fi
fi
if [ "$have_gh" -eq 1 ]; then
  echo "install-release: trusting attestations of $repo for $tag (release workflow on refs/tags/$tag)" >&2
  gh attestation verify "$work/$archive" --repo "$repo" \
    --signer-workflow "$repo/.github/workflows/release.yml" \
    --source-ref "refs/tags/$tag" --source-digest "$commit" \
    --deny-self-hosted-runners >/dev/null ||
    die "$archive has no valid build-provenance attestation from $repo's release workflow run on tag $tag at $commit"
else
  echo "install-release: caveat: only the checksums were verified, not the origin (no attestation check without gh)" >&2
fi
mkdir -p "$work/x"
[ -n "$(tar -tzf "$work/$archive" bin/whr 2>/dev/null)" ] ||
  die "$archive has no bin/whr: $tag has the old layout (separate host and guest archives); use that tag's own script: git show $tag:scripts/install-release.sh"
tar -xzf "$work/$archive" -C "$work/x" bin/whr guest/whr-shim-linux-arm64 guest/whr-proxy-linux-arm64 || die "cannot unpack $archive"
root="$work/x"
fi

# Warn about an existing prefix directory (judged by its target when it is a
# symlink) that is group- or world-writable, or that the running user does not
# own. Both are only warnings in the alpha (#493, #504), revisited at beta.
me="$(id -u)"
for d in "$prefix" "$prefix/bin" "$prefix/libexec" "$prefix/libexec/whr"; do
  if [ -d "$d" ] && [ -n "$(find -H "$d" -maxdepth 0 \( -perm -020 -o -perm -002 \) 2>/dev/null)" ]; then
    echo "install-release: warning: $d is group- or world-writable: whoever can write it can replace the whr binary that root runs; alpha policy (#504), revisit at beta" >&2
  fi
  if [ -d "$d" ] && [ -n "$(find -H "$d" -maxdepth 0 ! -user "$me" 2>/dev/null)" ]; then
    echo "install-release: warning: $d is not owned by uid $me; alpha policy (#493), revisit at beta" >&2
  fi
done

install -d -m 0755 "$prefix/bin" "$prefix/libexec/whr"
# Replace by rename, never by rewriting a binary in place: a running or cached
# whr keeps its old inode and a stale code signature cannot be left behind (#393).
# The release binaries keep the signature they were built with.
place() { # place <source> <destination>
  install -m 0755 "$1" "$2.new.$$" && mv -f "$2.new.$$" "$2" || { rm -f "$2.new.$$"; die "could not install $2"; }
}
place "$root/bin/whr" "$prefix/bin/whr"
place "$root/guest/whr-shim-linux-arm64" "$prefix/libexec/whr/whr-shim-linux-arm64"
place "$root/guest/whr-proxy-linux-arm64" "$prefix/libexec/whr/whr-proxy-linux-arm64"
printf '%s\n' "$tag" >"$work/VERSION"
install -m 0644 "$work/VERSION" "$prefix/libexec/whr/VERSION"

echo "installed whr $("$prefix/bin/whr" version), whr-shim and whr-proxy (linux-arm64) under $prefix" >&2
echo "next, as whr: $prefix/bin/whr tools build -store <tool store> -shim $prefix/libexec/whr/whr-shim-linux-arm64" >&2
}

# Run main unless the file is sourced (the tests source it for semver_lt). Piped
# into bash there is no BASH_SOURCE: run main then too.
if [ -z "${BASH_SOURCE[0]:-}" ] || [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
