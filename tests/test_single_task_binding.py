"""Direct T020 owning controls; no unavailable CLI can satisfy binding."""
import copy
import subprocess
import unittest

import test_single_task_completion as completion_fixture
from test_single_task import GIT, context_digest, digest
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

    def test_checked_rework_binding_preserves_uppercase_marker(self):
        # Primitive-owned state fixture isolates T020. This does not claim T022
        # entry authorization or T023 handoff wiring is implemented.
        import single_task_acceptance as acceptance
        self.bind()
        predecessor = self.attempt_id
        with state.open_store(self.root, require_existing=True, writable_existing=True) as store:
            old = store.read_attempt(predecessor)
            acceptance.execute_acceptance(self.root, old, self.review, store)
            store.append_event(predecessor, 'fixture-verified-handoff', {}, progress='handoff-ready')
        checked = self.after.replace(b'[x] T001', b'[X] T001')
        self.tasks.write_bytes(checked)
        baseline = self.commit_current()
        context = copy.deepcopy(self.context)
        context.update(mode='owner-authorized rework', prior_attempt_id=predecessor,
                       prior_implementation_commit=self.commit,
                       rework_approval={'owner_ref': 'synthetic:owner', 'evidence_ref': 'synthetic:rework'})
        for name, record in context['reviewed_artifacts'].items():
            record.update(commit=baseline, sha256=digest((self.fixture.feature/name).read_bytes()))
        context['source_assertion']['sha256'] = digest(checked)
        context['approval']['context_digest'] = context_digest(context)
        with state.open_store(self.root, require_existing=True, writable_existing=True) as store:
            prefix = store.read_events(predecessor)
            store.create_attempt(attempt_id='checked-rework', context=context, baseline=baseline,
                                 tasks_bytes=checked, entry_mode=context['mode'], predecessor=predecessor,
                                 approval_context_digest=context['approval']['context_digest'])
            attempt = store.read_attempt('checked-rework')
            review = self.fixture.review(context)
            review['attempt_id'] = 'checked-rework'
            acceptance.execute_acceptance(self.root, attempt, review, store)
            state.publish_completion(self.root, store, 'checked-rework')
            value = self.owner()(self.root, store, 'checked-rework', baseline)
            self.assertEqual(baseline, value['implementation_commit'])
            self.assertEqual(prefix, store.read_events(predecessor))
            intent = next(row['payload'] for row in store.read_events('checked-rework')
                          if row['kind'] == 'completion-intent')
            self.assertEqual(checked, bytes.fromhex(intent['tasks_before_hex']))
            self.assertEqual(checked, bytes.fromhex(intent['tasks_after_hex']))
        self.assertEqual(checked, self.tasks.read_bytes())

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
