"""Direct acceptance execution and durable evidence controls for T015."""
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest

from test_single_task import SingleTaskFixture, canonical_bytes, context_digest, digest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'scripts'))
import single_task_state as state
import single_task_scope as scope


class AcceptanceExecutionTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.context = self.fixture.context()
        self.trace = self.fixture.private/'trace.json'
        (self.root/'.gitignore').write_text('.symphony/\n')
        (self.root/'checks').mkdir()
        (self.root/'checks/.keep').touch()
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: acceptance inputs')

    def check(self, code='pass', **changes):
        return dict(id='check', argv=[sys.executable, '-B', '-c', code],
                    cwd='.', timeout_seconds=5, expected_exit=0, **changes)

    def begin(self, checks):
        self.context['checks'] = checks
        self.context['approval']['context_digest'] = context_digest(self.context)
        path = self.fixture.private/'context.json'
        path.write_bytes(canonical_bytes(self.context))
        run = subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'),
                              'begin', '--context', str(path)], cwd=self.root,
                             env=self.fixture.env, capture_output=True, timeout=15)
        self.assertEqual(0, run.returncode, 'Real validated entry must precede acceptance')
        self.attempt_id = json.loads(run.stdout)['attempt_id']
        with state.open_store(self.root, require_existing=True) as store:
            self.attempt = store.read_attempt(self.attempt_id)
        self.review = self.fixture.review(self.context)
        self.review['attempt_id'] = self.attempt_id
        self.tasks_before = (self.fixture.feature/'tasks.md').read_bytes()

    def owner(self):
        path = ROOT/'scripts/single_task_acceptance.py'
        self.assertTrue(path.is_file(), 'T015 owning acceptance execution is missing')
        spec = importlib.util.spec_from_file_location('single_task_acceptance', path)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def execute(self):
        owner = self.owner()
        with state.open_store(self.root) as store:
            return owner.execute_acceptance(self.root, self.attempt, self.review, store)

    def blocked(self, category):
        owner = self.owner()
        with state.open_store(self.root) as store:
            with self.assertRaises(owner.AcceptanceFailure) as caught:
                owner.execute_acceptance(self.root, self.attempt, self.review, store)
            self.assertEqual(category, caught.exception.category)
            self.assertEqual('active', store.read_attempt(self.attempt_id)['progress'])
            events = store.read_events(self.attempt_id)
            self.assertEqual('check-result', events[-1]['kind'])
            self.assertEqual(category, events[-1]['payload']['category'])
        self.assertEqual(self.tasks_before, (self.fixture.feature/'tasks.md').read_bytes())

    def test_ordered_literal_argv_and_cwd_with_durable_success(self):
        literal = ['space argument', ';', '$(touch sentinel)', '`touch sentinel`']
        first = self.check(f'from pathlib import Path; Path({str(self.trace)!r}).write_text("first\\n")')
        first['id'] = 'first'
        second = self.check('import json,os,sys; from pathlib import Path; '
                            f'Path({str(self.trace)!r}).open("a").write(json.dumps([os.getcwd(),sys.argv[1:]]))')
        second.update(id='second', cwd='checks')
        second['argv'] += literal
        self.begin([first, second])
        (self.root/'guide.md').write_bytes(b'Reviewed committed output')
        self.fixture.git('add', 'guide.md')
        self.fixture.git('commit', '-m', 'test: reviewed allowed output')
        expected_head = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        self.assertNotEqual(self.attempt['baseline'], expected_head)
        self.review = self.fixture.review(self.context)
        self.review['attempt_id'] = self.attempt_id
        expected_manifest = self.fixture.manifest(['guide.md', 'specs/synthetic/tasks.md'], 'T001')
        manifest_digest = digest(canonical_bytes(expected_manifest))
        review_digest = digest(canonical_bytes(self.review))
        evidence = self.execute()
        self.assertEqual({'schema_version': 1, 'attempt_id': self.attempt_id,
                          'task_id': 'T001', 'context_digest': context_digest(self.context),
                          'content_manifest': expected_manifest,
                          'content_manifest_digest': manifest_digest,
                          'semantic_review': self.review, 'semantic_review_digest': review_digest,
                          'verification_head': expected_head, 'checks': [
                              {'schema_version': 1, 'id': check['id'], 'argv': check['argv'],
                               'cwd': check['cwd'], 'expected_exit': 0, 'exit': 0,
                               'category': 'passed', 'timed_out': False,
                               'context_digest': context_digest(self.context),
                               'content_manifest_digest': manifest_digest,
                               'semantic_review_digest': review_digest}
                              for check in (first, second)]}, evidence)
        lines = self.trace.read_text().splitlines()
        self.assertEqual('first', lines[0])
        self.assertEqual([str((self.root/'checks').resolve()), literal], json.loads(lines[1]))
        self.assertFalse((self.root/'sentinel').exists())
        with state.open_store(self.root, require_existing=True) as store:
            self.assertEqual('acceptance-recorded', store.read_attempt(self.attempt_id)['progress'])
            event = store.read_events(self.attempt_id)[-1]
            self.assertEqual('acceptance', event['kind'])
            self.assertEqual(evidence, event['payload'])
            self.assertEqual(['first', 'second'], [row['id'] for row in evidence['checks']])
            self.assertTrue(all(row['exit'] == 0 for row in evidence['checks']))
        self.assertEqual(self.tasks_before, (self.fixture.feature/'tasks.md').read_bytes())

    def test_missing_executable(self):
        check = self.check()
        check['argv'] = [str(self.fixture.private/'absent-executable')]
        self.begin([check])
        self.blocked('missing_executable')

    def test_nonzero_exit_and_discarded_output(self):
        secret = 'synthetic-private-output'
        path = self.fixture.private/'secret'
        path.write_text(secret)
        self.begin([self.check(f'from pathlib import Path; import sys; print(Path({str(path)!r}).read_text()); sys.exit(17)')])
        self.blocked('nonzero_exit')
        for file in (self.root/'.symphony/single-task').rglob('*'):
            if file.is_file():
                self.assertNotIn(secret.encode(), file.read_bytes())

    def test_timeout_kills_descendant_group(self):
        ready, late = self.fixture.private/'ready', self.fixture.private/'late'
        child = ('from pathlib import Path; import time; '
                 f'Path({str(ready)!r}).touch(); time.sleep(1.5); Path({str(late)!r}).touch()')
        parent = ('import subprocess,sys,time; from pathlib import Path; '
                  f'subprocess.Popen([sys.executable,"-B","-c",{child!r}]); '
                  f'\nwhile not Path({str(ready)!r}).exists(): time.sleep(.01)\n'
                  'time.sleep(30)')
        check = self.check(parent)
        check['timeout_seconds'] = .5
        self.begin([check])
        self.blocked('timeout')
        self.assertTrue(ready.exists(), 'Actual descendant must reach its barrier')
        time.sleep(1.6)
        self.assertFalse(late.exists(), 'Timed-out descendants must stop executing')

    def test_successful_parent_cannot_leave_background_descendant(self):
        ready, late = self.fixture.private/'ready', self.fixture.private/'late'
        child = ('from pathlib import Path; import time; '
                 f'Path({str(ready)!r}).touch(); time.sleep(1.5); Path({str(late)!r}).touch()')
        parent = ('import subprocess,sys,time; from pathlib import Path; '
                  f'subprocess.Popen([sys.executable,"-B","-c",{child!r}]); '
                  f'\nwhile not Path({str(ready)!r}).exists(): time.sleep(.01)\n')
        self.begin([self.check(parent)])
        self.blocked('background_process')
        self.assertTrue(ready.exists(), 'Actual descendant must reach its barrier')
        time.sleep(1.6)
        self.assertFalse(late.exists(), 'Successful parent cannot leave mutation behind')

    def test_mutation_of_allowed_content(self):
        self.begin([self.check('from pathlib import Path; Path("guide.md").write_text("changed")')])
        self.blocked('content_changed')

    def test_mutation_of_outside_scope(self):
        self.begin([self.check('from pathlib import Path; Path("outside.md").touch()')])
        self.blocked('scope_changed')

    def test_mutation_of_tasks(self):
        self.begin([self.check('from pathlib import Path; p=Path("specs/synthetic/tasks.md"); '
                              'p.write_bytes(p.read_bytes()+b"drift")')])
        owner = self.owner()
        with state.open_store(self.root) as store:
            with self.assertRaises(owner.AcceptanceFailure) as caught:
                owner.execute_acceptance(self.root, self.attempt, self.review, store)
            self.assertEqual('scope_changed', caught.exception.category)
        self.assertNotEqual(self.tasks_before, (self.fixture.feature/'tasks.md').read_bytes())
        self.assertIn(b'- [ ] T001', (self.fixture.feature/'tasks.md').read_bytes())

    def test_stale_review_blocks_before_execution(self):
        self.begin([self.check(f'from pathlib import Path; Path({str(self.trace)!r}).touch()')])
        (self.root/'guide.md').write_bytes(b'Changed after review')
        owner = self.owner()
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises(ValueError):
                owner.execute_acceptance(self.root, self.attempt, self.review, store)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertFalse(self.trace.exists())

    def test_event_payload_survives_reopen_and_rejects_tampering(self):
        self.begin([self.check()])
        payload = {'safe': ['ordered', 1]}
        with state.open_store(self.root) as store:
            store.append_event(self.attempt_id, 'control', payload)
        with state.open_store(self.root, require_existing=True) as store:
            self.assertEqual(payload, store.read_events(self.attempt_id)[-1].get('payload'),
                             'T015 must retain actual safe payload, not only its digest')
        path = self.root/'.symphony/single-task/journal.sqlite3'
        with sqlite3.connect(path) as connection:
            trigger = connection.execute("SELECT sql FROM sqlite_master WHERE name='immutable_event_update'").fetchone()[0]
            connection.execute('DROP TRIGGER immutable_event_update')
            connection.execute("UPDATE events SET payload=? WHERE kind='control'", (b'{"safe":2}',))
            connection.execute(trigger)
        with self.assertRaises(state.InvalidState):
            with state.open_store(self.root, require_existing=True):
                self.fail('Changed payload must not become accepted evidence')
