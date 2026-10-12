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
        evidence = SCRIPT.parent.parent/'.symphony/t002-cli-evidence.jsonl'
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
