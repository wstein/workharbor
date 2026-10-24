# SPDX-License-Identifier: EUPL-1.2
"""Owner-invoked plugin actions; no startup or event hooks."""
import argparse
import os
import subprocess
import sys
from common import identifier, open_pane


def open_action(kind, placement, tinted):
    if kind not in ('picker', 'host') or placement not in ('tab', 'split'):
        raise ValueError('invalid action')
    target = identifier(os.environ.get('HERDR_PANE_ID'))
    values = [('WHR_PANE_BACKGROUND', '1' if tinted else '0')]
    if kind == 'picker':
        values = [('WHR_PICKER_PLACEMENT', placement), ('WHR_PANE_TARGET', target), *values]
    open_pane(kind, 'overlay' if kind == 'picker' else placement, values, target)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('kind', choices=['picker', 'host'])
    parser.add_argument('--placement', choices=['tab', 'split'], default='tab')
    parser.add_argument('--background', action='store_true')
    args = parser.parse_args()
    try:
        open_action(args.kind, args.placement, args.background)
    except (ValueError, OSError, subprocess.SubprocessError):
        print('workharbor plugin: cannot open pane; invoke from a host pane', file=sys.stderr)
        return 68
    return 0


if __name__ == '__main__':
    sys.exit(main())
