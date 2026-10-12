"""T003 durable local journal fixtures; no provider or guard authority."""
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from contextlib import closing

import task_handoff as handoff
import task_package as package
import approval_binding as binding


class ExclusiveOwnership:
    def __init__(self, state='exclusive', owner=None):
        self.state = state
        self.owner = owner

    def observe(self, repository, issue, owner):
        return handoff.OwnershipEvidence(repository, issue, self.owner or owner,
                                         self.state, 'fixture-verifier', 'fixture-observation')


class RecoveryTests(unittest.TestCase):
    """Host-injected recovery fixtures, never a real Feature 003 backend."""

    def setUp(self):
        import test_task_package as fixtures
        fixtures.PackageContractTests.setUp(self)
        self.value = package.build_package(self.input)
        self.key = handoff.HandoffKey(self.value['repository'], '17',
                                     self.value['package_digest'], 'guard-attempt')
        self.owner = handoff.Owner(str(self.root), 'host', 'process')
        self.calls = []
        self.binding = 'd' * 64
        self.verification_head = self.commit
        test = self

        class Guard:
            revision = test.value['execution']['guard_revision']
            capabilities = {name: package.CapabilityEvidence(True, test.value['execution']['guard_revision'], True, None)
                            for name in set(package.required_capabilities('resume', 'implement'))
                            | set(package.required_capabilities('completion', 'implement'))
                            | {'guard.rework'}}

            def validate_scope(self, snapshot, context, evidence):
                test.calls.append('scope')
                return package.GuardValidation(True)

            def validate_acceptance(self, context, evidence):
                test.calls.append('acceptance')
                return package.GuardValidation(True)

            def resume(self, context, attempt, evidence, *, operation_key):
                test.calls.append('resume')
                test.assertTrue(operation_key.startswith('guard-resume:'))
                return test.observation

            def rework(self, context, attempt, corrective, authorization, *, operation_key):
                test.calls.append('rework')
                test.assertTrue(operation_key.startswith('guard-rework:'))
                return handoff.GuardRecovery('corrective-attempt', 'owner-authorized rework', 'active',
                    None, None, test.commit, 'sha256:' + 'b' * 64, predecessor_attempt_id=attempt)

        self.guard = Guard()
        self.observation = handoff.GuardRecovery('guard-attempt', 'handoff-only', 'completed',
            self.commit, 'a' * 64, self.commit, 'sha256:' + 'b' * 64)

    def git(self, *args):
        import test_task_package as fixtures
        return fixtures.RepositoryPrimitiveTests.git(self, *args)

    def readiness(self, *, boundary):
        self.calls.append(boundary)
        return dict(readiness_valid=True, binding_digest=self.binding, blockers=[])

    def open(self):
        return handoff.open_journal(self.root, self.key.repository, self.key.issue)

    def recovery(self, journal, *, initialize=True):
        result = handoff.Recovery(journal, self.key, self.owner, ExclusiveOwnership())
        if initialize:
            result.initialize(self.value)
            result.advance('validated-offline', {'evidence_ref': 'sha256:' + 'c' * 64})
            result.advance('ready', {'binding_digest': self.binding})
        return result

    def resume(self, recovery, **ports):
        return recovery.resume(self.value, {'root': self.root, 'head': self.verification_head}, {},
            guard=ports.pop('guard', self.guard), readiness=ports.pop('readiness', self.readiness),
            **ports)

    def test_completed_resume_reuses_identity_and_tracks_separate_verification(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            first = self.resume(recovery)
            self.assertTrue(first['resume_valid'], first)
            self.assertEqual('completed-awaiting-handoff', first['progress'])
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.observation = handoff.GuardRecovery('guard-attempt', 'handoff-only', 'completed',
                self.commit, 'a' * 64, 'e' * 40, 'sha256:' + 'f' * 64)
            self.verification_head = 'e' * 40
            second = self.resume(recovery)
            self.assertTrue(second['resume_valid'], second)
            self.assertEqual(self.commit, second['implementation_commit'])
            self.assertEqual('e' * 40, second['verification_head'])
            self.assertEqual('handoff-only', second['effective_mode'])
            self.assertFalse(second['execution_eligible'])
            self.assertNotIn('rework', self.calls)

    def test_changed_serialized_inputs_and_different_key_conflict(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.value['approval_refs']['execution'] = 'sha256:' + 'e' * 64
            result = self.resume(recovery)
            self.assertIn('recovery-input-conflict', [x['code'] for x in result['blockers']])
            self.assertNotIn('resume', self.calls)
            other = handoff.HandoffKey(self.key.repository, '17', 'e' * 64, 'another-attempt')
            with self.assertRaises(handoff.StateConflict):
                handoff.Recovery(journal, other, self.owner, ExclusiveOwnership()).initialize(self.value)

    def test_failed_revalidation_preserves_completed_identity_and_appends_blocker(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            with patch.object(self.guard, 'validate_acceptance',
                              return_value=package.GuardValidation(False)):
                failed = self.resume(recovery)
            self.assertFalse(failed['resume_valid'])
            self.assertEqual('completed-awaiting-handoff', failed['progress'])
            self.assertEqual(self.commit, failed['implementation_commit'])
            self.assertEqual('lifecycle-blocker', journal.events(self.key)[-1]['kind'])
            self.assertNotIn('rework', self.calls)
            with self.assertRaises(handoff.StateConflict):
                recovery.advance('reviewed', {'candidate': self.commit,
                                             'evidence_ref': 'sha256:' + 'c' * 64})
            self.assertTrue(self.resume(recovery)['resume_valid'])
            recovery.advance('reviewed', {'candidate': self.commit,
                                         'evidence_ref': 'sha256:' + 'c' * 64})

    def test_missing_corrupt_changed_or_regressed_guard_completion_blocks(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            for observation in (None, True,
                    handoff.GuardRecovery('other', 'handoff-only', 'completed', self.commit,
                                          'a' * 64, self.commit, 'sha256:' + 'b' * 64),
                    handoff.GuardRecovery('guard-attempt', 'handoff-only', 'completed', 'e' * 40,
                                          'a' * 64, self.commit, 'sha256:' + 'b' * 64),
                    handoff.GuardRecovery('guard-attempt', 'implement', 'active', None,
                                          None, self.commit, 'sha256:' + 'b' * 64)):
                self.observation = observation
                with self.subTest(observation=observation):
                    result = self.resume(recovery)
                    self.assertFalse(result['resume_valid'])
                    self.assertEqual(self.commit, result['implementation_commit'])
                    self.assertEqual('completed-awaiting-handoff', result['progress'])

    def test_unavailable_guard_readiness_and_changed_postcheck_preserve_work(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            for ports in ({'guard': None}, {'readiness': None}):
                self.assertFalse(self.resume(recovery, **ports)['resume_valid'])
            def drift(*, boundary):
                return dict(readiness_valid=True, binding_digest=self.binding if boundary == 'entry'
                            else 'f' * 64, blockers=[])
            self.assertFalse(self.resume(recovery, readiness=drift)['resume_valid'])
            self.assertEqual('ready', recovery.status()['progress'])
            self.assertNotIn('rework', self.calls)

    def test_recovery_requires_fresh_ownership_at_each_guard_boundary(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            recovery.ownership = ExclusiveOwnership('unknown')
            self.assertFalse(self.resume(recovery)['resume_valid'])
            self.assertEqual([], self.calls)
            recovery.ownership = ExclusiveOwnership()
            original = self.guard.validate_acceptance
            def lost(*args):
                recovery.ownership = ExclusiveOwnership('conflict')
                return original(*args)
            with patch.object(self.guard, 'validate_acceptance', side_effect=lost):
                self.assertFalse(self.resume(recovery)['resume_valid'])
            self.assertNotIn('resume', self.calls)

    def test_monotonic_progress_and_unknown_results_remain_incomplete(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            with self.assertRaises(handoff.StateConflict):
                recovery.advance('published', {'operation_key': 'publication'})
            recovery.advance('reviewed', {'candidate': self.commit,
                                         'evidence_ref': 'sha256:' + 'c' * 64})
            journal.intent(self.key, 'publication', {'commit': self.commit})
            journal.result(self.key, 'publication', 'write-unknown', {})
            with self.assertRaises(handoff.StateConflict):
                recovery.advance('published', {'operation_key': 'publication'})
            self.assertEqual('reviewed', recovery.status()['progress'])
            journal.result(self.key, 'publication', 'succeeded', {'id': 'one-pr'})
            recovery.advance('published', {'operation_key': 'publication'})
            journal.intent(self.key, 'evidence', {'commit': self.commit})
            journal.result(self.key, 'evidence', 'succeeded', {'id': 'one-evidence'})
            recovery.advance('evidence-durable', {'operation_key': 'evidence'})
            journal.intent(self.key, 'labels', {'target': 'human-review'})
            recovery.advance('final-label-intent', {'operation_key': 'labels'})
            journal.result(self.key, 'labels', 'write-unknown', {})
            with self.assertRaises(handoff.StateConflict):
                recovery.advance('handed-off', {'operation_key': 'labels'})
            journal.result(self.key, 'labels', 'succeeded', {'confirmed': True})
            recovery.advance('handed-off', {'operation_key': 'labels'})
            self.assertEqual('handed-off', recovery.status()['progress'])

    def test_unknown_lifecycle_event_is_retained_and_blocks(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            journal.append(self.key, 'lifecycle-future', {'progress': 'magic'})
            before = journal.events(self.key)
            with self.assertRaises(handoff.InvalidState):
                recovery.status()
            self.assertEqual(before, journal.events(self.key))

    def test_changed_verification_head_requires_fresh_review_before_publication(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            recovery.advance('reviewed', {'candidate': self.commit,
                                         'evidence_ref': 'sha256:' + 'c' * 64})
            self.verification_head = 'e' * 40
            self.observation = handoff.GuardRecovery('guard-attempt', 'handoff-only', 'completed',
                self.commit, 'a' * 64, self.verification_head, 'sha256:' + 'f' * 64)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            journal.intent(self.key, 'publication', {'commit': self.verification_head})
            journal.result(self.key, 'publication', 'succeeded', {'id': 'one-pr'})
            with self.assertRaises(handoff.StateConflict):
                recovery.advance('published', {'operation_key': 'publication'})
            self.assertEqual('reviewed', recovery.status()['progress'])
            self.assertEqual(self.commit, recovery.status()['implementation_commit'])
            with self.assertRaises(handoff.StateConflict):
                recovery.advance('reviewed', {'candidate': self.verification_head,
                                             'evidence_ref': 'sha256:' + 'e' * 64})

    def test_rework_needs_exact_owner_authorization_scope_checks_and_predecessor(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            corrective = dict(prior_commit=self.commit, prior_content_digest='a' * 64,
                              scope=['guide.md'], checks=[['approved-check']],
                              approval_ref='sha256:' + 'c' * 64)
            class Authority:
                def verify(self, reference, target):
                    return binding.ApprovalVerification(True, 'authenticated-provider', target,
                                                        'sha256:' + 'd' * 64)
            for change in ({'prior_commit': 'e' * 40}, {'scope': []}, {'checks': []}):
                self.assertFalse(self.resume(recovery, corrective={**corrective, **change},
                                             authority=Authority())['resume_valid'])
            self.assertFalse(self.resume(recovery, corrective=corrective)['resume_valid'])
            self.assertNotIn('rework', self.calls)
            valid = self.resume(recovery, corrective=corrective, authority=Authority())
            self.assertTrue(valid['resume_valid'], valid)
            self.assertEqual('owner-authorized rework', valid['effective_mode'])
            self.assertEqual(self.commit, valid['implementation_commit'])
            self.assertEqual('completed-awaiting-handoff', valid['progress'])
            self.assertEqual(1, self.calls.count('rework'))
            self.assertEqual('corrective-attempt', valid['rework_attempt_id'])
            self.assertFalse(valid['verification_valid'])
            self.assertTrue(self.resume(recovery, corrective=corrective,
                                       authority=Authority())['resume_valid'])
            self.assertEqual(1, self.calls.count('rework'))
            self.observation = handoff.GuardRecovery('corrective-attempt', 'handoff-only', 'completed',
                'e' * 40, 'e' * 64, self.commit, 'sha256:' + 'e' * 64,
                predecessor_attempt_id='guard-attempt')
            complete = self.resume(recovery)
            self.assertTrue(complete['resume_valid'], complete)
            self.assertEqual(self.commit, complete['implementation_commit'])
            self.assertEqual('e' * 40, complete['rework_implementation_commit'])

    def test_interruption_after_guard_completion_reuses_attempt_and_operation_key(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            original = journal.append
            def interrupted(key, kind, payload):
                if kind == 'lifecycle-verification':
                    raise KeyboardInterrupt()
                return original(key, kind, payload)
            with patch.object(journal, 'append', side_effect=interrupted):
                with self.assertRaises(KeyboardInterrupt):
                    self.resume(recovery)
            pending = [event['payload']['operation_key'] for event in journal.events(self.key)
                       if event['kind'] == 'intent']
            self.assertEqual(1, len(pending))
            self.assertEqual('intent', journal.operation(self.key, pending[0])['status'])
            self.assertEqual('guard-active', recovery.status()['progress'])
        with self.open() as journal:
            recovery = self.recovery(journal, initialize=False)
            result = self.resume(recovery)
            self.assertTrue(result['resume_valid'], result)
            self.assertEqual(self.commit, result['implementation_commit'])
            self.assertEqual('succeeded', journal.operation(self.key, pending[0])['status'])
            self.assertEqual(1, len([event for event in journal.events(self.key)
                                    if event['kind'] == 'intent']))

    def test_actual_tracker_authority_routes_corrective_work_after_failed_acceptance(self):
        import task_tracker as tracker
        import test_task_tracker as fixtures
        from dataclasses import asdict
        import hashlib
        self.value['task_id'] = 'T014'
        self.value.pop('package_digest')
        self.value = package.build_package(self.value)
        self.key = handoff.HandoffKey(self.value['repository'], '17',
                                     self.value['package_digest'], 'guard-attempt')
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.assertTrue(self.resume(recovery)['resume_valid'])
            url = 'https://github.com/fixture/offline/issues/17#issuecomment-1'
            corrective = dict(prior_commit=self.commit, prior_content_digest='a' * 64,
                              scope=['guide.md'], checks=[['approved-check']], approval_ref=url)
            digest = hashlib.sha256(package.canonical_json(corrective)).hexdigest()
            target = tracker.DecisionTarget(self.value['repository'], self.value['feature'],
                self.value['task_id'], 'rework', self.commit, self.value['package_digest'], digest)
            payload = dict(object_id='comment-1', repository=self.value['repository'],
                actor_id='owner-1', state='approved', created_at=900, decided_at=900,
                updated_at=900, expires_at=2000, target=asdict(target))
            reference = tracker.ApprovalReference('comment-1', url,
                hashlib.sha256(fixtures.encoded(payload)).hexdigest(), target)
            config = tracker.TrackerConfig('github', self.value['repository'], 'repo-1',
                                           'owner-1', 'host-verifier', 'e' * 40)
            port = tracker.AuthenticatedTracker(config, fixtures.Transport(payload), clock=lambda: 1000)
            authority = tracker.ApprovalAuthority(port, self.value['feature'], 'T014', {url: reference})
            with patch.object(self.guard, 'validate_acceptance',
                              return_value=package.GuardValidation(False)):
                self.assertFalse(self.resume(recovery)['resume_valid'])
                result = self.resume(recovery, corrective=corrective, authority=authority)
            self.assertTrue(result['resume_valid'], result)
            self.assertEqual(self.commit, result['implementation_commit'])
            self.assertEqual('corrective-attempt', result['rework_attempt_id'])

    def test_missing_record_cannot_resume_or_create_baseline(self):
        with self.open() as journal:
            recovery = self.recovery(journal, initialize=False)
            with self.assertRaises(handoff.InvalidState):
                self.resume(recovery)
            self.assertEqual(0, journal._db.execute('SELECT count(*) FROM records').fetchone()[0])

    def test_resume_cli_default_blocks_and_injected_fixture_routes_exact_attempt(self):
        import task_automation as automation
        import contextlib
        import io
        import json
        (self.root / 'package.json').write_bytes(package.package_bytes(self.value))
        def invoke(**ports):
            stream = io.StringIO()
            with contextlib.redirect_stdout(stream):
                code = automation.main(['resume', '--package', 'package.json', '--attempt',
                    self.key.guard_attempt_id, '--offline'], root=self.root,
                    repository=self.value['repository'], **ports)
            return code, json.loads(stream.getvalue())
        code, result = invoke()
        self.assertEqual(3, code)
        self.assertFalse(result['resume_valid'])
        self.assertFalse((self.root / '.symphony').exists())
        with self.open() as journal:
            missing = self.recovery(journal, initialize=False)
            self.assertEqual(6, invoke(recovery=missing, recovery_ports={})[0])
            self.assertEqual(0, journal._db.execute('SELECT count(*) FROM records').fetchone()[0])
            recovery = self.recovery(journal)
            code, result = invoke(recovery=recovery, recovery_ports=dict(
                snapshot={'root': self.root, 'head': self.commit}, evidence={}, guard=self.guard,
                readiness=self.readiness))
            self.assertEqual(0, code, result)
            self.assertEqual(self.commit, result['implementation_commit'])
            self.assertFalse(result['execution_eligible'])
            other = handoff.Recovery(journal, handoff.HandoffKey(self.key.repository, '17',
                self.key.package_digest, 'other'), self.owner, ExclusiveOwnership())
            self.assertEqual(5, invoke(recovery=other, recovery_ports={})[0])
            other.key = handoff.HandoffKey('other/repository', '17', self.key.package_digest,
                                           self.key.guard_attempt_id)
            self.assertEqual(5, invoke(recovery=other, recovery_ports={})[0])
            with patch.object(journal, 'root', str(self.root / 'other')):
                self.assertEqual(5, invoke(recovery=recovery, recovery_ports={})[0])
            for failure in (handoff.PersistenceFailure('sensitive-output'),
                            handoff.InvalidState('sensitive-output')):
                with patch.object(recovery, 'resume', side_effect=failure):
                    code, result = invoke(recovery=recovery, recovery_ports={})
                self.assertEqual(6, code)
                self.assertNotIn('sensitive-output', str(result))


class PublicationTests(unittest.TestCase):
    git = RecoveryTests.git
    open = RecoveryTests.open
    recovery = RecoveryTests.recovery
    resume = RecoveryTests.resume
    readiness = RecoveryTests.readiness

    def setUp(self):
        RecoveryTests.setUp(self)
        import task_tracker as tracker
        self.tracker_module = tracker
        self.value.update(task_id='T015', issue={'repository': self.value['repository'], 'number': 17})
        self.value['execution']['external_actions'] = ['publish-pr', 'write-evidence']
        self.value.pop('package_digest')
        self.value = package.build_package(self.value)
        self.key = handoff.HandoffKey(self.value['repository'], '17',
                                     self.value['package_digest'], 'guard-attempt')
        self.request = dict(repository=str(self.root), source=self.commit, base_ref='refs/heads/main',
            base='b' * 40, head_ref='refs/heads/task', head=self.commit)
        self.source = dict(source_valid=True, blockers=[], snapshot=dict(root=str(self.root),
            checker=self.commit, base='b' * 40, head=self.commit))
        self.prepared = dict(status='PREPARED', source=self.commit, base='b' * 40,
            head=self.commit, tree='c' * 40, checkout='d' * 40, check_exit=0)
        self.writes = []
        self.remote = {}
        self.unknown = False
        self.absent_unknown = False
        test = self

        class Publisher:
            capabilities = {name: package.CapabilityEvidence(True, test.commit, True, None)
                            for name in ('tracker.pr-publication', 'tracker.evidence')}

            def find_pr(self, request):
                return test.remote.get('pr')

            def find_evidence(self, request):
                return test.remote.get('evidence')

            def write(self, kind, request, operation_key):
                test.writes.append((kind, operation_key))
                if not test.absent_unknown:
                    test.remote[kind] = tracker.PublicationObject(kind + '-1',
                        'https://github.com/fixture/offline/' + ('pull/19' if kind == 'pr'
                        else 'issues/17#issuecomment-1'), handoff._digest(package.canonical_json(request['content'])))
                if test.unknown or test.absent_unknown:
                    raise tracker.WriteUnknown('sensitive-provider-output')

            def publish_pr(self, request, *, operation_key):
                self.write('pr', request, operation_key)

            def write_evidence(self, request, *, operation_key):
                self.write('evidence', request, operation_key)

        class Authority:
            def verify(self, reference, target):
                return binding.ApprovalVerification(True, 'authenticated-provider', target, reference)

        class Reviewer:
            def verify(self, reference, target):
                return handoff.IndependentReview(target, 'author-1', 'reviewer-1', reference,
                                                 test.commit, 'authenticated-provider')

        self.publisher, self.authority, self.reviewer = Publisher(), Authority(), Reviewer()

    def publish(self, recovery, **ports):
        return handoff.publish(recovery, self.value, {'root': self.root, 'head': self.commit}, {},
            guard=ports.pop('guard', self.guard), readiness=self.readiness,
            tracker=ports.pop('tracker', self.publisher), authority=ports.pop('authority', self.authority),
            reviewer=ports.pop('reviewer', self.reviewer), implementer_id='author-1',
            review_ref='sha256:' + 'a' * 64,
            publication_ref=ports.pop('publication_ref', 'sha256:' + 'b' * 64),
            validated_source=self.source, preparation=self.request, reviewed_checker=self.commit,
            observe_refs=ports.pop('observe_refs', lambda: {'base': 'b' * 40, 'head': self.commit}),
            pr_content=ports.pop('pr_content', {'title': 'fixture', 'body': 'scope/check evidence'}),
            evidence_content={'checks': 'sha256:' + 'b' * 64}, **ports)

    def prepared_port(self):
        return patch('task_source.prepare_candidate', return_value=self.prepared)

    def test_publication_and_evidence_reuse_after_uncertain_success(self):
        self.unknown = True
        with self.open() as journal, self.prepared_port() as prepare:
            recovery = self.recovery(journal)
            result = self.publish(recovery)
            self.assertTrue(result['publication_valid'], result)
            self.assertEqual('evidence-durable', result['progress'])
            self.assertEqual(2, len(self.writes))
            self.assertEqual(self.commit, prepare.call_args.args[1]['source'])
        with self.open() as journal, self.prepared_port():
            result = self.publish(self.recovery(journal, initialize=False))
            self.assertTrue(result['publication_valid'], result)
            self.assertEqual(2, len(self.writes))
            self.assertEqual(self.commit, result['implementation_commit'])

    def test_unknown_absence_retries_are_bounded_and_retained_before_blocker(self):
        self.absent_unknown = True
        with self.open() as journal, self.prepared_port():
            recovery = self.recovery(journal)
            result = self.publish(recovery)
            self.assertFalse(result['publication_valid'])
            self.assertEqual(4, len(self.writes))
            self.assertEqual(1, len({key for _, key in self.writes}))
            self.assertEqual('reviewed', result['progress'])
            events = journal.events(self.key)
            self.assertEqual('lifecycle-blocker', events[-1]['kind'])
            self.assertTrue(any(event['kind'] == 'result' and event['payload']['status'] == 'write-unknown'
                                for event in events[:-1]))
            self.assertNotIn('sensitive-provider-output', str(events))

    def test_missing_review_authority_capability_failed_check_and_ref_drift_write_nothing(self):
        with self.open() as journal, self.prepared_port():
            recovery = self.recovery(journal)
            for ports in ({'reviewer': None}, {'authority': None}, {'tracker': None},
                          {'observe_refs': lambda: {'base': 'f' * 40, 'head': self.commit}}):
                self.assertFalse(self.publish(recovery, **ports)['publication_valid'])
            with patch.object(self.guard, 'validate_acceptance', return_value=package.GuardValidation(False)):
                self.assertFalse(self.publish(recovery)['publication_valid'])
            self.assertEqual([], self.writes)

    def test_changed_publication_input_conflicts_without_duplicate_write(self):
        with self.open() as journal, self.prepared_port():
            recovery = self.recovery(journal)
            self.assertTrue(self.publish(recovery)['publication_valid'])
            result = self.publish(recovery, pr_content={'title': 'changed', 'body': 'changed'})
            self.assertFalse(result['publication_valid'])
            self.assertEqual(2, len(self.writes))

    def test_actual_authenticated_publication_facade_and_ambiguous_timeout(self):
        import test_task_tracker as fixtures
        tracker = self.tracker_module
        test = self
        class Host:
            ambiguous = False
            objects = {}
            approval_payload = None
            reads = []

            def read(self, kind, identity):
                self.reads.append(kind)
                if kind == 'approval':
                    return tracker.AuthenticatedRead('github', 'repo-1', 'host-verifier', 'e' * 40,
                        fixtures.NOW, fixtures.encoded(self.approval_payload))
                found = self.objects.get(kind)
                matches = [] if found is None else [found] * (2 if self.ambiguous else 1)
                return tracker.AuthenticatedRead('github', 'repo-1', 'host-verifier', 'e' * 40,
                    fixtures.NOW, fixtures.encoded({'matches': matches}))

            def write(self, kind, request, *, operation_key):
                test.writes.append((kind, operation_key))
                self.objects[kind] = dict(object_id=kind + '-1',
                    url='https://github.com/fixture/offline/' + ('pull/19' if kind == 'pr'
                    else 'issues/17#issuecomment-1'), identity=request['identity'],
                    base=request['base'], head=request['head'], checker=request['checker'],
                    base_ref=request['base_ref'], head_ref=request['head_ref'],
                    head_repository=request['head_repository'],
                    content_digest=handoff._digest(package.canonical_json(request['content'])))
                if test.unknown:
                    self.ambiguous = True
                    raise tracker.WriteUnknown()
        config = tracker.TrackerConfig('github', self.value['repository'], 'repo-1',
                                       'owner-1', 'host-verifier', 'e' * 40)
        host = Host()
        actual = tracker.PublicationTracker(tracker.AuthenticatedTracker(config, host,
            clock=lambda: fixtures.NOW), host)
        actual.capabilities = self.publisher.capabilities
        identity = dict(repository=self.value['repository'], issue='17', feature=self.value['feature'],
            task_id='T015', package_digest=self.value['package_digest'], guard_attempt_id='guard-attempt',
            implementation_commit=self.commit, content_digest='a' * 64)
        common = dict(identity=identity, base='b' * 40, head=self.commit, checker=self.commit,
                      base_ref='refs/heads/main', head_ref='refs/heads/task',
                      head_repository=self.value['repository'])
        requests = dict(pr={**common, 'content': {'title': 'fixture', 'body': 'scope/check evidence'}},
                        evidence={**common, 'content': {'checks': 'sha256:' + 'b' * 64}})
        digest = handoff._digest(package.canonical_json(dict(requests=requests,
            preparation_tree='c' * 40, checks_ref='sha256:' + 'b' * 64)))
        from dataclasses import asdict
        target = tracker.DecisionTarget(self.value['repository'], self.value['feature'], 'T015',
            'publication', self.commit, self.value['package_digest'], digest)
        url = 'https://github.com/fixture/offline/issues/17#issuecomment-1'
        host.approval_payload = dict(object_id='comment-1', repository=self.value['repository'],
            actor_id='owner-1', state='approved', created_at=900, decided_at=900, updated_at=900,
            expires_at=2000, target=asdict(target))
        reference = tracker.ApprovalReference('comment-1', url,
            handoff._digest(fixtures.encoded(host.approval_payload)), target)
        authority = tracker.ApprovalAuthority(actual.reader, self.value['feature'], 'T015', {url: reference})
        self.unknown = True
        with self.open() as journal, self.prepared_port():
            recovery = self.recovery(journal)
            result = self.publish(recovery, tracker=actual, authority=authority, publication_ref=url)
            self.assertFalse(result['publication_valid'])
            self.assertEqual(1, len(self.writes))
            statuses = [event['payload']['status'] for event in journal.events(self.key)
                        if event['kind'] == 'result']
            self.assertIn('ambiguous', statuses)
            self.assertEqual('reviewed', result['progress'])
            self.assertIn('approval', host.reads)
            host.objects.clear()
            host.ambiguous = False
            retry = self.publish(recovery, tracker=actual, authority=authority, publication_ref=url)
            self.assertFalse(retry['publication_valid'])
            self.assertEqual(1, len(self.writes))

    def test_ref_drift_before_evidence_and_nonindependent_review_block(self):
        with self.open() as journal, self.prepared_port():
            recovery = self.recovery(journal)
            class SameActor:
                def verify(self, reference, target):
                    return handoff.IndependentReview(target, 'author-1', 'author-1', reference,
                        'e' * 40, 'authenticated-provider')
            self.assertFalse(self.publish(recovery, reviewer=SameActor())['publication_valid'])
            self.assertEqual([], self.writes)
            result = self.publish(recovery, observe_refs=lambda: {
                'base': 'b' * 40 if not self.writes else 'f' * 40, 'head': self.commit})
            self.assertFalse(result['publication_valid'])
            self.assertEqual('published', result['progress'])
            self.assertEqual(1, len(self.writes))

    def test_preparation_failure_and_wrong_checker_write_nothing(self):
        with self.open() as journal:
            recovery = self.recovery(journal)
            self.prepared['check_exit'] = 1
            with self.prepared_port():
                self.assertFalse(self.publish(recovery)['publication_valid'])
            self.prepared['check_exit'] = 0
            self.request['source'] = 'f' * 40
            with self.prepared_port():
                self.assertFalse(self.publish(recovery)['publication_valid'])
            self.assertEqual([], self.writes)

    def test_interruption_after_remote_write_reconciles_existing_object(self):
        with self.open() as journal, self.prepared_port():
            recovery = self.recovery(journal)
            def interrupted(*args):
                if args[1].startswith('publish-pr:'):
                    raise KeyboardInterrupt()
                return original(*args)
            original = journal.result
            with patch.object(journal, 'result', side_effect=interrupted):
                with self.assertRaises(KeyboardInterrupt):
                    self.publish(recovery)
            self.assertEqual(1, len(self.writes))
        with self.open() as journal, self.prepared_port():
            result = self.publish(self.recovery(journal, initialize=False))
            self.assertTrue(result['publication_valid'], result)
            self.assertEqual(2, len(self.writes))


class JournalTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.key = handoff.HandoffKey('fixture/repo', '17', 'a' * 64, 'guard-attempt')
        self.owner = handoff.Owner(str(self.root), 'fixture-host', 'fixture-process')

    def open(self, root=None, issue='17'):
        return handoff.open_journal(root or self.root, 'fixture/repo', issue)

    def child(self, code):
        return subprocess.run([sys.executable, '-B', '-c', code, str(self.root)],
                              env={**os.environ, 'PYTHONPATH': str(Path(__file__).parent)},
                              capture_output=True, text=True, timeout=10)

    def test_monotonic_keyed_events_and_reopen(self):
        with self.open() as journal:
            journal.create_record(self.key)
            self.assertEqual(1, journal.append(self.key, 'prepared', {'digest': 'a'}))
            self.assertEqual(2, journal.append(self.key, 'blocker', {'reason': 'fixture'}))
            other = handoff.HandoffKey('fixture/repo', '17', 'b' * 64, 'guard-attempt')
            journal.create_record(other)
            self.assertEqual(1, journal.append(other, 'prepared', {}))
        with self.open() as journal:
            self.assertEqual([1, 2], [event['sequence'] for event in journal.events(self.key)])
            self.assertEqual(3, journal.append(self.key, 'observed', {}))

    def test_intents_results_are_durable_idempotent_and_bound(self):
        with self.open() as journal:
            journal.create_record(self.key)
            first = journal.intent(self.key, 'publication', {'identity': 'exact', 'precondition': 'pin'})
            self.assertEqual(first, journal.intent(self.key, 'publication',
                                                  {'precondition': 'pin', 'identity': 'exact'}))
            with self.assertRaises(handoff.StateConflict):
                journal.intent(self.key, 'publication', {'identity': 'changed'})
            journal.result(self.key, 'publication', 'write-unknown', {'reason': 'timeout'})
        with self.open() as journal:
            self.assertEqual('write-unknown', journal.operation(self.key, 'publication')['status'])
            self.assertEqual('exact', journal.operation(self.key, 'publication')['intent']['identity'])
            journal.result(self.key, 'publication', 'succeeded', {'object': 'one'})
            count = len(journal.events(self.key))
            journal.result(self.key, 'publication', 'succeeded', {'object': 'one'})
            self.assertEqual(count, len(journal.events(self.key)))
            with self.assertRaises(handoff.StateConflict):
                journal.result(self.key, 'publication', 'write-unknown', {})
            with self.assertRaises(handoff.StateConflict):
                journal.result(self.key, 'missing-intent', 'succeeded', {})

    def test_rollback_cannot_commit_nested_append(self):
        with self.open() as journal:
            journal.create_record(self.key)
            with self.assertRaises(RuntimeError):
                with journal.transaction():
                    journal.append(self.key, 'discarded', {})
                    raise RuntimeError('fixture')
            self.assertEqual([], journal.events(self.key))

    def test_crash_after_intent_retains_it_and_releases_lock(self):
        result = self.child("""
import os, sys
from pathlib import Path
import task_handoff as h
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    key = h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt')
    j.create_record(key)
    j.intent(key, 'publication', {'identity': 'one'})
    os._exit(23)
""")
        self.assertEqual(23, result.returncode, result.stderr)
        with self.open() as journal:
            self.assertEqual('intent', journal.operation(self.key, 'publication')['status'])

    def test_crash_before_transaction_commit_retains_prior_evidence(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'prepared', {})
        result = self.child("""
import os, sys
from pathlib import Path
import task_handoff as h
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    with j.transaction():
        j.append(h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt'), 'lost', {})
        os._exit(24)
""")
        self.assertEqual(24, result.returncode, result.stderr)
        with self.open() as journal:
            self.assertEqual(['prepared'], [event['kind'] for event in journal.events(self.key)])

    def test_competing_process_cannot_open_same_issue(self):
        with self.open():
            result = self.child("""
import sys
from pathlib import Path
import task_handoff as h
try:
    with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17'):
        pass
except h.StateConflict:
    sys.exit(5)
""")
            self.assertEqual(5, result.returncode, result.stderr)
        with self.open():
            pass

    def test_issue_locks_are_separate(self):
        with self.open():
            with self.open(issue='18'):
                pass

    def test_ownership_requires_fresh_cross_host_port(self):
        with self.open() as journal:
            journal.claim_owner(self.owner)
            with self.assertRaises(handoff.StateConflict):
                journal.require_ownership(self.owner)
            journal.require_ownership(self.owner, ExclusiveOwnership())
            for state in ('unknown', 'conflict'):
                with self.subTest(state=state), self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(self.owner, ExclusiveOwnership(state))
            self.assertEqual(self.owner, journal.owner())
        with self.open() as journal:
            with self.assertRaises(handoff.StateConflict):
                journal.require_ownership(self.owner)
            self.assertEqual(self.owner, journal.owner())

    def test_competing_checkout_host_process_preserves_owner(self):
        with self.open() as journal:
            journal.claim_owner(self.owner)
            for other in (handoff.Owner(str(self.root), 'other-host', 'fixture-process'),
                          handoff.Owner(str(self.root), 'fixture-host', 'other-process')):
                with self.subTest(other=other), self.assertRaises(handoff.StateConflict):
                    journal.claim_owner(other)
                with self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(self.owner, ExclusiveOwnership(owner=other))
            self.assertEqual(self.owner, journal.owner())
        with tempfile.TemporaryDirectory() as other_root:
            with self.open(Path(other_root).resolve()) as journal:
                other = handoff.Owner(str(Path(other_root).resolve()), 'other-host', 'other-process')
                journal.claim_owner(other)
                with self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(other, ExclusiveOwnership(owner=self.owner))

    def test_guard_journal_is_untouched_and_handoff_is_ignored(self):
        guard = self.root / '.symphony/single-task/journal.sqlite3'
        guard.parent.mkdir(parents=True)
        guard.write_bytes(b'guard-owned-fixture')
        with self.open() as journal:
            journal.create_record(self.key)
        self.assertEqual(b'guard-owned-fixture', guard.read_bytes())
        repo = Path(__file__).resolve().parents[1]
        result = subprocess.run(['git', 'check-ignore', '.symphony/task-handoff/example/journal.sqlite3'],
                                cwd=repo, capture_output=True, text=True)
        self.assertEqual(0, result.returncode, result.stderr)

    def test_corrupt_or_unknown_schema_retained(self):
        with self.open():
            pass
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        for mutation in ('corrupt', 'version', 'trigger'):
            original = database.read_bytes()
            if mutation == 'corrupt':
                database.write_bytes(b'not sqlite')
            else:
                with closing(sqlite3.connect(database)) as connection:
                    connection.execute('PRAGMA user_version=99' if mutation == 'version'
                                       else 'DROP TRIGGER immutable_events_delete')
                    connection.commit()
            before = database.read_bytes()
            with self.subTest(mutation=mutation), self.assertRaises(handoff.InvalidState):
                with self.open():
                    pass
            self.assertEqual(before, database.read_bytes())
            database.write_bytes(original)

    def test_unsafe_paths_and_hardlinks_rejected(self):
        with self.open():
            pass
        directory = next(self.root.glob('.symphony/task-handoff/*'))
        database = directory / 'journal.sqlite3'
        copy = self.root / 'retained'
        database.rename(copy)
        database.symlink_to(copy)
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        database.unlink()
        os.link(copy, database)
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        database.unlink()
        copy.rename(database)
        with tempfile.TemporaryDirectory() as outside:
            directory.rename(self.root / 'retained-directory')
            directory.symlink_to(outside, target_is_directory=True)
            with self.assertRaises(handoff.InvalidState):
                with self.open():
                    pass
            self.assertEqual([], list(Path(outside).iterdir()))

    def test_missing_journal_does_not_invent_new_baseline(self):
        with self.open() as journal:
            journal.claim_owner(self.owner)
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        database.unlink()
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass

    def test_append_only_sql_and_reserved_operation_events(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'prepared', {})
            for kind in ('intent', 'result'):
                with self.assertRaises(handoff.InvalidState):
                    journal.append(self.key, kind, {})
            for statement in ('DELETE FROM events', "UPDATE events SET kind='changed'",
                              'DELETE FROM records'):
                with self.subTest(statement=statement), self.assertRaises(sqlite3.IntegrityError):
                    journal._db.execute(statement)
            self.assertEqual(['prepared'], [event['kind'] for event in journal.events(self.key)])

    def test_unknown_ownership_port_failure_preserves_identity(self):
        class Broken:
            def observe(self, *args):
                raise RuntimeError('untrusted diagnostic fixture')

        class Mismatched:
            def observe(self, repository, issue, owner):
                return handoff.OwnershipEvidence('fixture/other', issue, owner,
                                                 'exclusive', 'fixture-verifier', 'fixture-ref')

        with self.open() as journal:
            journal.claim_owner(self.owner)
            for port in (Broken(), Mismatched()):
                with self.subTest(port=type(port).__name__), self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(self.owner, port)
            self.assertEqual(self.owner, journal.owner())

    def test_directory_durability_failure_rolls_back_event(self):
        with self.open() as journal:
            journal.create_record(self.key)
            with patch('task_handoff.os.fsync', side_effect=OSError('fixture fsync failure')):
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.append(self.key, 'uncommitted', {})
            with self.assertRaises(handoff.PersistenceFailure):
                journal.events(self.key)
        with self.open() as journal:
            self.assertEqual([], journal.events(self.key))

    def test_symlinked_ancestor_rejected(self):
        actual = self.root / 'actual'
        actual.mkdir()
        alias = self.root / 'alias'
        alias.symlink_to(actual, target_is_directory=True)
        child = actual / 'child'
        child.mkdir()
        with self.assertRaises(handoff.InvalidState):
            with self.open(alias / 'child'):
                pass
        self.assertEqual([], list(child.iterdir()))

    def test_large_uncommitted_crash_retains_only_committed_snapshot(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'prepared', {})
        result = self.child("""
import os, sys
from pathlib import Path
import task_handoff as h
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    j._db.execute('PRAGMA cache_size=5')
    with j.transaction():
        for i in range(48):
            j.append(h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt'),
                     'uncommitted', {'evidence': 'x'*65536})
        os._exit(25)
""")
        self.assertEqual(25, result.returncode, result.stderr)
        with self.open() as journal:
            self.assertEqual(['prepared'], [event['kind'] for event in journal.events(self.key)])

    def test_checkpoint_crashes_reopen_complete_old_or_new_state(self):
        with self.open() as journal:
            journal.create_record(self.key)
        for after in (False, True):
            result = self.child("""
import os, sys
from pathlib import Path
from unittest.mock import patch
import task_handoff as h
actual = os.replace
def interrupted(*args, **kwargs):
    if AFTER:
        actual(*args, **kwargs)
    os._exit(26)
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    with patch('task_handoff.os.replace', side_effect=interrupted):
        j.append(h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt'), 'checkpoint', {})
""".replace('AFTER', repr(after)))
            self.assertEqual(26, result.returncode, result.stderr)
            with self.open() as journal:
                self.assertEqual(int(after), len(journal.events(self.key)))
        directory = next(self.root.glob('.symphony/task-handoff/*'))
        retained = list(directory.glob('candidate-*.sqlite3'))
        self.assertEqual(1, len(retained))
        before = retained[0].read_bytes()
        with self.open() as journal:
            journal.append(self.key, 'later', {})
        self.assertEqual(before, retained[0].read_bytes())

    def test_directory_swap_cannot_write_outside_validated_descriptor(self):
        with self.open() as journal:
            journal.create_record(self.key)
        directory = next(self.root.glob('.symphony/task-handoff/*'))
        retained = self.root / 'retained-issue'
        with tempfile.TemporaryDirectory() as outside:
            outside = Path(outside).resolve()
            sentinel = outside / 'journal.sqlite3'
            sentinel.write_bytes(b'outside sentinel')
            actual = os.replace
            def swapped(*args, **kwargs):
                directory.rename(retained)
                directory.symlink_to(outside, target_is_directory=True)
                return actual(*args, **kwargs)
            with self.open() as journal:
                with patch('task_handoff.os.replace', side_effect=swapped):
                    with self.assertRaises(handoff.InvalidState):
                        journal.append(self.key, 'raced', {})
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.append(self.key, 'later', {})
            self.assertEqual(b'outside sentinel', sentinel.read_bytes())
            self.assertEqual(['journal.sqlite3'], [path.name for path in outside.iterdir()])
            self.assertTrue((retained / 'journal.sqlite3').is_file())

    def test_database_bound_checked_before_deserialization(self):
        with self.open():
            pass
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        with database.open('r+b') as stream:
            stream.truncate(handoff.MAX_DATABASE + 1)
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        self.assertEqual(handoff.MAX_DATABASE + 1, database.stat().st_size)

    def test_journal_growth_bound_rejects_without_truncating_evidence(self):
        with patch('task_handoff.MAX_DATABASE', 65536):
            with self.open() as journal:
                journal.create_record(self.key)
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.append(self.key, 'too-large', {'evidence': 'x' * 131072})
            with self.open() as journal:
                self.assertEqual([], journal.events(self.key))

    def test_replaced_or_modified_committed_file_not_overwritten(self):
        with self.open() as journal:
            journal.create_record(self.key)
            database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
            database.write_bytes(b'unknown replacement evidence')
            with self.assertRaises(handoff.InvalidState):
                journal.append(self.key, 'unsafe', {})
            self.assertEqual(b'unknown replacement evidence', database.read_bytes())

    def test_snapshot_replacement_during_load_is_preserved_and_rejected(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'initial', {})
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        with tempfile.TemporaryDirectory() as other_root:
            with self.open(Path(other_root).resolve()) as other:
                other.create_record(self.key)
                other.append(self.key, 'new-evidence', {})
            alternate = next(Path(other_root).glob('.symphony/task-handoff/*/journal.sqlite3')).read_bytes()
        for boundary in ('initialize', 'validate'):
            replacement = self.root / 'replacement.sqlite3'
            replacement.write_bytes(alternate)
            method = '__init__' if boundary == 'initialize' else '_validate'
            actual = getattr(handoff.Journal, method)
            def swapped(journal, *args):
                os.replace(replacement, database)
                return actual(journal, *args)
            with self.subTest(boundary=boundary), patch.object(handoff.Journal, method, swapped):
                with self.assertRaises(handoff.InvalidState):
                    with self.open():
                        pass
            self.assertEqual(alternate, database.read_bytes())

    def test_sqlite_snapshot_api_unavailable_fails_closed(self):
        class Unsupported:
            def close(self):
                pass
        with patch('task_handoff.sqlite3.connect', return_value=Unsupported()):
            with self.assertRaises(handoff.PersistenceFailure):
                with self.open():
                    pass
        self.assertEqual([], list(self.root.glob('.symphony/task-handoff/*/journal.sqlite3')))

    def test_key_scope_and_payload_validation(self):
        with self.open() as journal:
            with self.assertRaises(handoff.InvalidState):
                journal.create_record(handoff.HandoffKey('fixture/other', '17', 'a'*64, 'attempt'))
            journal.create_record(self.key)
            for value in ({'nan': float('nan')}, {'huge': 'x' * 1048576}):
                with self.assertRaises(handoff.InvalidState):
                    journal.append(self.key, 'observed', value)
            self.assertEqual([], journal.events(self.key))


if __name__ == '__main__':
    unittest.main()
