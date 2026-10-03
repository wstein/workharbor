#!/usr/bin/env bash
# The pin-update step for Antigravity (#164). `whr tools build` trusts the pin
# in internal/toolstore/pins.json alone and never reads the vendor's manifest,
# which names only the latest release. Run this when you write or update a pin:
# it fetches each manifest over https and compares version, archive URL and
# sha512 with the pin. A difference means a newer release (re-pin) or a changed
# host (investigate). It reads no credential and writes nothing. Needs curl, jq.
#
#   scripts/antigravity-pin-check.sh [pins.json]
set -euo pipefail

pins="${1:-internal/toolstore/pins.json}"
rc=0
while IFS=$'\t' read -r platform base version url sha512; do
  case "$base" in https://*) ;; *) echo "refusing a non-https manifest base: $base" >&2; exit 2 ;; esac
  m="$(curl --proto '=https' --tlsv1.2 -fsS --max-filesize 1048576 "$base/${platform//-/_}.json")"
  got="$(jq -r '[.version, .url, .sha512] | @tsv' <<<"$m")"
  want="$(printf '%s\t%s\t%s' "$version" "$url" "$sha512")"
  if [ "$got" = "$want" ]; then
    echo "ok   $platform $version"
  else
    echo "DIFF $platform: pin $version, manifest ${got%%$'\t'*}" >&2
    rc=1
  fi
done < <(jq -r '.tools[] | select(.format == "antigravity") | [.platform, .base_url, .version, .archive_url, .sha512] | @tsv' "$pins")
exit "$rc"
