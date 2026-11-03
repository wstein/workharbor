#!/bin/bash
T=$CLAUDE_JOB_DIR/tmp; . $T/lin/img.env
L=dev.workharbor.spike=319
for v in whr-mise-test-a whr-mise-test-b whr-mise-test-c; do docker volume rm $v >/dev/null 2>&1; done
docker volume create --label $L --label whr.mise.version=2026.10.25 --label whr.mise.platform=linux-arm64 --label whr.mise.lock=sha256:aaa whr-mise-test-a >/dev/null
docker volume create --label $L whr-mise-test-b >/dev/null
docker volume create whr-mise-test-c >/dev/null   # unlabeled, must never be pruned by label filter
# running user of a (ro)
docker run -d --name whr-mise-user --rm -v whr-mise-test-a:/mise/data:ro $IMG sleep 30 >/dev/null
echo "[1] rm of in-use volume:"; docker volume rm whr-mise-test-a 2>&1 | head -2
echo "[2] prune with label filter (a in use, b unused, c unlabeled):"; docker volume prune -f --filter label=$L 2>&1
echo "    left:"; docker volume ls -q | grep whr-mise-tes
echo "[3] stopped container referencing volume still protects it"; docker create --name whr-mise-stopped -v whr-mise-test-b:/mise/data:ro $IMG true >/dev/null 2>&1 || { docker volume create --label $L whr-mise-test-b >/dev/null; docker create --name whr-mise-stopped -v whr-mise-test-b:/mise/data:ro $IMG true >/dev/null; }
docker volume prune -f --filter label=$L 2>&1 | head -3; docker volume ls -q | grep whr-mise-tes
docker rm whr-mise-stopped >/dev/null; docker kill whr-mise-user >/dev/null; sleep 1
echo "[4] after users gone: prune (default filter only anonymous? use --all)"; docker volume prune -f --filter label=$L 2>&1 | head -3; docker volume prune -a -f --filter label=$L 2>&1 | head -4; docker volume ls -q | grep whr-mise-tes
docker volume rm whr-mise-test-c >/dev/null 2>&1; docker volume ls -q | grep -E "whr-mise-test"
