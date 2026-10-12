"""Local T010 port contracts; no real Feature 003, owner or live execution proof."""
import copy
import hashlib
import os
import unittest
from unittest.mock import patch

import approval_binding as binding
import task_package as package
import test_task_package as fixtures


REFERENCE = 'sha256:' + 'e' * 64


class ApprovalBridgeTests(unittest.TestCase):
    git = fixtures.RepositoryPrimitiveTests.git

    def setUp(self):
        fixtures.PackageContractTests.setUp(self)
        self.input['approval_refs']['execution'] = REFERENCE
        self.approved = package.build_package(self.input)
        self.current = copy.deepcopy(self.approved)
        self.calls = []
        test = self

        class Guard:
            revision = test.commit
            capabilities = {name: package.CapabilityEvidence(True, test.commit, True, None)
                            for name in package.required_capabilities('entry', 'implement')}
            comparison = 'progress-only'
            scope = package.GuardValidation(True)

            def validate_context(self, context, evidence):
                test.calls.append(('context', copy.deepcopy(context), evidence))
                if evidence != {'fixture': 'fresh'}:
                    raise ValueError('secret-canary')
                digest = hashlib.sha256(package.canonical_json(context)).hexdigest()
                return binding.ValidatedContract(digest, tuple(sorted(
                    item['path'] for item in test.approved['source']['artifact_revisions'])))

            def compare_artifacts(self, approved, current, evidence):
                test.calls.append(('comparison', approved, current))
                return binding.ArtifactComparison(self.comparison, REFERENCE)

            def validate_scope(self, snapshot, context, evidence):
                test.calls.append(('scope', snapshot, context, evidence))
                return self.scope

        class Authority:
            assurance = 'supplied-local'

            def verify(self, reference, target):
                test.calls.append(('approval', reference, target))
                return binding.ApprovalVerification(True, self.assurance, target, REFERENCE)

        self.guard, self.authority = Guard(), Authority()

    def assess(self, **kwargs):
        return package.assess_content_approval(self.approved, self.current, self.snapshot,
            kwargs.pop('evidence', {'fixture': 'fresh'}),
            guard=kwargs.pop('guard', self.guard), authority=kwargs.pop('authority', self.authority),
            **kwargs)

    def advance(self, path, data):
        (self.root / path).write_bytes(data)
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture update')
        revision = self.git('rev-parse', 'HEAD').decode().strip()
        source = self.current['source']
        source['proposal_commit'] = revision
        source['proposal_tree'] = self.git('rev-parse', 'HEAD^{tree}').decode().strip()
        source['proposal_commits'] = [revision]
        for artifact in source['artifact_revisions']:
            artifact['commit'] = revision
            artifact['sha256'] = hashlib.sha256((self.root / artifact['path']).read_bytes()).hexdigest()
        self.current.pop('package_digest', None)
        self.current = package.build_package(self.current)

    def assert_blocked(self, result, code):
        self.assertFalse(result['guard_entry_valid'], result)
        self.assertIn(code, [item['code'] for item in result['blockers']])
        self.assertFalse(result['execution_eligible'])
        self.assertIsNone(result['live_execution'])

    def test_identical_calls_existing_binding_and_guard_scope_only(self):
        with patch.object(binding, 'assess_retention', wraps=binding.assess_retention) as adapter:
            result = self.assess()
        adapter.assert_called_once()
        self.assertTrue(result['content_approval_retained'], result)
        self.assertTrue(result['guard_entry_valid'], result)
        self.assertEqual('identical', result['comparison']['result'])
        self.assertEqual('supplied-local', result['approval_assurance'])
        self.assertFalse(result['authenticated_approval'])
        self.assertFalse(result['execution_eligible'])
        self.assertEqual(['context', 'context', 'approval', 'scope'], [call[0] for call in self.calls])

    def test_unrelated_source_preserves_original_target_and_distinct_raw_package(self):
        self.advance('guide.md', b'unrelated accepted documentation\n')
        self.current['approval_refs']['execution'] = 'sha256:' + 'f' * 64
        result = self.assess()
        self.assertTrue(result['guard_entry_valid'], result)
        original, current = result['original_approval'], result['current_snapshot']
        self.assertEqual(self.commit, original['reviewed_revision'])
        self.assertEqual(self.approved['package_digest'], original['package_digest'])
        self.assertEqual(REFERENCE, original['reference'])
        self.assertNotEqual(original['reviewed_revision'], current['revision'])
        self.assertNotEqual(original['package_digest'], current['package_digest'])
        self.assertNotEqual(original['revision_digest'], current['revision_digest'])
        self.assertEqual(original['binding_digest'], current['binding_digest'])
        target = next(call[2] for call in self.calls if call[0] == 'approval')
        self.assertEqual(binding.ApprovalTarget('execution', self.commit,
                         self.approved['package_digest'], original['binding_digest']), target)

    def test_guard_proven_progress_records_distinct_bindings_without_rewriting_approval(self):
        path = self.approved['source']['artifact_revisions'][0]['path']
        self.advance(path, b'fixture exact approved progress pair\n')
        approved_before, current_before = copy.deepcopy(self.approved), copy.deepcopy(self.current)
        result = self.assess()
        self.assertTrue(result['guard_entry_valid'], result)
        self.assertEqual('progress-only', result['comparison']['result'])
        self.assertEqual(REFERENCE, result['comparison']['comparison_evidence_ref'])
        self.assertNotEqual(result['original_approval']['binding_digest'],
                            result['current_snapshot']['binding_digest'])
        self.assertEqual(approved_before, self.approved)
        self.assertEqual(current_before, self.current)

    def test_renewal_and_ambiguous_never_verify_authority_or_scope(self):
        path = self.approved['source']['artifact_revisions'][0]['path']
        self.advance(path, b'changed protected input\n')
        for verdict in ('renewal-required', 'ambiguous'):
            with self.subTest(verdict=verdict):
                self.calls.clear()
                self.guard.comparison = verdict
                result = self.assess()
                self.assert_blocked(result, 'content-approval-blocked')
                self.assertFalse(result['content_approval_retained'])
                self.assertEqual(verdict, result['comparison']['result'])
                self.assertNotIn('approval', [call[0] for call in self.calls])
                self.assertNotIn('scope', [call[0] for call in self.calls])

    def test_context_dependency_scope_and_stale_evidence_stay_guard_owned(self):
        for change in ({'dependencies': ['fixture:missing']}, {'checks': [['changed']]},
                       {'scope': ['changed']}, {'mode': 'changed'}):
            with self.subTest(change=change):
                self.current = copy.deepcopy(self.approved)
                self.current['context'].update(change)
                self.current.pop('package_digest')
                self.current = package.build_package(self.current)
                result = self.assess()
                self.assert_blocked(result, 'content-approval-blocked')
                self.assertEqual('renewal-required', result['comparison']['result'])
        self.current = copy.deepcopy(self.approved)
        self.assert_blocked(self.assess(evidence={'fixture': 'stale'}), 'content-approval-blocked')

    def test_missing_unverified_or_mismatched_guard_capability_never_invokes_adapter(self):
        for name in package.required_capabilities('entry', 'implement'):
            with self.subTest(capability=name):
                capabilities = self.guard.capabilities.copy()
                capabilities.pop(name)
                with patch.object(self.guard, 'capabilities', capabilities), patch.object(
                        binding, 'assess_retention') as adapter:
                    self.assert_blocked(self.assess(), 'capability-unavailable')
                    adapter.assert_not_called()
        self.assert_blocked(self.assess(guard=None), 'capability-unavailable')
        self.assertEqual([], self.calls)
        for observation, code in (
                (package.CapabilityEvidence(True, self.commit, False, None), 'capability-unverified'),
                (package.CapabilityEvidence(True, None, True, None), 'capability-unverified'),
                (package.CapabilityEvidence(True, 'f' * 40, True, None), 'guard-revision-mismatch')):
            with self.subTest(observation=observation):
                with patch.object(self.guard, 'capabilities', {
                        name: observation for name in self.guard.capabilities}):
                    self.assert_blocked(self.assess(), code)
        with patch.object(self.guard, 'revision', 'f' * 40):
            self.assert_blocked(self.assess(), 'guard-revision-mismatch')

    def test_missing_authority_and_dirty_current_source_fail_closed(self):
        self.assert_blocked(self.assess(authority=None), 'content-approval-blocked')
        (self.root / 'guide.md').write_text('dirty\n')
        self.assert_blocked(self.assess(), 'content-approval-blocked')

    def test_scope_rejection_and_malformed_results_do_not_erase_retention_evidence(self):
        for verdict in (package.GuardValidation(False, ('fixture:missing',)), True,
                        package.GuardValidation(True, ('fixture:missing',))):
            with self.subTest(verdict=verdict):
                self.guard.scope = verdict
                result = self.assess()
                self.assert_blocked(result, 'guard-scope-blocked')
                self.assertTrue(result['content_approval_retained'])

    def test_live_capability_observations_and_authenticated_approval_are_separate(self):
        result = self.assess(live=True)
        self.assert_blocked(result, 'live-capability-unobserved')
        self.assertEqual([], self.calls)
        self.guard.capabilities = {name: package.CapabilityEvidence(True, self.commit, True, REFERENCE)
                                   for name in self.guard.capabilities}
        self.assert_blocked(self.assess(live=True), 'authenticated-approval-required')
        self.authority.assurance = 'authenticated-provider'
        result = self.assess(live=True)
        self.assertTrue(result['guard_entry_valid'], result)
        self.assertTrue(result['authenticated_approval'])
        self.assertFalse(result['execution_eligible'])
        self.assertIsNone(result['independent_review'])
        self.assertIsNone(result['live_execution'])

    def test_invalid_identity_reference_action_and_guard_errors_are_sanitized(self):
        for key, value in (('repository', 'other/repository'), ('feature', '998-other'),
                           ('task_id', 'fixture:other')):
            with self.subTest(key=key):
                self.current = copy.deepcopy(self.approved)
                self.current[key] = value
                if key != 'feature':
                    self.current.pop('package_digest')
                    self.current = package.build_package(self.current)
                self.assert_blocked(self.assess(), 'approval-package-mismatch' if key != 'feature'
                                    else 'invalid-package-artifacts')
        self.current = copy.deepcopy(self.approved)
        self.assert_blocked(self.assess(action='secret-canary'), 'invalid-approval-action')
        with patch.object(self.guard, 'validate_scope', side_effect=ValueError('secret-canary')):
            result = self.assess()
        self.assert_blocked(result, 'guard-scope-blocked')
        self.assertNotIn('secret-canary', package.canonical_json(result).decode())

    def test_approval_actions_remain_distinct_and_missing_original_reference_blocks(self):
        self.assert_blocked(self.assess(action='spec'), 'approval-reference-missing')
        for action in ('spec', 'plan', 'matrix', 'rework'):
            with self.subTest(action=action):
                self.approved['approval_refs'][action] = REFERENCE
                self.calls.clear()
                result = self.assess(action=action)
                self.assertTrue(result['guard_entry_valid'], result)
                target = next(call[2] for call in self.calls if call[0] == 'approval')
                self.assertEqual(action, target.action)

    def test_unsafe_adapter_evidence_is_not_exposed(self):
        def verify(reference, target):
            return binding.ApprovalVerification(True, 'supplied-local', target,
                'https://secret-canary@github.com/fixture/offline/pull/1?token=secret-canary')
        with patch.object(self.authority, 'verify', side_effect=verify):
            result = self.assess()
        self.assert_blocked(result, 'content-approval-blocked')
        self.assertIsNone(result['comparison'])
        self.assertNotIn('secret-canary', package.canonical_json(result).decode())

    def test_unsealed_and_tampered_packages_are_not_adopted_as_approval_baselines(self):
        for target in (self.approved, self.current):
            original = copy.deepcopy(target)
            target.pop('package_digest')
            self.assert_blocked(self.assess(), 'package-digest-missing')
            target.update(copy.deepcopy(original))
            target['context']['changed'] = True
            self.assert_blocked(self.assess(), 'package-digest-mismatch')
            target.clear()
            target.update(copy.deepcopy(original))
        self.assertEqual([], self.calls)

    def test_bridge_git_children_exclude_credentials_and_disable_lazy_fetch(self):
        run = binding.subprocess.run
        children = []

        def observe(argv, **kwargs):
            children.append((argv, kwargs['env']))
            return run(argv, **kwargs)

        canary = 'credential-boundary-canary'
        with patch.dict(os.environ, GITHUB_TOKEN=canary, AWS_SESSION_TOKEN=canary,
                        UNRELATED_HOST_VARIABLE=canary), patch.object(
                            binding.subprocess, 'run', side_effect=observe):
            result = self.assess()
        self.assertTrue(result['guard_entry_valid'], result)
        self.assertTrue(children)
        for argv, environment in children:
            self.assertFalse(canary in environment.values(), 'host credential inherited')
            self.assertIn('--no-lazy-fetch', argv)
            self.assertEqual('1', environment['GIT_NO_LAZY_FETCH'])

    def test_root_replacement_during_adapter_or_scope_invalidates_entry(self):
        for owner, method in ((binding, 'assess_retention'), (self.guard, 'validate_scope')):
            with self.subTest(boundary=method):
                operation = getattr(owner, method)
                moved = self.root.with_name('relocated-repository')

                def replace_root(*args, **kwargs):
                    self.root.rename(moved)
                    self.root.symlink_to(moved, target_is_directory=True)
                    return operation(*args, **kwargs)

                try:
                    with patch.object(owner, method, side_effect=replace_root):
                        result = self.assess()
                    self.assert_blocked(result, 'repository-input-raced')
                finally:
                    if self.root.is_symlink():
                        self.root.unlink()
                        moved.rename(self.root)


class ReadinessTests(unittest.TestCase):
    """Full readiness uses disposable ports, never actual dispatch authority."""
    git = fixtures.RepositoryPrimitiveTests.git

    def setUp(self):
        # This disposable repository has local source pins, independent of the
        # enclosing Actions checkout. Restore host event evidence on cleanup.
        events = patch.dict(os.environ)
        events.start()
        self.addCleanup(events.stop)
        for name in ('GITHUB_EVENT_NAME', 'GITHUB_SHA', 'GITHUB_EVENT_PATH'):
            os.environ.pop(name, None)
        ApprovalBridgeTests.setUp(self)
        import task_handoff as handoff
        import task_tracker as tracker
        from pathlib import Path
        import shutil
        # Exercise the real source helper with its exact checker pin, rather
        # than replacing source/history validation with a permissive fake.
        (self.root / 'scripts').mkdir()
        for name in ('check_ci.py', 'task_outcomes.py', 'task_package.py', 'single_task_state.py'):
            shutil.copy2(Path(package.__file__).with_name(name), self.root / 'scripts' / name)
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture checker')
        self.commit = self.git('rev-parse', 'HEAD').decode().strip()
        tree = self.git('rev-parse', 'HEAD^{tree}').decode().strip()
        self.input['source'].update(accepted_commit=self.commit, accepted_tree=tree,
                                   proposal_commit=self.commit, proposal_tree=tree)
        for artifact in self.input['source']['artifact_revisions']:
            artifact['commit'] = self.commit
        self.guard.revision = self.commit
        self.input['execution']['guard_revision'] = self.commit
        self.input.update(issue=dict(repository='fixture/offline', number=7), task_id='T013')
        self.input['execution'].update(build_provenance='d' * 64,
            external_actions=['publish-pr', 'write-evidence', 'replace-labels'],
            publication_scope=dict(repository='fixture/offline', base_ref='refs/heads/main',
                                   head_ref='refs/heads/task'))
        self.input['approval_refs'].update(spec=REFERENCE, plan=REFERENCE, execution=REFERENCE)
        self.approved = package.build_package(self.input)
        self.current = copy.deepcopy(self.approved)
        self.snapshot.update(accepted=self.commit, checker=self.commit,
                             source_context=('local', self.commit, self.commit, self.commit))
        self.authority.assurance = 'authenticated-provider'
        self.owner = handoff.Owner(str(self.root), 'fixture-host', 'fixture-process')
        self.issue = tracker.IssueObservation('fixture', 'repo-id', 'issue-id', 'fixture/offline',
            7, 'open', ('unrelated',), 1, 'f' * 64, 1, 'verifier', self.commit)
        test = self
        capabilities = {name: package.CapabilityEvidence(True, self.commit, True, REFERENCE)
                        for name in package.required_capabilities('ready', 'implement')}
        self.guard.capabilities = capabilities

        class Tracker:
            def read_issue(self, identity):
                test.calls.append(('issue', identity))
                return test.issue

            def replace_labels(self, identity, expected, desired):
                test.calls.append(('write', identity, expected, desired))
                return True

        class Runtime:
            def observe_effective(self, value, evidence, *, live=False):
                test.calls.append(('runtime', value, live))
                execution = dict(value['execution'],
                    argv_digest=hashlib.sha256(package.canonical_json(
                        [check['argv'] for check in value['outcomes']])).hexdigest(),
                    actions_digest=hashlib.sha256(package.canonical_json(
                        value['execution']['external_actions'])).hexdigest())
                return dict(local_verified=True, live_verified=live, execution=execution,
                            binding_digest='c' * 64, live_execution=REFERENCE, blockers=[])

        class Ownership:
            def observe(self, repository, issue, owner):
                test.calls.append(('ownership',))
                return handoff.OwnershipEvidence(repository, issue, owner, 'exclusive',
                                                'fixture-verifier', REFERENCE)

        self.tracker, self.runtime, self.ownership = Tracker(), Runtime(), Ownership()
        self.tracker.capabilities = dict(capabilities)
        self.runtime.capabilities = dict(capabilities)
        self.ports = dict(guard=self.guard, authority=self.authority, tracker=self.tracker,
            runtime=self.runtime, ownership=self.ownership, owner=self.owner,
            authorized_actions=tuple(self.input['execution']['external_actions']),
            action_refs={'publication': REFERENCE}, live=True)
        self.evidence = {'fixture': 'fresh'}

    def assess(self, **ports):
        return package.assess_readiness(self.approved, self.current, self.snapshot,
                                       self.evidence, **dict(self.ports, **ports))

    def ready(self, **ports):
        import task_automation as automation
        return automation.ready(self.approved, self.current, self.snapshot,
                                self.evidence, **dict(self.ports, **ports))

    def assert_blocked(self, result):
        self.assertFalse(result['readiness_valid'], result)
        self.assertFalse(result['execution_eligible'], result)
        self.assertFalse(any(call[0] == 'write' for call in self.calls))

    def test_one_selected_issue_eligible_only_after_full_revalidation(self):
        assessed = self.assess()
        self.assertTrue(assessed['readiness_valid'], assessed)
        self.assertFalse(assessed['execution_eligible'])
        result = self.ready()
        self.assertTrue(result['execution_eligible'], result)
        writes = [call for call in self.calls if call[0] == 'write']
        self.assertEqual(1, len(writes))
        self.assertEqual(7, writes[0][1].number)
        self.assertEqual(('unrelated', 'workharbor-ready'), writes[0][3])
        self.assertEqual('write', self.calls[-1][0])

    def test_missing_entire_execution_capabilities_write_nothing(self):
        for name in package.required_capabilities('ready', 'implement'):
            category = name.split('.')[0]
            port = self.ports[category]
            original = port.capabilities
            port.capabilities = dict(original, **{name: package.CapabilityEvidence(False, None, None, None)})
            with self.subTest(capability=name):
                self.assert_blocked(self.ready())
            port.capabilities = original

    def test_github_safe_labels_false_blocks_without_write(self):
        import task_tracker as tracker
        configured = tracker.AuthenticatedTracker(tracker.TrackerConfig('github', 'fixture/offline',
            'repo-id', 'owner-id', 'verifier', self.commit), type('Transport', (), {'read': lambda *a: None})())
        self.assert_blocked(self.ready(tracker=configured))

    def test_separate_actions_and_authenticated_owner_approvals_required(self):
        for action in ('spec', 'plan', 'execution', 'publication'):
            original = self.authority.verify
            def reject(reference, target):
                if target.action == action:
                    return binding.ApprovalVerification(False, 'authenticated-provider', target, REFERENCE)
                return original(reference, target)
            with self.subTest(action=action), patch.object(self.authority, 'verify', side_effect=reject):
                self.assert_blocked(self.ready())
        self.authority.assurance = 'supplied-local'
        self.assert_blocked(self.ready())
        self.authority.assurance = 'authenticated-provider'
        self.assert_blocked(self.ready(authorized_actions=('replace-labels',)))
        self.assert_blocked(self.ready(action_refs={}))

    def test_owner_source_dirty_retry_and_semantic_execution_drift_preserve_inputs(self):
        with patch.object(self.ownership, 'observe', return_value=None):
            self.assert_blocked(self.ready())
        path = self.root / 'data/input.json'
        original = path.read_bytes()
        path.write_bytes(b'dirty retry')
        self.assert_blocked(self.ready())
        self.assertEqual(b'dirty retry', path.read_bytes())
        path.write_bytes(original)
        for key in ('workflow_digest', 'symphony_revision', 'configuration_digest'):
            self.current = copy.deepcopy(self.approved)
            self.current.pop('package_digest')
            self.current['execution'][key] = 'f' * len(self.current['execution'][key])
            self.current = package.build_package(self.current)
            with self.subTest(key=key):
                result = self.ready()
                self.assert_blocked(result)
                self.assertIn('execution-approval-renewal-required', [b['code'] for b in result['blockers']])

    def test_check_argv_change_requires_renewal(self):
        self.current.pop('package_digest')
        self.current['outcomes'][0]['argv'][-2] = 'other_*.py'
        self.current = package.build_package(self.current)
        result = self.ready()
        self.assert_blocked(result)
        self.assertIn('execution-approval-renewal-required', [b['code'] for b in result['blockers']])

    def test_drift_during_runtime_observation_blocks_before_write(self):
        original = self.runtime.observe_effective
        def drift(*args, **kwargs):
            result = original(*args, **kwargs)
            (self.root / 'data/input.json').write_bytes(b'changed during observation')
            return result
        with patch.object(self.runtime, 'observe_effective', side_effect=drift):
            self.assert_blocked(self.ready())

    def test_entry_and_post_check_revalidate_without_reset_or_baseline_adoption(self):
        prior = self.assess()
        self.assertTrue(prior['readiness_valid'], prior)
        for boundary in ('entry', 'post-check'):
            result = package.revalidate_readiness(prior, self.approved, self.current,
                self.snapshot, self.evidence, boundary=boundary, **self.ports)
            self.assertTrue(result['readiness_valid'], result)
        self.tracker.capabilities.pop('guard.recovery', None)
        self.guard.capabilities.pop('guard.recovery')
        self.assert_blocked(package.revalidate_readiness(prior, self.approved, self.current,
            self.snapshot, self.evidence, boundary='entry', **self.ports))

    def test_runtime_and_provider_errors_are_sanitized(self):
        for port, method in ((self.runtime, 'observe_effective'), (self.tracker, 'read_issue'),
                             (self.ownership, 'observe')):
            with self.subTest(method=method), patch.object(port, method, side_effect=RuntimeError('secret-canary')):
                result = self.ready()
                self.assert_blocked(result)
                self.assertNotIn('secret-canary', package.canonical_json(result).decode())

    def test_unknown_write_is_incomplete_and_never_retried(self):
        for effect in (False, RuntimeError('secret-canary')):
            self.calls.clear()
            with self.subTest(effect=type(effect).__name__):
                if isinstance(effect, Exception):
                    replacement = patch.object(self.tracker, 'replace_labels', side_effect=effect)
                else:
                    replacement = patch.object(self.tracker, 'replace_labels', return_value=effect)
                with replacement as write:
                    result = self.ready()
                self.assertFalse(result['execution_eligible'], result)
                self.assertEqual(1, write.call_count)
                self.assertIn(result['blockers'][-1]['code'],
                              ('eligibility-write-incomplete', 'eligibility-write-uncertain'))
                self.assertNotIn('secret-canary', package.canonical_json(result).decode())

    def test_final_issue_read_drift_blocks_before_conditional_write(self):
        for changed in ('source', 'safe-labels', 'runtime', 'ownership'):
            with self.subTest(changed=changed):
                self.calls.clear()
                original = self.tracker.read_issue
                reads = 0
                runtime = self.runtime.observe_effective
                path = self.root / 'data/input.json'
                before = path.read_bytes()
                caps = dict(self.tracker.capabilities)
                def late_read(identity):
                    nonlocal reads
                    reads += 1
                    if reads == 4:
                        if changed == 'source':
                            path.write_bytes(b'late dirty retry')
                        elif changed == 'safe-labels':
                            self.tracker.capabilities.pop('tracker.safe-labels')
                        elif changed == 'runtime':
                            def drift(*args, **kwargs):
                                observed = runtime(*args, **kwargs)
                                observed['binding_digest'] = 'f' * 64
                                return observed
                            self.runtime.observe_effective = drift
                        else:
                            self.ownership.observe = lambda *args: None
                    return original(identity)
                observe = self.ownership.observe
                try:
                    with patch.object(self.tracker, 'read_issue', side_effect=late_read):
                        self.assert_blocked(self.ready())
                    self.assertEqual(4, reads)
                    if changed == 'source':
                        self.assertEqual(b'late dirty retry', path.read_bytes())
                finally:
                    path.write_bytes(before)
                    self.tracker.capabilities = caps
                    self.runtime.observe_effective = runtime
                    self.ownership.observe = observe

    def test_missing_or_tampered_package_seals_never_adopted(self):
        for which in ('approved', 'current'):
            value = getattr(self, which)
            for seal in (None, 'f' * 64):
                changed = copy.deepcopy(value)
                if seal is None:
                    changed.pop('package_digest')
                else:
                    changed['package_digest'] = seal
                with self.subTest(which=which, seal=seal), patch.object(self, which, changed):
                    result = self.ready()
                    self.assert_blocked(result)
                    self.assertIn(result['blockers'][-1]['code'],
                                  ('package-digest-missing', 'package-digest-mismatch'))

    def test_extra_approved_capability_drop_or_change_blocks_at_final_proof(self):
        name = 'runtime.additional-observation'
        self.approved.pop('package_digest')
        self.approved['execution']['capabilities']['ready'] = [name]
        self.approved = package.build_package(self.approved)
        self.current = copy.deepcopy(self.approved)
        extra = package.CapabilityEvidence(True, self.commit, True, REFERENCE)
        original = self.runtime.observe_effective
        for change in ('drop', 'revision'):
            self.calls.clear()
            self.runtime.capabilities[name] = extra
            observations = 0
            def changed(*args, **kwargs):
                nonlocal observations
                observations += 1
                observed = original(*args, **kwargs)
                if observations == 3:
                    if change == 'drop':
                        self.runtime.capabilities.pop(name)
                    else:
                        self.runtime.capabilities[name] = package.CapabilityEvidence(
                            True, 'f' * 40, True, REFERENCE)
                return observed
            with self.subTest(change=change), patch.object(self.runtime, 'observe_effective', side_effect=changed):
                self.assert_blocked(self.ready())

    def test_entry_runtime_binding_drift_blocks_without_adoption(self):
        prior = self.assess()
        original = self.runtime.observe_effective
        def changed(*args, **kwargs):
            result = original(*args, **kwargs)
            result['binding_digest'] = 'f' * 64
            return result
        with patch.object(self.runtime, 'observe_effective', side_effect=changed):
            result = package.revalidate_readiness(prior, self.approved, self.current,
                self.snapshot, self.evidence, boundary='post-check', **self.ports)
        self.assert_blocked(result)
        self.assertEqual('c' * 64, original(self.current, self.evidence)['binding_digest'])
        self.assertIn('readiness-binding-drift', [b['code'] for b in result['blockers']])

    def test_ready_cli_defaults_unavailable_without_host_ports(self):
        import contextlib
        import io
        import task_automation as automation
        (self.root / 'package.json').write_bytes(package.package_bytes(self.current))
        (self.root / 'live.json').write_bytes(b'{"provider":"github"}')
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            code = automation.main(['ready', '--package', 'package.json', '--live-config', 'live.json'],
                                   root=self.root, repository='fixture/offline')
        response = package.parse_json(output.getvalue().encode())
        self.assertEqual(3, code)
        self.assertFalse(response['execution_eligible'])
        self.assertEqual([{'code': 'readiness-host-unavailable'}], response['blockers'])
        self.assertFalse(any(call[0] == 'write' for call in self.calls))

    def test_readiness_git_children_never_inherit_host_credentials(self):
        import subprocess
        original = subprocess.run
        children = []
        def observe(argv, **kwargs):
            if argv[0] == 'git':
                children.append((argv, kwargs.get('env', {})))
            return original(argv, **kwargs)
        with patch.dict(os.environ, {'GITHUB_TOKEN': 'secret-canary',
                                     'UNRELATED_HOST_SECRET': 'secret-canary',
                                     'PYTHONPATH': '/secret-canary'}), \
                patch.object(subprocess, 'run', side_effect=observe):
            result = self.assess()
        self.assertTrue(result['readiness_valid'], result)
        self.assertTrue(children)
        for argv, environment in children:
            self.assertFalse('secret-canary' in environment.values(), 'host credential inherited')
            self.assertNotIn('PYTHONPATH', environment)
            self.assertIn('--no-lazy-fetch', argv)
            self.assertEqual('1', environment.get('GIT_NO_LAZY_FETCH'))
            self.assertEqual('', environment.get('GIT_ALLOW_PROTOCOL'))


class ReadinessFixtureEnvironmentTests(unittest.TestCase):
    def test_local_fixture_isolates_and_restores_host_event_without_weakening_gate(self):
        import check_ci
        for event in ('push', 'pull_request'):
            host = dict(GITHUB_EVENT_NAME=event, GITHUB_SHA='f' * 40,
                        GITHUB_EVENT_PATH='/unread-host-event.json')
            with self.subTest(event=event), patch.dict(os.environ, host):
                fixture = ReadinessTests('test_one_selected_issue_eligible_only_after_full_revalidation')
                try:
                    fixture.setUp()
                    for name in host:
                        self.assertNotIn(name, os.environ)
                    self.assertTrue(fixture.ready()['execution_eligible'])
                    # Production must still reject mismatched host-event pins.
                    with patch.dict(os.environ, host), self.assertRaisesRegex(
                            ValueError, 'Stale or unsupported event'):
                        check_ci.source_context(fixture.root, *fixture.snapshot['source_context'])
                finally:
                    fixture.doCleanups()
                self.assertEqual(host, {name: os.environ.get(name) for name in host})


if __name__ == '__main__':
    unittest.main()
