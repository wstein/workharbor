#!/bin/sh
set -eu
container list --all --format json | jq '[.[] | {id, state: .status.state, image: .configuration.image.descriptor.digest, labels: .configuration.labels, networks: [.configuration.networks[].network], mounts: [.configuration.mounts[] | {destination, type: (.type | keys), options}]}]'
container network inspect whtmp-mcp-net | jq '[.[] | {id, mode: .configuration.mode}]'
container exec whtmp-mcp-ag1 /bin/sh -c 'command -v codex || true; for candidate in /tools/codex /usr/local/bin/codex /usr/bin/codex; do if test -f "$candidate"; then printf "%s present\n" "$candidate"; else printf "%s absent\n" "$candidate"; fi; done'
container exec whtmp-mcp-ag1 /bin/ls -1 /tools
container list --format json | jq '[.[] | select(.id == "whtmp-mcp-p1") | {id, executable: .configuration.initProcess.executable, arguments: .configuration.initProcess.arguments}]'
