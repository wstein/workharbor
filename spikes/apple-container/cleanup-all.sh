#!/usr/bin/env bash
# Remove every whspike-* container, volume and network. Touches nothing else.
. "$(dirname "$0")/lib.sh"
cleanup
container ls -a | grep -c "${PREFIX}-" | sed 's/^/whspike containers left: /'
