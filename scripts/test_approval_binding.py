"""Offline binding adapter fixtures; no real Feature 003 or owner authentication."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tests'))
from approval_binding import (
    ApprovalVerification, Artifact, ArtifactComparison, BindingBlocked, ValidatedContract,
    assess_retention,
)
from test_single_task import SingleTaskFixture


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True,
                                    separators=(',', ':')).encode()).hexdigest()


class SyntheticGuard:
    """Finite test port, deliberately not the Feature 003 TaskContext schema.

    Context/evidence assertions and a single explicitly enumerated progress pair
    are the oracle. No general Markdown or approval normalizer is implemented.
    """
    revision = 'a' * 40

    def __init__(self, paths):
        self.paths = paths
        self.progress_pair = None

    def validate_context(self, context, evidence):
        expected = {'fixture_schema', 'scope', 'requirements', 'checks',
                    'dependencies', 'mode'}
        if (not isinstance(context, dict) or set(context) != expected
                or context['fixture_schema'] != 1 or evidence != 'fresh-fixture'):
            raise BindingBlocked('synthetic-context-or-evidence-rejected')
        return ValidatedContract(digest(context), self.paths)

    def compare_artifacts(self, approved, current, evidence):
        if (approved.artifacts, current.artifacts) == self.progress_pair:
            return ArtifactComparison('progress-only', 'fixture:exact-progress-pair')
        return ArtifactComparison('renewal-required', 'fixture:unproved-change')


class SyntheticAuthority:
    """Supplied-local only; never claims a real owner or provider identity."""
    def __init__(self):
        self.accepted_target = None
        self.before_return = None
        self.valid = True

    def verify(self, approval_ref, target):
        if self.before_return:
            self.before_return()
        if self.accepted_target is None:
            self.accepted_target = target
        return ApprovalVerification(
            self.valid and approval_ref == 'fixture:approval', 'supplied-local',
            self.accepted_target, 'fixture:verified-offline')


class ApprovalBindingTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.paths = tuple(sorted(f'specs/synthetic/{name}'
                                  for name in ('spec.md', 'plan.md', 'tasks.md')))
        self.guard = SyntheticGuard(self.paths)
        self.authority = SyntheticAuthority()
        context = dict(fixture_schema=1, scope=['guide.md'], requirements='one edit',
                       checks=[['fixture-check']], dependencies=[], mode='implement')
        self.request = dict(schema_version=1,
                            approved_revision=self.fixture.baseline,
                            current_revision=self.fixture.baseline,
                            approved_context=context,
                            current_context=copy.deepcopy(context),
                            evidence='fresh-fixture', approval_ref='fixture:approval',
                            approval_action='execution', approved_package_digest='c' * 64)

    def assess(self, **kwargs):
        return assess_retention(self.root, self.request,
                                guard=kwargs.get('guard', self.guard),
                                authority=kwargs.get('authority', self.authority))

    def commit(self):
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: fixture update')
        self.request['current_revision'] = self.fixture.git('rev-parse', 'HEAD').stdout.strip()

    def rejected(self, result, blocker=None):
        self.assertFalse(result['retained'], result)
        self.assertIsNotNone(result['blocker'])
        if blocker:
            self.assertEqual(blocker, result['blocker'])

    def test_unrelated_new_base_preserves_binding_and_records_distinct_tree(self):
        first = self.assess()
        self.assertTrue(first['retained'])
        (self.root / 'guide.md').write_text('Unrelated accepted update\n')
        self.commit()
        second = self.assess()
        self.assertTrue(second['retained'], second)
        self.assertEqual('supplied-local', second['approval_assurance'])
        self.assertEqual('identical', second['result'])
        self.assertEqual(first['binding_digest'], second['binding_digest'])
        self.assertNotEqual(second['approved_snapshot']['tree'], second['current_snapshot']['tree'])

    def test_changed_scope_requirements_checks_dependencies_or_mode_requires_renewal(self):
        for key, value in [('scope', ['other.md']), ('requirements', 'two edits'),
                           ('checks', [['other-check']]), ('dependencies', ['T001']),
                           ('mode', 'owner-authorized rework')]:
            with self.subTest(field=key):
                old = self.request['current_context'][key]
                self.request['current_context'][key] = value
                result = self.assess()
                self.rejected(result, 'task-contract-changed')
                self.assertEqual('renewal-required', result['result'])
                self.request['current_context'][key] = old

    def test_schema_extra_fields_and_stale_evidence_rejected_by_port(self):
        for key, value in [('fixture_schema', 2), ('extraneous', True)]:
            with self.subTest(field=key):
                context = copy.deepcopy(self.request['current_context'])
                self.request['current_context'][key] = value
                self.rejected(self.assess(), 'adapter-rejected')
                self.request['current_context'] = context
        self.request['evidence'] = 'stale-fixture'
        self.rejected(self.assess(), 'adapter-rejected')

    def test_request_schema_extra_missing_fields_and_short_revision_fail(self):
        original = copy.deepcopy(self.request)
        for mutation in ('schema', 'extra', 'missing', 'revision'):
            with self.subTest(mutation=mutation):
                self.request = copy.deepcopy(original)
                if mutation == 'schema':
                    self.request['schema_version'] = True
                elif mutation == 'extra':
                    self.request['owner'] = 'invented'
                elif mutation == 'missing':
                    del self.request['approval_ref']
                else:
                    self.request['approved_revision'] = 'HEAD'
                self.rejected(self.assess())

    def test_semantic_artifact_bytes_require_renewal(self):
        (self.fixture.feature / 'spec.md').write_bytes(b'Changed requirement\n')
        self.commit()
        result = self.assess()
        self.rejected(result, 'artifact-approval-renewal-required')
        self.assertEqual('renewal-required', result['result'])

    def test_progress_retained_only_for_exact_guard_proved_pair(self):
        before = tuple(Artifact(path, '100644', (self.root / path).read_bytes())
                       for path in self.paths)
        tasks = self.fixture.feature / 'tasks.md'
        original = tasks.read_bytes()
        changed = original.replace(b'- [ ] T001 ', b'- [x] T001 ', 1)
        self.assertNotEqual(original, changed)
        tasks.write_bytes(changed)
        after = tuple(Artifact(path, '100644', (self.root / path).read_bytes())
                      for path in self.paths)
        self.guard.progress_pair = (before, after)
        self.commit()
        result = self.assess()
        self.assertTrue(result['retained'], result)
        self.assertEqual('progress-only', result['result'])
        self.assertNotEqual(result['approved_snapshot']['artifacts'],
                            result['current_snapshot']['artifacts'])
        tasks.write_bytes(changed + b'\nChanged task semantics\n')
        self.commit()
        self.rejected(self.assess(), 'artifact-approval-renewal-required')

    def test_invalid_comparator_cannot_claim_identical_for_changed_bytes(self):
        (self.fixture.feature / 'plan.md').write_bytes(b'drift')
        self.commit()
        self.guard.compare_artifacts = lambda *args: 'identical'
        self.rejected(self.assess(), 'invalid-artifact-comparison')

    def test_modes_and_symlinks_require_renewal(self):
        path = self.fixture.feature / 'spec.md'
        path.chmod(0o755)
        self.commit()
        self.rejected(self.assess(), 'protected-artifact-mode-changed')
        path.unlink()
        path.symlink_to('plan.md')
        self.commit()
        self.rejected(self.assess(), 'unsafe-protected-artifact')

    def test_hidden_physical_mode_change_rejected(self):
        self.fixture.git('config', 'core.filemode', 'false')
        (self.fixture.feature / 'spec.md').chmod(0o755)
        self.rejected(self.assess(), 'worktree-artifact-drift')

    def test_staged_hidden_change_dirty_worktree_and_untracked_block(self):
        path = self.root / 'guide.md'
        original = path.read_bytes()
        path.write_bytes(b'staged')
        self.fixture.git('add', 'guide.md')
        path.write_bytes(original)
        self.rejected(self.assess(), 'dirty-current-source')

    def test_dirty_untracked_source_and_wrong_current_head_block(self):
        (self.root / 'unexpected').write_bytes(b'untracked')
        self.rejected(self.assess(), 'dirty-current-source')
        (self.root / 'unexpected').unlink()
        (self.root / 'guide.md').write_bytes(b'base update')
        self.commit()
        self.request['current_revision'] = self.fixture.baseline
        self.rejected(self.assess(), 'current-head-mismatch')

    def test_default_ports_fail_closed(self):
        self.rejected(assess_retention(self.root, self.request),
                      'feature003-context-comparator-unavailable')
        self.rejected(assess_retention(self.root, self.request, guard=self.guard),
                      'approval-authority-unavailable')

    def test_stale_binding_and_unverified_authority_block(self):
        self.authority.accepted_target = 'stale-target'
        self.rejected(self.assess(), 'approval-not-verified')
        self.authority.accepted_target = None
        self.authority.valid = False
        self.rejected(self.assess(), 'approval-not-verified')

    def test_original_action_package_and_reviewed_revision_cannot_be_relabelled(self):
        self.assertTrue(self.assess()['retained'])
        target = self.authority.accepted_target
        for field, value in [('approval_action', 'publication'),
                             ('approved_package_digest', 'd' * 64)]:
            with self.subTest(field=field):
                previous = self.request[field]
                self.request[field] = value
                self.rejected(self.assess(), 'approval-not-verified')
                self.request[field] = previous
        (self.root / 'guide.md').write_bytes(b'unrelated base advance')
        self.commit()
        self.assertTrue(self.assess()['retained'])
        self.assertEqual(target.reviewed_revision, self.fixture.baseline)
        self.request['approved_revision'] = self.request['current_revision']
        self.rejected(self.assess(), 'approval-not-verified')

    def test_unproved_progress_semantic_footer_and_whitespace_edits_require_renewal(self):
        tasks = self.fixture.feature / 'tasks.md'
        original = tasks.read_bytes()
        for suffix in (b'\n## Execution evidence\n- Evidence: T001 invalid stale\n',
                       b'\nChanged dependency/order\n', b' '):
            with self.subTest(suffix=suffix):
                tasks.write_bytes(original + suffix)
                self.commit()
                self.rejected(self.assess(), 'artifact-approval-renewal-required')

    def test_progress_without_durable_proof_is_rejected(self):
        (self.fixture.feature / 'plan.md').write_bytes(b'unproved progress')
        self.commit()
        self.guard.compare_artifacts = lambda *args: ArtifactComparison('progress-only', '')
        self.rejected(self.assess(), 'invalid-artifact-comparison')

    def test_external_verification_source_race_blocks(self):
        self.authority.before_return = lambda: (self.root / 'guide.md').write_bytes(b'race')
        self.rejected(self.assess(), 'dirty-current-source')

    def test_unsafe_guard_path_or_changed_path_set_blocks(self):
        self.guard.paths = ('../outside',)
        self.rejected(self.assess(), 'invalid-protected-path')
        self.guard.paths = self.paths
        validate = self.guard.validate_context
        calls = []
        def differing(context, evidence):
            calls.append(1)
            value = validate(context, evidence)
            return value if len(calls) == 1 else ValidatedContract(value.contract_digest, ('guide.md',))
        self.guard.validate_context = differing
        self.rejected(self.assess(), 'task-contract-changed')

    def test_sensitive_adapter_errors_are_sanitized(self):
        def broken(*args):
            raise RuntimeError('sensitive-provider-payload')
        self.authority.verify = broken
        result = self.assess()
        self.rejected(result, 'validation-unavailable')
        self.assertNotIn('sensitive-provider-payload', json.dumps(result))


if __name__ == '__main__':
    unittest.main()
