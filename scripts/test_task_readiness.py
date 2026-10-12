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


if __name__ == '__main__':
    unittest.main()
