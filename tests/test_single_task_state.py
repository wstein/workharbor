"""T002's reviewed red matrix; journal/CLI behavior is still unimplemented.

Only disposable T001 repositories receive proposed CLI calls. The first five
cases currently stop at begin's unsupported --context interface. Their remaining
assertions are retained for T003/T004; a verifier match does not exercise them.
SQLite/file helpers below inspect private persistence, not a public Python API.
"""
import copy
import json
import selectors
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from contextlib import closing
from pathlib import Path

from test_single_task import (
    GIT, SCRIPT, SingleTaskFixture, canonical_bytes, context_digest, digest,
)


class SingleTaskStateTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.fixture = SingleTaskFixture(directory.name)
        self.root = self.fixture.root
        # Ignored input/state must not make a valid clean entry dirty. Bind the
        # fixture's reviewed artifacts to this actual local preparation commit.
        (self.root/'.gitignore').write_text('.symphony/\n', encoding='utf-8')
        self.fixture.git('add', '.gitignore')
        self.fixture.git('commit', '-m', 'test: isolate synthetic task state')
        self.baseline = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        for artifact in self.fixture.reviewed_artifacts.values():
            artifact['commit'] = self.baseline
        self.tasks = self.fixture.feature/'tasks.md'
        self.before_tasks = self.tasks.read_bytes()
        self.before_index = self.fixture.git('ls-tree', 'HEAD', '--',
                                             'specs/synthetic/tasks.md').stdout
        self.state = self.root/'.symphony/single-task'
        self.context = self.fixture.context('T001')
        self.context_path = self.root/'.symphony/input.json'
        self.context_path.parent.mkdir()
        self.write_context(self.context)
        self.assertEqual('', self.fixture.git('status', '--porcelain').stdout)
        self.assertEqual([], self.context['dependencies'])
        self.assertEqual([], self.context['prerequisite_evidence'])
        self.assertEqual(context_digest(self.context), self.context['approval']['context_digest'])
        for artifact in self.context['reviewed_artifacts'].values():
            self.assertEqual(artifact['sha256'], digest(self.fixture.git(
                'show', artifact['commit'] + ':' + artifact['path']).stdout.encode()))

    def write_context(self, value):
        self.context_path.write_bytes(canonical_bytes(value))

    def cli(self, *args, injection=None, script=SCRIPT):
        argv = [sys.executable, str(script), *args]
        if injection is not None:
            argv = [sys.executable, '-c', injection, str(SCRIPT), *args]
        result = subprocess.run(argv, cwd=self.root, env=self.fixture.env,
                                capture_output=True, text=True, timeout=10)
        # Raw synthetic-only observations are separate from verifier acceptance.
        evidence = self.fixture.private/'t002-cli-evidence.jsonl'
        evidence.parent.mkdir(exist_ok=True)
        with evidence.open('a', encoding='utf-8') as stream:
            stream.write(json.dumps({'test_id': self.id(), 'argv': argv,
                                     'exit': result.returncode, 'stdout': result.stdout,
                                     'stderr': result.stderr}) + '\n')
        return result

    def unchanged_tasks(self):
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.assertEqual(self.before_index, self.fixture.git(
            'ls-tree', 'HEAD', '--', 'specs/synthetic/tasks.md').stdout)
        self.assertEqual('', self.fixture.git('diff', '--cached', '--',
                                             'specs/synthetic/tasks.md').stdout)
        self.assertEqual('', self.fixture.git('diff', '--', 'specs/synthetic/tasks.md').stdout)

    def snapshot(self, directory=None):
        directory = self.state if directory is None else directory
        return {path.relative_to(directory).as_posix(): path.read_bytes()
                for path in directory.rglob('*') if path.is_file()}

    def retained(self, before, directory=None):
        after = self.snapshot(directory)
        for path, content in before.items():
            self.assertIn(path, after)
            self.assertEqual(content, after[path])
        # Acquiring the mandatory command lock may create its file. It grants
        # no permission to create a new journal/context or replace evidence.
        for path in after.keys() - before.keys():
            self.assertIn('lock', Path(path).name.lower())

    def assert_exit(self, result, expected, command):
        self.unchanged_tasks()
        if result.returncode == 2 and expected != 2:
            # Only the reviewed unsupported interface qualifies, not arbitrary
            # exit 2 (missing tools, fixture errors or persistence errors).
            self.assertIn('usage:', result.stderr)
            if command == 'begin':
                self.assertIn('--feature', result.stderr)
            else:
                self.assertIn("invalid choice: 'status'", result.stderr)
            self.fail(f'T002/{self._testMethodName}: {command} exit 2; expected {expected}')
        self.assertEqual(expected, result.returncode, result.stderr)

    def response(self, result):
        try:
            value = json.loads(result.stdout)
        except json.JSONDecodeError:
            self.assertEqual(2, result.returncode)
            self.assertIn('usage:', result.stderr)
            self.fail(f'T002/{self._testMethodName}: structured JSON response absent')
        self.assertIsInstance(value, dict)
        self.assertEqual(1, value['schema_version'])
        for field in ('attempt_id', 'mode', 'progress', 'local_verification'):
            self.assertIn(field, value)
        self.assertIs(False, value['owner_identity_authenticated'])
        self.assertIs(False, value['live_dispatch_verified'])
        return value

    def errors(self, result):
        value = self.response(result)
        self.assertTrue(value['errors'])
        for error in value['errors']:
            self.assertIsInstance(error['code'], str)
            self.assertTrue(error['code'])
            self.assertIsInstance(error['message'], str)
            self.assertTrue(error['message'])
            self.assertIn('affected_paths', error)
            self.assertIn('affected_ids', error)
            self.assertIsInstance(error['affected_paths'], list)
            self.assertIsInstance(error['affected_ids'], list)
        return value['errors']

    def begin(self):
        result = self.cli('begin', '--context', str(self.context_path))
        self.assert_exit(result, 0, 'begin')
        value = self.response(result)
        self.assertEqual('implement', value['mode'])
        self.assertEqual('active', value['progress'])
        self.assertIsInstance(value['attempt_id'], str)
        self.assertTrue(value['attempt_id'])
        return value['attempt_id']

    def journal(self):
        # Locate SQLite by its actual file header rather than prescribing a
        # database basename. Fail if persistence is absent or ambiguous.
        paths = [path for path in self.state.rglob('*') if path.is_file()
                 and path.read_bytes()[:16] == b'SQLite format 3\x00']
        self.assertEqual(1, len(paths))
        return paths[0]

    def journal_rows(self):
        with sqlite3.connect(f'file:{self.journal()}?mode=ro', uri=True) as connection:
            connection.row_factory = sqlite3.Row
            tables = [row[0] for row in connection.execute(
                "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")]
            rows = {}
            for name in tables:
                quoted = '"' + name.replace('"', '""') + '"'
                rows[name] = [dict(row) for row in connection.execute('SELECT * FROM ' + quoted)]
            return rows

    def records(self):
        rows = self.journal_rows()
        # Semantic column adapter: no import of a not-yet-existing state module.
        attempts = [row for table in rows.values() for row in table if 'entry_mode' in row]
        events = [row for table in rows.values() for row in table
                  if 'kind' in row and 'payload_digest' in row]
        self.assertEqual(1, len(attempts))
        self.assertTrue(events)
        return attempts[0], events

    def sequence(self, event):
        field = 'sequence' if 'sequence' in event else 'seq'
        self.assertIsInstance(event[field], int)
        return event[field]

    def context_copies(self):
        found = {}
        for path in self.state.rglob('*'):
            if path.is_file():
                try:
                    value = json.loads(path.read_bytes())
                except (ValueError, UnicodeDecodeError):
                    continue
                if isinstance(value, dict) and value.get('approval', {}).get(
                        'context_digest') == self.context['approval']['context_digest']:
                    found[path.relative_to(self.state).as_posix()] = path.read_bytes()
        self.assertTrue(found, 'A durable copied context is required')
        return found

    def holder(self):
        # Resolve the created advisory-lock file; never create a substitute lock.
        locks = [p for p in self.state.rglob('*') if p.is_file() and 'lock' in p.name.lower()
                 and not p.name.endswith(('-wal', '-shm'))]
        self.assertEqual(1, len(locks))
        program = (
            'import fcntl,sys\n'
            'with open(sys.argv[1], "r+b") as lock:\n'
            ' fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)\n'
            ' print("locked", flush=True)\n'
            ' sys.stdin.readline()\n'
        )
        process = subprocess.Popen([sys.executable, '-c', program, str(locks[0])],
                                   cwd=self.root, env=self.fixture.env, text=True,
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE)
        def cleanup():
            if process.poll() is None:
                process.kill()
            process.communicate(timeout=5)
        self.addCleanup(cleanup)
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            self.assertTrue(selector.select(timeout=5), 'Lock holder did not signal readiness')
            self.assertEqual('locked\n', process.stdout.readline())
        return process

    def release(self, process):
        stdout, stderr = process.communicate('\n', timeout=5)
        self.assertEqual(0, process.returncode, stderr)
        self.assertEqual('', stdout)

    def test_one_lock_owner(self):
        attempt = self.begin()
        owner = self.holder()
        before = self.snapshot()
        result = self.cli('status', '--attempt', attempt)
        self.assertEqual(5, result.returncode)
        self.errors(result)
        self.assertEqual(before, self.snapshot())
        self.unchanged_tasks()
        self.release(owner)

    def test_lock_released_after_exit(self):
        attempt = self.begin()
        owner = self.holder()
        self.release(owner)
        fresh_owner = self.holder()  # Independent process proves actual reacquisition.
        self.release(fresh_owner)
        before = self.records()
        result = self.cli('status', '--attempt', attempt)
        self.assertEqual(0, result.returncode, result.stderr)
        value = self.response(result)
        self.assertEqual(attempt, value['attempt_id'])
        self.assertEqual('active', value['progress'])
        self.assertEqual(before, self.records())
        self.unchanged_tasks()

    def mismatched_context(self):
        changed = copy.deepcopy(self.context)
        changed['allowed_paths'] = ['different.md']
        changed['approval']['context_digest'] = context_digest(changed)
        self.write_context(changed)
        return changed

    def test_transaction_rollback(self):
        self.begin()
        before = self.journal_rows()
        copies = self.context_copies()
        self.mismatched_context()
        # Inject a real SQLite failure after a journal write, inside the CLI
        # process. Rollback must cover both events and derived progress. No
        # production environment switch or fake persistence backend is used.
        injection = '''import runpy, sqlite3, sys
connect = sqlite3.connect
class FailingConnection(sqlite3.Connection):
    def execute(self, sql, *args, **kwargs):
        result = super().execute(sql, *args, **kwargs)
        if sql.lstrip().upper().startswith(('INSERT', 'UPDATE', 'DELETE')):
            print('T002 injected after SQLite write', file=sys.stderr, flush=True)
            raise sqlite3.OperationalError('synthetic transaction failure')
        return result
    def executemany(self, sql, *args, **kwargs):
        result = super().executemany(sql, *args, **kwargs)
        if sql.lstrip().upper().startswith(('INSERT', 'UPDATE', 'DELETE')):
            print('T002 injected after SQLite write', file=sys.stderr, flush=True)
            raise sqlite3.OperationalError('synthetic transaction failure')
        return result
    def cursor(self, *args, **kwargs):
        kwargs['factory'] = FailingCursor
        return super().cursor(*args, **kwargs)
class FailingCursor(sqlite3.Cursor):
    def execute(self, sql, *args, **kwargs):
        result = super().execute(sql, *args, **kwargs)
        if sql.lstrip().upper().startswith(('INSERT', 'UPDATE', 'DELETE')):
            print('T002 injected after SQLite write', file=sys.stderr, flush=True)
            raise sqlite3.OperationalError('synthetic transaction failure')
        return result
    def executemany(self, sql, *args, **kwargs):
        result = super().executemany(sql, *args, **kwargs)
        if sql.lstrip().upper().startswith(('INSERT', 'UPDATE', 'DELETE')):
            print('T002 injected after SQLite write', file=sys.stderr, flush=True)
            raise sqlite3.OperationalError('synthetic transaction failure')
        return result
def failing_connect(*args, **kwargs):
    kwargs['factory'] = FailingConnection
    return connect(*args, **kwargs)
sqlite3.connect = failing_connect
sys.argv = sys.argv[1:]
sys.path.insert(0, str(__import__('pathlib').Path(sys.argv[0]).parent))
runpy.run_path(sys.argv[0], run_name='__main__')
'''
        result = self.cli('begin', '--context', str(self.context_path), injection=injection)
        self.assertIn('T002 injected after SQLite write', result.stderr)
        self.assertEqual(6, result.returncode, result.stderr)
        self.errors(result)
        self.assertEqual(before, self.journal_rows())
        self.assertEqual(copies, self.context_copies())
        self.unchanged_tasks()

    def test_append_only_events(self):
        attempt = self.begin()
        previous, events = self.records()
        status = self.response(self.cli('status', '--attempt', attempt))
        self.mismatched_context()
        result = self.cli('begin', '--context', str(self.context_path))
        self.assertIn(result.returncode, (2, 3))
        self.errors(result)
        current, appended = self.records()
        ordered = sorted(events, key=self.sequence)
        new_ordered = sorted(appended, key=self.sequence)
        self.assertEqual(ordered, new_ordered[:len(ordered)])
        self.assertGreater(len(new_ordered), len(ordered))
        sequences = [self.sequence(event) for event in new_ordered]
        self.assertEqual(sorted(set(sequences)), sequences)
        self.assertGreater(sequences[-1], self.sequence(ordered[-1]))
        self.assertEqual('blocker', new_ordered[-1]['kind'])
        for event in new_ordered:
            self.assertRegex(event['payload_digest'], r'^[0-9a-f]{64}$')
        # A failed operation appends a blocker without advancing execution mode
        # or progress. The persisted and derived status must agree.
        self.assertEqual(previous, current)
        after = self.response(self.cli('status', '--attempt', attempt))
        self.assertEqual(status['progress'], after['progress'])
        self.assertEqual(status['mode'], after['mode'])
        self.unchanged_tasks()

    def test_immutable_context(self):
        attempt = self.begin()
        row, events = self.records()
        copies = self.context_copies()
        self.assertEqual('implement', row['entry_mode'])
        self.assertIn(self.baseline, row.values())
        self.assertIn(self.context['approval']['context_digest'], row.values())
        self.assertTrue(any(value in (self.before_tasks, self.before_tasks.decode())
                            for value in row.values()), 'Immutable baseline task bytes are required')
        self.mismatched_context()
        result = self.cli('begin', '--context', str(self.context_path))
        self.assertIn(result.returncode, (2, 3))
        self.errors(result)
        retained, new_events = self.records()
        self.assertEqual(row, retained)
        self.assertEqual(copies, self.context_copies())
        self.assertEqual(sorted(events, key=self.sequence),
                         sorted(new_events, key=self.sequence)[:len(events)])
        status = self.response(self.cli('status', '--attempt', attempt))
        self.assertEqual(attempt, status['attempt_id'])
        self.assertEqual('implement', status['mode'])
        self.unchanged_tasks()

    def test_corrupt_state_retained(self):
        self.state.mkdir()
        path = self.state/'journal.sqlite3'
        path.write_bytes(b'SQLite format 3\x00deliberately truncated journal')
        before = self.snapshot()
        result = self.cli('status', '--attempt', 'synthetic-corrupt')
        self.retained(before)
        self.assert_exit(result, 3, 'status')
        self.errors(result)
        self.retained(before)
        self.unchanged_tasks()

    def test_unsupported_state_retained(self):
        self.state.mkdir()
        with sqlite3.connect(self.state/'journal.sqlite3') as connection:
            connection.execute('PRAGMA user_version=2147483647')
            connection.execute('CREATE TABLE retained_evidence (payload BLOB NOT NULL)')
            connection.execute('INSERT INTO retained_evidence VALUES (?)', (b'keep unsupported evidence',))
        before = self.snapshot()
        result = self.cli('status', '--attempt', 'synthetic-unsupported')
        self.retained(before)
        self.assert_exit(result, 3, 'status')
        self.errors(result)
        self.retained(before)
        self.unchanged_tasks()

    def test_legacy_state_retained(self):
        # Reviewed fixture bytes preserve actual legacy behavior without an
        # orphaned Git object dependency in fresh/shallow CI checkouts.
        source = (SCRIPT.parent.parent/'tests/fixtures/single-task/legacy_single_task.py').read_bytes()
        self.assertEqual('7c50b5170e31eed9c5c0dbeb047919c8845f6c266a32def55868a4ab9b863bc4',
                         digest(source))
        legacy_script = self.fixture.private/'legacy_single_task.py'
        legacy_script.write_bytes(source)
        legacy = self.cli('begin', '--feature', 'synthetic', '--task', 'T001',
                          '--allow', 'guide.md', '--check', '["git", "diff", "--check"]',
                          script=legacy_script)
        self.assertEqual(0, legacy.returncode, legacy.stderr)
        path = self.root/'.symphony/single-task.json'
        value = json.loads(path.read_bytes())
        self.assertEqual(self.baseline, value['baseline'])
        self.assertEqual('T001', value['task'])
        self.assertEqual('implement', value['phase'])
        value['retained_synthetic_evidence'] = {'exit': 0, 'reference': 'synthetic:legacy'}
        path.write_text(json.dumps(value), encoding='utf-8')
        before = self.snapshot(self.root/'.symphony')
        result = self.cli('status', '--attempt', 'synthetic-legacy')
        self.retained(before, self.root/'.symphony')
        self.assert_exit(result, 3, 'status')
        errors = self.errors(result)
        self.assertIn('migrat', json.dumps(errors).lower())
        self.retained(before, self.root/'.symphony')
        self.unchanged_tasks()

    def test_sanitized_structured_error(self):
        secret = 'synthetic-secret-T002-context'
        malformed = copy.deepcopy(self.context)
        malformed['approval']['owner_ref'] = secret
        malformed['checks'][0]['argv'] = ['synthetic-command', secret]
        malformed['unknown_secret_field'] = secret
        self.write_context(malformed)
        before = self.snapshot(self.root/'.symphony')
        result = self.cli('begin', '--context', str(self.context_path))
        self.assertEqual(2, result.returncode)
        self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertNotIn('synthetic-command', result.stdout + result.stderr)
        self.assertNotIn(self.context_path.read_text(), result.stdout + result.stderr)
        self.retained(before, self.root/'.symphony')
        self.unchanged_tasks()
        errors = self.errors(result)
        repeated = self.cli('begin', '--context', str(self.context_path))
        self.assertEqual(2, repeated.returncode)
        self.assertEqual([error['code'] for error in errors],
                         [error['code'] for error in self.errors(repeated)])
        self.assertNotIn(secret, repeated.stdout + repeated.stderr)
        self.retained(before, self.root/'.symphony')
        self.unchanged_tasks()

    def test_invalid_entry_preserves_tasks(self):
        context = copy.deepcopy(self.context)
        context['task_id'] = 'T999'
        context['approval']['context_digest'] = context_digest(context)
        self.write_context(context)
        # Invalid entry must neither adopt nor overwrite prior ignored evidence.
        marker = self.root/'.symphony/prior-evidence.bin'
        marker.write_bytes(b'prior local evidence\x00retain exactly')
        before = self.snapshot(self.root/'.symphony')
        index = self.fixture.git('diff', '--cached', '--raw').stdout
        result = self.cli('begin', '--context', str(self.context_path))
        self.assertEqual(2, result.returncode)
        self.retained(before, self.root/'.symphony')
        self.assertEqual(index, self.fixture.git('diff', '--cached', '--raw').stdout)
        self.unchanged_tasks()
        self.errors(result)
        self.retained(before, self.root/'.symphony')
        self.unchanged_tasks()


sys.path.insert(0, str(SCRIPT.parent))


class TaskRecoveryTests(unittest.TestCase):
    """T018 proposal: real CLI fixtures; subprocess exits model interruption.

    Crash hooks run only in disposable child processes. No future event schema,
    commit-binding implementation or authenticated approval is manufactured.
    """
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.tasks = self.fixture.feature/'tasks.md'
        (self.root/'.gitignore').write_text('.symphony/\n')
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: recovery fixture baseline')
        self.context = self.fixture.context()
        self.log = self.fixture.private/'checks.jsonl'
        self.fail_check = self.fixture.private/'fail-check'
        program = ('import pathlib,sys; '
                   'p=pathlib.Path(sys.argv[1]); '
                   'p.open("a").write(sys.argv[3]+"\\n"); '
                   'sys.exit(1 if pathlib.Path(sys.argv[2]).exists() else 0)')
        self.context['checks'] = [
            {'id': name, 'argv': [sys.executable, '-B', '-c', program,
                                  str(self.log), str(self.fail_check), name],
             'cwd': '.', 'timeout_seconds': 5, 'expected_exit': 0}
            for name in ('first', 'second')]
        self.context['approval']['context_digest'] = context_digest(self.context)
        context_path = self.fixture.private/'context.json'
        context_path.write_bytes(canonical_bytes(self.context))
        begun = self.cli('begin', '--context', str(context_path))
        self.assertEqual(0, begun.returncode, 'T010 begin must succeed before a T018 body')
        self.attempt_id = self.outcome(begun)['attempt_id']
        self.before = self.tasks.read_bytes()
        self.after = self.before.replace(b'[ ] T001', b'[x] T001')
        (self.root/'guide.md').write_bytes(b'Recovery output\n')
        self.review_path = self.fixture.private/'review.json'
        self.write_review()
        self.initial_attempt, self.initial_events = self.saved()
        self.assertEqual('active', self.initial_attempt['progress'])
        self.assertEqual([], self.checks())

    def cli(self, *args, injection=None):
        argv = [sys.executable, '-B', str(SCRIPT), *args]
        if injection is not None:
            argv = [sys.executable, '-B', '-c', injection, str(SCRIPT), *args]
        return subprocess.run(argv, cwd=self.root, env=self.fixture.env,
                              capture_output=True, text=True, timeout=15)

    def outcome(self, result):
        self.assertEqual('', result.stderr)
        value = json.loads(result.stdout)
        self.assertEqual(1, value['schema_version'])
        self.assertIs(False, value['owner_identity_authenticated'])
        self.assertIs(False, value['live_dispatch_verified'])
        return value

    def checks(self):
        return self.log.read_text().splitlines() if self.log.exists() else []

    def write_review(self, **changes):
        review = self.fixture.review(self.context)
        review.update(attempt_id=self.attempt_id, **changes)
        self.review_path.write_bytes(canonical_bytes(review))

    def saved(self):
        import single_task_state as backend
        with backend.open_store(self.root, require_existing=True) as store:
            return store.read_attempt(self.attempt_id), store.read_events(self.attempt_id)

    def call(self, command, expected=0, *extra, injection=None):
        args = [command, '--attempt', self.attempt_id]
        if command in ('verify', 'complete', 'resume'):
            args += ['--review', str(self.review_path)]
        result = self.cli(*args, *extra, injection=injection)
        value = self.outcome(result)
        code = value['errors'][0]['code'] if value['errors'] else 'none'
        self.assertEqual(expected, result.returncode,
                         f'T018/{self._testMethodName}: {command} expected exit {expected}; '
                         f'actual {result.returncode} ({code})')
        return value

    def complete(self):
        value = self.call('complete')
        self.assertEqual('completed-awaiting-commit', value['progress'])
        self.assertEqual(self.after, self.tasks.read_bytes())
        return value

    def commit(self):
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: actual selected implementation')
        return self.fixture.git('rev-parse', 'HEAD').stdout.strip()

    def bind(self, commit, expected=0):
        return self.call('record-commit', expected, '--commit', commit)

    def retained(self, attempt, events):
        actual, current = self.saved()
        for field in ('attempt_id', 'baseline', 'tasks_bytes', 'entry_mode', 'context',
                      'context_copy_sha256', 'approval_context_digest', 'predecessor'):
            self.assertEqual(attempt[field], actual[field])
        self.assertEqual(events, current[:len(events)])
        self.assertEqual(1, len({event['attempt_id'] for event in current}))
        return actual, current

    def crash(self, boundary):
        # Hooks use existing owners and actual durable operations, never a new API.
        injection = r"""import os,pathlib,runpy,stat,sys
sys.path.insert(0,str(pathlib.Path(sys.argv[1]).parent))
import single_task_state as state
import single_task_acceptance as acceptance
boundary=sys.argv.pop(2)
append=state._Store.append_event
replace=state.os.replace
fsync=state.os.fsync
execute=acceptance.execute_acceptance
def append_hook(self,attempt_id,kind,*args,**kwargs):
    if boundary=='before-intent' and kind=='completion-intent': os._exit(91)
    result=append(self,attempt_id,kind,*args,**kwargs)
    if (boundary,kind) in {('after-check-result','check-result'),
                          ('after-acceptance','acceptance'),
                          ('after-intent','completion-intent'),
                          ('after-checkbox','checkbox-applied'),
                          ('after-completion','completion')}: os._exit(91)
    return result
def replace_hook(*args,**kwargs):
    result=replace(*args,**kwargs)
    if boundary=='after-replace': os._exit(91)
    return result
def fsync_hook(fd):
    result=fsync(fd)
    tasks=pathlib.Path('specs/synthetic/tasks.md')
    if (boundary=='after-directory-flush' and stat.S_ISDIR(os.fstat(fd).st_mode)
            and b'[x] T001' in tasks.read_bytes()): os._exit(91)
    return result
def execute_hook(*args,**kwargs):
    if boundary=='before-acceptance': os._exit(91)
    return execute(*args,**kwargs)
state._Store.append_event=append_hook
state.os.replace=replace_hook
state.os.fsync=fsync_hook
acceptance.execute_acceptance=execute_hook
sys.argv=sys.argv[1:]
runpy.run_path(sys.argv[0],run_name='__main__')
"""
        result = self.cli(boundary, 'complete', '--attempt', self.attempt_id,
                          '--review', str(self.review_path), injection=injection)
        self.assertEqual(91, result.returncode, 'Crash must hit its named boundary')
        self.assertEqual('', result.stdout)
        self.assertEqual('', result.stderr)
        attempt, events = self.saved()
        expected = {
            'before-acceptance': ('active', self.before, 0, False),
            'after-check-result': ('active', self.before, 1, False),
            'after-acceptance': ('acceptance-recorded', self.before, 2, False),
            'before-intent': ('acceptance-recorded', self.before, 2, False),
            'after-intent': ('completion-prepared', self.before, 2, True),
            'after-replace': ('completion-prepared', self.after, 2, True),
            'after-directory-flush': ('completion-prepared', self.after, 2, True),
            'after-checkbox': ('checkbox-applied', self.after, 2, True),
            'after-completion': ('completed-awaiting-commit', self.after, 2, True),
        }[boundary]
        self.assertEqual(expected[0], attempt['progress'])
        self.assertEqual(expected[1], self.tasks.read_bytes())
        self.assertEqual(expected[2], len(self.checks()))
        intents = [event for event in events if event['kind'] == 'completion-intent']
        self.assertEqual(int(expected[3]), len(intents))
        if intents:
            intent = intents[0]['payload']
            self.assertEqual(self.before, bytes.fromhex(intent['tasks_before_hex']))
            self.assertEqual(self.after, bytes.fromhex(intent['tasks_after_hex']))
            self.assertEqual(digest(self.before), intent['tasks_before_sha256'])
            self.assertEqual(digest(self.after), intent['tasks_after_sha256'])
        self.retained(self.initial_attempt, self.initial_events)
        return attempt, events

    def recover_boundary(self, boundary):
        attempt, events = self.crash(boundary)
        old_checks = self.checks()
        if boundary == 'after-completion':
            self.call('resume', 3)
            self.assertEqual(self.after, self.tasks.read_bytes())
            self.retained(attempt, events)
            return
        # Independently query the on-disk journal immediately before any marker
        # replace. Eventual correct state alone cannot prove durable write order.
        probe = r"""import os,pathlib,runpy,sqlite3,sys
sys.path.insert(0,str(pathlib.Path(sys.argv[1]).parent))
import single_task_state as state
replace=state.os.replace
boundary=sys.argv[2]
checklog=pathlib.Path(sys.argv[3])
attempt_id=sys.argv[6]
def checked_replace(*args,**kwargs):
    connection=sqlite3.connect('file:.symphony/single-task/journal.sqlite3?mode=ro',uri=True)
    try:
        events=connection.execute('SELECT kind,payload FROM events WHERE attempt_id=? ORDER BY sequence', (attempt_id,)).fetchall()
        kinds=[row[0] for row in events]
        assert kinds.count('completion-intent')==1, 'marker write requires one durable intent'
        assert kinds.count('acceptance')>=2 or boundary in ('before-acceptance','after-check-result'), 'fresh acceptance required'
        assert checklog.read_text().splitlines()[-2:]==['first','second']
    finally: connection.close()
    return replace(*args,**kwargs)
state.os.replace=checked_replace
sys.argv=[sys.argv[1],*sys.argv[4:]]
runpy.run_path(sys.argv[0],run_name='__main__')
"""
        result = self.cli(boundary, str(self.log), 'resume', '--attempt', self.attempt_id,
                          '--review', str(self.review_path), injection=probe)
        value = self.outcome(result)
        self.assertEqual(0, result.returncode,
                         f'T018/{self._testMethodName}: resume expected exit 0; '
                         f'actual {result.returncode} (' +
                         (value['errors'][0]['code'] if value['errors'] else 'none') + ')')
        self.assertEqual(self.attempt_id, value['attempt_id'])
        self.assertEqual('handoff-only', value['mode'])
        self.assertEqual('completed-awaiting-commit', value['progress'])
        self.assertEqual('passed', value['local_verification'])
        self.assertEqual(old_checks + ['first', 'second'], self.checks())
        self.assertEqual(self.after, self.tasks.read_bytes())
        current, retained = self.retained(attempt, events)
        self.assertEqual(attempt['entry_mode'], current['entry_mode'])
        self.assertEqual(1, sum(event['kind'] == 'completion-intent' for event in retained))
        self.assertEqual(1, sum(event['kind'] == 'completion' for event in retained))
        # Awaiting-commit may recover, but cannot assert handoff-ready.
        self.assertNotEqual('handoff-ready', value['progress'])

    def blocked_recovery(self, boundary, mutation, expected):
        attempt, events = self.crash(boundary)
        if mutation == 'review':
            self.write_review(content_manifest_digest='0' * 64)
        elif mutation == 'missing-review':
            self.review_path.unlink()
        elif mutation == 'content':
            (self.root/'guide.md').write_bytes(b'Changed sealed bytes\n')
            self.write_review()  # Fresh review cannot adopt changed sealed content.
        elif mutation == 'task-bytes':
            self.tasks.write_bytes(self.tasks.read_bytes() + b'\n')
            self.write_review()
        elif mutation == 'check':
            self.fail_check.write_bytes(b'fail')
        before = self.tasks.read_bytes()
        old_checks = self.checks()
        self.call('resume', expected)
        self.assertEqual(before, self.tasks.read_bytes())
        current, retained = self.retained(attempt, events)
        self.assertEqual(attempt['progress'], current['progress'])
        self.assertNotEqual('handoff-ready', current['progress'])
        if mutation == 'check':
            self.assertEqual(old_checks + ['first'], self.checks())
        else:
            self.assertEqual(old_checks, self.checks())
        self.assertGreater(len(retained), len(events), 'A blocker must retain a safe event')

    def test_record_matching_commit_and_fresh_handoff(self):
        self.complete()
        implementation = self.commit()
        attempt, events = self.saved()
        before_checks = self.checks()
        value = self.bind(implementation)
        self.assertNotEqual('handoff-ready', value['progress'], 'Binding alone is not fresh verification')
        self.assertEqual(before_checks, self.checks())
        value = self.call('resume')
        self.assertEqual('handoff-ready', value['progress'])
        self.assertEqual(before_checks + ['first', 'second'], self.checks())
        self.assertEqual(self.after, self.tasks.read_bytes())
        self.retained(attempt, events)
        self.assertEqual(implementation, self.fixture.git('rev-parse', 'HEAD').stdout.strip())

    def test_record_unavailable_commit_retains_evidence(self):
        self.complete()
        attempt, events = self.saved()
        self.bind('f' * 40, 3)
        self.assertEqual(self.after, self.tasks.read_bytes())
        self.retained(attempt, events)

    def test_record_commit_without_completed_marker(self):
        self.complete()
        self.bind(self.initial_attempt['baseline'], 3)
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_record_commit_with_mismatched_sealed_output(self):
        self.complete()
        (self.root/'guide.md').write_bytes(b'Unsealed commit bytes\n')
        wrong = self.commit()
        (self.root/'guide.md').write_bytes(b'Recovery output\n')
        self.bind(wrong, 3)
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_record_commit_rejects_staged_hidden_violation(self):
        self.complete()
        implementation = self.commit()
        (self.root/'outside.md').write_bytes(b'Unauthorized staged bytes\n')
        self.fixture.git('add', 'outside.md')
        (self.root/'outside.md').unlink()
        self.bind(implementation, 3)
        self.assertIn('outside.md', self.fixture.git('diff', '--cached', '--name-only').stdout)

    def test_record_commit_rejects_reverted_history_violation(self):
        self.complete()
        implementation = self.commit()
        outside = self.root/'outside.md'
        outside.write_bytes(b'Unauthorized historical bytes\n')
        self.commit()
        outside.unlink()
        self.commit()
        self.bind(implementation, 3)
        self.assertFalse(outside.exists())

    def test_record_commit_rejects_changed_mode(self):
        self.complete()
        implementation = self.commit()
        self.fixture.git('config', 'core.filemode', 'false')
        output = self.root/'guide.md'
        output.chmod(output.stat().st_mode | 0o111)
        self.bind(implementation, 3)
        self.assertTrue(output.stat().st_mode & 0o111)

    def test_verification_retains_original_implementation_identity(self):
        self.complete()
        implementation = self.commit()
        self.bind(implementation)
        attempt, events = self.saved()
        self.fixture.git('commit', '--allow-empty', '-m', 'test: later verification head')
        later = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        self.assertNotEqual(implementation, later)
        for command in ('verify', 'resume', 'resume'):
            self.call(command)
        self.bind(implementation)
        self.bind(later, 3)  # Same tree cannot replace the original identity.
        self.retained(attempt, events)
        self.assertEqual(later, self.fixture.git('rev-parse', 'HEAD').stdout.strip())
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_failed_handoff_rerun_preserves_bound_evidence(self):
        self.complete()
        implementation = self.commit()
        self.bind(implementation)
        self.call('resume')
        attempt, events = self.saved()
        self.fail_check.write_bytes(b'fail')
        before_checks = self.checks()
        self.call('resume', 4)
        self.assertEqual(before_checks + ['first'], self.checks())
        self.assertEqual(self.after, self.tasks.read_bytes())
        self.retained(attempt, events)
        self.fail_check.unlink()
        self.call('resume')
        self.bind(implementation)

    def test_later_progress_without_intent_is_corruption(self):
        # Valid storage API, deliberately inconsistent semantic progress fixture.
        import single_task_state as backend
        with backend.open_store(self.root, require_existing=True, writable_existing=True) as store:
            store.append_event(self.attempt_id, 'fixture-inconsistent-progress', {},
                               progress='completion-prepared')
        attempt, events = self.saved()
        self.call('resume', 3)
        self.assertEqual([], self.checks())
        self.assertEqual(self.before, self.tasks.read_bytes())
        self.retained(attempt, events)

    def test_missing_journal_is_not_reconstructed(self):
        journal = self.root/'.symphony/single-task/journal.sqlite3'
        retained = self.fixture.private/'retained-journal.sqlite3'
        journal.rename(retained)
        captured = retained.read_bytes()
        self.call('resume', 3)
        self.assertFalse(journal.exists())
        self.assertEqual(captured, retained.read_bytes())
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_corrupt_journal_is_retained(self):
        journal = self.root/'.symphony/single-task/journal.sqlite3'
        captured = journal.read_bytes()
        corrupt = b'Invalid SQLite evidence\n'
        journal.write_bytes(corrupt)
        self.call('resume', 3)
        self.assertEqual(corrupt, journal.read_bytes())
        self.assertNotEqual(captured, corrupt)
        self.assertEqual(self.before, self.tasks.read_bytes())


    def test_corrupt_latest_acceptance_blocks_recovery(self):
        self.call('verify')
        import single_task_state as backend
        with backend.open_store(self.root, require_existing=True, writable_existing=True) as store:
            events = store.read_events(self.attempt_id)
            evidence = copy.deepcopy(next(event['payload'] for event in reversed(events)
                                          if event['kind'] == 'acceptance'))
            evidence['checks'][0]['content_manifest_digest'] = '0' * 64
            store.append_event(self.attempt_id, 'acceptance', evidence)
        attempt, events = self.saved()
        self.call('resume', 3)
        self.assertEqual(self.before, self.tasks.read_bytes())
        self.retained(attempt, events)

    def test_contradictory_intent_blocks_recovery(self):
        self.crash('after-intent')
        import single_task_state as backend
        with backend.open_store(self.root, require_existing=True, writable_existing=True) as store:
            intent = copy.deepcopy(next(event['payload'] for event in store.read_events(self.attempt_id)
                                        if event['kind'] == 'completion-intent'))
            intent['tasks_after_sha256'] = '0' * 64
            store.append_event(self.attempt_id, 'completion-intent', intent)
        attempt, events = self.saved()
        self.call('resume', 3)
        self.assertEqual(self.before, self.tasks.read_bytes())
        self.retained(attempt, events)

    def test_corrupt_context_copy_is_retained(self):
        copies = list((self.root/'.symphony/single-task/contexts').iterdir())
        self.assertEqual(1, len(copies))
        copies[0].chmod(0o600)
        copies[0].write_bytes(b'{"corrupt":true}')
        copies[0].chmod(0o400)
        self.call('resume', 3)
        self.assertEqual(b'{"corrupt":true}', copies[0].read_bytes())
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_record_unreachable_existing_commit(self):
        self.complete()
        implementation = self.commit()
        tree = self.fixture.git('rev-parse', implementation + '^{tree}').stdout.strip()
        result = subprocess.run([GIT, '-c', f'core.hooksPath={self.fixture.hooks}',
                                 '-c', 'commit.gpgSign=false', 'commit-tree', tree,
                                 '-p', implementation, '-m', 'Synthetic unreachable commit'],
                                cwd=self.root, env=self.fixture.env, capture_output=True,
                                text=True, timeout=10, check=True)
        orphan = result.stdout.strip()
        self.assertNotEqual(implementation, orphan)
        self.assertEqual(tree, self.fixture.git('rev-parse', orphan + '^{tree}').stdout.strip())
        self.bind(orphan, 3)
        self.assertEqual(implementation, self.fixture.git('rev-parse', 'HEAD').stdout.strip())

    def recover_checked_rework(self, boundary, mutation=None, expected=3):
        # Do not fabricate a predecessor binding. Its missing public capability
        # is an explicit BODY assertion; later assertions remain unexecuted red.
        self.complete()
        implementation = self.commit()
        predecessor, prior_events = self.saved()
        self.bind(implementation)
        self.call('resume')
        prior_id = self.attempt_id
        self.context.update(mode='owner-authorized rework', prior_attempt_id=prior_id,
                            prior_implementation_commit=implementation,
                            rework_approval={'owner_ref': self.fixture.identities['owner'],
                                            'evidence_ref': 'synthetic:explicit-corrective-approval'})
        self.context['approval']['context_digest'] = context_digest(self.context)
        path = self.fixture.private/'corrective-context.json'
        path.write_bytes(canonical_bytes(self.context))
        result = self.cli('rework', '--context', str(path))
        self.assertEqual(0, result.returncode, 'T018 checked recovery requires genuine rework entry')
        self.attempt_id = self.outcome(result)['attempt_id']
        self.assertNotEqual(prior_id, self.attempt_id)
        self.before = self.after = self.tasks.read_bytes()
        self.assertIn(b'[x] T001', self.before)
        self.initial_attempt, self.initial_events = self.saved()
        self.assertEqual(prior_id, self.initial_attempt['predecessor'])
        (self.root/'guide.md').write_bytes(b'Approved corrective output\n')
        self.write_review()
        self.log.unlink()
        if mutation == 'missing-intent':
            import single_task_state as backend
            with backend.open_store(self.root, require_existing=True, writable_existing=True) as store:
                store.append_event(self.attempt_id, 'fixture-inconsistent-progress', {},
                                   progress='completion-prepared')
            attempt, events = self.saved()
            self.call('resume', 3)
            self.assertEqual(self.before, self.tasks.read_bytes())
            self.assertEqual([], self.checks())
            self.retained(attempt, events)
        elif mutation is not None:
            self.blocked_recovery(boundary, mutation, expected)
        else:
            self.recover_boundary(boundary)
        import single_task_state as backend
        with backend.open_store(self.root, require_existing=True, writable_existing=True) as store:
            self.assertEqual(prior_events, store.read_events(prior_id)[:len(prior_events)])
            self.assertEqual(predecessor['tasks_bytes'], store.read_attempt(prior_id)['tasks_bytes'])


T018_BOUNDARIES = (
    'before-acceptance', 'after-check-result', 'after-acceptance', 'before-intent', 'after-intent',
    'after-replace', 'after-directory-flush', 'after-checkbox', 'after-completion',
)
for _boundary in T018_BOUNDARIES:
    def _case(self, boundary=_boundary):
        self.recover_boundary(boundary)
    setattr(TaskRecoveryTests, 'test_resume_' + _boundary.replace('-', '_'), _case)

for _boundary in ('before-acceptance', 'after-acceptance'):
    def _case(self, boundary=_boundary):
        self.recover_checked_rework(boundary)
    setattr(TaskRecoveryTests, 'test_checked_rework_' + _boundary.replace('-', '_'), _case)

for _mutation, _exit in (('review', 3), ('content', 3), ('check', 4), ('missing-intent', 3)):
    def _case(self, mutation=_mutation, expected=_exit):
        self.recover_checked_rework('after-acceptance', mutation, expected)
    setattr(TaskRecoveryTests, 'test_checked_rework_block_' + _mutation.replace('-', '_'), _case)

for _boundary in ('before-acceptance', 'after-acceptance', 'after-intent', 'after-replace'):
    for _mutation, _exit in (('review', 3), ('missing-review', 3), ('task-bytes', 3), ('check', 4)):
        def _case(self, boundary=_boundary, mutation=_mutation, expected=_exit):
            self.blocked_recovery(boundary, mutation, expected)
        setattr(TaskRecoveryTests, 'test_block_' + _boundary.replace('-', '_') + '_' +
                _mutation.replace('-', '_'), _case)
for _boundary in ('after-acceptance', 'after-intent', 'after-replace'):
    def _case(self, boundary=_boundary):
        self.blocked_recovery(boundary, 'content', 3)
    setattr(TaskRecoveryTests, 'test_block_' + _boundary.replace('-', '_') + '_content', _case)


class CompetingTaskProcessTests(unittest.TestCase):
    """T024: real CLI owners paused by pipes at named durable boundaries."""

    cli = TaskRecoveryTests.cli
    outcome = TaskRecoveryTests.outcome
    saved = TaskRecoveryTests.saved
    call = TaskRecoveryTests.call
    checks = TaskRecoveryTests.checks
    write_review = TaskRecoveryTests.write_review
    complete = TaskRecoveryTests.complete
    commit = TaskRecoveryTests.commit
    bind = TaskRecoveryTests.bind
    retained = TaskRecoveryTests.retained

    def setUp(self):
        TaskRecoveryTests.setUp(self)
        self.context_path = self.fixture.private/'competing-context.json'
        self.context_path.write_bytes(canonical_bytes(self.context))

    def pause(self, boundary, *args):
        injection = r'''import pathlib,runpy,sys
sys.path.insert(0,str(pathlib.Path(sys.argv[1]).parent))
import single_task_state as state
boundary=sys.argv.pop(2)
append=state._Store.append_event
def paused(self,attempt_id,kind,*args,**kwargs):
    result=append(self,attempt_id,kind,*args,**kwargs)
    if kind==boundary:
        print('boundary:'+kind,flush=True)
        assert sys.stdin.readline()=='release\n'
    return result
state._Store.append_event=paused
sys.argv=sys.argv[1:]
runpy.run_path(sys.argv[0],run_name='__main__')
'''
        process = subprocess.Popen(
            [sys.executable, '-B', '-c', injection, str(SCRIPT), boundary, *args],
            cwd=self.root, env=self.fixture.env, text=True, stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        def cleanup():
            if process.poll() is None:
                process.kill()
            process.communicate(timeout=5)
        self.addCleanup(cleanup)
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            self.assertTrue(selector.select(timeout=10), 'CLI did not reach named boundary')
            self.assertEqual('boundary:'+boundary+'\n', process.stdout.readline())
        return process

    def finish(self, process):
        stdout, stderr = process.communicate('release\n', timeout=15)
        result = subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)
        value = self.outcome(result)
        self.assertEqual(0, process.returncode, value)
        return value

    def conflict(self, *args):
        # Observe committed SQLite rows directly while the real command owns the
        # advisory lock. A conflicting CLI must neither append nor reset state.
        path = self.root/'.symphony/single-task/journal.sqlite3'
        def rows():
            with closing(sqlite3.connect(path.as_uri()+'?mode=ro', uri=True)) as db:
                return {name: db.execute('SELECT * FROM '+name).fetchall()
                        for name in ('attempts', 'events', 'progress')}
        before = rows()
        tasks = self.tasks.read_bytes()
        checks = self.checks()
        result = self.cli(*args)
        value = self.outcome(result)
        self.assertEqual(5, result.returncode, value)
        self.assertEqual('concurrent_attempt', value['errors'][0]['code'])
        self.assertEqual('not-run', value['local_verification'])
        self.assertEqual(before, rows())
        self.assertEqual(tasks, self.tasks.read_bytes())
        self.assertEqual(checks, self.checks())

    def competitors(self):
        self.conflict('begin', '--context', str(self.context_path))
        self.conflict('complete', '--attempt', self.attempt_id,
                      '--review', str(self.review_path))
        self.conflict('rework', '--context', str(self.context_path))
        self.conflict('status', '--attempt', self.attempt_id)

    def test_complete_has_one_owner_and_conflicting_commands_do_not_write(self):
        owner = self.pause('completion-intent', 'complete', '--attempt', self.attempt_id,
                           '--review', str(self.review_path))
        self.competitors()
        result = self.finish(owner)
        self.assertEqual(self.attempt_id, result['attempt_id'])
        self.assertEqual('completed-awaiting-commit', result['progress'])
        attempt, events = self.retained(self.initial_attempt, self.initial_events)
        self.assertEqual(self.after, self.tasks.read_bytes())
        self.assertEqual(['first', 'second'], self.checks())
        for kind in ('completion-intent', 'checkbox-applied', 'completion'):
            self.assertEqual(1, sum(event['kind'] == kind for event in events))
        self.call('complete')
        _, current = self.retained(attempt, events)
        for kind in ('completion-intent', 'checkbox-applied', 'completion'):
            self.assertEqual(1, sum(event['kind'] == kind for event in current))
        self.assertEqual(['first', 'second'] * 2, self.checks())

    def test_begin_has_one_owner_with_all_competitors_blocked(self):
        # The setUp attempt belongs to another disposable fixture. Create a
        # fresh clean repository for an actual first-entry competition.
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.tasks = self.fixture.feature/'tasks.md'
        (self.root/'.gitignore').write_text('.symphony/\n')
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: competing entry baseline')
        self.context = self.fixture.context()
        self.context_path = self.fixture.private/'context.json'
        self.context_path.write_bytes(canonical_bytes(self.context))
        self.log = self.fixture.private/'checks.jsonl'
        self.review_path = self.fixture.private/'review.json'
        before = self.tasks.read_bytes()
        owner = self.pause('entry', 'begin', '--context', str(self.context_path))
        self.competitors()
        value = self.finish(owner)
        self.attempt_id = value['attempt_id']
        attempt, events = self.saved()
        self.assertEqual('active', attempt['progress'])
        self.assertEqual(['entry'], [event['kind'] for event in events])
        self.assertEqual(before, self.tasks.read_bytes())
        result = self.cli('begin', '--context', str(self.context_path))
        self.assertEqual(3, result.returncode)
        current, appended = self.saved()
        self.assertEqual(attempt, current)
        self.assertEqual(events, appended[:len(events)])

    def prepare_rework(self):
        self.complete()
        implementation = self.commit()
        self.bind(implementation)
        self.call('resume')
        predecessor = self.attempt_id
        old = self.saved()
        self.context.update(mode='owner-authorized rework', prior_attempt_id=predecessor,
                            prior_implementation_commit=implementation,
                            rework_approval={'owner_ref': self.fixture.identities['owner'],
                                            'evidence_ref': 'synthetic:explicit-correction'})
        self.context['approval']['context_digest'] = context_digest(self.context)
        self.context_path.write_bytes(canonical_bytes(self.context))
        return predecessor, old

    def test_rework_has_one_owner_and_preserves_predecessor_history(self):
        predecessor, old = self.prepare_rework()
        before = self.tasks.read_bytes()
        owner = self.pause('entry', 'rework', '--context', str(self.context_path))
        self.competitors()
        value = self.finish(owner)
        self.assertEqual('owner-authorized rework', value['mode'])
        self.assertEqual(old, self.saved())
        self.attempt_id = value['attempt_id']
        attempt, events = self.saved()
        self.assertEqual(predecessor, attempt['predecessor'])
        self.assertEqual('active', attempt['progress'])
        self.assertEqual(['entry'], [event['kind'] for event in events])
        self.assertEqual(before, self.tasks.read_bytes())
        result = self.cli('rework', '--context', str(self.context_path))
        self.assertEqual(3, result.returncode)
        self.assertEqual(attempt, self.saved()[0])

    def test_killed_owner_releases_lock_and_same_attempt_resumes(self):
        owner = self.pause('completion-intent', 'complete', '--attempt', self.attempt_id,
                           '--review', str(self.review_path))
        self.competitors()
        owner.kill()
        stdout, stderr = owner.communicate(timeout=5)
        self.assertLess(owner.returncode, 0)
        self.assertEqual(('', ''), (stdout, stderr))
        attempt, events = self.saved()  # Actual reacquisition after SIGKILL.
        self.assertEqual('completion-prepared', attempt['progress'])
        self.assertEqual(self.before, self.tasks.read_bytes())
        value = self.call('resume')
        self.assertEqual(self.attempt_id, value['attempt_id'])
        self.assertEqual(self.after, self.tasks.read_bytes())
        _, current = self.retained(attempt, events)
        self.assertEqual(['first', 'second'] * 2, self.checks())
        for kind in ('completion-intent', 'completion'):
            self.assertEqual(1, sum(event['kind'] == kind for event in current))

    def persistence_failure(self, boundary):
        injection = r'''import os,pathlib,runpy,sqlite3,stat,sys
sys.path.insert(0,str(pathlib.Path(sys.argv[1]).parent))
import single_task_state as state
boundary=sys.argv.pop(2)
connect=sqlite3.connect
fsync=state.os.fsync
fired=False
class FailingConnection(sqlite3.Connection):
    def execute(self,sql,*args,**kwargs):
        global fired
        result=super().execute(sql,*args,**kwargs)
        if (boundary=='sqlite-intent' and not fired and sql.startswith('INSERT INTO events')
                and args[0][2]=='completion-intent'):
            fired=True
            raise sqlite3.OperationalError('synthetic durable intent failure')
        return result
def failing_connect(*args,**kwargs):
    kwargs['factory']=FailingConnection
    return connect(*args,**kwargs)
def failing_fsync(fd):
    global fired
    target=os.fstat(fd)
    tasks=pathlib.Path('specs/synthetic/tasks.md')
    if not fired and boundary=='publication-flush' and stat.S_ISREG(target.st_mode):
        directory=pathlib.Path('.symphony/single-task/publication')
        if directory.exists() and any(p.stat().st_ino==target.st_ino for p in directory.iterdir()):
            fired=True
            raise OSError('synthetic publication flush failure')
    if (not fired and boundary=='marker-directory-flush' and stat.S_ISDIR(target.st_mode)
            and (target.st_dev,target.st_ino)==(tasks.parent.stat().st_dev,tasks.parent.stat().st_ino)
            and b'[x] T001' in tasks.read_bytes()):
        fired=True
        raise OSError('synthetic marker directory flush failure')
    return fsync(fd)
sqlite3.connect=failing_connect
state.os.fsync=failing_fsync
sys.argv=sys.argv[1:]
try:
    runpy.run_path(sys.argv[0],run_name='__main__')
finally:
    assert fired, 'Named durable failure must execute'
'''
        result = self.cli(boundary, 'complete', '--attempt', self.attempt_id,
                          '--review', str(self.review_path), injection=injection)
        value = self.outcome(result)
        self.assertEqual(6, result.returncode, value)
        self.assertEqual('persistence_failure', value['errors'][0]['code'])
        attempt, events = self.retained(self.initial_attempt, self.initial_events)
        # Persistence failure is reported safely without trying to journal via
        # the failed durability layer. The last successful seal is recoverable.
        self.assertEqual('not-run', value['local_verification'])
        expected = 'acceptance-recorded' if boundary == 'sqlite-intent' else 'completion-prepared'
        self.assertEqual(expected, attempt['progress'])
        self.assertEqual(self.after if boundary == 'marker-directory-flush' else self.before,
                         self.tasks.read_bytes())
        self.assertEqual(0, sum(event['kind'] == 'completion' for event in events))
        self.call('resume')
        _, recovered = self.retained(attempt, events)
        self.assertEqual(self.after, self.tasks.read_bytes())
        self.assertEqual(['first', 'second'] * 2, self.checks())
        for kind in ('completion-intent', 'completion'):
            self.assertEqual(1, sum(event['kind'] == kind for event in recovered))

    def test_sqlite_intent_failure_retains_acceptance_and_recovers(self):
        self.persistence_failure('sqlite-intent')

    def test_publication_flush_failure_retains_intent_and_recovers(self):
        self.persistence_failure('publication-flush')

    def test_marker_directory_flush_failure_retains_intent_and_recovers(self):
        self.persistence_failure('marker-directory-flush')
