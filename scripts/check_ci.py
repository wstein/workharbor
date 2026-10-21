#!/usr/bin/env python3
"""Explicit repository acceptance; retained assertion failures remain raw red."""
import sys

if __name__ == '__main__' and not sys.flags.isolated:
    print('CI blocked: launch the checker with python -I -B.', file=sys.stderr)
    raise SystemExit(1)

import argparse
import hashlib
import importlib
import json
import os
import re
import runpy
import shlex
import signal
import subprocess
import tempfile
import threading
import time
import unittest
from contextlib import contextmanager
from pathlib import Path
from typing import NamedTuple

ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = 'wstein/workharbor'
MANIFEST = 'docs/development/ci-acceptance.json'
# Native macOS observations: raw 490s, qualified 838s. Keep aggregate
# headroom separate from the 300s utility-command and approved task deadlines.
RAW_AGGREGATE_TIMEOUT_SECONDS = 600
QUALIFIED_AGGREGATE_TIMEOUT_SECONDS = 1200


def plain_branch(ref):
    """Conservative branch name: slash-separated words, no refs/, '..', leading '-' or '.'."""
    return (re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)*', ref) is not None
            and '..' not in ref and not ref.endswith(('.', '.lock')) and '.lock/' not in ref
            and not ref.startswith(('refs/', 'heads/')))


def _source_context(root, event=None, base=None, head=None, checkout=None):
    """Bind checked code to a head or an actual ordered PR merge, not approval."""
    root = Path(root)
    if root.is_symlink() or not root.is_dir():
        raise ValueError('Expected regular checkout root')
    actual = git(root, 'rev-parse', 'HEAD').decode().strip()
    provided = (event, base, head, checkout)
    stacked = False
    if any(value is not None for value in provided) and not all(provided):
        raise ValueError('Source context requires all four pins')
    if 'GITHUB_EVENT_NAME' in os.environ:
        github_event = os.environ['GITHUB_EVENT_NAME']
        if github_event not in ('push', 'pull_request') or os.environ.get('GITHUB_SHA') != actual:
            raise ValueError('Stale or unsupported event')
        payload = json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text())
        if github_event == 'pull_request':
            pr = payload['pull_request']
            if pr['base']['repo']['full_name'] != REPOSITORY:
                raise ValueError('PR must target repository main or a branch of ' + REPOSITORY)
            if pr['base']['ref'] != 'main':
                # Stacked PR: the parent branch of this same repository, never a
                # fork, and already contained in the head (checked below).
                if (pr['head']['repo']['full_name'] != REPOSITORY
                        or not isinstance(pr['base']['ref'], str) or not plain_branch(pr['base']['ref'])):
                    raise ValueError('Stacked PR base must be a plain branch of ' + REPOSITORY)
                stacked = True
            observed = SourceContext(github_event, pr['base']['sha'], pr['head']['sha'], actual)
        else:
            if payload['repository']['full_name'] != 'wstein/workharbor' or payload['after'] != actual:
                raise ValueError('Stale or foreign push')
            observed = SourceContext(github_event, actual, actual, actual)
        if all(provided) and SourceContext(*provided) != observed:
            raise ValueError('Explicit pins disagree with event')
        event, base, head, checkout = observed
    elif not any(provided):
        event, base, head, checkout = 'local', actual, actual, actual
    if event not in ('local', 'push', 'pull_request') or checkout != actual:
        raise ValueError('Unexpected checkout identity')
    for revision in (base, head, checkout):
        if (not isinstance(revision, str) or not re.fullmatch('[0-9a-f]{40}', revision)
                or git(root, 'cat-file', '-t', revision) != b'commit\n'):
            raise ValueError('Missing exact commit object')
    if event == 'pull_request':
        parents = git(root, 'rev-list', '--parents', '-n', '1', checkout).decode().split()[1:]
        if base == head or len(parents) != 2 or parents[1] != head:
            raise ValueError('Unexpected synthetic merge parents')
        # Into main GitHub merges head into the base tip. For a native stack it
        # merges head into the parent PR's own merge commit, whose second
        # parent is this PR's base (the parent PR head); any depth has that shape.
        parent = parents[0]
        if parent != base:
            if not stacked:
                raise ValueError('Unexpected synthetic merge parents')
            if (not re.fullmatch('[0-9a-f]{40}', parent)
                    or git(root, 'cat-file', '-t', parent) != b'commit\n'):
                raise ValueError('Missing exact parent merge commit')
            grandparents = git(root, 'rev-list', '--parents', '-n', '1', parent).decode().split()[1:]
            if len(grandparents) != 2 or grandparents[1] != base:
                raise ValueError('Unexpected parent PR merge')
        if stacked:
            try:
                git(root, 'merge-base', '--is-ancestor', base, head)
            except subprocess.CalledProcessError as error:
                raise ValueError('Stacked PR base is not an ancestor of head') from error
        tree = git(root, 'merge-tree', '--write-tree', parent, head).decode().strip()
        if tree != git(root, 'rev-parse', checkout + '^{tree}').decode().strip():
            raise ValueError('Synthetic checkout differs from actual merge')
    elif head != checkout:
        raise ValueError('Non-PR checkout must be head')
    clean_checkout(root, checkout)
    return SourceContext(event, base, head, checkout)


class SourceContext(NamedTuple):
    event: str
    base: str
    head: str
    checkout: str


def clean_checkout(root, revision, allow_generated=False):
    """Compare bytes and modes explicitly, including assume-unchanged files."""
    for options in ((), ('--cached',)):
        git(root, 'diff', *options, '--quiet', revision, '--')
    if not allow_generated and git(root, 'ls-files', '--others', '-z'):
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


def source_context(root, event=None, base=None, head=None, checkout=None):
    try:
        return _source_context(root, event, base, head, checkout)
    except (OSError, subprocess.CalledProcessError, KeyError, TypeError, UnicodeError) as error:
        raise ValueError('Unavailable or invalid source evidence') from error


@contextmanager
def isolated_python_cache():
    """Keep cache reads and generated files outside the candidate checkout."""
    with tempfile.TemporaryDirectory(prefix='workharbor-ci-cache-') as cache:
        # Runner/check interpreters use -I. Normal CLI subprocesses under test
        # still need Python's script-directory imports for adjacent modules.
        # Legacy tests invoke python3 by name. Bind that spelling to the selected
        # interpreter without adding isolated flags to ordinary script children.
        aliases = Path(cache)/'bin'
        aliases.mkdir()
        (aliases/'python3').symlink_to(sys.executable)
        names = {'PYTHONPYCACHEPREFIX': cache, 'PYTHONDONTWRITEBYTECODE': '1',
                 'PYTHONNOUSERSITE': '1', 'GIT_NO_REPLACE_OBJECTS': '1',
                 'PATH': str(aliases) + os.pathsep + os.environ.get('PATH', os.defpath)}
        previous = {name: value for name, value in os.environ.items() if name.startswith('PYTHON') or name in ('PATH', 'GIT_NO_REPLACE_OBJECTS')}
        prefix, no_write = sys.pycache_prefix, sys.dont_write_bytecode
        for name in previous:
            os.environ.pop(name)
        os.environ.update(names)
        sys.pycache_prefix, sys.dont_write_bytecode = cache, True
        try:
            yield cache
        finally:
            sys.pycache_prefix, sys.dont_write_bytecode = prefix, no_write
            for name in tuple(os.environ):
                if name.startswith('PYTHON') or name in ('PATH', 'GIT_NO_REPLACE_OBJECTS'):
                    os.environ.pop(name)
            os.environ.update(previous)


@contextmanager
def baseline_execution(root):
    # Keep the worktree if process cleanup cannot be established. Removing its
    # files would race a surviving check and manufacture missing-file failures.
    directory = tempfile.mkdtemp(prefix='workharbor-baseline-')
    retain = False
    try:
        execution = Path(directory)/'checkout'
        revision = git(root, 'rev-parse', 'HEAD').decode().strip()
        git(root, 'worktree', 'add', '--quiet', '--detach', str(execution), revision)
        try:
            clean_checkout(execution, revision)
            yield execution
        except ProcessCleanupError:
            retain = True
            print(f'CI blocked: retained check checkout at {execution}', file=sys.stderr)
            raise
        finally:
            if not retain:
                git(root, 'worktree', 'remove', '--force', str(execution))
    finally:
        if not retain:
            os.rmdir(directory)


def git(root, *args):
    # Repository config and attributes are executable inputs. Do not trigger
    # clean/process filters, fsmonitor, custom merge drivers or external diffs.
    # Source validation is reused by authenticated host readiness. Local object
    # reads need neither tracker credentials nor interpreter/host settings.
    env = {key: os.environ[key] for key in ('PATH', 'LANG', 'LC_ALL', 'TMPDIR')
           if key in os.environ}
    env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull, GIT_OPTIONAL_LOCKS='0',
               GIT_NO_REPLACE_OBJECTS='1', GIT_NO_LAZY_FETCH='1',
               GIT_TERMINAL_PROMPT='0', GIT_ALLOW_PROTOCOL='')
    command = ['git', '--no-replace-objects', '--no-lazy-fetch', '-c', 'core.fsmonitor=false', '-c', 'core.hooksPath=' + os.devnull,
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
    common = subprocess.run(command + ['rev-parse', '--path-format=absolute', '--git-common-dir'],
                           cwd=root, env=env, capture_output=True, check=True).stdout.strip()
    replacements = subprocess.run(command + ['for-each-ref', '--format=%(refname)', 'refs/replace/'],
                                  cwd=root, env=env, capture_output=True, check=True).stdout
    info = Path(os.fsdecode(common))/'info'
    graft = info/'grafts'
    if info.is_symlink() or graft.exists() or graft.is_symlink() or replacements:
        raise ValueError('Git replacement or graft metadata is unsupported')
    if args and args[0] in ('diff', 'diff-tree'):
        args = (args[0], '--no-ext-diff', '--no-textconv', *args[1:])
    return subprocess.run(command + list(args), cwd=root, env=env,
                          capture_output=True, check=True).stdout


def python_argv(*args):
    prefix = ['-X', 'pycache_prefix=' + sys.pycache_prefix] if sys.pycache_prefix else []
    return [sys.executable, '-I', '-B', *prefix, *args]


_apps = runpy.run_path(str(Path(__file__).with_name('application_checks.py')))
check_applications = _apps['check_applications']


def load_manifest(root):
    def unique_object(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('Duplicate manifest key')
            result[key] = value
        return result
    value = json.loads((root/MANIFEST).read_text(), object_pairs_hook=unique_object)
    if (not isinstance(value, dict)
            or set(value) != {'schema_version', 'required_test_ids', 'retained_assertion_failures', 'application_tests'}
            or type(value['schema_version']) is not int or value['schema_version'] != 2):
        raise ValueError('Unsupported acceptance manifest')
    required = value['required_test_ids']
    if (not isinstance(required, list) or not required or
            any(not isinstance(item, str) or not item for item in required) or len(required) != len(set(required))):
        raise ValueError('Required test IDs must be nonempty and unique')
    retained = value['retained_assertion_failures']
    if not isinstance(retained, list):
        raise ValueError('Retained failures must be a list')
    ids = []
    for case in retained:
        if (not isinstance(case, dict) or set(case) != {'id', 'message', 'reason', 'removal_condition'}
                or any(not isinstance(item, str) or not item.strip() for item in case.values())
                or case['id'] not in required):
            raise ValueError('Retained failure requires a required ID, exact message, reason and removal condition')
        ids.append(case['id'])
    if len(ids) != len(set(ids)):
        raise ValueError('Duplicate retained failure ID')
    _apps['validate_inventory'](value['application_tests'])
    return value


# Load the adjacent shared implementation by exact path, including under -I.
_outcomes = runpy.run_path(str(Path(__file__).with_name('task_outcomes.py')))
flatten = _outcomes['flatten']
Outcomes = _outcomes['Outcomes']
evaluate = _outcomes['evaluate']
validate_collection = _outcomes['validate_collection']


def package_acceptance(value, repository_snapshot, evidence, capabilities, *,
                       approved_revision, approved_checks, guard=None, matrix_approved=False):
    """Check one host-approved package; local decisions grant no execution authority.

    The host supplies exact serialization/check approval and guard ports separately
    from package data. This optional entry does not change repository acceptance.
    Checks execute only after the shared validator clears every blocker; absent
    guards fail closed. The outcome runner owns argv, collection and classification.
    """
    # Package primitives import the adjacent state traversal library. Only this
    # requested entry loads them; ordinary repository CI needs no guard/package.
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    try:
        import task_package as package
    finally:
        sys.path.pop(0)
    result = dict(accepted=False, execution_eligible=False, authenticated_approval=None,
                  independent_review=None, live_execution=None,
                  validation=None, outcomes=None, blocker=None)
    try:
        value = package.parse_json(package.package_bytes(value))
        approved = package.parse_json(package.canonical_json(approved_checks))
        if (type(approved_revision) is not str
                or approved_revision != package.package_revision_digest(value)):
            raise ValueError('approval_mismatch')
        _outcomes['validate_checks'](approved)
        if _outcomes['canonical'](approved) != _outcomes['canonical'](value['outcomes']):
            raise ValueError('approval_mismatch')
        root = Path(repository_snapshot['root'])
        validation = package.validate(value, repository_snapshot, evidence, capabilities,
                                      guard=guard, outcome_validator=_outcomes['validate_checks'])
        result['validation'] = validation
        if not validation['structural_valid'] or validation['blockers']:
            result['blocker'] = 'package_validation_blocked'
            return result
        outcome = _outcomes['run_checks'](root, value['outcomes'], approved_checks=approved,
                                         matrix_approved=matrix_approved)
        result['outcomes'] = outcome
        validation['raw_exits'] = [check['raw_exit'] for check in outcome['checks']]
        result['accepted'] = outcome['accepted'] is True
        result['blocker'] = None if result['accepted'] else 'package_outcomes_blocked'
    except (OSError, ValueError, TypeError, KeyError, UnicodeError, subprocess.SubprocessError):
        result['blocker'] = 'invalid_or_unavailable_package_inputs'
    return result


def collect(root):
    # Separate loaders avoid top-level-directory state between the two roots.
    sys.path[:0] = [str(root/'tests'), str(root/'scripts')]
    suite = unittest.TestSuite()
    for directory in ('tests', 'scripts'):
        pattern = 'test*.py' if directory == 'tests' else 'test_*.py'
        suite.addTests(unittest.TestLoader().discover(str(root/directory), pattern=pattern,
                                                     top_level_dir=str(root/directory)))
    for module, name in (('verify_t004_cli', 'CliBoundaryTests'), ('verify_t003_state', 'StatePrimitiveTests')):
        suite.addTests(unittest.TestLoader().loadTestsFromTestCase(getattr(importlib.import_module(module), name)))
    return suite


def acceptance_suite(root):
    manifest = load_manifest(root)
    suite = collect(root)
    ids = [case.id() for case in flatten(suite)]
    validate_collection(ids, manifest['required_test_ids'])
    result = unittest.TextTestRunner(verbosity=2, resultclass=Outcomes).run(suite)
    accepted = evaluate(ids, manifest, result)
    print(f'Acceptance: {result.testsRun} tests; {len(result.failures)} raw failures; '
          f'{len(result.errors)} errors; manifest-qualified {"GREEN" if accepted else "RED"}.', flush=True)
    return accepted


def source_identity(root, source):
    if not re.fullmatch('[0-9a-f]{40}', source) or git(root, 'cat-file', '-t', source) != b'commit\n':
        raise ValueError('Source must identify an exact available commit')
    for name in ('check_ci.py', 'task_outcomes.py', 'task_package.py', 'single_task_state.py', 'application_checks.py'):
        checker = Path(__file__).with_name(name)
        relative = 'scripts/' + name
        entry = git(root, 'ls-tree', source, '--', relative).split()
        mode = b'100755' if checker.stat().st_mode & 0o111 else b'100644'
        if (len(entry) != 4 or entry[:2] != [mode, b'blob'] or checker.is_symlink()
                or git(root, 'show', source + ':' + relative) != checker.read_bytes()
                or (root/relative).read_bytes() != checker.read_bytes()
                or bool((root/relative).stat().st_mode & 0o111) != (mode == b'100755')):
            raise ValueError('Source pin must match loaded and checked runner code; it is not approval')


class ProcessCleanupError(ValueError):
    """The disposable checkout must survive uncertain process cleanup."""


def drain_group(process):
    """Stop ordinary descendants and establish that no live group member remains."""
    try:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            pass
        process.wait(timeout=10)
        deadline = time.monotonic() + 10
        while True:
            try:
                os.killpg(process.pid, 0)
            except ProcessLookupError:
                return
            except PermissionError:
                # macOS can report EPERM for an exiting process group. The
                # native snapshot must still establish absence or zombie state.
                pass
            # Orphan zombies can remain visible on Linux. They cannot execute or
            # hold files; do not mistake them for work still using the checkout.
            snapshot = subprocess.run(['ps', '-axo', 'pgid=,stat='],
                                      capture_output=True, text=True, check=True,
                                      timeout=max(.01, deadline - time.monotonic()))
            states = [state for group, state in
                      (line.split() for line in snapshot.stdout.splitlines())
                      if int(group) == process.pid]
            if not states or all(state.startswith('Z') for state in states):
                return
            if time.monotonic() >= deadline:
                raise ProcessCleanupError('Check process group did not stop')
            time.sleep(.01)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        raise ProcessCleanupError('Unable to establish check process group cleanup') from error


def run(argv, root, *, timeout=300, env=None, output_path=None):
    print('+ ' + shlex.join(argv), flush=True)
    # Make and test runners spawn children. Own their ordinary process group so
    # no check can keep using the disposable checkout after this call finishes.
    capture = output_path.open('xb') if output_path is not None else None
    try:
        process = subprocess.Popen(argv, cwd=root, start_new_session=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env)
    except BaseException:
        if capture is not None:
            capture.close()
        raise
    output_errors = []

    def forward_output():
        try:
            for line in process.stdout:
                if capture is not None:
                    capture.write(line)
                sys.stdout.buffer.write(line)
                sys.stdout.buffer.flush()
        except OSError as error:
            output_errors.append(error)

    reader = threading.Thread(target=forward_output, daemon=True)
    reader.start()
    try:
        return process.wait(timeout=timeout)
    finally:
        # Also stop background children when their immediate parent has exited,
        # and on interruption. SIGKILL cannot be ignored by a nested test.
        drain_group(process)
        # Drain inherited output before baseline_execution removes its checkout.
        # A zombie owns no pipe; polling group existence would mistake it for work.
        reader.join(timeout=10)
        if reader.is_alive():
            raise ProcessCleanupError('Check process output did not close after group termination')
        process.stdout.close()
        if capture is not None:
            capture.close()
        if output_errors:
            raise output_errors[0]


def check(root, context=None, source=None):
    root = Path(root)
    try:
        context = source_context(root, *context) if context else source_context(root)
        if source is not None:
            source_identity(root, source)
        manifest = load_manifest(root)
        with isolated_python_cache(), baseline_execution(root) as execution:
            # Raw make remains honest. Its expected assertion reds stop Make
            # before shell/prerequisite checks, so run those independently below.
            raw = run(['make', 'check-raw', 'PYTHON=' + shlex.join(python_argv())], execution,
                      timeout=RAW_AGGREGATE_TIMEOUT_SECONDS)
            state = 'retained assertions remain RED' if manifest['retained_assertion_failures'] else 'full suite must be GREEN'
            print(f'Raw make check-raw exit: {raw}; {state}.', flush=True)
            valid = raw == (2 if manifest['retained_assertion_failures'] else 0)
            commands = [python_argv('scripts/check_repo.py'), ['git', 'diff', '--check']]
            commands += [['bash', '-n', str(script.relative_to(execution))]
                         for directory in ('.specify/scripts/bash', 'scripts')
                         for script in sorted((execution/directory).glob('*.sh'))]
            for argv in commands:
                valid = run(argv, execution) == 0 and valid
            env = dict(os.environ, SPECIFY_FEATURE_DIRECTORY='specs/001-workspace-lifecycle', SPECIFY_FEATURE_NO_PERSIST='1')
            valid = subprocess.run(['bash', '.specify/scripts/bash/check-prerequisites.sh', '--json',
                                    '--require-tasks', '--include-tasks'], cwd=execution, env=env).returncode == 0 and valid
            # A fresh process keeps discovery/import state out of the parent.
            code = ('import runpy,sys; m=runpy.run_path(sys.argv[1]); '
                    'raise SystemExit(0 if m["acceptance_suite"](m["Path"](sys.argv[2])) else 1)')
            # Aggregate repository discovery needs its own finite budget as green suites grow.
            # This does not change any approved package per-check deadline.
            valid = run(python_argv('-c', code, str(execution/'scripts/check_ci.py'), str(execution)),
                        execution, timeout=QUALIFIED_AGGREGATE_TIMEOUT_SECONDS) == 0 and valid
            valid = check_applications(execution, manifest['application_tests'], run) and valid
            clean_checkout(execution, context.checkout, allow_generated=True)
        if source_context(root, *context) != context:
            raise ValueError('Source changed during checks')
        return 0 if valid else 1
    except (OSError, ValueError, subprocess.CalledProcessError, subprocess.TimeoutExpired, ImportError) as error:
        print('CI blocked: ' + str(error), file=sys.stderr)
        return 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    for name in ('event', 'base', 'head', 'checkout'):
        parser.add_argument('--' + name)
    parser.add_argument('--applications-only', action='store_true', help='Focused application checks; full make check still required')
    parser.add_argument('--source', help='Exact checker code pin; does not establish owner review')
    parser.add_argument('--policy-source', help='Deprecated alias for --source (no adoption semantics)')
    args = parser.parse_args(argv)
    if args.source and args.policy_source and args.source != args.policy_source:
        parser.error('Source arguments disagree')
    if args.policy_source:
        print('Deprecated: --policy-source is now --source; the pin identifies code, not approval.', file=sys.stderr)
    pins = (args.event, args.base, args.head, args.checkout)
    if any(pins) and not all(pins):
        parser.error('Source context requires all four pins')
    if args.applications_only:
        try:
            check_applications(args.root, load_manifest(args.root)['application_tests'], run)
            return 0
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            print('Application checks blocked: ' + str(error), file=sys.stderr)
            return 1
    return check(args.root, SourceContext(*pins) if all(pins) else None, args.source or args.policy_source)


if __name__ == '__main__':
    raise SystemExit(main())
