#!/usr/bin/env python3
"""Sealed T003 acceptance and T004 preparation; other trees require green."""
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from contextlib import contextmanager
from pathlib import Path
from typing import NamedTuple
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SEALED_T003 = '586696f7cc1956d430647b918869fbf8caedd660'
ACCEPTED_T003 = 'a36bf7e1fc9b09bd07226078b7d70c5d299d9324'
T002_MERGE = '029a4ec83e1240b40f72eed004f513ff8fb8b5b0'
T002_BASELINE = '74ec3d91cc313aeca4e73919c7fc271ec7f4c0c5'
REVIEWED_DOCS = 'c329e4c71e246eb286bcd49c6b3689c93317f313'
DOC_PINS = {
    'AGENTS.md': 'd86eeec03ae97f35714b9b30cca4629e5fa229c5270b8aacd374ea98efa98b6d',
    'WORKFLOW.md': '7781d2cf5b3d2ed932a88e60bc77647e60bbf026a314ae2e657e755c117aec32',
}
CI_PATHS = {'.github/workflows/check.yml', 'scripts/check_ci.py',
            'docs/development/harness.md'}
PINS = {
    'docs/development/bootstrap-t003.json': 'b5cc969a053f461ccd2c0bcda68dac075d036adad6cebfc1e7c4b7689ee91ba6',
    'docs/development/bootstrap-t002.json': '597b2c2d31f90afd8118b0e593f9cf1b998ec1bfd7f21558e5cff45ab0596461',
    'scripts/verify_t003_state.py': '3bba2eeff6d6e1abe14ead36ce98c5f4e2a749fbb71f7d4c03012af9e6e78b0f',
    'scripts/verify_t002_red.py': 'c863cb1f16f5c0eda3c57631e24401612fd56abf49708dd294b389974a9cfffb',
    'scripts/single_task.py': '7c50b5170e31eed9c5c0dbeb047919c8845f6c266a32def55868a4ab9b863bc4',
    'specs/003-single-task-entry/tasks.md': 'c180e72fbb9e0fba51579629657cd24aa41808ddec79d8a58c191dd6d451c6ab',
}
T004_SOURCE = '5d1dc6563c7ae1f8479f01af0901c39792b3f8b8'
T004_PREPARATION = 'f6a69470f8a3e5c396e134326c033a3b1195de7b'
T004_REVIEWED_CI = '4826a9a36b3f7f8e74d46b2238cf7653e3947911'
T004_CI_PATHS = ('scripts/check_ci.py', 'docs/development/harness.md')
T004_PREP_PINS = {
    'docs/development/bootstrap-t004.json': ('3c655a98d91a9ddf160be9795c6843843a04269e1d09ff19103e2b3478dc712c', False),
    'docs/development/bootstrap-t004.md': ('f8404622ae94fcce8c18b5a2d9023e35c4241b313f4f938dd873b5538e8f1ded', False),
    'scripts/verify_t004_cli.py': ('39aaa54427eb12085b65513bcb64e8d1cc56f50e7dfb3e396c8bf8be9752b074', True),
}
T004_REJECTION_SHA256 = '60f9589d0fabb34d3b95847ea2521d6458a2a3d0a10ce8a091ffa30f8321d0e0'
DOCUMENTATION_BASE = '8f8211afdda9e46984ccfb95505f83434da06db7'
# Accepted CI authority, distinct from the unchanged behavioral baseline pins.
DOCUMENTATION_POLICY_SOURCE = '2d965c15587cdc4d9fc9a00083bd00d7788b8306'

# A bounded operator-tool category. Extending it requires a reviewed policy source.
OPERATOR_TOOLS = {
    'scripts/check_ci.py': 'scripts/test_check_ci.py',
    'scripts/prepare_publication.py': 'scripts/test_prepare_publication.py',
    'scripts/approval_binding.py': 'scripts/test_approval_binding.py',
}
OPERATOR_POLICY_PATHS = {'scripts/check_ci.py', '.github/workflows/check.yml', 'AGENTS.md', 'WORKFLOW.md'}
OPERATOR_CONTRACT_PATHS = {
    '.specify/memory/constitution.md',
    'docs/development/harness.md',
    'docs/development/prepublication.md',
    'docs/development/approval-binding.md',
    'specs/004-speckit-symphony-automation/bounded-source-contract.md',
    'specs/004-speckit-symphony-automation/contracts/cli.md',
    'specs/004-speckit-symphony-automation/data-model.md',
}
OPERATOR_PATHS = (set(OPERATOR_TOOLS) | set(OPERATOR_TOOLS.values())
                  | OPERATOR_POLICY_PATHS | OPERATOR_CONTRACT_PATHS)


class SourceContext(NamedTuple):
    event: str
    base: str
    head: str
    checkout: str


def clean_checkout(root, revision):
    """Compare bytes and modes explicitly, including assume-unchanged files."""
    for options in ((), ('--cached',)):
        git(root, 'diff', *options, '--quiet', revision, '--')
    if git(root, 'ls-files', '--others', '-z'):
        raise ValueError('CI execution requires a pristine isolated checkout, including ignored files; use prepublication scratch')
    algorithm = git(root, 'rev-parse', '--show-object-format').decode().strip()
    for entry in git(root, 'ls-files', '--stage', '-z').split(b'\0'):
        if not entry:
            continue
        metadata, name = entry.split(b'\t', 1)
        mode, blob, stage = metadata.split()
        path = root/os.fsdecode(name)
        if (stage != b'0' or mode not in (b'100644', b'100755')
                or path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1
                or bool(path.stat().st_mode & 0o111) != (mode == b'100755')
                or any(parent.is_symlink() for parent in path.parents if root in parent.parents)):
            raise ValueError('Unsafe checkout type or mode')
        content = path.read_bytes()
        actual = hashlib.new(algorithm, b'blob ' + str(len(content)).encode() + b'\0' + content).hexdigest()
        if actual.encode() != blob:
            raise ValueError('Checkout bytes differ from index')


def _source_context(root, event=None, base=None, head=None, checkout=None):
    """Validate proposal identity independently of its integration checkout.

    Explicit pins are caller-supplied trusted metadata, never approval evidence.
    GitHub metadata is additionally checked when present; no stale-event override.
    """
    root = Path(root)
    if root.is_symlink() or not root.is_dir():
        raise ValueError('Expected regular checkout root')
    actual = git(root, 'rev-parse', 'HEAD').decode().strip()
    provided = (event, base, head, checkout)
    if any(value is not None for value in provided) and not all(provided):
        raise ValueError('Source context requires all four pins')
    github_event = os.environ.get('GITHUB_EVENT_NAME')
    if github_event is not None:
        if github_event not in ('push', 'pull_request') or os.environ.get('GITHUB_SHA') != actual:
            raise ValueError('Stale or unsupported event')
        payload = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
        if github_event == 'pull_request':
            pr = payload['pull_request']
            if any(pr[side]['repo']['full_name'] != 'wstein/workharbor-nxt' for side in ('base', 'head')):
                raise ValueError('Unexpected event repository')
            observed = SourceContext('pull_request', pr['base']['sha'], pr['head']['sha'], actual)
        else:
            if payload['repository']['full_name'] != 'wstein/workharbor-nxt' or payload['after'] != actual:
                raise ValueError('Stale push event')
            observed = SourceContext('push', DOCUMENTATION_BASE, actual, actual)
        if all(provided) and SourceContext(*provided) != observed:
            raise ValueError('Explicit pins disagree with event')
        event, base, head, checkout = observed
    elif not any(provided):
        event, base, head, checkout = 'local', DOCUMENTATION_BASE, actual, actual
    if event not in ('pull_request', 'push', 'local') or checkout != actual:
        raise ValueError('Unexpected checkout identity')
    for revision in (base, head, checkout):
        if (not isinstance(revision, str) or not re.fullmatch('[0-9a-f]{40}', revision)
                or git(root, 'cat-file', '-t', revision) != b'commit\n'):
            raise ValueError('Missing exact commit object')
    if event == 'pull_request':
        parents = git(root, 'rev-list', '--parents', '-n', '1', checkout).decode().split()[1:]
        if base == head or parents != [base, head]:
            raise ValueError('Unexpected synthetic merge parents')
        # A valid merge includes independent base updates. Never require head-tree equality.
        merged = git(root, 'merge-tree', '--write-tree', base, head).decode().strip()
        tree = git(root, 'rev-parse', checkout + '^{tree}').decode().strip()
        if merged != tree:
            raise ValueError('Synthetic checkout differs from actual merge')
    elif checkout != head:
        raise ValueError('Non-PR checkout must be proposal head')
    clean_checkout(root, checkout)
    return SourceContext(event, base, head, checkout)


def source_context(root, event=None, base=None, head=None, checkout=None):
    try:
        return _source_context(root, event, base, head, checkout)
    except (OSError, subprocess.CalledProcessError, KeyError, TypeError, UnicodeError) as error:
        raise ValueError('Unavailable or invalid source evidence') from error


def documentation_path(name):
    path = Path(name)
    protected = set(PINS) | set(T004_PREP_PINS) | set(DOC_PINS) | OPERATOR_CONTRACT_PATHS | {
        'docs/development/bootstrap-t002.md', 'docs/development/bootstrap-t003.md'}
    return (name not in protected and not name.startswith((
        'specs/001-workspace-lifecycle/', 'specs/002-symphony-doc-smoke/',
        'specs/003-single-task-entry/')) and (name == 'README.md' or (
        path.parts[0] in ('docs', 'specs', 'contracts') and path.suffix in ('.md', '.json'))))


def linear_commits(root, baseline, revision):
    """Reject ambiguous policy transitions instead of guessing a merge ordering."""
    git(root, 'merge-base', '--is-ancestor', baseline, revision)
    commits = git(root, 'rev-list', '--reverse', baseline + '..' + revision).decode().splitlines()
    previous = baseline
    for commit in commits:
        parents = git(root, 'rev-list', '--parents', '-n', '1', commit).decode().split()[1:]
        if parents != [previous]:
            raise ValueError('Policy history must be linear')
        previous = commit
    return commits


def policy_manifest(root, revision):
    """Raw tracked bytes/types/modes and inventory, solely from Git objects."""
    entries = git(root, 'ls-tree', '-z', revision, '--', *sorted(OPERATOR_PATHS))
    inventory = tuple(entry for entry in git(root, 'ls-tree', '-r', '-z', revision, '--', 'scripts').split(b'\0')
                      if entry and re.fullmatch(rb'scripts/test_[^/]+\.py', entry.split(b'\t', 1)[1]))
    return entries, inventory


def history_scope(root, baseline, revision, allowed, operator=False, policy_states=None):
    """Inspect every changed version, so a revert cannot erase scope/type drift."""
    git(root, 'merge-base', '--is-ancestor', baseline, revision)
    commits = (linear_commits(root, baseline, revision) if policy_states is not None else
               git(root, 'rev-list', baseline + '..' + revision).decode().splitlines())
    state_index = 0
    for commit in commits:
        changed = git(root, 'diff-tree', '--root', '-m', '-r', '--no-commit-id',
                      '--no-renames', '--name-only', '-z', commit, '--')
        for raw in filter(None, changed.split(b'\0')):
            name = os.fsdecode(raw)
            if not allowed(name):
                return False
            entry = git(root, 'ls-tree', commit, '--', name)
            modes = (b'100644 blob ', b'100755 blob ') if operator and name in OPERATOR_TOOLS else (b'100644 blob ',)
            if entry and not entry.startswith(modes):
                return False
        if policy_states is not None:
            # Only reviewed forward transitions may be collapsed by a squash.
            # Reverts, mixed states and invented intermediate content fail closed.
            try:
                state_index = policy_states.index(policy_manifest(root, commit), state_index)
            except ValueError:
                return False
    return True


def protected_baseline(root, revisions):
    # Content, Git type and mode are immutable at head, updated base and integration.
    pins = dict(PINS, **{name: value[0] for name, value in T004_PREP_PINS.items()})
    for name, digest in pins.items():
        baseline = git(root, 'ls-tree', DOCUMENTATION_BASE, '--', name)
        if not baseline:
            return False
        for revision in revisions:
            if git(root, 'ls-tree', revision, '--', name) != baseline:
                return False
        if hashlib.sha256((root/name).read_bytes()).hexdigest() != digest:
            return False
    return True


def approved_documentation(root, context=None):
    """Validate PR delta and independently revalidate changed-base prerequisites."""
    try:
        context = source_context(root, *context) if context else source_context(root)
        base = context.base if context.event == 'pull_request' else DOCUMENTATION_POLICY_SOURCE
        fork = git(root, 'merge-base', '--all', base, context.head).decode().splitlines()
        if len(fork) != 1:
            return False
        for revision in (base, fork[0], context.head):
            git(root, 'merge-base', '--is-ancestor', DOCUMENTATION_POLICY_SOURCE, revision)
        # Trust only accepted policy ancestry; head scope excludes independent base changes.
        return (history_scope(root, DOCUMENTATION_POLICY_SOURCE, base, documentation_path)
                and history_scope(root, DOCUMENTATION_POLICY_SOURCE, fork[0], documentation_path)
                and history_scope(root, fork[0], context.head, documentation_path)
                and protected_baseline(root, (base, context.head, context.checkout)))
    except (OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError):
        return False


def approved_operator_tooling(root, context, policy_source):
    """Local validation only, gated by externally adopted reviewed policy source.

    A supplied immutable pin is host trust input. This function cannot authenticate
    the review/adoption action, and never grants task execution or publication.
    """
    try:
        if (not isinstance(policy_source, str) or not re.fullmatch('[0-9a-f]{40}', policy_source)
                or git(root, 'cat-file', '-t', policy_source) != b'commit\n'):
            return False
        policy_file = Path(__file__)
        entry = git(root, 'ls-tree', policy_source, '--', 'scripts/check_ci.py')
        if (policy_file.is_symlink() or not policy_file.is_file()
                or not entry.startswith(b'100755 blob ' if policy_file.stat().st_mode & 0o111 else b'100644 blob ')
                or git(root, 'show', policy_source + ':scripts/check_ci.py') != policy_file.read_bytes()):
            return False
        context = source_context(root, *context) if context else source_context(root)
        base = context.base if context.event == 'pull_request' else DOCUMENTATION_POLICY_SOURCE
        allowed = lambda name: name in OPERATOR_PATHS or documentation_path(name)
        # The external source authenticates content and ordered reviewed states;
        # candidate commit identities never authenticate or refresh that approval.
        states = [policy_manifest(root, DOCUMENTATION_POLICY_SOURCE)]
        for commit in linear_commits(root, DOCUMENTATION_POLICY_SOURCE, policy_source):
            state = policy_manifest(root, commit)
            if state != states[-1]:
                states.append(state)
        fork = git(root, 'merge-base', '--all', base, context.head).decode().splitlines()
        if (len(fork) != 1
                or not history_scope(root, DOCUMENTATION_POLICY_SOURCE, policy_source, allowed, operator=True)
                or not history_scope(root, DOCUMENTATION_POLICY_SOURCE, base, allowed, operator=True, policy_states=states)
                or not history_scope(root, DOCUMENTATION_POLICY_SOURCE, context.head, allowed, operator=True, policy_states=states)
                or not protected_baseline(root, (policy_source, base, context.head, context.checkout))):
            return False
        if any(policy_manifest(root, revision) != states[-1]
               for revision in (context.head, context.checkout)):
            return False
        tests = {path.relative_to(root).as_posix() for path in root.glob('scripts/test_*.py')}
        required = {test for tool, test in OPERATOR_TOOLS.items() if (root/tool).is_file()}
        return tests == required and bool(tests)
    except (OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError):
        return False


@contextmanager
def isolated_python_cache():
    """Keep cache reads and generated files outside the candidate checkout."""
    with tempfile.TemporaryDirectory(prefix='workharbor-ci-cache-') as cache:
        names = {'PYTHONPYCACHEPREFIX': cache, 'PYTHONDONTWRITEBYTECODE': '1'}
        previous = {name: os.environ.get(name) for name in names}
        prefix, no_write = sys.pycache_prefix, sys.dont_write_bytecode
        os.environ.update(names)
        sys.pycache_prefix, sys.dont_write_bytecode = cache, True
        try:
            yield
        finally:
            sys.pycache_prefix, sys.dont_write_bytecode = prefix, no_write
            for name, value in previous.items():
                if value is None:
                    os.environ.pop(name, None)
                else:
                    os.environ[name] = value


def operator_suite(root):
    # Run all bounded new tests in a fresh interpreter, with a deadline and strict
    # unittest outcomes. The parent has already checked the complete inventory.
    modules = [Path(test).stem for tool, test in OPERATOR_TOOLS.items() if (root/tool).is_file()]
    code = ("import runpy,sys; m=runpy.run_path(sys.argv[1]); "
            "raise SystemExit(0 if m['strict_suite'](m['Path'](sys.argv[2]), "
            "'scripts', 'test_*.py', sys.argv[3:]) else 1)")
    try:
        clean_checkout(root, git(root, 'rev-parse', 'HEAD').decode().strip())
        with isolated_python_cache():
            result = subprocess.run([sys.executable, '-B', '-c', code, str(Path(__file__).resolve()),
                                     str(root.resolve()), *modules], cwd=root, timeout=300)
        return result.returncode == 0
    except (OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired):
        return False


def approved_t004_preparation(root):
    """Recognize only one CI commit above the immutable three-artifact seal.

    This is a content boundary, never self-authentication of this checker. The
    final CI commit still needs independent review. Any later commit expires it.
    """
    try:
        git(root, 'merge-base', '--is-ancestor', T004_SOURCE, T004_PREPARATION)
        head = git(root, 'rev-parse', 'HEAD').decode().strip()
        candidate = head
        parents = git(root, 'rev-list', '--parents', '-n', '1', 'HEAD').decode().split()[1:]
        if len(parents) == 2:
            # Actions checks out refs/pull/N/merge. Accept that wrapper only
            # when its trusted event and parent/tree identities agree exactly.
            if (os.environ.get('GITHUB_EVENT_NAME') != 'pull_request'
                    or os.environ.get('GITHUB_SHA') != head or parents[0] != T004_SOURCE):
                return False
            event = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())['pull_request']
            if (event['base']['sha'] != T004_SOURCE or event['head']['sha'] != parents[1]
                    or event['base']['ref'] != 'chore/issue-2-bootstrap-entry'
                    or event['head']['ref'] != 'chore/5-t004-preparation'
                    or event['base']['repo']['full_name'] != 'wstein/workharbor-nxt'
                    or event['head']['repo']['full_name'] != 'wstein/workharbor-nxt'):
                return False
            candidate = parents[1]
            git(root, 'diff', '--quiet', candidate, head, '--')
        elif len(parents) != 1:
            return False
        git(root, 'merge-base', '--is-ancestor', T004_PREPARATION, candidate)
        count = len(git(root, 'rev-list', T004_PREPARATION + '..' + candidate).splitlines())
        # One narrowly scoped PR-wrapper fix may follow the reviewed CI head.
        # No later commit, runtime change or general history allowance follows.
        if count != 1 and not (count == 2 and
                git(root, 'rev-parse', candidate + '^').decode().strip() == T004_REVIEWED_CI):
            return False
        for name, (digest, executable) in T004_PREP_PINS.items():
            path = root/name
            if (path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1
                    or bool(path.stat().st_mode & 0o111) != executable
                    or hashlib.sha256(path.read_bytes()).hexdigest() != digest):
                return False
        exclusions = tuple(':(exclude)' + name for name in T004_CI_PATHS)
        for revision in (('HEAD',), (), ('--cached',)):
            git(root, 'diff', '--quiet', T004_PREPARATION, *revision, '--', '.', *exclusions)
        # The two reviewed CI edits must themselves be clean at this exact HEAD;
        # neither staged nor working-tree overrides are part of the seal.
        for options in ((), ('--cached',)):
            git(root, 'diff', *options, '--quiet', 'HEAD', '--', *T004_CI_PATHS)
        return not git(root, 'ls-files', '--others', '--exclude-standard', '-z')
    except (OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError):
        return False


def expected_t004_rejection(result):
    """Exact safe preimplementation report, not an arbitrary nonzero exit."""
    try:
        value = json.loads(result.stdout)
        encoded = json.dumps(value, sort_keys=True, separators=(',', ':'),
                             ensure_ascii=False, allow_nan=False).encode('utf-8')
        return (result.returncode == 1 and not result.stderr
                and hashlib.sha256(encoded).hexdigest() == T004_REJECTION_SHA256)
    except (ValueError, TypeError, UnicodeError):
        return False


def check_t004_preparation(root, documentation=False, context=None, policy_source=None):
    print('CI mode: operator validation only; task execution and live approval are FALSE.'
          if policy_source else 'CI mode: documentation only; accepted baseline behavior, no implementation acceptance.'
          if documentation else 'CI mode: sealed T004 PREPARATION ONLY; no implementation acceptance.', flush=True)
    for argv in ([sys.executable, 'scripts/verify_t004_cli.py', '--self-test'],
                 [sys.executable, '-m', 'py_compile', 'scripts/verify_t004_cli.py'],
                 [sys.executable, 'scripts/verify_t003_state.py'],
                 [sys.executable, 'scripts/verify_t002_red.py'],
                 [sys.executable, 'scripts/check_repo.py'], ['git', 'diff', '--check']):
        if run(argv, root):
            return 1
    rejection = subprocess.run([sys.executable, 'scripts/verify_t004_cli.py'], cwd=root,
                               capture_output=True, text=True, timeout=120)
    if not expected_t004_rejection(rejection):
        print('Preparation blocked: T004 preimplementation rejection changed.', flush=True)
        return 1
    print('Exact T004 rejection verified: 53 tests; implementation acceptance is FALSE.', flush=True)
    for script in sorted((root/'.specify/scripts/bash').glob('*.sh')) + sorted((root/'scripts').glob('*.sh')):
        if run(['bash', '-n', str(script.relative_to(root))], root):
            return 1
    env = os.environ.copy()
    env.update(SPECIFY_FEATURE_DIRECTORY='specs/001-workspace-lifecycle', SPECIFY_FEATURE_NO_PERSIST='1')
    if subprocess.run(['bash', '.specify/scripts/bash/check-prerequisites.sh', '--json',
                       '--require-tasks', '--include-tasks'], cwd=root, env=env).returncode:
        return 1
    raw = run(['make', 'check', 'PYTHON=' + sys.executable], root)
    print(f'Raw make check exit: {raw}; full suite remains RED; preparation only.', flush=True)
    if policy_source:
        valid = operator_suite(root) and approved_operator_tooling(root, context, policy_source)
    else:
        boundary = approved_documentation if documentation else approved_t004_preparation
        valid = boundary(root, context) if documentation else boundary(root)
    return 0 if raw == 2 and valid else 1


def git(root, *args):
    # Repository config and attributes are executable inputs. Do not trigger
    # clean/process filters, fsmonitor, custom merge drivers or external diffs.
    env = {key: value for key, value in os.environ.items() if not key.startswith('GIT_')}
    env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull, GIT_OPTIONAL_LOCKS='0')
    command = ['git', '-c', 'core.fsmonitor=false', '-c', 'core.hooksPath=' + os.devnull,
               '-c', 'core.attributesFile=' + os.devnull, '-c', 'submodule.recurse=false']
    config = subprocess.run(command + ['config', '--null', '--list', '--no-includes'],
                            cwd=root, env=env, capture_output=True, check=True).stdout
    for entry in filter(None, config.split(b'\0')):
        key = entry.split(b'\n', 1)[0].lower()
        if (key.startswith((b'filter.', b'include.', b'includeif.'))
                or (key.startswith(b'merge.') and key.endswith(b'.driver'))
                or (key.startswith(b'diff.') and key.endswith((b'.command', b'.textconv')))
                or key == b'diff.external'):
            raise ValueError('Executable or external Git configuration is unsupported')
    if args and args[0] in ('diff', 'diff-tree'):
        args = (args[0], '--no-ext-diff', '--no-textconv', *args[1:])
    return subprocess.run(command + list(args), cwd=root, env=env,
                          capture_output=True, check=True).stdout


def reviewed_docs(root):
    try:
        for name, digest in DOC_PINS.items():
            path = root/name
            if (path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o111
                    or hashlib.sha256(path.read_bytes()).hexdigest() != digest):
                return False
        # Pin both index and worktree, including Git file modes, to the reviewed pair.
        for options in ((), ('--cached',)):
            git(root, 'diff', *options, '--quiet', REVIEWED_DOCS, '--', *sorted(DOC_PINS))
        return True
    except (OSError, subprocess.CalledProcessError):
        return False


def approved_t003(root):
    try:
        for name, digest in PINS.items():
            path = root/name
            if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                return False
        # The owner merged PR #32 by rebase; its accepted commit has a new identity.
        for anchor in (ACCEPTED_T003, SEALED_T003):
            try:
                git(root, 'merge-base', '--is-ancestor', anchor, 'HEAD')
                break
            except subprocess.CalledProcessError:
                continue
        else:
            return False
        git(root, 'merge-base', '--is-ancestor', T002_BASELINE, anchor)
        git(root, 'merge-base', '--is-ancestor', T002_MERGE, T002_BASELINE)
        # Preserve the original prerequisite implementation and accepted blobs.
        for name in ('tests/test_single_task_state.py', 'specs/003-single-task-entry/tasks.md'):
            original = git(root, 'ls-tree', T002_BASELINE, '--', name)
            if not original or original != git(root, 'ls-tree', T002_MERGE, '--', name):
                return False
        allowed = CI_PATHS | (set(DOC_PINS) if reviewed_docs(root) else set())
        # Any successor implementation, marker, test or other content expires this gate.
        git(root, 'diff', '--quiet', anchor, '--', '.',
            *(f':(exclude){name}' for name in sorted(allowed)))
        untracked = git(root, 'ls-files', '--others', '--exclude-standard', '-z')
        return all(os.fsdecode(name) in CI_PATHS for name in untracked.split(b'\0') if name)
    except (OSError, subprocess.CalledProcessError):
        return False


def run(argv, root):
    print('+ ' + ' '.join(argv), flush=True)
    return subprocess.run(argv, cwd=root).returncode


def strict_suite(root, directory='tests', pattern='test*.py', required_modules=()):
    # unittest's successful exit alone also permits skips/xfail and empty discovery.
    suite = unittest.defaultTestLoader.discover(str(root/directory), pattern=pattern)
    def cases(value):
        for test in value:
            yield from cases(test) if isinstance(test, unittest.TestSuite) else (test,)
    ids = [test.id() for test in cases(suite)]
    if len(ids) != len(set(ids)) or any(not any(name.startswith(module + '.') for name in ids)
                                      for module in required_modules):
        return False
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    return (result.wasSuccessful() and result.testsRun > 0 and not result.skipped
            and not result.expectedFailures)


def check(root, context=None, policy_source=None):
    try:
        clean_checkout(root, git(root, 'rev-parse', 'HEAD').decode().strip())
    except (OSError, ValueError, subprocess.CalledProcessError):
        print('CI blocked: use a pristine isolated checkout, including ignored files (prepublication scratch).', flush=True)
        return 1
    with isolated_python_cache():
        return _check(root, context, policy_source)


def _check(root, context=None, policy_source=None):
    if context is not None or policy_source is not None or 'GITHUB_EVENT_NAME' in os.environ:
        try:
            context = source_context(root, *context) if context else source_context(root)
        except (OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError):
            print('CI blocked: invalid source context.', flush=True)
            return 1
    if policy_source is not None:
        if not approved_operator_tooling(root, context, policy_source):
            print('CI blocked: untrusted policy source or operator scope.', flush=True)
            return 1
        return check_t004_preparation(root, context=context, policy_source=policy_source)
    if approved_t004_preparation(root):
        return check_t004_preparation(root)
    if approved_documentation(root, context):
        return check_t004_preparation(root, documentation=True, context=context or source_context(root))
    scoped = approved_t003(root)
    print('CI mode: approved T003 phase' if scoped else 'CI mode: strict full green', flush=True)
    if scoped:
        for script in ('verify_t003_state.py', 'verify_t002_red.py'):
            if run([sys.executable, 'scripts/' + script], root):
                return 1
        if run([sys.executable, 'scripts/check_repo.py'], root) or run(['git', 'diff', '--check'], root):
            return 1
        for script in sorted((root/'.specify/scripts/bash').glob('*.sh')) + sorted((root/'scripts').glob('*.sh')):
            if run(['bash', '-n', str(script.relative_to(root))], root):
                return 1
        env = os.environ.copy()
        env.update(SPECIFY_FEATURE_DIRECTORY='specs/001-workspace-lifecycle', SPECIFY_FEATURE_NO_PERSIST='1')
        if subprocess.run(['bash', '.specify/scripts/bash/check-prerequisites.sh', '--json',
                           '--require-tasks', '--include-tasks'], cwd=root, env=env).returncode:
            return 1
    elif (root/'scripts/single_task_state.py').exists():
        if run([sys.executable, 'scripts/verify_t003_state.py'], root):
            return 1
    raw = run(['make', 'check', 'PYTHON=' + sys.executable], root)
    print(f'Raw make check exit: {raw}; full suite ' + ('remains RED (not implementation acceptance).' if scoped else 'must be GREEN.'), flush=True)
    if raw != (2 if scoped else 0):
        return 1
    accepted = scoped or strict_suite(root)
    if context is not None:
        try:
            accepted = accepted and source_context(root, *context) == context
        except (OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError):
            accepted = False
    return 0 if accepted else 1


def self_test():
    class GateTests(unittest.TestCase):
        def test_approved_content_and_prerequisites(self):
            self.assertTrue(approved_t003(ROOT) or approved_t004_preparation(ROOT) or approved_documentation(ROOT))

        def test_rebased_anchor_without_original_commit(self):
            original_git = git
            def without_original(root, *args):
                if SEALED_T003 in args:
                    raise subprocess.CalledProcessError(128, args)
                return original_git(root, *args)
            with mock.patch(__name__ + '.git', side_effect=without_original):
                self.assertTrue(approved_t003(ROOT) or approved_t004_preparation(ROOT) or approved_documentation(ROOT))

        def test_reviewed_docs_and_successor_mutations(self):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)/'checkout'
                subprocess.run(['git', 'clone', '--quiet', '--shared', str(ROOT), str(root)], check=True)
                subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', T004_SOURCE], check=True)
                self.assertTrue(reviewed_docs(root))
                self.assertTrue(approved_t003(root))
                for name in (*DOC_PINS, 'scripts/single_task_state.py',
                             'tests/test_single_task_state.py', 'specs/003-single-task-entry/tasks.md'):
                    path = root/name
                    content, mode = path.read_bytes(), path.stat().st_mode
                    for mutation in ('changed', 'missing', 'symlink', 'executable'):
                        with self.subTest(path=name, mutation=mutation):
                            path.unlink()
                            if mutation == 'symlink':
                                path.symlink_to(ROOT/name)
                            elif mutation != 'missing':
                                path.write_bytes(content + (b'\n' if mutation == 'changed' else b''))
                                path.chmod(mode | 0o111 if mutation == 'executable' else mode)
                            self.assertFalse(approved_t003(root))
                            if path.is_symlink() or path.exists():
                                path.unlink()
                            path.write_bytes(content)
                            path.chmod(mode)
                # Either doc pin alone is insufficient, even if both are regular files.
                (root/'AGENTS.md').write_bytes((root/'AGENTS.md').read_bytes() + b'\n')
                self.assertFalse(reviewed_docs(root))

        def test_every_content_pin_refuses_changes_missing_files_and_symlinks(self):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                for name in PINS:
                    path = root/name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes((ROOT/name).read_bytes())
                with mock.patch(__name__ + '.git', return_value=b'approved blob'):
                    for name in PINS:
                        path = root/name
                        content = path.read_bytes()
                        for mutation in ('changed', 'missing', 'symlink'):
                            with self.subTest(path=name, mutation=mutation):
                                path.unlink()
                                if mutation == 'changed':
                                    path.write_bytes(content + b'\n')
                                elif mutation == 'symlink':
                                    path.symlink_to(ROOT/name)
                                self.assertFalse(approved_t003(root))
                                if path.is_symlink() or path.exists():
                                    path.unlink()
                                path.write_bytes(content)

        def test_unknown_history_successor_tree_and_untracked_code_refused(self):
            def source(*args):
                if args[1] == 'ls-files':
                    return b'scripts/single_task_context.py\0'
                return b'approved blob'
            with mock.patch(__name__ + '.git', side_effect=source):
                self.assertFalse(approved_t003(ROOT))
            for command in ('merge-base', 'ls-tree', 'diff', 'ls-files'):
                def fail(root, *args):
                    if args[0] == command:
                        raise subprocess.CalledProcessError(1, args)
                    return b'approved blob'
                with self.subTest(command=command), mock.patch(__name__ + '.git', side_effect=fail):
                    self.assertFalse(approved_t003(ROOT))

        def test_prerequisite_blob_mismatch_refused(self):
            def mismatch(root, *args):
                return b'wrong blob' if T002_MERGE in args else b'approved blob'
            with mock.patch(__name__ + '.git', side_effect=mismatch):
                self.assertFalse(approved_t003(ROOT))

        def test_strict_fallback_requires_green(self):
            with mock.patch(__name__ + '.clean_checkout'), \
                    mock.patch(__name__ + '.approved_t004_preparation', return_value=False), \
                    mock.patch(__name__ + '.approved_documentation', return_value=False), \
                    mock.patch(__name__ + '.approved_t003', return_value=False), \
                    mock.patch(__name__ + '.strict_suite', return_value=True):
                for raw in (0, 1, 2):
                    with self.subTest(raw=raw), mock.patch(__name__ + '.run', side_effect=[0, raw]):
                        self.assertEqual(0 if raw == 0 else 1, check(ROOT))
            with mock.patch(__name__ + '.clean_checkout'), \
                    mock.patch(__name__ + '.approved_t004_preparation', return_value=False), \
                    mock.patch(__name__ + '.approved_documentation', return_value=False), \
                    mock.patch(__name__ + '.approved_t003', return_value=False), \
                    mock.patch(__name__ + '.run', return_value=0), \
                    mock.patch(__name__ + '.strict_suite', return_value=False):
                self.assertEqual(1, check(ROOT))

        def test_strict_suite_refuses_empty_skipped_and_expected_failures(self):
            class Green(unittest.TestCase):
                def runTest(self):
                    pass
            class Skipped(Green):
                @unittest.skip('synthetic')
                def runTest(self):
                    pass
            class ExpectedFailure(Green):
                @unittest.expectedFailure
                def runTest(self):
                    self.fail('synthetic')
            for case, expected in ((None, False), (Green, True), (Skipped, False), (ExpectedFailure, False)):
                suite = unittest.TestSuite([] if case is None else [case()])
                with self.subTest(case=case), mock.patch.object(unittest.defaultTestLoader, 'discover', return_value=suite):
                    self.assertEqual(expected, strict_suite(ROOT))

        def test_scoped_checks_and_raw_red_required(self):
            with mock.patch(__name__ + '.clean_checkout'), \
                    mock.patch(__name__ + '.approved_t004_preparation', return_value=False), \
                    mock.patch(__name__ + '.approved_documentation', return_value=False), \
                    mock.patch(__name__ + '.approved_t003', return_value=True), \
                    mock.patch('subprocess.run', return_value=mock.Mock(returncode=0)):
                for raw in (0, 1, 2):
                    with self.subTest(raw=raw), mock.patch(__name__ + '.run', side_effect=lambda argv, root: raw if argv[0] == 'make' else 0):
                        self.assertEqual(0 if raw == 2 else 1, check(ROOT))
                for failing in ('verify_t003_state.py', 'verify_t002_red.py', 'check_repo.py', 'git', 'bash'):
                    with self.subTest(failing=failing), mock.patch(__name__ + '.run', side_effect=lambda argv, root: 1 if failing in ' '.join(argv) else 0):
                        self.assertEqual(1, check(ROOT))
                with mock.patch(__name__ + '.run', return_value=0), \
                        mock.patch('subprocess.run', return_value=mock.Mock(returncode=1)):
                    self.assertEqual(1, check(ROOT))
        def test_documentation_mutations_expire_gate(self):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)/'checkout'
                subprocess.run(['git', 'clone', '--quiet', '--shared', str(ROOT), str(root)], check=True)
                for name in ('docs/proposal.json', 'specs/proposal/spec.md'):
                    path = root/name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text('{}\n' if path.suffix == '.json' else '# Proposal\n')
                subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
                subprocess.run(['git', '-C', str(root), '-c', 'user.name=Synthetic',
                                '-c', 'user.email=fixture@example.invalid', 'commit', '--quiet',
                                '-m', 'docs: synthetic proposals'], check=True)
                self.assertTrue(approved_documentation(root))
                names = (*T004_PREP_PINS, 'scripts/check_ci.py', 'scripts/single_task.py',
                         'scripts/single_task_state.py', 'tests/test_single_task_state.py',
                         'specs/003-single-task-entry/tasks.md', '.github/workflows/check.yml',
                         'docs/proposal.json', 'specs/proposal/spec.md')
                for name in names:
                    path = root/name
                    content, mode = path.read_bytes(), path.stat().st_mode
                    for mutation in ('changed', 'missing', 'symlink', 'mode'):
                        with self.subTest(path=name, mutation=mutation):
                            path.unlink()
                            if mutation == 'symlink':
                                path.symlink_to(ROOT/'README.md')
                            elif mutation != 'missing':
                                path.write_bytes(content + (b'\n' if mutation == 'changed' else b''))
                                path.chmod(mode ^ 0o111 if mutation == 'mode' else mode)
                            self.assertFalse(approved_documentation(root))
                            subprocess.run(['git', '-C', str(root), 'add', '--', name], check=True)
                            self.assertFalse(approved_documentation(root))
                            subprocess.run(['git', '-C', str(root), 'restore', '--staged', '--worktree', '--', name], check=True)
                extra = root/'docs/untracked.md'
                extra.write_text('# Untracked\n')
                self.assertFalse(approved_documentation(root))
                extra.unlink()
                # A committed runtime successor cannot inherit docs-only acceptance.
                (root/'scripts/single_task.py').write_text('pass\n')
                subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
                subprocess.run(['git', '-C', str(root), '-c', 'user.name=Synthetic',
                                '-c', 'user.email=fixture@example.invalid', 'commit', '--quiet',
                                '-m', 'test: executable successor'], check=True)
                self.assertFalse(approved_documentation(root))

        def test_preparation_unknown_history_refused(self):
            with mock.patch(__name__ + '.git', side_effect=subprocess.CalledProcessError(1, ['git'])):
                self.assertFalse(approved_t004_preparation(ROOT))

        def test_documentation_synthetic_merge_is_exact(self):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)/'checkout'
                subprocess.run(['git', 'clone', '--quiet', '--shared', str(ROOT), str(root)], check=True)
                (root/'docs/synthetic-proposal.md').write_text('# Synthetic proposal\n')
                subprocess.run(['git', '-C', str(root), 'add', 'docs/synthetic-proposal.md'], check=True)
                subprocess.run(['git', '-C', str(root), '-c', 'user.name=Synthetic',
                                '-c', 'user.email=fixture@example.invalid', 'commit', '--quiet',
                                '-m', 'docs: synthetic proposal'], check=True)
                candidate = git(root, 'rev-parse', 'HEAD').decode().strip()
                tree = git(root, 'rev-parse', 'HEAD^{tree}').decode().strip()
                env = dict(os.environ, GIT_AUTHOR_NAME='Synthetic', GIT_COMMITTER_NAME='Synthetic',
                           GIT_AUTHOR_EMAIL='fixture@example.invalid', GIT_COMMITTER_EMAIL='fixture@example.invalid')
                event_path = Path(directory)/'event.json'
                event = {'pull_request': {
                    'base': {'sha': DOCUMENTATION_POLICY_SOURCE, 'ref': 'chore/issue-2-bootstrap-entry',
                             'repo': {'full_name': 'wstein/workharbor-nxt'}},
                    'head': {'sha': candidate, 'ref': 'chore/5-t004-preparation',
                             'repo': {'full_name': 'wstein/workharbor-nxt'}}}}
                event_path.write_text(json.dumps(event))
                def merge(parents, selected_tree=tree):
                    argv = ['git', 'commit-tree', selected_tree]
                    for parent in parents:
                        argv += ['-p', parent]
                    revision = subprocess.run(argv, cwd=root, env=env, input='synthetic wrapper\n',
                                              text=True, capture_output=True, check=True).stdout.strip()
                    subprocess.run(['git', '-C', str(root), 'switch', '--quiet', '--detach', revision], check=True)
                    return revision
                revision = merge([DOCUMENTATION_POLICY_SOURCE, candidate])
                with mock.patch.dict(os.environ, {'GITHUB_EVENT_NAME': 'pull_request',
                        'GITHUB_SHA': revision, 'GITHUB_EVENT_PATH': str(event_path)}):
                    self.assertTrue(approved_documentation(root))
                    for key, wrong in (('GITHUB_SHA', candidate), ('GITHUB_EVENT_NAME', 'workflow_dispatch')):
                        with mock.patch.dict(os.environ, {key: wrong}):
                            self.assertFalse(approved_documentation(root))
                    changed = json.loads(json.dumps(event))
                    changed['pull_request']['head']['sha'] = DOCUMENTATION_POLICY_SOURCE
                    event_path.write_text(json.dumps(changed))
                    self.assertFalse(approved_documentation(root))
                    event_path.write_text(json.dumps(event))
                    (root/'scripts/check_ci.py').write_text('dirty wrapper checker\n')
                    self.assertFalse(approved_documentation(root))
                    subprocess.run(['git', '-C', str(root), 'restore', '--', 'scripts/check_ci.py'], check=True)
                    for parents, selected_tree in (([candidate, DOCUMENTATION_POLICY_SOURCE], tree),
                            ([DOCUMENTATION_POLICY_SOURCE, candidate, ACCEPTED_T003], tree),
                            ([DOCUMENTATION_POLICY_SOURCE, candidate], git(root, 'rev-parse', DOCUMENTATION_POLICY_SOURCE + '^{tree}').decode().strip())):
                        wrong = merge(parents, selected_tree)
                        with mock.patch.dict(os.environ, {'GITHUB_SHA': wrong}):
                            self.assertFalse(approved_documentation(root))

        def test_preparation_rejection_is_exact(self):
            observed = subprocess.run([sys.executable, 'scripts/verify_t004_cli.py'], cwd=ROOT,
                                      capture_output=True, text=True, timeout=120)
            self.assertTrue(expected_t004_rejection(observed))
            value = json.loads(observed.stdout)
            for code, output, stderr in ((0, observed.stdout, ''), (2, observed.stdout, ''),
                                        (1, '', ''), (1, observed.stdout, 'unexpected error'),
                                        (1, '{}', ''), (1, 'not JSON', '')):
                self.assertFalse(expected_t004_rejection(subprocess.CompletedProcess([], code, output, stderr)))
            for key, changed in (('tests_run', 0), ('proposal_matrix_verified', True),
                                 ('validated_entry_implemented', True), ('full_suite_green', True)):
                modified = dict(value, **{key: changed})
                self.assertFalse(expected_t004_rejection(subprocess.CompletedProcess([], 1, json.dumps(modified), '')))
            for kind in ('skip', 'xfail', 'unexpected-success', 'error', 'pass'):
                modified = json.loads(observed.stdout)
                modified['outcomes'][0]['outcome'] = kind
                if modified != value:
                    self.assertFalse(expected_t004_rejection(subprocess.CompletedProcess([], 1, json.dumps(modified), '')))

        def test_preparation_failure_does_not_fall_through(self):
            with mock.patch(__name__ + '.approved_t004_preparation', return_value=True), \
                    mock.patch(__name__ + '.check_t004_preparation', return_value=1), \
                    mock.patch(__name__ + '.approved_t003') as older:
                self.assertEqual(1, check(ROOT))
                older.assert_not_called()

    with tempfile.TemporaryDirectory() as directory:
        fixture = Path(directory)/'accepted'
        subprocess.run(['git', 'clone', '--quiet', '--shared', str(ROOT), str(fixture)], check=True)
        subprocess.run(['git', '-C', str(fixture), 'checkout', '--quiet', DOCUMENTATION_POLICY_SOURCE], check=True)
        with mock.patch(__name__ + '.ROOT', fixture):
            result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(GateTests))
    return 0 if result.wasSuccessful() and result.testsRun == 14 else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--policy-source', help='Host-adopted independently reviewed immutable policy pin')
    parser.add_argument('--event', choices=('pull_request', 'push', 'local'))
    for name in ('base', 'head', 'checkout'):
        parser.add_argument('--' + name)
    args = parser.parse_args(argv)
    context = (args.event, args.base, args.head, args.checkout)
    if any(context) and not all(context):
        parser.error('event/base/head/checkout must be supplied together')
    if args.self_test:
        if any(context) or args.root != ROOT or args.policy_source:
            parser.error('self-test does not accept source overrides')
        return self_test()
    return check(args.root, SourceContext(*context) if all(context) else None, args.policy_source)


if __name__ == '__main__':
    raise SystemExit(main())
