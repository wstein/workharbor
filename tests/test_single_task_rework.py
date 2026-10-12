"""Direct T022 authorization/linked-attempt controls; handoff fixture is primitive."""
import copy
import unittest
import test_single_task_binding as fixtures
from test_single_task import context_digest
import single_task_state as state
import single_task_context as contexts
import single_task_acceptance as acceptance


class AuthorizedReworkTests(unittest.TestCase):
    commit_current = fixtures.ImplementationBindingTests.commit_current

    def setUp(self):
        fixtures.ImplementationBindingTests.setUp(self)
        with state.open_store(self.root) as store:
            if self._testMethodName != 'test_unbound_predecessor_rejected':
                state.bind_implementation(self.root, store, self.attempt_id, self.commit)
            # T022 fixture boundary: T023 supplies the real ready transition.
            acceptance.execute_acceptance(self.root, store.read_attempt(self.attempt_id), self.review, store)
            store.append_event(self.attempt_id, 'fixture-verified-handoff', {}, progress='handoff-ready')
            self.old_attempt = store.read_attempt(self.attempt_id)
            self.old_events = store.read_events(self.attempt_id)
        self.context = copy.deepcopy(self.context)
        self.context.update(mode='owner-authorized rework', prior_attempt_id=self.attempt_id,
                            prior_implementation_commit=self.commit,
                            rework_approval={'owner_ref': 'synthetic:owner', 'evidence_ref': 'synthetic:correction'})
        self.context['checks'][0]['id'] = 'corrective-check'
        self.context['approval']['context_digest'] = context_digest(self.context)

    def owner(self):
        function = getattr(state, 'create_rework', None)
        self.assertTrue(callable(function), 'T022 owning authorized rework entry is missing')
        return function

    def create(self):
        with state.open_store(self.root) as store:
            value = self.owner()(self.root, store, self.context)
            self.assertEqual(self.old_attempt, store.read_attempt(self.attempt_id))
            self.assertEqual(self.old_events, store.read_events(self.attempt_id))
            return value

    def reject(self, error=state.InvalidState):
        with state.open_store(self.root) as store:
            with self.assertRaises(error):
                self.owner()(self.root, store, self.context)
            self.assertEqual(self.old_attempt, store.read_attempt(self.attempt_id))
            self.assertEqual(self.old_events, store.read_events(self.attempt_id))
            self.assertIsNone(store.active_attempt())
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_authorized_entry_links_new_immutable_attempt(self):
        value = self.create()
        self.assertNotEqual(self.attempt_id, value['attempt_id'])
        self.assertEqual(self.attempt_id, value['predecessor'])
        self.assertEqual(self.commit, value['baseline'])
        self.assertEqual(self.after, value['tasks_bytes'])
        self.assertEqual(self.context, value['context'])
        self.assertEqual('owner-authorized rework', value['entry_mode'])
        self.assertEqual('active', value['progress'])
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_unbound_predecessor_rejected(self):
        self.reject()

    def test_absent_predecessor_rejected(self):
        self.context['prior_attempt_id'] = 'synthetic:missing'
        self.context['approval']['context_digest'] = context_digest(self.context)
        self.reject()

    def test_wrong_bound_commit_rejected(self):
        self.context['prior_implementation_commit'] = self.old_attempt['baseline']
        self.context['approval']['context_digest'] = context_digest(self.context)
        self.reject()

    def test_missing_authorization_rejected(self):
        del self.context['rework_approval']
        self.context['approval']['context_digest'] = context_digest(self.context)
        self.reject(contexts.ContextError)

    def test_unrenewed_corrective_checks_rejected(self):
        self.context['checks'][0]['id'] = 'unapproved-check'
        self.reject(contexts.ContextError)

    def test_unrenewed_corrective_scope_rejected(self):
        self.context['allowed_paths'] = ['correction.md']
        self.reject(contexts.ContextError)

    def test_clean_later_reviewed_baseline_keeps_original_binding(self):
        (self.root/'outside.md').write_bytes(b'Later independently authorized work')
        later = self.commit_current()
        value = self.create()
        self.assertEqual(later, value['baseline'])
        with state.open_store(self.root) as store:
            self.assertEqual(self.commit, state.implementation_binding(store, self.attempt_id)['implementation_commit'])

    def test_corrupt_predecessor_completion_phase_is_rejected(self):
        with state.open_store(self.root) as store:
            store.append_event(self.attempt_id, 'checkbox-applied',
                               {'schema_version': 1, 'intent_digest': '0' * 64,
                                'tasks_sha256': '0' * 64})
            self.old_events = store.read_events(self.attempt_id)
        self.reject()

    def test_actual_corrective_completion_keeps_checked_marker_and_old_events(self):
        value = self.create()
        (self.root/'guide.md').write_bytes(b'Owner authorized corrective output')
        review = self.fixture.review(self.context)
        review['attempt_id'] = value['attempt_id']
        with state.open_store(self.root) as store:
            acceptance.execute_acceptance(self.root, value, review, store)
            state.publish_completion(self.root, store, value['attempt_id'])
            intent = next(row['payload'] for row in store.read_events(value['attempt_id'])
                          if row['kind'] == 'completion-intent')
            self.assertEqual(self.after, bytes.fromhex(intent['tasks_before_hex']))
            self.assertEqual(self.after, bytes.fromhex(intent['tasks_after_hex']))
            self.assertEqual(self.old_attempt, store.read_attempt(self.attempt_id))
            self.assertEqual(self.old_events, store.read_events(self.attempt_id))
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_retained_schema_rejects_boolean_versions_and_sequence(self):
        intent = next(row['payload'] for row in self.old_events if row['kind'] == 'completion-intent')
        evidence = next(row['payload'] for row in self.old_events if row['kind'] == 'acceptance')
        state.validate_completion_intent_schema(intent)
        state.validate_acceptance_schema(evidence)
        for key in ('schema_version', 'acceptance_sequence'):
            invalid = copy.deepcopy(intent)
            invalid[key] = True
            with self.assertRaises(state.InvalidState):
                state.validate_completion_intent_schema(invalid)
        evidence = copy.deepcopy(evidence)
        evidence['schema_version'] = True
        with self.assertRaises(state.InvalidState):
            state.validate_acceptance_schema(evidence)
