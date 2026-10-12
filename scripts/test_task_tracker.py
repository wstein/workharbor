"""Credential-free injected provider boundary tests; no tracker/network access."""
from dataclasses import asdict, replace
import hashlib
import json
import unittest
from unittest.mock import patch

import approval_binding as binding
import task_tracker as tracker


NOW = 1000
REVISION = 'a' * 40
DIGEST = 'b' * 64


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


class Transport:
    def __init__(self, payload):
        self.payload = payload
        self.calls = []
        self.failures = []

    def read(self, kind, object_id):
        self.calls.append((kind, object_id))
        if self.failures:
            raise self.failures.pop(0)
        return tracker.AuthenticatedRead('github', 'repo-1', 'host-verifier', 'e' * 40, NOW,
                                         encoded(self.payload))


class PublicationTrackerTests(unittest.TestCase):
    def setUp(self):
        self.config = tracker.TrackerConfig('github', 'fixture/repo', 'repo-1',
                                           'owner-1', 'host-verifier', 'e' * 40)
        self.identity = dict(repository='fixture/repo', issue='17', feature='004-fixture',
            task_id='T015', package_digest='a' * 64, guard_attempt_id='attempt',
            implementation_commit='b' * 40, content_digest='c' * 64)
        self.request = dict(identity=self.identity, base='d' * 40, head='e' * 40,
                            checker='f' * 40, base_ref='refs/heads/main', head_ref='refs/heads/task',
                            head_repository='fixture/repo', content={'title': 'fixture', 'body': 'safe'})
        self.object = dict(object_id='pr-1', url='https://github.com/fixture/repo/pull/19',
            identity=self.identity, base=self.request['base'], head=self.request['head'],
            base_ref=self.request['base_ref'], head_ref=self.request['head_ref'],
            head_repository=self.request['head_repository'],
            checker=self.request['checker'], content_digest=hashlib.sha256(
                encoded(self.request['content'])).hexdigest())
        self.transport = Transport(dict(matches=[self.object]))
        self.reader = tracker.AuthenticatedTracker(self.config, self.transport, clock=lambda: NOW)

    def test_exact_authenticated_remote_lookup_and_ambiguous_conflicts(self):
        publisher = tracker.PublicationTracker(self.reader)
        found = publisher.find_pr(self.request)
        self.assertEqual('pr-1', found.object_id)
        self.transport.payload = {'matches': []}
        self.assertIsNone(publisher.find_pr(self.request))
        for matches in ([self.object, self.object], [{**self.object, 'head': 'f' * 40}],
                        [{**self.object, 'base_ref': 'refs/heads/other'}],
                        [{**self.object, 'head_ref': 'refs/heads/other'}],
                        [{**self.object, 'head_repository': 'other/repository'}],
                        [{**self.object, 'identity': {**self.identity, 'guard_attempt_id': 'other'}}]):
            self.transport.payload = {'matches': matches}
            with self.assertRaises(tracker.TrackerBlocked):
                publisher.find_pr(self.request)

    def test_writes_require_explicit_host_transport_and_sanitize_unknown_results(self):
        publisher = tracker.PublicationTracker(self.reader)
        with self.assertRaises(tracker.TrackerBlocked):
            publisher.publish_pr(self.request, operation_key='publish-pr:fixture')
        class Writer:
            def write(self, *args, **kwargs):
                raise RuntimeError('sensitive-provider-output')
        publisher = tracker.PublicationTracker(self.reader, Writer())
        with self.assertRaises(tracker.WriteUnknown) as failure:
            publisher.publish_pr(self.request, operation_key='publish-pr:fixture')
        self.assertNotIn('sensitive-provider-output', str(failure.exception))

    def test_evidence_lookup_requires_exact_content_and_provider_envelope(self):
        self.object['url'] = 'https://github.com/fixture/repo/issues/17#issuecomment-1'
        publisher = tracker.PublicationTracker(self.reader)
        self.assertEqual('pr-1', publisher.find_evidence(self.request).object_id)
        self.object['content_digest'] = 'f' * 64
        with self.assertRaises(tracker.TrackerBlocked):
            publisher.find_evidence(self.request)
        good = self.transport.read('evidence', 'fixture')
        with patch.object(self.transport, 'read', return_value=replace(good, repository_id='other')):
            with self.assertRaises(tracker.TrackerBlocked):
                publisher.find_evidence(self.request)


class ConditionalLabelTests(unittest.TestCase):
    def setUp(self):
        self.config = tracker.TrackerConfig('github', 'fixture/repo', 'repo-1',
                                           'owner-1', 'host-verifier', 'e' * 40)
        self.identity = tracker.IssueIdentity('fixture/repo', 17)
        self.payload = dict(object_id='issue-17', repository='fixture/repo', number=17,
                            state='open', labels=['human-review', 'unrelated'], updated_at=900)
        self.transport = Transport(self.payload)
        self.reader = tracker.AuthenticatedTracker(self.config, self.transport, clock=lambda: NOW)
        self.expected = dict(object_digest='a' * 64, labels=('unrelated', 'workharbor-ready'))
        self.desired = ('human-review', 'unrelated')
        self.key = 'final-labels:' + 'b' * 64

    def test_missing_conditional_writer_performs_no_provider_call(self):
        port = tracker.ConditionalLabels(self.reader)
        with self.assertRaises(tracker.TrackerBlocked):
            port.replace_labels(self.identity, self.expected, self.desired, operation_key=self.key)
        self.assertEqual([], self.transport.calls)

    def test_authenticated_returned_receipt_needs_no_postwrite_read(self):
        test = self
        class Writer:
            def replace_labels(self, identity, expected, desired, *, operation_key):
                test.assertEqual(test.identity, identity)
                test.assertEqual(test.expected, expected)
                test.assertEqual(test.desired, desired)
                test.assertEqual(test.key, operation_key)
                return test.transport.read('fixture-receipt', 'fixture')
        port = tracker.ConditionalLabels(self.reader, Writer())
        receipt = port.replace_labels(self.identity, self.expected, self.desired, operation_key=self.key)
        self.assertEqual(self.desired, receipt.labels)
        self.assertEqual('open', receipt.state)
        self.assertEqual([('fixture-receipt', 'fixture')], self.transport.calls)

    def test_closed_wrong_labels_or_unauthenticated_receipt_remain_unknown(self):
        test = self
        class Writer:
            def replace_labels(self, *args, **kwargs):
                return test.transport.read('fixture-receipt', 'fixture')
        port = tracker.ConditionalLabels(self.reader, Writer())
        for changes in ({'state': 'closed'}, {'labels': ['human-review']},
                        {'repository': 'other/repository'}):
            self.transport.payload = {**self.payload, **changes}
            with self.subTest(changes=changes), self.assertRaises(tracker.WriteUnknown):
                port.replace_labels(self.identity, self.expected, self.desired, operation_key=self.key)
        self.transport.payload = self.payload
        response = self.transport.read('fixture-receipt', 'fixture')
        with patch.object(self.transport, 'read', return_value=replace(response, repository_id='other')):
            with self.assertRaises(tracker.WriteUnknown):
                port.replace_labels(self.identity, self.expected, self.desired, operation_key=self.key)


class TrackerTests(unittest.TestCase):
    def setUp(self):
        self.config = tracker.TrackerConfig('github', 'fixture/repo', 'repo-1',
                                             'owner-1', 'host-verifier', 'e' * 40)
        self.target = tracker.DecisionTarget('fixture/repo', '004-fixture', 'T011',
                                            'execution', REVISION, DIGEST, 'c' * 64)
        self.payload = dict(object_id='comment-1', repository='fixture/repo',
                            actor_id='owner-1', state='approved', created_at=900,
                            decided_at=900, updated_at=900, expires_at=2000,
                            target=asdict(self.target))
        self.url = 'https://github.com/fixture/repo/issues/17#issuecomment-1'
        self.reference = tracker.ApprovalReference('comment-1', self.url,
                                                  hashlib.sha256(encoded(self.payload)).hexdigest(),
                                                  self.target)
        self.transport = Transport(self.payload)
        self.port = tracker.AuthenticatedTracker(self.config, self.transport, clock=lambda: NOW)

    def reject(self, code=None):
        with self.assertRaises(tracker.TrackerBlocked) as caught:
            self.port.read_approval(self.reference)
        if code:
            self.assertEqual(code, str(caught.exception))

    def test_authenticated_decision_preserves_original_target_and_safe_evidence(self):
        decision = self.port.read_approval(self.reference)
        self.assertEqual(self.target, decision.target)
        self.assertEqual('authenticated-provider', decision.assurance)
        self.assertEqual(self.reference.content_digest, decision.object_digest)
        self.assertEqual(('owner-1', 'owner-1', 'host-verifier'),
                         (decision.actor_id, decision.owner_id, decision.verifier_id))
        self.assertEqual('e' * 40, decision.verifier_revision)
        self.assertEqual((900, 900, 900, 2000, NOW),
                         (decision.created_at, decision.decided_at, decision.updated_at,
                          decision.expires_at, decision.observed_at))

    def test_every_call_refetches_and_rejects_changed_or_deleted_objects(self):
        self.port.read_approval(self.reference)
        self.payload['state'] = 'dismissed'
        self.reject()
        self.transport.payload = None
        self.reject()
        self.assertEqual([('approval', 'comment-1')] * 3, self.transport.calls)

    def test_all_original_target_fields_and_owner_must_match(self):
        for field, other in [('repository', 'other/repo'), ('feature', '004-other'),
                             ('task_id', 'T012'), ('action', 'plan'),
                             ('reviewed_revision', 'd' * 40),
                             ('reviewed_package_digest', 'd' * 64),
                             ('binding_digest', 'd' * 64)]:
            with self.subTest(field=field):
                self.transport.payload = {**self.payload, 'target': {**asdict(self.target), field: other}}
                self.reject()
        self.transport.payload = {**self.payload, 'actor_id': 'other-owner'}
        self.reject()

    def test_provider_repository_and_verifier_envelope_are_host_pinned(self):
        good = self.transport.read('approval', 'comment-1')
        for field in ('provider_id', 'repository_id', 'verifier_id', 'verifier_revision'):
            with self.subTest(field=field), patch.object(self.transport, 'read', return_value=replace(good, **{field: 'wrong'})):
                self.reject()

    def test_edited_dismissed_pending_deleted_stale_future_and_expired_reject(self):
        for changes in ({'updated_at': 901}, {'state': 'dismissed'}, {'state': 'pending'},
                        {'state': 'deleted'}, {'expires_at': NOW}, {'created_at': NOW + 1},
                        {'created_at': 1, 'decided_at': 1, 'updated_at': 1}):
            with self.subTest(changes=changes):
                self.transport.payload = {**self.payload, **changes}
                reference = replace(self.reference, content_digest=hashlib.sha256(encoded(self.transport.payload)).hexdigest())
                port = tracker.AuthenticatedTracker(replace(self.config, max_decision_age=500),
                                                     self.transport, clock=lambda: NOW)
                with self.assertRaises(tracker.TrackerBlocked):
                    port.read_approval(reference)
        self.transport.payload = self.payload
        good = self.transport.read('approval', 'comment-1')
        for observed in (NOW - 301, NOW + 1):
            with patch.object(self.transport, 'read', return_value=replace(good, observed_at=observed)):
                self.reject()

    def test_local_assertion_is_distinct_and_cannot_enter_authenticated_port(self):
        local = tracker.supplied_local(self.reference)
        self.assertEqual('supplied-local', local.assurance)
        self.assertIsNone(local.verifier_id)
        self.assertIsNone(local.verifier_revision)
        with self.assertRaises(tracker.TrackerBlocked):
            self.port.read_approval(local)
        self.assertEqual([], self.transport.calls)

    def test_decision_kinds_cannot_substitute_for_each_other(self):
        for action in ('spec', 'plan', 'execution', 'matrix', 'rework', 'review', 'publication'):
            target = replace(self.target, action=action)
            self.transport.payload = {**self.payload, 'target': asdict(target)}
            reference = replace(self.reference, target=target,
                                content_digest=hashlib.sha256(encoded(self.transport.payload)).hexdigest())
            self.assertEqual(action, self.port.read_approval(reference).target.action)
            with self.assertRaises(tracker.TrackerBlocked):
                self.port.read_approval(self.reference if action != 'execution' else replace(reference, target=replace(target, action='plan')))

    def test_authority_facade_binds_feature_task_and_original_binding(self):
        authority = tracker.ApprovalAuthority(self.port, '004-fixture', 'T011', {self.url: self.reference})
        target = binding.ApprovalTarget('execution', REVISION, DIGEST, 'c' * 64)
        verified = authority.verify(self.url, target)
        self.assertEqual(binding.ApprovalVerification(True, 'authenticated-provider', target,
                                                     self.url), verified)
        for changed in (replace(target, action='plan'), replace(target, binding_digest='d' * 64)):
            with self.assertRaises(tracker.TrackerBlocked):
                authority.verify(self.url, changed)
        with self.assertRaises(tracker.TrackerBlocked):
            tracker.ApprovalAuthority(self.port, '004-other', 'T011', {self.url: self.reference}).verify(self.url, target)

    def test_transport_retries_only_transient_failures_at_most_three_times(self):
        self.transport.failures = [tracker.TransientRead(), tracker.TransientRead(), tracker.TransientRead()]
        self.assertEqual('authenticated-provider', self.port.read_approval(self.reference).assurance)
        self.assertEqual(4, len(self.transport.calls))
        self.transport.calls.clear()
        self.transport.failures = [tracker.TransientRead()] * 5
        self.reject('tracker-read-unavailable')
        self.assertEqual(4, len(self.transport.calls))
        self.transport.calls.clear()
        self.transport.failures = [RuntimeError('synthetic-secret-never-log')]
        self.reject('tracker-read-unavailable')
        self.assertEqual(1, len(self.transport.calls))

    def test_strict_bounded_payloads_and_safe_diagnostics(self):
        good = self.transport.read('approval', 'comment-1')
        for payload in (b'{"object_id":1,"object_id":2}', b'NaN', b'\xff',
                        b'x' * (1024 * 1024 + 1), encoded({**self.payload, 'token': 'synthetic-secret'})):
            with self.subTest(size=len(payload)), patch.object(self.transport, 'read', return_value=replace(good, payload=payload)):
                self.reject()
        with patch.object(self.transport, 'read', side_effect=RuntimeError('synthetic-secret')):
            with self.assertRaises(tracker.TrackerBlocked) as caught:
                self.port.read_approval(self.reference)
            self.assertNotIn('synthetic-secret', repr(caught.exception))
            self.assertIsNone(caught.exception.__context__)

    def test_issue_reads_bind_exact_configured_repository_and_identity(self):
        identity = tracker.IssueIdentity('fixture/repo', 17)
        self.transport.payload = dict(object_id='issue-17', repository='fixture/repo', number=17,
                                      state='open', labels=['unrelated'], updated_at=900)
        issue = self.port.read_issue(identity)
        self.assertEqual((17, ('unrelated',), 'open'), (issue.number, issue.labels, issue.state))
        for changed in ({'number': 18}, {'repository': 'other/repo'}, {'state': 'deleted'},
                        {'labels': ['duplicate', 'duplicate']}):
            original = self.transport.payload
            self.transport.payload = {**original, **changed}
            with self.subTest(changed=changed), self.assertRaises(tracker.TrackerBlocked):
                self.port.read_issue(identity)
            self.transport.payload = original
        before = len(self.transport.calls)
        with self.assertRaises(tracker.TrackerBlocked):
            self.port.read_issue(tracker.IssueIdentity('other/repo', 17))
        self.assertEqual(before, len(self.transport.calls))

    def test_invalid_configuration_and_reference_are_rejected_before_transport(self):
        for field, value in (('provider_id', 'unconfigured'), ('owner_id', 'bad identity'),
                             ('repository_id', ''), ('verifier_revision', 'main'),
                             ('max_observation_age', True), ('max_decision_age', 0)):
            with self.subTest(field=field), self.assertRaises(tracker.TrackerBlocked):
                tracker.AuthenticatedTracker(replace(self.config, **{field: value}), self.transport)
        for reference in (replace(self.reference, url=self.url + '?token=synthetic-secret'),
                          replace(self.reference, content_digest='short'),
                          replace(self.reference, target=replace(self.target, action='unknown'))):
            with self.subTest(reference=reference), self.assertRaises(tracker.TrackerBlocked):
                self.port.read_approval(reference)
        self.assertEqual([], self.transport.calls)

    def test_no_files_logs_children_or_ambient_credentials_are_used(self):
        with patch('builtins.open', side_effect=AssertionError('file access')), \
                patch('subprocess.run', side_effect=AssertionError('child access')), \
                patch('os.getenv', side_effect=AssertionError('ambient credentials')), \
                patch('builtins.print', side_effect=AssertionError('log access')):
            self.port.read_approval(self.reference)
        self.assertEqual(set(), set(self.port.capabilities))
        for method in ('publish_pr', 'write_evidence', 'replace_labels'):
            self.assertFalse(hasattr(self.port, method))


if __name__ == '__main__':
    unittest.main()
