# SPDX-License-Identifier: EUPL-1.2
"""Overlay picker using public read-only whr discovery, with no Docker discovery."""
import curses
import json
import os
import subprocess
import sys
from common import background, identifier, open_pane
from identity import valid_name


def names_from_result(document):
    if (not isinstance(document, dict) or type(document.get('schema_version')) is not int
            or document['schema_version'] != 1 or document.get('command') != 'workspace.ls'
            or document.get('ok') is not True or type(document.get('exit_code')) is not int
            or document['exit_code'] != 0 or not isinstance(document.get('result'), dict)
            or not isinstance(document['result'].get('workspaces'), list)):
        raise ValueError('invalid local discovery result')
    # FR-018/OQ-12: no other record field is consumed or displayed.
    names = set()
    for record in document['result']['workspaces']:
        if isinstance(record, dict):
            name, state = record.get('name'), record.get('state')
            if state == 'running' and valid_name(name):
                names.add(name)
    return sorted(names)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate discovery key')
        result[key] = value
    return result


def discover():
    result = subprocess.run(['whr', 'ls', '--local', '--json'], check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=10)
    return names_from_result(json.loads(result.stdout, object_pairs_hook=unique_object))


def choose(names):
    def screen(window):
        window.keypad(True)
        selected = 0
        while True:
            window.erase()
            height, width = window.getmaxyx()
            if height < 1 or width < 2:
                return None
            start = max(0, selected - height + 1)
            for row, name in enumerate(names[start:start + height]):
                window.addnstr(row, 0, name, width - 1,
                               curses.A_REVERSE if start + row == selected else curses.A_NORMAL)
            window.refresh()
            key = window.getch()
            if key in (27, ord('q'), 3):
                return None
            if key in (10, 13, curses.KEY_ENTER):
                return names[selected]
            if key in (curses.KEY_DOWN, ord('j')):
                selected = (selected + 1) % len(names)
            elif key in (curses.KEY_UP, ord('k')):
                selected = (selected - 1) % len(names)
    return curses.wrapper(screen)


def run():
    placement = os.environ.get('WHR_PICKER_PLACEMENT', 'tab')
    if placement not in ('tab', 'split'):
        raise ValueError('invalid picker placement')
    target = os.environ.get('WHR_PANE_TARGET')
    if target:
        identifier(target)
    if placement == 'split' and not target:
        raise ValueError('split picker requires its original host pane')
    tinted = background()
    names = discover()
    if not names:
        print('workharbor plugin: no eligible running local workspaces', file=sys.stderr)
        return
    name = choose(names)
    if name is None:
        return
    if not valid_name(name) or name not in names:
        raise ValueError('invalid selection')
    open_pane('workspace', placement, [('WHR_PANE_WORKSPACE', name),
              ('WHR_PANE_BACKGROUND', '1' if tinted else '0')], target)


def main():
    try:
        run()
    except KeyboardInterrupt:
        return 0
    except (ValueError, OSError, subprocess.SubprocessError, curses.error, RecursionError):
        print('workharbor plugin: picker failed; check whr and the host session', file=sys.stderr)
        return 68
    return 0


if __name__ == '__main__':
    sys.exit(main())
