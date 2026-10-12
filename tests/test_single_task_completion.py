"""Direct T016 durable-intent and atomic marker-publication controls."""
import copy
import json
import os
from pathlib import Path
import sqlite3
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from test_single_task import SingleTaskFixture, canonical_bytes, context_digest, digest
sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'scripts'))
import single_task_state as state
import single_task_acceptance as acceptance

ROOT = Path(__file__).resolve().parents[1]


class CompletionPublicationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        (self.root/'.gitignore').write_text('.symphony/\n')
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: completion baseline')
        self.context = self.fixture.context()
        self.fail_control = self.fixture.private/'fail-check'
        program = 'import pathlib,sys; sys.exit(1 if pathlib.Path(sys.argv[1]).exists() else 0)'
        self.context['checks'] = [{'id': 'control', 'argv': [sys.executable, '-B', '-c', program,
                                                          str(self.fail_control)],
                                  'cwd': '.', 'timeout_seconds': 5, 'expected_exit': 0}]
        self.context['approval']['context_digest'] = context_digest(self.context)
        path = self.fixture.private/'context.json'
        path.write_bytes(canonical_bytes(self.context))
        run = subprocess.run([sys.executable, '-B', str(ROOT/'scripts/single_task.py'),
                              'begin', '--context', str(path)], cwd=self.root,
                             env=self.fixture.env, capture_output=True, timeout=15)
        self.assertEqual(0, run.returncode, 'Actual T010 entry is prerequisite to completion')
        self.attempt_id = json.loads(run.stdout)['attempt_id']
        self.tasks = self.fixture.feature/'tasks.md'
        self.before = self.tasks.read_bytes()
        offset = self.before.index(b'[ ] T001') + 1
        self.after = self.before[:offset] + b'x' + self.before[offset + 1:]
        with state.open_store(self.root, require_existing=True) as store:
            self.attempt = store.read_attempt(self.attempt_id)
        self.review = self.fixture.review(self.context)
        self.review['attempt_id'] = self.attempt_id

    def accept(self):
        with state.open_store(self.root) as store:
            return acceptance.execute_acceptance(self.root, self.attempt, self.review, store)

    def owner(self):
        function = getattr(state, 'publish_completion', None)
        self.assertTrue(callable(function), 'T016 owning durable marker publication is missing')
        return function

    def test_intent_is_durable_before_atomic_replace(self):
        evidence = self.accept()
        function = self.owner()
        replace = os.replace
        observed = []
        with state.open_store(self.root) as store:
            old_events = store.read_events(self.attempt_id)

            def inspect_intent(*args, **kwargs):
                with sqlite3.connect(self.root/'.symphony/single-task/journal.sqlite3') as connection:
                    progress = connection.execute('SELECT value FROM progress').fetchone()[0]
                    sequence, blob, payload_digest = connection.execute(
                        "SELECT sequence,payload,payload_digest FROM events WHERE kind='completion-intent'").fetchone()
                intent = json.loads(blob)
                self.assertEqual('completion-prepared', progress)
                self.assertEqual(self.before, bytes.fromhex(intent['tasks_before_hex']))
                self.assertEqual(self.after, bytes.fromhex(intent['tasks_after_hex']))
                self.assertEqual(digest(self.before), intent['tasks_before_sha256'])
                self.assertEqual(digest(self.after), intent['tasks_after_sha256'])
                self.assertEqual(evidence['content_manifest'], intent['content_manifest'])
                self.assertEqual(digest(canonical_bytes(evidence)), intent['acceptance_digest'])
                self.assertEqual(evidence['semantic_review_digest'], intent['semantic_review_digest'])
                self.assertEqual(evidence['context_digest'], intent['context_digest'])
                self.assertEqual(self.before, self.tasks.read_bytes())
                self.assertEqual(digest(blob), payload_digest)
                observed.append(sequence)
                return replace(*args, **kwargs)

            with mock.patch.object(state.os, 'replace', side_effect=inspect_intent):
                result = function(self.root, store, self.attempt_id)
            self.assertEqual(1, len(observed))
            self.assertEqual('completed-awaiting-commit', result['progress'])
            self.assertEqual('implement', result['entry_mode'])
            self.assertEqual(old_events, store.read_events(self.attempt_id)[:len(old_events)])
            self.assertEqual(['completion-intent', 'checkbox-applied', 'completion'],
                             [row['kind'] for row in store.read_events(self.attempt_id)[-3:]])
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_missing_acceptance_cannot_publish(self):
        function = self.owner()
        with state.open_store(self.root) as store:
            with self.assertRaises(state.InvalidState):
                function(self.root, store, self.attempt_id)
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_changed_content_after_acceptance_cannot_publish(self):
        self.accept()
        (self.root/'guide.md').write_bytes(b'Changed sealed output')
        function = self.owner()
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                function(self.root, store, self.attempt_id)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_later_failed_acceptance_invalidates_prior_success(self):
        self.accept()
        with state.open_store(self.root, require_existing=True) as store:
            successful_events = store.read_events(self.attempt_id)
        self.fail_control.write_bytes(b'fail')
        with self.assertRaises(acceptance.AcceptanceFailure) as failure:
            self.accept()
        self.assertEqual('nonzero_exit', failure.exception.category)
        function = self.owner()
        with state.open_store(self.root) as store:
            failed_events = store.read_events(self.attempt_id)
            self.assertEqual(successful_events, failed_events[:len(successful_events)])
            self.assertEqual('check-result', failed_events[-1]['kind'])
            self.assertEqual('nonzero_exit', failed_events[-1]['payload']['category'])
            with self.assertRaises(state.InvalidState):
                function(self.root, store, self.attempt_id)
            self.assertEqual(failed_events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_manual_marker_cannot_become_completion_intent(self):
        self.tasks.write_bytes(self.after)
        self.accept()
        function = self.owner()
        with state.open_store(self.root) as store:
            with self.assertRaises(state.InvalidState):
                function(self.root, store, self.attempt_id)
            self.assertNotIn('completion-intent', [row['kind'] for row in store.read_events(self.attempt_id)])
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_intent_failure_keeps_marker_pending(self):
        self.accept()
        function = self.owner()
        with state.open_store(self.root) as store:
            append = store.append_event

            def fail_intent(attempt_id, kind, *args, **kwargs):
                if kind == 'completion-intent':
                    raise state.PersistenceFailure('Injected intent durability failure')
                return append(attempt_id, kind, *args, **kwargs)

            with mock.patch.object(store, 'append_event', side_effect=fail_intent):
                with self.assertRaises(state.PersistenceFailure):
                    function(self.root, store, self.attempt_id)
            self.assertEqual('acceptance-recorded', store.read_attempt(self.attempt_id)['progress'])
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_replace_failure_retains_durable_intent(self):
        self.accept()
        function = self.owner()
        with state.open_store(self.root) as store:
            with mock.patch.object(state.os, 'replace', side_effect=OSError('Injected replace failure')):
                with self.assertRaises(state.PersistenceFailure):
                    function(self.root, store, self.attempt_id)
            self.assertEqual('completion-prepared', store.read_attempt(self.attempt_id)['progress'])
            self.assertEqual('completion-intent', store.read_events(self.attempt_id)[-1]['kind'])
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_directory_flush_failure_keeps_exact_post_bytes_and_intent(self):
        self.accept()
        function = self.owner()
        flush = os.fsync
        with state.open_store(self.root) as store:
            def fail_directory(fd):
                if stat.S_ISDIR(os.fstat(fd).st_mode) and self.tasks.read_bytes() == self.after:
                    raise OSError('Injected publication directory flush failure')
                return flush(fd)

            with mock.patch.object(state.os, 'fsync', side_effect=fail_directory):
                with self.assertRaises(state.PersistenceFailure):
                    function(self.root, store, self.attempt_id)
            self.assertEqual('completion-prepared', store.read_attempt(self.attempt_id)['progress'])
            self.assertEqual('completion-intent', store.read_events(self.attempt_id)[-1]['kind'])
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_corrupt_acceptance_binding_cannot_publish(self):
        evidence = copy.deepcopy(self.accept())
        evidence['checks'][0]['content_manifest_digest'] = '0' * 64
        function = self.owner()
        with state.open_store(self.root) as store:
            store.append_event(self.attempt_id, 'acceptance', evidence)
            with self.assertRaises(state.InvalidState):
                function(self.root, store, self.attempt_id)
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_corrupt_manifest_type_cannot_publish(self):
        evidence = copy.deepcopy(self.accept())
        evidence['content_manifest']['schema_version'] = True
        function = self.owner()
        with state.open_store(self.root) as store:
            store.append_event(self.attempt_id, 'acceptance', evidence)
            with self.assertRaises(state.InvalidState):
                function(self.root, store, self.attempt_id)
        self.assertEqual(self.before, self.tasks.read_bytes())
