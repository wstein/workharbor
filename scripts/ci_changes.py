#!/usr/bin/env python3
"""Classify a pull request as docs-only so CI may run its fast subset; fail closed.

Only prose that no check reads or pins counts as docs. Anything else, and every
case that is unclear or errors, is not docs-only and runs the full checks.
"""
import argparse
import os
import re
import subprocess
import sys
from pathlib import Path

SHA = re.compile(r'[0-9a-f]{40}')
# Plain words only: no '..', spaces, newlines or backslashes can reach a prefix match.
MARKDOWN = re.compile(r'(?:[A-Za-z0-9_][A-Za-z0-9._-]*/)*[A-Za-z0-9_][A-Za-z0-9._-]*\.md')
REGULAR_MODES = {'000000', '100644'}  # absent or regular non-executable; never symlink, exec bit or submodule
# Prose that no test, script or manifest reads or hashes (checked 2026-10-23). Left out on purpose:
# README/AGENTS/WORKFLOW and .specify (read by checks), docs/releasing.md, docs/container-runtime.md,
# docs/development/ (read by tooling), specs 001-003 (hash and tree pinned), specs 005 (contract text read
# by tests), contracts/ and tests/fixtures/. check_repo.py still lints all Markdown in the fast subset.
PROSE_FILES = frozenset(f'docs/{name}.md' for name in ('product', 'architecture', 'index', 'milestones', 'quality', 'references'))
PROSE_DIRECTORIES = ('docs/adr/', 'docs/archive/', 'docs/design/', 'docs/exec-plans/', 'docs/papers/', 'docs/quality/',
                     'specs/004-speckit-symphony-automation/', 'specs/006-whr-cli/', 'specs/007-bundledock-shell-policy/',
                     'specs/008-workspace-sandbox/', 'specs/009-host-setup-wizard/')


def prose(path):
    return MARKDOWN.fullmatch(path) is not None and (path in PROSE_FILES or path.startswith(PROSE_DIRECTORIES))


def docs_only(entries):
    """entries: (old mode, new mode, path); an empty change is unsure, so not docs-only."""
    entries = list(entries)
    return bool(entries) and all(old in REGULAR_MODES and new in REGULAR_MODES and prose(path)
                                 for old, new, path in entries)


def changed_entries(root, base, head, env=None):
    # Merge-base diff: only the pull request's own change, even when its base moved on.
    out = subprocess.run(['git', 'diff', '--raw', '-z', '--no-renames', '--no-abbrev', '--no-ext-diff',
                          '--no-textconv', f'{base}...{head}', '--'],
                         cwd=root, env=env, check=True, capture_output=True).stdout
    tokens = out.split(b'\0')
    if tokens and tokens[-1] == b'':
        tokens.pop()
    if len(tokens) % 2:
        raise ValueError('unparsable diff')
    entries = []
    for meta, path in zip(tokens[0::2], tokens[1::2]):
        fields = meta.decode('ascii').split()
        if not fields[0].startswith(':') or len(fields) != 5:
            raise ValueError('unparsable diff')
        entries.append((fields[0][1:], fields[1], path.decode('utf-8')))
    return entries


def classify(root, event, base, head, env=None):
    try:
        if event != 'pull_request' or not SHA.fullmatch(base or '') or not SHA.fullmatch(head or '') or base == head:
            return False
        return docs_only(changed_entries(root, base, head, env))
    except (OSError, ValueError, UnicodeError, subprocess.SubprocessError):
        return False


def main(argv=None, env=None):
    env = os.environ if env is None else env
    try:
        parser = argparse.ArgumentParser(description=__doc__)
        parser.add_argument('--root', type=Path, default=Path.cwd())
        parser.add_argument('--event', default=env.get('GITHUB_EVENT_NAME', ''))
        parser.add_argument('--base', default='')
        parser.add_argument('--head', default='')
        args = parser.parse_args(argv)
        result = classify(args.root, args.event, args.base, args.head, dict(env))
    except SystemExit:
        result = False
    except Exception as error:  # Unsure means full checks, never a failed or skipped gate.
        print(f'Change detection failed: {error}', file=sys.stderr)
        result = False
    line = f'docs_only={"true" if result else "false"}'
    print(line)
    if env.get('GITHUB_OUTPUT'):
        with open(env['GITHUB_OUTPUT'], 'a', encoding='utf-8') as output:
            output.write(line + '\n')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
