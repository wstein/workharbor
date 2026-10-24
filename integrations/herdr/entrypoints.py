# SPDX-License-Identifier: EUPL-1.2
"""Set cosmetic identity, optionally tint this pane, then replace with its shell."""
import argparse
import os
import subprocess
import sys
from common import background, herdr, identifier
from identity import identity, HOST_TITLE, HOST_TINT


def launch(kind):
    if kind == 'workspace':
        name = os.environ.get('WHR_PANE_WORKSPACE')
        title, tint = identity(name)
        argv = ['whr', 'shell', '--local', name]
    elif kind == 'host':
        title, tint = HOST_TITLE, HOST_TINT
        shell = os.environ.get('SHELL') or '/bin/sh'
        if not os.path.isabs(shell) or '\n' in shell or '\x00' in shell:
            raise ValueError('SHELL must name an absolute host shell executable')
        argv = [shell, '-l']
    else:
        raise ValueError('invalid pane kind')
    pane = identifier(os.environ.get('HERDR_PANE_ID'))
    tab = identifier(os.environ.get('HERDR_TAB_ID'))
    tinted = background()
    herdr(['pane', 'rename', pane, title])
    herdr(['tab', 'rename', tab, title])
    if tinted:
        sys.stdout.write('\x1b]11;' + tint + '\x1b\\')
        sys.stdout.flush()
    os.execvp(argv[0], argv)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('kind', choices=['workspace', 'host'])
    args = parser.parse_args()
    try:
        launch(args.kind)
    except (ValueError, OSError, subprocess.SubprocessError):
        print('workharbor plugin: pane setup failed; check the host context and tools', file=sys.stderr)
        return 68
    return 0


if __name__ == '__main__':
    sys.exit(main())
