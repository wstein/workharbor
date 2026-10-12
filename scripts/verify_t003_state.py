#!/usr/bin/env python3
"""Direct T003 acceptance; independent of the still-unimplemented public CLI."""
import copy
import hashlib
import importlib.util
import json
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT/'scripts/single_task_state.py'
sys.path.insert(0, str(ROOT/'tests'))
from test_single_task import SingleTaskFixture, canonical_bytes


def load_backend():
    spec = importlib.util.spec_from_file_location('single_task_state', MODULE)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


class StatePrimitiveTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.backend = load_backend()

    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.fixture = SingleTaskFixture(temp.name)
        self.root = self.fixture.root
        self.context = self.fixture.context()
        self.tasks_path = self.root/'specs/synthetic/tasks.md'
        self.tasks = self.tasks_path.read_bytes()

    def tearDown(self):
        self.assertEqual(self.tasks, self.tasks_path.read_bytes())

    def open(self):
        return self.backend.open_store(self.root)

    def create(self, store, attempt='synthetic-attempt'):
        return store.create_attempt(attempt_id=attempt, context=self.context,
                                    baseline=self.fixture.baseline, tasks_bytes=self.tasks,
                                    entry_mode='implement', predecessor=None)

    def test_durable_immutable_attempt(self):
        original = copy.deepcopy(self.context)
        with self.open() as store:
            self.create(store)
            value = store.read_attempt('synthetic-attempt')
            self.assertEqual('synthetic-attempt', value['attempt_id'])
            self.assertEqual(original, value['context'])
            self.assertEqual(self.fixture.baseline, value['baseline'])
            self.assertEqual(self.tasks, value['tasks_bytes'])
            self.assertEqual('implement', value['entry_mode'])
            self.assertIsNone(value['predecessor'])
            self.assertTrue(value['created_at'])
            self.assertEqual(hashlib.sha256(canonical_bytes(original)).hexdigest(), value['context_copy_sha256'])
            self.context['allowed_paths'].append('mutated.md')
            value['context']['allowed_paths'].append('also-mutated.md')
        with self.open() as store:
            value = store.read_attempt('synthetic-attempt')
            self.assertEqual(original, value['context'])
            self.assertEqual(self.tasks, value['tasks_bytes'])
        copies = list((self.root/'.symphony/single-task/contexts').glob('*.json'))
        self.assertEqual(1, len(copies))
        self.assertEqual(original, json.loads(copies[0].read_bytes()))

    def test_one_active_attempt(self):
        with self.open() as store:
            self.create(store)
            before = store.read_events('synthetic-attempt')
            with self.assertRaises(self.backend.StateConflict):
                self.create(store, 'second-attempt')
            self.assertEqual(before, store.read_events('synthetic-attempt'))
            self.assertEqual('active', store.read_attempt('synthetic-attempt')['progress'])

    def test_cross_process_lock_and_release(self):
        # Parent is a synchronized holder: it owns the lock before child launch.
        code = '''import importlib.util, pathlib, sys
spec = importlib.util.spec_from_file_location('single_task_state', sys.argv[1])
m = importlib.util.module_from_spec(spec); sys.modules[spec.name] = m; spec.loader.exec_module(m)
try:
    with m.open_store(pathlib.Path(sys.argv[2])): pass
except m.StateConflict: sys.exit(5)
'''
        argv = [sys.executable, '-c', code, str(MODULE), str(self.root)]
        with self.open():
            child = subprocess.run(argv, env=self.fixture.env, capture_output=True, timeout=5)
            self.assertEqual(5, child.returncode)
        child = subprocess.run(argv, env=self.fixture.env, capture_output=True, timeout=5)
        self.assertEqual(0, child.returncode)

    def test_lock_released_after_holder_exit(self):
        code = '''import importlib.util, pathlib, sys
spec = importlib.util.spec_from_file_location('single_task_state', sys.argv[1])
m = importlib.util.module_from_spec(spec); sys.modules[spec.name] = m; spec.loader.exec_module(m)
with m.open_store(pathlib.Path(sys.argv[2])):
    print('locked', flush=True)
    sys.stdin.read(1)
'''
        child = subprocess.Popen([sys.executable, '-c', code, str(MODULE), str(self.root)],
                                 env=self.fixture.env, stdin=subprocess.PIPE,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            import selectors
            with selectors.DefaultSelector() as selector:
                selector.register(child.stdout, selectors.EVENT_READ)
                self.assertTrue(selector.select(timeout=5), 'Holder never signaled acquisition')
            self.assertEqual('locked\n', child.stdout.readline())
            with self.assertRaises(self.backend.StateConflict):
                with self.open():
                    self.fail('Concurrent store acquired')
            child.kill()
            child.communicate(timeout=5)
            with self.open():
                pass
        finally:
            if child.poll() is None:
                child.kill()
            child.communicate(timeout=5)

    def test_append_only_events_and_progress(self):
        with self.open() as store:
            self.create(store)
            before = store.read_events('synthetic-attempt')
            payload = {'check_id': 'synthetic-check', 'exit': 0}
            store.append_event('synthetic-attempt', 'acceptance', payload,
                               progress='acceptance-recorded')
            after = store.read_events('synthetic-attempt')
            self.assertEqual(before, after[:-1])
            event = after[-1]
            self.assertEqual((before[-1]['sequence'] if before else 0) + 1, event['sequence'])
            self.assertEqual('synthetic-attempt', event['attempt_id'])
            self.assertEqual('acceptance', event['kind'])
            self.assertTrue(event['created_at'])
            self.assertEqual(hashlib.sha256(canonical_bytes(payload)).hexdigest(), event['payload_digest'])
            self.assertEqual('acceptance-recorded', store.read_attempt('synthetic-attempt')['progress'])
        with self.open() as store:
            self.assertEqual(after, store.read_events('synthetic-attempt'))

    def test_transaction_failure_rolls_back(self):
        with self.open() as store:
            self.create(store)
            before = store.read_events('synthetic-attempt')
            with self.assertRaises(self.backend.PersistenceFailure):
                with store.transaction():
                    store.append_event('synthetic-attempt', 'acceptance', {'check': 'synthetic'},
                                       progress='acceptance-recorded')
                    raise sqlite3.OperationalError('synthetic transaction failure')
            self.assertEqual(before, store.read_events('synthetic-attempt'))
            self.assertEqual('active', store.read_attempt('synthetic-attempt')['progress'])
        with self.open() as store:
            self.assertEqual(before, store.read_events('synthetic-attempt'))

    def test_corrupt_journal_retained(self):
        directory = self.root/'.symphony/single-task'
        directory.mkdir(parents=True)
        journal = directory/'journal.sqlite3'
        journal.write_bytes(b'synthetic corrupt database')
        with self.assertRaises(self.backend.InvalidState):
            with self.open():
                self.fail('Corrupt database accepted')
        self.assertEqual(b'synthetic corrupt database', journal.read_bytes())

    def test_unsupported_version_retained(self):
        with self.open() as store:
            self.create(store)
        journal = self.root/'.symphony/single-task/journal.sqlite3'
        with sqlite3.connect(journal) as connection:
            connection.execute('PRAGMA user_version = 999')
        before = journal.read_bytes()
        with self.assertRaises(self.backend.InvalidState):
            with self.open():
                self.fail('Unsupported version accepted')
        self.assertEqual(before, journal.read_bytes())

    def test_legacy_evidence_retained(self):
        legacy = self.root/'.symphony/single-task.json'
        legacy.parent.mkdir()
        legacy.write_bytes(b'{"synthetic_legacy_evidence":true}\n')
        before = legacy.read_bytes()
        with self.assertRaises(self.backend.InvalidState):
            with self.open():
                self.fail('Legacy state implicitly adopted')
        self.assertEqual(before, legacy.read_bytes())
        self.assertFalse((self.root/'.symphony/single-task').exists())

    def test_unsafe_state_path_rejected(self):
        for component in ('.symphony', '.symphony/single-task',
                          '.symphony/single-task/contexts',
                          '.symphony/single-task/journal.sqlite3'):
            with self.subTest(component=component), tempfile.TemporaryDirectory() as temp:
                fixture = SingleTaskFixture(temp)
                target = fixture.root/component
                target.parent.mkdir(parents=True, exist_ok=True)
                outside = fixture.root.parent/'outside'
                if component.endswith('.sqlite3'):
                    outside.write_bytes(b'synthetic external database')
                    target.symlink_to(outside)
                else:
                    outside.mkdir()
                    target.symlink_to(outside, target_is_directory=True)
                with self.assertRaises(self.backend.InvalidState):
                    with self.backend.open_store(fixture.root):
                        self.fail('Symlinked state path accepted')
                if outside.is_file():
                    self.assertEqual(b'synthetic external database', outside.read_bytes())
                else:
                    self.assertEqual([], list(outside.iterdir()))

    def test_unsafe_context_copy_rejected(self):
        with self.open() as store:
            self.create(store)
        copy_path = next((self.root/'.symphony/single-task/contexts').glob('*.json'))
        original = copy_path.read_bytes()
        outside = self.root.parent/'external-context.json'
        outside.write_bytes(original)
        copy_path.unlink()
        copy_path.symlink_to(outside)
        with self.assertRaises(self.backend.InvalidState):
            with self.open() as store:
                store.read_attempt('synthetic-attempt')
        self.assertEqual(original, outside.read_bytes())

    def test_durability_failure_blocks(self):
        with mock.patch('os.fsync', side_effect=OSError('synthetic flush failure')):
            with self.assertRaises(self.backend.PersistenceFailure):
                with self.open() as store:
                    self.create(store)


if __name__ == '__main__':
    if not MODULE.is_file() or MODULE.is_symlink():
        raise SystemExit('T003 blocked: regular scripts/single_task_state.py is not implemented')
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(StatePrimitiveTests)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    raise SystemExit(0 if result.wasSuccessful() and result.testsRun == 12 and
                     not result.skipped and not result.expectedFailures else 1)
