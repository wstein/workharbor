#!/usr/bin/env python3
"""Check pinned remote revisions in disposable storage; never publish."""
import sys

if __name__ == '__main__' and not sys.flags.isolated:
    print('Prepublication blocked: launch the trusted tool with python -I -B.', file=sys.stderr)
    raise SystemExit(3)

import argparse
import json
import math
import os
import re
import signal
import subprocess
import tempfile
import time
from pathlib import Path
from urllib.parse import urlsplit

OID = re.compile(r'[0-9a-f]{40}\Z')


class Blocked(Exception):
    pass


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Blocked('invalid_arguments')


class Commands:
    """Bound subprocess lifetime and discard potentially sensitive diagnostics."""
    def __init__(self, temporary, timeout):
        self.deadline = time.monotonic() + timeout
        self.env = dict(PATH=os.environ.get('PATH', os.defpath),
                        HOME=str(temporary), TMPDIR=str(temporary),
                        LC_ALL='C', PYTHONDONTWRITEBYTECODE='1',
                        GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_TERMINAL_PROMPT='0', GIT_ASKPASS='/usr/bin/false',
                        GIT_ALLOW_PROTOCOL='file:https',
                        GIT_AUTHOR_NAME='Prepublication',
                        GIT_AUTHOR_EMAIL='prepublication@invalid',
                        GIT_COMMITTER_NAME='Prepublication',
                        GIT_COMMITTER_EMAIL='prepublication@invalid',
                        GIT_AUTHOR_DATE='2000-01-01T00:00:00Z',
                        GIT_COMMITTER_DATE='2000-01-01T00:00:00Z')

    def run(self, argv, cwd, failure, *, checked=True):
        if time.monotonic() >= self.deadline:
            raise Blocked('timeout')
        with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
            try:
                process = subprocess.Popen(argv, cwd=cwd, env=self.env,
                                           stdin=subprocess.DEVNULL, stdout=out,
                                           stderr=err, start_new_session=True)
            except OSError:
                raise Blocked('tool_unavailable') from None
            try:
                process.wait(timeout=max(0.001, self.deadline - time.monotonic()))
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
                raise Blocked('timeout') from None
            if checked and process.returncode:
                raise Blocked(failure)
            out.seek(0)
            output = out.read(1024 * 1024 + 1)
            if len(output) > 1024 * 1024:
                raise Blocked('output_limit')
            return process.returncode, output

    def git(self, cwd, *argv):
        return self.run(['git', *argv], cwd, 'git_failed')[1].decode('ascii')


def repository_location(value):
    if not isinstance(value, str) or any(ord(c) < 32 for c in value):
        raise Blocked('invalid_repository')
    url = urlsplit(value)
    if url.scheme:
        if (url.scheme != 'https' or not url.hostname or url.username is not None
                or url.password is not None or url.query or url.fragment):
            raise Blocked('invalid_repository')
        return value
    path = Path(value)
    if not path.is_absolute() or path.is_symlink() or not path.is_dir():
        raise Blocked('invalid_repository')
    return str(path.resolve(strict=True))


def remote_pins(commands, root, repository, refs):
    output = commands.git(root, 'ls-remote', '--refs', '--exit-code', '--',
                          repository, *refs)
    pins = {}
    for line in output.splitlines():
        fields = line.split('\t')
        if (len(fields) != 2 or not OID.fullmatch(fields[0])
                or fields[1] not in refs or fields[1] in pins):
            raise Blocked('invalid_remote_refs')
        pins[fields[1]] = fields[0]
    if set(pins) != set(refs):
        raise Blocked('missing_remote_ref')
    return pins


def prepare(repository, source, base_ref, base, head_ref, head, timeout=300):
    result = dict(schema_version=1, status='BLOCKED', source=None, base=None,
                  head=None, checkout=None, tree=None, check_exit=None, blocker=None)
    try:
        if (not all(isinstance(pin, str) and OID.fullmatch(pin)
                    for pin in (source, base, head)) or base == head
                or not isinstance(timeout, (int, float)) or isinstance(timeout, bool)
                or not math.isfinite(timeout) or not 0 < timeout <= 3600):
            raise Blocked('invalid_arguments')
        repository = repository_location(repository)
        refs = (base_ref, head_ref)
        if (base_ref == head_ref or any(not isinstance(ref, str)
                or not re.fullmatch(r'refs/heads/[A-Za-z0-9._/-]+', ref) for ref in refs)):
            raise Blocked('invalid_refs')
        result.update(source=source, base=base, head=head)
        with tempfile.TemporaryDirectory(prefix='workharbor-prepublication-') as directory:
            temporary = Path(directory).resolve()
            commands = Commands(temporary, timeout)
            for ref in refs:
                commands.git(temporary, 'check-ref-format', ref)
            expected = {base_ref: base, head_ref: head}
            if remote_pins(commands, temporary, repository, refs) != expected:
                raise Blocked('remote_moved_before_check')
            integration, trusted = temporary/'integration', temporary/'trusted'
            commands.git(temporary, 'init', '--quiet', '--template=', str(integration))
            commands.git(integration, 'fetch', '--quiet', '--no-tags', '--', repository,
                         f'{base_ref}:refs/pins/base', f'{head_ref}:refs/pins/head', source)
            if (commands.git(integration, 'rev-parse', 'refs/pins/base').strip() != base
                    or commands.git(integration, 'rev-parse', 'refs/pins/head').strip() != head):
                raise Blocked('remote_moved_during_fetch')
            commands.git(integration, 'cat-file', '-e', source + '^{commit}')
            entry = commands.git(integration, 'ls-tree', source, '--',
                                 'scripts/check_ci.py').split()
            if len(entry) != 4 or entry[0] not in ('100644', '100755') or entry[1] != 'blob':
                raise Blocked('checker_unavailable')
            tree = commands.git(integration, 'merge-tree', '--write-tree', base, head).strip()
            if not OID.fullmatch(tree):
                raise Blocked('merge_unavailable')
            checkout = commands.git(integration, 'commit-tree', tree, '-p', base,
                                    '-p', head, '-m', 'Prepublication synthetic merge').strip()
            commands.git(integration, 'checkout', '--quiet', '--detach', checkout)
            commands.git(integration, 'worktree', 'add', '--quiet', '--detach',
                         str(trusted), source)
            checker = trusted/'scripts/check_ci.py'
            if checker.is_symlink() or not checker.is_file():
                raise Blocked('checker_unavailable')
            result.update(checkout=checkout, tree=tree)
            code, _ = commands.run([sys.executable, '-I', '-B', str(checker), '--root', str(integration),
                                    '--event', 'pull_request', '--base', base,
                                    '--head', head, '--checkout', checkout,
                                    '--policy-source', source],
                                   integration, 'check_failed', checked=False)
            result['check_exit'] = code
            # Observe both refs even when checks fail; a race never becomes a pass.
            if remote_pins(commands, temporary, repository, refs) != expected:
                raise Blocked('remote_moved_after_check')
            if code:
                raise Blocked('check_failed')
            result['status'] = 'PREPARED'
    except Blocked as exc:
        result['blocker'] = str(exc)
    except (OSError, ValueError, UnicodeError):
        result['blocker'] = 'interface_unavailable'
    return result


def main(argv=None):
    parser = Parser(description=__doc__)
    for name in ('repository', 'source', 'base-ref', 'base', 'head-ref', 'head'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--timeout', type=float, default=300)
    try:
        result = prepare(**vars(parser.parse_args(argv)))
    except Blocked:
        result = dict(schema_version=1, status='BLOCKED', blocker='invalid_arguments')
    print(json.dumps(result, sort_keys=True))
    return 0 if result['status'] == 'PREPARED' else 3


if __name__ == '__main__':
    raise SystemExit(main())
