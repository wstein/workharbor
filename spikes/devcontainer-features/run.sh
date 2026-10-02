#!/bin/sh
# Spike #108: can devcontainer features be built into an image on Apple Container?
#
# For each of two common features (node, python) and each base the console and the
# agent environments use (Fedora, Ubuntu LTS; both pinned by digest as in
# internal/baseimage), this
#   1. fetches the feature from ghcr.io as an OCI artifact, anonymously (an anonymous
#      pull token, no account, no login), and checks the blob against its digest,
#   2. reads which keys its devcontainer-feature.json sets beyond what workharbor's
#      safe subset (D38) allows,
#   3. builds `FROM <base>` + `COPY feature` + `RUN ./install.sh` with `container build`,
#      options as upper-case environment variables and _REMOTE_USER=root, as the
#      devcontainer CLI's generated Dockerfile does, and
#   4. runs the result and records the tool's version.
# Everything it creates is labelled workharbor.temp=true and named whtmp-*/whtmp/*.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d /tmp/whr-spike108.XXXXXX)
LAB="--label workharbor.temp=true --label workharbor.lane=wh/runtime --label workharbor.purpose=spike-108"
FEDORA=docker.io/library/fedora@sha256:43b29f65a41eb9c35e1cd5323e3bdf3b655c2357a9f4f1ff2f9c2798e5045d80
UBUNTU=docker.io/library/ubuntu@sha256:008173c23f95b170204355c12626cb5a965d779a7e1283b09e9cffbb1bf33ca3
say() { printf '%s\n' "$*"; }
cleanup() { for i in $(container image ls --format json 2>/dev/null | python3 -c 'import sys,json
for e in json.load(sys.stdin):
    n=e["configuration"]["name"]
    if n.startswith("whtmp/spike108"): print(n)'); do container image delete "$i" >/dev/null 2>&1; done; rm -rf "$W"; }
trap cleanup EXIT

say "# spike #108 on $(container --version 2>&1 | head -1), $(date -u +%Y-%m-%dT%H:%M:%SZ)"

fetch() { # name version -> $W/<name>/ (the extracted feature), prints the digests
  name=$1 ver=$2
  tok=$(curl -fsS "https://ghcr.io/token?scope=repository:devcontainers/features/$name:pull" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
  [ -n "$tok" ] || { say "fetch $name: no anonymous token"; return 1; }
  mf=$(curl -fsS -H "Authorization: Bearer $tok" -H "Accept: application/vnd.oci.image.manifest.v1+json" "https://ghcr.io/v2/devcontainers/features/$name/manifests/$ver") || return 1
  digest=$(printf '%s' "$mf" | python3 -c 'import sys,json; m=json.load(sys.stdin); print(m["layers"][0]["digest"])')
  curl -fsSL -H "Authorization: Bearer $tok" "https://ghcr.io/v2/devcontainers/features/$name/blobs/$digest" -o "$W/$name.tgz" || return 1
  got="sha256:$(shasum -a 256 "$W/$name.tgz" | cut -d' ' -f1)"
  [ "$got" = "$digest" ] || { say "fetch $name: blob digest $got is not $digest"; return 1; }
  mkdir -p "$W/$name" && tar -xf "$W/$name.tgz" -C "$W/$name" || return 1
  say "feature $name:$ver blob $digest ($(wc -c < "$W/$name.tgz" | tr -d ' ') bytes), files: $(ls "$W/$name" | tr '\n' ' ' | sed 's/ $//')"
}

keys() { # the keys of devcontainer-feature.json the safe subset (D38) would refuse or ignore
  python3 - "$W/$1/devcontainer-feature.json" <<'PY'
import json, sys
j = json.load(open(sys.argv[1]))
refused = [k for k in ("privileged", "capAdd", "securityOpt", "mounts", "init", "entrypoint") if k in j]
hooks = [k for k in j if k.endswith("Command")]
print("  declares: id=%s version=%s options=%s installsAfter=%s" % (j.get("id"), j.get("version"), ",".join(sorted(j.get("options", {}))), j.get("installsAfter", [])))
print("  keys outside the safe subset: refused=%s lifecycle=%s containerEnv=%s" % (refused or "none", hooks or "none", sorted(j.get("containerEnv", {})) or "none"))
PY
}

build() { # label base feature env... -> builds, runs the check command
  lbl=$1 base=$2 feat=$3 opts=$4 check=$5
  d="$W/build-$lbl"; mkdir -p "$d/feature"; cp -R "$W/$feat/." "$d/feature/"
  # The feature's containerEnv becomes ENV lines of the image, as the devcontainer
  # CLI's generated Dockerfile does (a feature puts its tool on PATH this way).
  envlines=$(python3 - "$W/$feat/devcontainer-feature.json" <<'PY'
import json, sys
for k, v in json.load(open(sys.argv[1])).get("containerEnv", {}).items():
    print("ENV %s=%s" % (k, json.dumps(v)))
PY
)
  cat > "$d/Dockerfile" <<DF
FROM $base
COPY feature/ /tmp/feature/
RUN cd /tmp/feature && chmod +x install.sh && $opts _REMOTE_USER=root _REMOTE_USER_HOME=/root _CONTAINER_USER=root _CONTAINER_USER_HOME=/root ./install.sh && rm -rf /tmp/feature
$envlines
DF
  say "  generated Dockerfile ENV lines: $(printf '%s' "$envlines" | tr '\n' ';')"
  tag="whtmp/spike108-$lbl:1"
  t0=$(date +%s)
  if container build --progress plain --tag "$tag" $LAB --file "$d/Dockerfile" -- "$d" > "$W/$lbl.log" 2>&1; then
    t1=$(date +%s)
    say "build $lbl: ok in $((t1 - t0)) s"
    say "  run: $(container run --rm $LAB --name "whtmp-spike108-$lbl" "$tag" sh -c "$check" 2>&1 | tail -3 | tr '\n' ' ')"
  else
    t1=$(date +%s)
    say "build $lbl: FAILED after $((t1 - t0)) s; last lines of the build log:"
    tail -6 "$W/$lbl.log" | sed 's/^/    /'
  fi
}

for f in "node 1" "python 1"; do
  set -- $f
  fetch "$1" "$2" || { say "could not fetch $1"; continue; }
  keys "$1"
done
build node-ubuntu "$UBUNTU" node "VERSION=lts" 'node --version && npm --version'
build node-fedora "$FEDORA" node "VERSION=lts" 'node --version && npm --version'
build python-ubuntu "$UBUNTU" python "VERSION=os-provided" 'python3 --version'
build python-fedora "$FEDORA" python "VERSION=os-provided" 'python3 --version'
