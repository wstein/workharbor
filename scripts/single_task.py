#!/usr/bin/env python3
"""Local single-task scope/acceptance guard; not a sandbox or approval authority."""
import argparse
import errno
import fcntl
import hashlib
import json
import os
import re
import stat
import subprocess
import sys
from pathlib import Path
from contextlib import contextmanager

# Entry/status must not create source-checkout bytecode as a side effect.
sys.dont_write_bytecode = True
import single_task_state as backend
import single_task_context as contexts
import single_task_scope as entry_scope
import single_task_acceptance as acceptance

STATE = '.symphony/single-task.json'
TASK = re.compile(r'^(- \[)([ xX])(\] (T\d{3}) .+)$', re.M)


class CliError(Exception):
    def __init__(self, exit_code, code, diagnostic=None, *, affected_paths=(), category=None):
        self.exit_code = exit_code
        self.code = code
        self.diagnostic = diagnostic
        self.affected_paths = list(affected_paths)
        self.category = category


MESSAGES = {
    'invalid_arguments': 'Invalid command arguments; use the reviewed command form.',
    'command_unavailable': 'Command unavailable until its approved implementation task.',
    'scope_blocker': 'Scope or completion evidence rejected; retain evidence for review.',
    'state_blocker': 'State evidence rejected; retain it for explicit operator migration or recovery.',
    'acceptance_failed': 'Acceptance failed; review the check and rerun current-content verification.',
    'concurrent_attempt': 'Another command owns the worktree lock; retry after it exits.',
    'persistence_failure': 'Required persistence unavailable; retain evidence for operator recovery.',
}
for _code in ('context_schema', 'invalid_json', 'scope_context', 'dirty_baseline',
              'artifact_commit', 'artifact_digest', 'approval_digest', 'artifact_drift',
              'dependency_table', 'dependency_mismatch', 'prerequisite_evidence'):
    MESSAGES[_code] = 'Task context rejected; review the approved local inputs and retained evidence.'


def response(error=None, state=None, verification='not-run', attempt=None, inspection=None):
    # Legacy metadata is deliberately distinct from journal progress/readiness.
    value = dict(schema_version=1, attempt_id=None, mode=None, progress=None,
                 local_verification=verification, owner_identity_authenticated=False,
                 live_dispatch_verified=False, errors=[])
    if state is not None:
        value['mode'] = state['phase']
        value['progress'] = 'legacy-completed' if state['phase'] == 'handoff-only' else 'legacy-active'
    if attempt is not None:
        value['attempt_id'] = attempt['attempt_id']
        value['progress'] = attempt['progress']
        value['mode'] = ('handoff-only' if attempt['progress'] in
                         ('checkbox-applied', 'completed-awaiting-commit', 'handoff-ready')
                         else attempt['entry_mode'])
    if inspection is not None:
        value.update(inspection)
    if error is not None:
        value['errors'] = [dict(code=error.code, message=MESSAGES[error.code],
                                affected_paths=error.affected_paths, affected_ids=[])]
        if error.category is not None:
            value['errors'][0]['category'] = error.category
        if error.diagnostic:
            print(error.diagnostic, file=sys.stderr)
    print(json.dumps(value, sort_keys=True))


@contextmanager
def lock_only(root):
    """Temporary legacy/empty-state adapter; migrate callers at T010/T017/T023.

    Share T003's canonical inode without adopting legacy JSON or creating SQLite.
    The caller retains this nonblocking lock through its response publication.
    """
    descriptors = []
    try:
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        descriptors.append(root_fd)
        backend._local_filesystem(root_fd)
        symphony = backend._directory(root_fd, '.symphony', create=True)
        descriptors.append(symphony)
        directory = backend._directory(symphony, 'single-task', create=True)
        descriptors.append(directory)
        for name in os.listdir(directory):
            if name == 'contexts':
                contexts = backend._directory(directory, name)
                descriptors.append(contexts)
                for child in os.listdir(contexts):
                    backend._regular(contexts, child, required=True)
            elif name in ('lock', 'journal.sqlite3', 'journal.sqlite3-journal'):
                backend._regular(directory, name, required=True)
            else:
                raise backend.InvalidState('Unsupported state evidence')
        backend._regular(directory, 'lock')
        lock = os.open('lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC,
                       0o600, dir_fd=directory)
        descriptors.append(lock)
        info = os.fstat(lock)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise backend.InvalidState('Unsafe lock')
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            if error.errno in (errno.EACCES, errno.EAGAIN):
                raise backend.StateConflict('Worktree lock held') from error
            raise backend.PersistenceFailure('Lock unavailable') from error
        os.fsync(lock)
        os.fsync(directory)
        legacy = backend._regular(symphony, 'single-task.json')
        if backend._regular(symphony, 'single-task.tmp'):
            raise backend.InvalidState('Unfinished legacy publication')
        journal = backend._regular(directory, 'journal.sqlite3')
        contexts_exist = 'contexts' in os.listdir(directory)
        if (legacy and (journal or contexts_exist)) or (contexts_exist and not journal):
            raise backend.InvalidState('Mixed or incomplete evidence')
        yield legacy, journal
    finally:
        for fd in reversed(descriptors):
            os.close(fd)


def git(root, *args):
    return subprocess.check_output(['git', *args], cwd=root, stderr=subprocess.PIPE).decode()


def path_in_repo(root, value):
    path = Path(value)
    if (path.is_absolute() or not value or '..' in path.parts
            or path.as_posix() != value or path.parts[0] in ('.git', '.symphony')):
        raise ValueError(f'Invalid repository path: {value}')
    if not (root/path).resolve().is_relative_to(root):
        raise ValueError(f'Path escapes repository: {value}')
    return value


def task_lines(text):
    rows = list(TASK.finditer(text))
    ids = [row[4] for row in rows]
    if len(ids) != len(set(ids)):
        raise ValueError('Duplicate task IDs')
    return {row[4]: row for row in rows}


def write_state(root, state):
    directory = os.open(root/'.symphony', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        backend._regular(directory, 'single-task.json')
        fd = os.open('single-task.tmp', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW
                     | os.O_CLOEXEC, 0o600, dir_fd=directory)
        with os.fdopen(fd, 'w') as stream:
            stream.write(json.dumps(state, indent=2) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace('single-task.tmp', 'single-task.json', src_dir_fd=directory, dst_dir_fd=directory)
        os.fsync(directory)
    finally:
        os.close(directory)


def begin(root, args):
    if (root/STATE).exists():
        raise ValueError('Existing task state: resume with verify/complete; do not reset it')
    if git(root, 'status', '--porcelain').strip():
        raise ValueError('Begin requires a clean checkout, including untracked files')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', args.feature):
        raise ValueError('Feature must be a direct specs/ directory name')
    feature = root/'specs'/args.feature
    if feature.is_symlink() or not feature.is_dir():
        raise ValueError('Feature directory missing or symlinked')
    for name in ('spec.md', 'plan.md', 'tasks.md'):
        if not (feature/name).is_file() or (feature/name).is_symlink():
            raise ValueError(f'Missing regular feature artifact: {name}')
    tasks = f'specs/{args.feature}/tasks.md'
    text = (root/tasks).read_text()
    rows = task_lines(text)
    if args.task not in rows or rows[args.task][2] != ' ':
        raise ValueError('Select exactly one existing pending task')
    protected = {f'specs/{args.feature}/{name}' for name in ('spec.md', 'plan.md', 'tasks.md')}
    allowed = sorted({path_in_repo(root, value) for value in args.allow})
    if protected.intersection(allowed):
        raise ValueError('Feature artifacts cannot be allowed implementation paths')
    checks = [json.loads(value) for value in args.check]
    if not all(isinstance(cmd, list) and cmd and all(isinstance(x, str) and x for x in cmd) for cmd in checks):
        raise ValueError('Each check must be a JSON array of command arguments')
    state = dict(feature=args.feature, task=args.task, baseline=git(root, 'rev-parse', 'HEAD').strip(),
                 tasks_path=tasks, tasks_before=text, allowed=allowed, checks=checks,
                 phase='implement', results=[])
    write_state(root, state)
    return state


def scope(root, state):
    baseline = state['baseline']
    git(root, 'merge-base', '--is-ancestor', baseline, 'HEAD')
    changed = set(filter(None, git(root, 'diff', '--no-renames', '--name-only', '-z', baseline, '--').split('\0')))
    staged = set(filter(None, git(root, 'diff', '--cached', '--no-renames', '--name-only', '-z', baseline, '--').split('\0')))
    changed.update(staged)
    commits = git(root, 'rev-list', f'{baseline}..HEAD').splitlines()
    historical = {}
    for commit in commits:
        paths = set(filter(None, git(root, 'diff-tree', '--root', '-m', '-r',
                                    '--no-commit-id', '--no-renames', '--name-only',
                                    '-z', commit, '--').split('\0')))
        historical[commit] = paths
        changed.update(paths)
    changed.update(filter(None, git(root, 'ls-files', '--others', '--exclude-standard', '-z').split('\0')))
    allowed = set(state['allowed']) | {state['tasks_path']}
    extra = changed - allowed
    if extra:
        raise ValueError('Out-of-scope changes: ' + ', '.join(sorted(extra)))
    # Inspect every recorded version too: neither a worktree restore nor a later
    # revert may hide a staged or committed violation of the task contract.
    versions = [('index', staged)] + list(historical.items())
    for revision, paths in versions:
        for value in paths:
            path_in_repo(root, value)
            if revision == 'index':
                entries = git(root, 'ls-files', '--stage', '-z', '--', value).split('\0')
                modes = [entry.split(' ', 1)[0] for entry in entries if entry]
                source = f':{value}'
            else:
                entries = git(root, 'ls-tree', '-z', revision, '--', value).split('\0')
                modes = [entry.split(' ', 1)[0] for entry in entries if entry]
                source = f'{revision}:{value}'
            if any(mode not in ('100644', '100755') for mode in modes):
                raise ValueError(f'Unsafe file type in {revision}: {value}')
            if value == state['tasks_path']:
                if not modes:
                    raise ValueError('Task file deleted in index/history')
                version = git(root, 'show', source)
                rows = task_lines(version)
                row = rows.get(state['task'])
                if row is None or version[:row.start(2)] + ' ' + version[row.end(2):] != state['tasks_before']:
                    raise ValueError('Only the selected task checkbox may change in index/history')
    for value in changed:
        path_in_repo(root, value)
        if (root/value).is_symlink():
            raise ValueError(f'Symlink change not allowed: {value}')
    current = (root/state['tasks_path']).read_text()
    before = state['tasks_before']
    rows = task_lines(current)
    if state['task'] not in rows:
        raise ValueError('Selected task missing')
    row = rows[state['task']]
    normalized = current[:row.start(2)] + ' ' + current[row.end(2):]
    if normalized != before:
        raise ValueError('Only the selected task checkbox may change in tasks.md')
    if row[2] != ' ' and (len(state['results']) != len(state['checks'])
                         or any(result['exit_code'] for result in state['results'])):
        raise ValueError('Completed checkbox has no passing acceptance evidence')
    signature = {}
    for value in sorted(allowed):
        path = root/value
        try:
            metadata = path.lstat()
        except FileNotFoundError:
            signature[value] = dict(type='absent', mode=None, sha256=None)
            continue
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError(f'Expected regular file: {value}')
        data = normalized.encode() if value == state['tasks_path'] else path.read_bytes()
        signature[value] = dict(type='regular', mode='100755' if metadata.st_mode & stat.S_IXUSR else '100644',
                                sha256=hashlib.sha256(data).hexdigest())
    if (row[2] != ' ' or state['phase'] == 'handoff-only') and signature != state.get('verified_files'):
        raise ValueError('Completed task content changed; owner-authorized rework required')
    return current, row, signature


def verify(root, state):
    _, _, signature = scope(root, state)
    previous = list(state['results'])
    results = []
    write_state(root, state)
    for command in state['checks']:
        try:
            run = subprocess.run(command, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        except OSError as error:
            state['last_failure'] = [dict(check_index=len(results), category='unavailable')]
            write_state(root, state)
            raise CliError(4, 'acceptance_failed') from error
        results.append(dict(command=command, exit_code=run.returncode))
        if run.returncode:
            state['last_failure'] = [dict(check_index=len(results) - 1, exit_code=run.returncode)]
            write_state(root, state)
            raise CliError(4, 'acceptance_failed')
    # Checks may themselves mutate files: inspect again before accepting completion.
    # Retain previous completed-task evidence while validating current checks.
    state['results'] = previous
    try:
        _, _, after = scope(root, state)
    except ValueError as error:
        raise CliError(4, 'acceptance_failed', diagnostic(error)) from error
    if after != signature:
        raise CliError(4, 'acceptance_failed')
    state['results'] = results
    state['verified_files'] = signature
    state.pop('last_failure', None)
    state['verified_head'] = git(root, 'rev-parse', 'HEAD').strip()
    write_state(root, state)


def complete(root, state):
    verify(root, state)
    text, row, _ = scope(root, state)
    (root/state['tasks_path']).write_text(text[:row.start(2)] + 'x' + text[row.end(2):])
    state['phase'] = 'handoff-only'
    write_state(root, state)


class SafeParser(argparse.ArgumentParser):
    def error(self, message):
        raise CliError(2, 'invalid_arguments')


def arguments():
    # No argparse diagnostic/help path may echo untrusted argv or bypass locking.
    parser = SafeParser(description=__doc__, add_help=False, allow_abbrev=False)
    sub = parser.add_subparsers(dest='command', required=True)
    for name in ('begin', 'rework', 'inspect', 'verify', 'complete', 'record-commit', 'resume', 'status'):
        command = sub.add_parser(name, add_help=False, allow_abbrev=False)
        if name in ('begin', 'rework'):
            command.add_argument('--context')
        if name == 'begin':
            command.add_argument('--feature')
            command.add_argument('--task')
            command.add_argument('--allow', action='append')
            command.add_argument('--check', action='append')
        if name not in ('begin', 'rework'):
            command.add_argument('--attempt')
        if name in ('verify', 'complete', 'resume'):
            command.add_argument('--review')
        if name == 'record-commit':
            command.add_argument('--commit')
    args = parser.parse_args()
    args.legacy = False
    if args.command == 'begin':
        old = (args.feature, args.task, args.allow, args.check)
        valid = bool(args.context) and not any(item is not None for item in old)
    elif args.command == 'rework':
        valid = bool(args.context)
    elif args.command in ('verify', 'complete'):
        valid = bool(args.attempt and args.review)
    else:
        valid = bool(args.attempt)
        if args.command == 'resume':
            valid = valid and bool(args.review)
        if args.command == 'record-commit':
            valid = valid and bool(args.commit)
    if not valid:
        raise CliError(2, 'invalid_arguments')
    return args


def diagnostic(error):
    # Match only fixed internal guard messages, then emit a fixed regression token.
    message = str(error)
    for token in ('Out-of-scope', 'evidence', 'checkbox', 'rework', 'Unsafe file type', 'clean checkout'):
        if token in message:
            return token
    return None


def read_legacy(root):
    directory = os.open(root/'.symphony', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        backend._regular(directory, 'single-task.json', required=True)
        fd = os.open('single-task.json', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(fd) as stream:
            state = json.load(stream)
        if (not isinstance(state, dict) or state.get('phase') not in ('implement', 'handoff-only')
                or not all(isinstance(state.get(key), str) and state[key]
                           for key in ('feature', 'task', 'baseline', 'tasks_path', 'tasks_before'))
                or not isinstance(state.get('allowed'), list) or not state['allowed']
                or not all(isinstance(path, str) for path in state['allowed'])
                or not isinstance(state.get('checks'), list) or not state['checks']
                or not all(isinstance(cmd, list) and cmd and all(isinstance(x, str) and x for x in cmd)
                           for cmd in state['checks'])
                or not isinstance(state.get('results'), list)):
            raise backend.InvalidState('Invalid legacy evidence')
        path_in_repo(root, state['tasks_path'])
        for path in state['allowed']:
            path_in_repo(root, path)
        return state
    except (ValueError, KeyError, TypeError, UnicodeError) as error:
        raise backend.InvalidState('Unreadable legacy evidence') from error
    finally:
        os.close(directory)


def execute(root, args, legacy, journal):
    if not args.legacy:
        if legacy:
            raise backend.InvalidState('Legacy migration required')
        raise CliError(2, 'command_unavailable')
    if journal:
        raise backend.InvalidState('Journal cannot be used by legacy commands')
    state = None
    try:
        if args.command == 'begin':
            state = begin(root, args)
        else:
            state = read_legacy(root)
            if args.command == 'verify':
                verify(root, state)
            else:
                complete(root, state)
    except ValueError as error:
        code = 'invalid_arguments' if args.command == 'begin' else 'scope_blocker'
        raise CliError(2 if args.command == 'begin' else 3, code, diagnostic(error)) from error
    response(state=state, verification='not-run' if args.command == 'begin' else 'passed')


ERRORS = (CliError, backend.StateConflict, backend.InvalidState, backend.PersistenceFailure,
          OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError, UnicodeError)


def failure(caught):
    if isinstance(caught, CliError):
        error = caught
    elif isinstance(caught, contexts.ContextError):
        error = CliError(3 if caught.code == 'scope_blocker' else 2, caught.code,
                         affected_paths=getattr(caught, 'affected_paths', ()))
    elif isinstance(caught, backend.StateConflict):
        error = CliError(5, 'concurrent_attempt')
    elif isinstance(caught, backend.InvalidState):
        error = CliError(3, 'state_blocker')
    elif isinstance(caught, (backend.PersistenceFailure, OSError)):
        error = CliError(6, 'persistence_failure')
    else:
        error = CliError(3, 'state_blocker')
    response(error, verification='failed' if error.exit_code == 4 else 'not-run')
    return error.exit_code


def load_review(path):
    """Read one regular local JSON object without ambiguous keys or numbers."""
    def unique(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError('Duplicate review field')
            value[key] = item
        return value

    def invalid_number(value):
        raise ValueError('Nonfinite review number')

    descriptor = None
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            raise ValueError('Review must be regular')
        with os.fdopen(descriptor, 'rb') as stream:
            descriptor = None
            return json.loads(stream.read().decode('utf-8'), object_pairs_hook=unique,
                              parse_constant=invalid_number)
    except (OSError, ValueError, UnicodeError):
        raise CliError(3, 'scope_blocker') from None
    finally:
        if descriptor is not None:
            os.close(descriptor)


def validate_completion_phase(root, store, attempt):
    """Retain exact entry bytes before intent and the immutable seal afterward."""
    task_path = f"specs/{attempt['context']['feature']}/tasks.md"
    current = entry_scope.read_regular(root, task_path)
    if attempt['progress'] in ('active', 'acceptance-recorded'):
        if current != attempt['tasks_bytes']:
            raise CliError(3, 'scope_blocker', affected_paths=[task_path])
        return
    if attempt['progress'] not in ('completed-awaiting-commit', 'handoff-ready'):
        raise backend.InvalidState('Prepared transition requires explicit recovery')
    intents = [event for event in store.read_events(attempt['attempt_id'])
               if event['kind'] == 'completion-intent']
    if len(intents) != 1:
        raise backend.InvalidState('Completed attempt requires one durable intent')
    intent = intents[0]['payload']
    inspection = entry_scope.prepare_inspection(root, attempt)
    if (current != bytes.fromhex(intent['tasks_after_hex']) or
            hashlib.sha256(current).hexdigest() != intent['tasks_after_sha256'] or
            inspection['content_manifest'] != intent['content_manifest'] or
            inspection['content_manifest_digest'] != intent['content_manifest_digest'] or
            inspection['context_digest'] != intent['context_digest']):
        raise CliError(3, 'scope_blocker', affected_paths=[task_path])


def main():
    try:
        try:
            args = arguments()
            parse_error = None
        except CliError as error:
            args = None
            parse_error = error
        root = Path(os.fsdecode(entry_scope.git(Path.cwd(), 'rev-parse', '--show-toplevel').stdout.strip())).resolve()
        if parse_error is None and args.command == 'begin':
            prepared = {}

            def validate_entry():
                context = contexts.load_context(args.context)
                if context['mode'] != 'implement':
                    raise contexts.ContextError('context_schema')
                scope = entry_scope.validate_entry_scope(root, context)
                contexts.validate_reviewed_artifacts(root, context)
                contexts.validate_prerequisites(root, context)
                prepared.update(context=context, scope=scope)

            with backend.open_store(root, preflight=validate_entry) as store:
                active = store.active_attempt()
                if active is not None:
                    store.append_event(active['attempt_id'], 'blocker', {'code': 'state_blocker'})
                    raise CliError(3, 'state_blocker')
                import uuid
                attempt_id = uuid.uuid4().hex
                scope = prepared['scope']
                with store.transaction():
                    store.create_attempt(attempt_id=attempt_id, context=prepared['context'],
                                         baseline=scope['baseline_commit'], tasks_bytes=scope['tasks_bytes'],
                                         entry_mode='implement',
                                         approval_context_digest=prepared['context']['approval']['context_digest'])
                    store.append_event(attempt_id, 'entry', {'context_digest': prepared['context']['approval']['context_digest']})
                response(attempt=store.read_attempt(attempt_id))
            return 0
        if parse_error is None and args.command == 'inspect':
            with backend.open_store(root, require_existing=True) as store:
                attempt = store.read_attempt(args.attempt)
                try:
                    inspection = entry_scope.prepare_inspection(root, attempt)
                except contexts.ContextError as error:
                    raise CliError(3, 'scope_blocker',
                                   affected_paths=getattr(error, 'affected_paths', ())) from None
                response(attempt=attempt, inspection=inspection)
            return 0
        if parse_error is None and args.command in ('verify', 'complete'):
            with backend.open_store(root, require_existing=True, writable_existing=True) as store:
                attempt = None
                try:
                    attempt = store.read_attempt(args.attempt)
                    validate_completion_phase(root, store, attempt)
                    review = load_review(args.review)
                    acceptance.execute_acceptance(root, attempt, review, store)
                    if args.command == 'complete' and attempt['progress'] in ('active', 'acceptance-recorded'):
                        # No outer SQL transaction: durable intent must be visible
                        # independently before the atomic marker publication.
                        backend.publish_completion(root, store, args.attempt)
                    response(attempt=store.read_attempt(args.attempt), verification='passed')
                    return 0
                except acceptance.AcceptanceFailure as error:
                    store.append_event(args.attempt, 'blocker',
                                       {'code': 'acceptance_failed', 'category': error.category})
                    return failure(CliError(4, 'acceptance_failed', category=error.category))
                except ERRORS as error:
                    if attempt is not None and isinstance(error, (CliError, contexts.ContextError, backend.InvalidState)):
                        code = error.code if isinstance(error, (CliError, contexts.ContextError)) else 'state_blocker'
                        store.append_event(args.attempt, 'blocker', {'code': code})
                    return failure(error)
        if parse_error is None and args.command == 'status':
            with backend.open_store(root, require_existing=True) as store:
                response(attempt=store.read_attempt(args.attempt))
            return 0
        with lock_only(root) as (legacy, journal):
            try:
                if parse_error:
                    raise parse_error
                if legacy:
                    raise backend.InvalidState('Legacy migration required')
                if not journal:
                    raise CliError(2, 'command_unavailable')
            except ERRORS as error:
                return failure(error)
            # No mutable journal/task data is carried across this lock transition.
            # T003 revalidates all evidence under its own full-lifetime lock.
        with backend.open_store(root, require_existing=True):
            return failure(CliError(2, 'command_unavailable'))
    except ERRORS as error:
        return failure(error)


if __name__ == '__main__':
    sys.exit(main())
