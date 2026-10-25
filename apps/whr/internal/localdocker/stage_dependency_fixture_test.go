//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

// Exact tracked fixture bytes: Anchorage 0f72d7226b6921e7d0c2642aca034ef44fe26c56;
// BundleDock 9f16cb790c108cf7165fc8bfb1ede2180717dd3b. Never executed by these tests.
var stageDependencyBodyFixtures = map[string]string{
	".devcontainer/greeting-info.sh": `#!/bin/sh
# Managed by .devcontainer/setup.sh: facts for the fastfetch greeting. Usage: anchorage-info TOPIC
# Reads anchorage-info.env (WORKSPACE, PINS) written next to this file at setup time.
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
[ -r "$here/anchorage-info.env" ] && . "$here/anchorage-info.env"
cache="${TMPDIR:-/tmp}/anchorage-greeting-$(id -u)"

if [ -n "${NO_COLOR-}" ]; then
    ok='✓'
    err='✗'
    warn='⚠'
    yellow=''
    dim=''
    reset=''
else
    ok=$(printf '\033[32m✓\033[0m')
    err=$(printf '\033[31m✗\033[0m')
    warn=$(printf '\033[33m⚠\033[0m')
    yellow=$(printf '\033[33m')
    dim=$(printf '\033[2m')
    reset=$(printf '\033[0m')
fi

# First version-looking number of a command's version output.
version() {
    command -v "$1" >/dev/null 2>&1 || return 1
    out=$("$@" 2>&1 | head -n 1 | grep -o '[0-9][0-9]*\.[0-9][0-9.]*' | head -n 1)
    [ -n "$out" ] && printf '%s %s' "$name" "$out"
}

# Print "name version" pairs joined by " · " for "name:command args" specs; cached for the container.
versions() {
    key=$1
    shift
    if [ -r "$cache.$key" ]; then cat "$cache.$key"; return; fi
    result=
    for spec in "$@"; do
        name=${spec%%:*}
        command=${spec#*:}
        # shellcheck disable=SC2086
        found=$(version $command) || continue
        result="${result:+$result · }$found"
    done
    printf '%s' "$result" | tee "$cache.$key" 2>/dev/null
}

case ${1:-} in
host|engine|hostname)
    file=${ANCHORAGE_HOST_ENV:-${ANCHORAGE_WORKSPACE:-${WORKSPACE:-.}}/.devcontainer/host.env}
    if [ ! -r "$file" ]; then
        [ "$1" = host ] && echo 'unknown (run make devcontainer-up on the host)'
        exit 0
    fi
    . "$file"
    if [ "$1" = hostname ]; then
        printf '%s\n' "$HOST_NAME"
    elif [ "$1" = host ]; then
        printf '%s%s · %s · %s\n' "$HOST_NAME" "${HOST_IP:+ ($HOST_IP)}" "$HOST_OS" "$HOST_ARCH"
    else
        printf '%s%s%s\n' "$HOST_ENGINE" "${HOST_ENGINE_ARCH:+ · $HOST_ENGINE_ARCH}" "${HOST_CONTEXT:+ · context $HOST_CONTEXT}"
    fi
    ;;
role)
    if [ -n "${ANCHORAGE_ROLE-}" ]; then
        printf '%s\n' "$ANCHORAGE_ROLE"
        exit 0
    fi
    cname=$(hostname -s 2>/dev/null || hostname 2>/dev/null || true)
    case $cname in
        *-staging) printf 'staging\n' ;;
        *-[0-9]*) printf 'temp\n' ;;
    esac
    ;;
smoke)
    dir=${ANCHORAGE_WORKSPACE:-${WORKSPACE:-.}}/.devcontainer
    file=${ANCHORAGE_SMOKE_ENV:-$dir/smoke.env}
    if [ ! -r "$file" ]; then printf '%snot yet run on this host (make devcontainer-smoke)%s\n' "$yellow" "$reset"; exit 0; fi
    . "$file"
    current=$(sh "$dir/pins-hash.sh" 2>/dev/null)
    if [ "$SMOKE_PINS" = "$current" ]; then
        printf '%s passed %s on %s\n' "$ok" "$SMOKE_AT" "$SMOKE_HOST"
    else
        printf '%s not yet run for these pins (last %s on %s)\n' "$warn" "$SMOKE_AT" "$SMOKE_HOST"
    fi
    ;;
up)
    elapsed=$(ps -o etime= -p 1 2>/dev/null | tr -d ' ')
    printf '%s\n' "$elapsed" | awk -F'[-:]' 'NF { n = NF; s = $n + 60 * $(n - 1); if (n >= 3) s += 3600 * $(n - 2); if (n == 4) s += 86400 * $1
        d = int(s / 86400); h = int(s % 86400 / 3600); m = int(s % 3600 / 60)
        out = (d ? d "d " : "") (h ? h "h " : "") m "m"; print (out == "0m" ? "<1m" : out) }'
    ;;
workspace) printf '%s\n' "${ANCHORAGE_WORKSPACE:-${WORKSPACE:-$PWD}}" ;;
git)
    cd "${ANCHORAGE_WORKSPACE:-${WORKSPACE:-.}}" 2>/dev/null || exit 0
    branch=$(git symbolic-ref --short -q HEAD 2>/dev/null || git rev-parse --short HEAD 2>/dev/null) || exit 0
    dirty=$(git status --porcelain --ignore-submodules=dirty 2>/dev/null | wc -l | tr -d ' ')
    line=$branch
    [ "$dirty" != 0 ] && line="$line ✱$dirty"
    if counts=$(git rev-list --left-right --count '@{u}...HEAD' 2>/dev/null); then
        behind=${counts%%[[:space:]]*}
        ahead=${counts##*[[:space:]]}
        [ "$ahead" != 0 ] && line="$line ↑$ahead"
        [ "$behind" != 0 ] && line="$line ↓$behind"
    else
        line="$line · no upstream"
    fi
    printf '%s\n' "$line"
    ;;
commit)
    cd "${ANCHORAGE_WORKSPACE:-${WORKSPACE:-.}}" 2>/dev/null || exit 0
    subject=$(git log -1 --format='%h %s' 2>/dev/null) || exit 0
    if [ "${#subject}" -gt 64 ]; then
        subject="$(printf '%s' "$subject" | cut -c1-64 | sed 's/ [^ ]*$//')…"
    fi
    printf '%s\n' "$subject"
    ;;
agents|bundles|toolbox)
    root=${BDOCK_ROOT:-/opt/bdock}
    out=
    for pin in ${PINS:-}; do
        name=${pin%%=*}
        tag=${pin#*=}
        case "$1:$name" in
            agents:claude|agents:codex|agents:agy) ;;
            toolbox:toolbox) ;;
            bundles:toolbox|bundles:claude|bundles:codex|bundles:agy) continue ;;
            bundles:*) ;;
            *) continue ;;
        esac
        if [ "$1" = toolbox ]; then
            item="$tag"
        else
            item="$name $tag"
        fi
        if [ -d "$root" ]; then
            if [ ! -r "$root/$name/manifest.json" ]; then
                item="$item $err not mounted"
            elif [ "$name" != toolbox ]; then
                mounted=$(grep -m1 '"version"' "$root/$name/manifest.json" | sed 's/[^0-9]*\([0-9][0-9.]*\).*/\1/')
                [ "$mounted" = "${tag%%-*}" ] || item="$item $warn mounted ${mounted:-unknown}"
            fi
        fi
        out="${out:+$out · }$item"
    done
    printf '%s\n' "${out:-unknown}"
    ;;
auth)
    codex=${CODEX_HOME:-$HOME/.codex}
    mark() { if "$@" 2>/dev/null; then printf '%s' "$ok"; else printf '%s' "$err"; fi; }
    printf 'agy/gh %s · claude %s · codex %s\n' \
        "$(mark test -s "${XDG_CONFIG_HOME:-$HOME/.config}/gh/hosts.yml")" \
        "$(mark grep -qs oauthAccount "$HOME/.claude.json")" \
        "$(mark test -s "$codex/auth.json")"
    ;;
auth-hint)
    codex=${CODEX_HOME:-$HOME/.codex}
    hint=
    test -s "${XDG_CONFIG_HOME:-$HOME/.config}/gh/hosts.yml" || hint="${hint:+$hint · }gh auth login"
    grep -qs oauthAccount "$HOME/.claude.json" || hint="${hint:+$hint · }claude /login"
    test -s "$codex/auth.json" || hint="${hint:+$hint · }codex login"
    [ -n "$hint" ] && printf '%s\n' "$hint"
    ;;
langs-image) versions image 'go:go version' 'node:node --version' 'python3:python3 --version' 'perl:perl --version' 'gcc:gcc --version' 'ruby:ruby --version' 'java:java -version' 'rustc:rustc --version' 'php:php --version'; echo ;;
langs-bundle) versions bundle 'node:node --version' 'npm:npm --version' 'uv:uv --version' 'ruff:ruff --version' 'typst:typst --version' 'nu:nu --version' 'deno:deno --version' 'bun:bun --version' 'tofu:tofu version'; echo ;;
*) echo "usage: anchorage-info host|engine|hostname|role|smoke|up|workspace|git|commit|agents|bundles|toolbox|auth|auth-hint|langs-image|langs-bundle" >&2; exit 2 ;;
esac
`,
	".devcontainer/pins-hash.sh": `#!/bin/sh
# Print a short hash of the bundle pins (image reference and volume per bundle) in
# devcontainer.json, so smoke results can be matched to the pins they covered.
# Usage: pins-hash.sh [devcontainer.json]
set -eu
python3 - "${1:-$(dirname "$0")/devcontainer.json}" <<'PY'
import hashlib, json, sys
commands = json.load(open(sys.argv[1]))['initializeCommand']
pins = sorted((name, command[2], command[3]) for name, command in commands.items() if name != 'host')
print(hashlib.sha256(json.dumps(pins).encode()).hexdigest()[:12])
PY
`,
	".devcontainer/initialize.sh": `#!/bin/sh
set -eu
# Usage: initialize.sh IMAGE VOLUME [BUNDLE]. Fetch the immutable registry image,
# then initialize its read-only bundle volume before the consumer starts.
docker pull "$1"
exec sh "$(dirname "$0")/../bundledock/scripts/bundle-volume.sh" "$1" "${3:-toolbox}" "$2"
`,
	"bundledock/scripts/bundle-volume.sh": `#!/bin/sh
set -eu
bundle=${2:-toolbox}
case "$bundle" in toolbox|code-server|node|node-current|bun|codex|agy|claude|opentofu) ;; *) echo 'Invalid bundle name' >&2; exit 1 ;; esac
image=${1:-bundledock-$bundle:poc}
# Resolve the mutable tag once; both the volume and initializer use this ID.
id=$(docker image inspect --format '{{.Id}}' -- "$image")
digest=${id#sha256:}
case "$id" in sha256:*) ;; *) echo 'Invalid Docker image ID' >&2; exit 1 ;; esac
case "$digest" in ''|*[!0-9a-f]*) echo 'Invalid Docker image ID' >&2; exit 1 ;; esac
test "${#digest}" -eq 64 || { echo 'Invalid Docker image ID' >&2; exit 1; }
# An explicit name keeps pre-rename volumes selectable without copying data.
volume=${3:-"bundledock-$bundle-$digest"}
case "$volume" in
    ''|[!a-zA-Z0-9]*|*[!a-zA-Z0-9_.-]*) echo 'Invalid Docker volume name' >&2; exit 1 ;;
esac
docker volume create "$volume" >/dev/null
docker run --rm --network none --mount "type=volume,src=$volume,dst=/bundle,volume-nocopy" "$id"
printf 'Volume: %s\n' "$volume"
`,
}
