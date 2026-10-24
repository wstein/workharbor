# SPDX-License-Identifier: EUPL-1.2
"""Small argv-only Herdr boundary. No installation, state files or credentials."""
import os
import re
import subprocess

PLUGIN_ID = 'workharbor.local'


def identifier(value):
    if not isinstance(value, str) or re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9:_-]{0,127}', value) is None:
        raise ValueError('missing or invalid Herdr context; invoke from a host pane')
    return value


def background():
    value = os.environ.get('WHR_PANE_BACKGROUND', '0')
    if value not in ('0', '1'):
        raise ValueError('invalid background option')
    return value == '1'


def herdr(arguments):
    subprocess.run([os.environ.get('HERDR_BIN_PATH', 'herdr'), *arguments], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=5)


def open_pane(entrypoint, placement, values, target=None):
    args = ['plugin', 'pane', 'open', '--plugin', PLUGIN_ID, '--entrypoint', entrypoint,
            '--placement', placement, '--focus']
    # Herdr 0.9.3 validates these as mutually exclusive placement contracts.
    if placement == 'tab':
        workspace = os.environ.get('HERDR_WORKSPACE_ID')
        if workspace:
            args += ['--workspace', identifier(workspace)]
    elif placement == 'split':
        args += ['--target-pane', identifier(target)]
    elif placement != 'overlay':
        raise ValueError('invalid pane placement')
    for key, value in values:
        args += ['--env', key + '=' + value]
    herdr(args)
