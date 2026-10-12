"""Direct T020 owning controls; no unavailable CLI can satisfy binding."""
import subprocess
import unittest

import test_single_task_completion as completion_fixture
from test_single_task import GIT
import single_task_state as state
import single_task_scope as scope


class ImplementationBindingTests(unittest.TestCase):
    def setUp(self):
        completion_fixture.CompletionPublicationTests.setUp(self)
        completion_fixture.CompletionPublicationTests.accept(self)
        with state.open_store(self.root, require_existing=True, writable_existing=True) as store:
            state.publish_completion(self.root, store, self.attempt_id)
        self.commit = self.commit_current()

    def commit_current(self):
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: selected completed implementation')
        return self.fixture.git('rev-parse', 'HEAD').stdout.strip()

    def owner(self):
        function = getattr(state, 'bind_implementation', None)
        self.assertTrue(callable(function), 'T020 owning immutable commit binding is missing')
        return function

    def bind(self, commit=None):
        with state.open_store(self.root, require_existing=True, writable_existing=True) as store:
            return self.owner()(self.root, store, self.attempt_id, commit or self.commit)

    def rejected(self, commit=None):
        with state.open_store(self.root, require_existing=True, writable_existing=True) as store:
            before = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, commit or self.commit)
            self.assertEqual(before, store.read_events(self.attempt_id))
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_matching_binding_is_immutable_and_separate_from_verification(self):
        result = self.bind()
        self.assertEqual(self.commit, result['implementation_commit'])
        with state.open_store(self.root, require_existing=True) as store:
            old = store.read_events(self.attempt_id)
            self.assertEqual('implementation-binding', old[-1]['kind'])
            self.assertEqual('completed-awaiting-commit', store.read_attempt(self.attempt_id)['progress'])
            self.assertEqual(self.context['approval']['context_digest'], result['context_digest'])
            intent = next(event for event in old if event['kind'] == 'completion-intent')
            self.assertEqual(intent['payload_digest'], result['completion_intent_digest'])
            self.assertEqual(intent['payload']['content_manifest_digest'], result['content_manifest_digest'])
        self.fixture.git('commit', '--allow-empty', '-m', 'test: later verification head')
        later = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        self.assertNotEqual(self.commit, later)
        self.assertEqual(result, self.bind())
        with state.open_store(self.root, require_existing=True) as store:
            events = store.read_events(self.attempt_id)
            self.assertEqual(old, events[:len(old)])
            self.assertEqual(1, sum(event['kind'] == 'implementation-binding' for event in events))
            self.assertEqual(later, events[-1]['payload']['verification_head'])
            self.assertEqual(self.commit, events[-1]['payload']['implementation_commit'])
        self.rejected(later)

    def test_unavailable_commit_rejected(self):
        self.rejected('f' * 40)

    def test_unreachable_existing_commit_rejected(self):
        tree = self.fixture.git('rev-parse', self.commit + '^{tree}').stdout.strip()
        orphan = subprocess.run([GIT, '-c', f'core.hooksPath={self.fixture.hooks}',
                                 '-c', 'commit.gpgSign=false', 'commit-tree', tree, '-p', self.commit,
                                 '-m', 'test: unreachable'], cwd=self.root, env=self.fixture.env,
                                capture_output=True, check=True, timeout=10)
        # commit-tree needs explicit synthetic identity, not host configuration.
        self.rejected(orphan.stdout.strip().decode())

    def test_commit_without_completed_marker_rejected(self):
        self.rejected(self.attempt['baseline'])

    def test_candidate_content_mismatch_rejected(self):
        (self.root/'guide.md').write_bytes(b'Unsealed candidate\n')
        wrong = self.commit_current()
        (self.root/'guide.md').write_bytes(b'Before\n')
        self.rejected(wrong)

    def test_candidate_mode_mismatch_rejected(self):
        (self.root/'guide.md').chmod(0o755)
        wrong = self.commit_current()
        (self.root/'guide.md').chmod(0o644)
        self.rejected(wrong)

    def test_physical_sealed_content_drift_rejected(self):
        (self.root/'guide.md').write_bytes(b'Unsealed physical output\n')
        self.rejected()

    def test_physical_mode_drift_rejected(self):
        self.fixture.git('config', 'core.filemode', 'false')
        (self.root/'guide.md').chmod(0o755)
        self.rejected()

    def test_staged_hidden_outside_change_rejected(self):
        (self.root/'outside.md').write_bytes(b'Unauthorized stage\n')
        self.fixture.git('add', 'outside.md')
        (self.root/'outside.md').unlink()
        self.rejected()

    def test_staged_hidden_allowed_drift_rejected(self):
        (self.root/'guide.md').write_bytes(b'Unsealed stage\n')
        self.fixture.git('add', 'guide.md')
        (self.root/'guide.md').write_bytes(b'Before\n')
        self.rejected()

    def test_reverted_other_task_history_rejected(self):
        original = self.tasks.read_bytes()
        self.tasks.write_bytes(original.replace(b'[ ] T002', b'[x] T002'))
        self.commit_current()
        self.tasks.write_bytes(original)
        self.commit_current()
        self.rejected()
