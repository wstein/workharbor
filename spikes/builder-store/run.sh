#!/bin/sh
# Spike (issue #133): can the BUILDER see the host's local image store?
#
# `container build` runs BuildKit in a builder container. If it resolves a FROM
# against the host's image store, a repository's Dockerfile could build FROM an image
# the supervisor built for another repository (a `whr.invalid/` tag, D38). This
#   1. builds image X (alpine + /marker) and tags it whr.invalid/whtmp/x:1 and whtmp/x:1,
#      so the host store holds both and neither was ever pushed anywhere,
#   2. builds `FROM <tag>` + `RUN cat /marker` in a fresh context, without --pull and
#      with --pull, for each of the two names, recording status, time and the error.
# Everything it creates is labelled workharbor.temp=true and tagged whtmp/* or
# whr.invalid/whtmp/*; the trap deletes the tags.
set -u
W=$(mktemp -d /tmp/whr-spike-bs.XXXXXX)
LAB="--label workharbor.temp=true --label workharbor.lane=wh/runtime --label workharbor.purpose=builder-store"
ALPINE=docker.io/library/alpine:latest
HAD_ALPINE=$(container image ls 2>/dev/null | awk '$1=="alpine" && $2=="latest"' | wc -l | tr -d ' ')
say() { printf '%s\n' "$*"; }
cleanup() {
  for i in whr.invalid/whtmp/x:1 whtmp/x:1 whtmp/y:1; do container image delete "$i" >/dev/null 2>&1; done
  [ "$HAD_ALPINE" = 0 ] && container image delete "$ALPINE" >/dev/null 2>&1
  rm -rf "$W"
}
trap cleanup EXIT

say "# spike builder-store on $(container --version 2>&1 | head -1), $(date -u +%Y-%m-%dT%H:%M:%SZ)"
container image pull "$ALPINE" >/dev/null 2>&1 || say "note: could not pull $ALPINE (using the local copy if any)"

mkdir -p "$W/x"
printf 'FROM %s\nRUN echo marker-X > /marker\n' "$ALPINE" > "$W/x/Dockerfile"
if container build --progress plain --tag whr.invalid/whtmp/x:1 --tag whtmp/x:1 $LAB --file "$W/x/Dockerfile" -- "$W/x" > "$W/x.log" 2>&1; then
  say "build X: ok"
else
  say "build X: FAILED"; tail -6 "$W/x.log" | sed 's/^/    /'; exit 1
fi
say "host store: $(container image ls | awk '$1 ~ /whtmp/ {print $1 ":" $2}' | tr '\n' ' ')"

try() { # label base flag
  lbl=$1 base=$2 flag=$3
  d="$W/c-$lbl"; mkdir -p "$d"
  printf 'FROM %s\nRUN cat /marker\n' "$base" > "$d/Dockerfile"
  t0=$(date +%s)
  # shellcheck disable=SC2086
  container build --progress plain --tag whtmp/y:1 $LAB $flag --file "$d/Dockerfile" -- "$d" > "$W/$lbl.log" 2>&1
  rc=$?
  t1=$(date +%s)
  if [ $rc = 0 ]; then
    say "FROM $base flag='$flag': exit 0 in $((t1 - t0)) s; marker printed: $(grep -c marker-X "$W/$lbl.log") line(s); local image used"
  else
    say "FROM $base flag='$flag': exit $rc in $((t1 - t0)) s; error:"
    grep -iE 'error|fail|not found|denied|resolve|unauthorized|lookup' "$W/$lbl.log" | head -4 | sed 's/^/    /'
  fi
  container image delete whtmp/y:1 >/dev/null 2>&1
}

try inv-nopull whr.invalid/whtmp/x:1 ""
try inv-pull whr.invalid/whtmp/x:1 "--pull"
try loc-nopull whtmp/x:1 ""
try loc-pull whtmp/x:1 "--pull"

# COPY --from and RUN --mount from=, the other two ways a Dockerfile can name an image.
try2() { # label dockerfile-text flag
  lbl=$1 text=$2 flag=$3
  d="$W/c-$lbl"; mkdir -p "$d"
  printf '%s\n' "$text" > "$d/Dockerfile"
  t0=$(date +%s)
  # shellcheck disable=SC2086
  container build --progress plain --tag whtmp/y:1 $LAB $flag --file "$d/Dockerfile" -- "$d" > "$W/$lbl.log" 2>&1
  rc=$?
  t1=$(date +%s)
  if [ $rc = 0 ]; then
    say "$lbl flag='$flag': exit 0 in $((t1 - t0)) s; marker-X lines in log: $(grep -c marker-X "$W/$lbl.log"); local image used"
  else
    say "$lbl flag='$flag': exit $rc in $((t1 - t0)) s; error:"
    grep -iE 'error|fail|not found|denied|resolve|unauthorized|lookup' "$W/$lbl.log" | head -4 | sed 's/^/    /'
  fi
  container image delete whtmp/y:1 >/dev/null 2>&1
}

COPYDF='FROM scratch
COPY --from=whr.invalid/whtmp/x:1 /marker /marker'
MOUNTDF="FROM $ALPINE
RUN --mount=type=bind,from=whr.invalid/whtmp/x:1,target=/m cat /m/marker"
try2 copy-from-nopull "$COPYDF" ""
try2 copy-from-pull "$COPYDF" "--pull"
try2 mount-from-nopull "$MOUNTDF" ""
try2 mount-from-pull "$MOUNTDF" "--pull"
