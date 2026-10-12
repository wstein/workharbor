"""Local Git source/equivalence fixtures; never execute candidate source."""
import copy
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import approval_binding as binding
import task_source as source
import check_ci as ci
import task_package as package


class SourceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)/'repo'
        self.root.mkdir()
        self.git('init', '--quiet', '-b', 'main')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'user.email', 'fixture@invalid')
        (self.root/'scripts').mkdir()
        for name in ('check_ci.py', 'task_outcomes.py', 'task_package.py', 'single_task_state.py'):
            origin = Path(__file__).with_name(name)
            target = self.root/'scripts'/name
            target.write_bytes(origin.read_bytes())
            target.chmod(origin.stat().st_mode & 0o777)
        (self.root/'contract.txt').write_text('approved task contract\n')
        (self.root/'policy.txt').write_text('approved policy\n')
        self.accepted = self.commit('baseline')
        self.head = self.change('proposal.txt', 'task change\n')
        self.git('checkout', '--quiet', '--detach', self.accepted)
        self.base = self.change('base.txt', 'accepted update\n')
        self.wrapper = self.merge(self.base, self.head)
        self.context = ci.SourceContext('pull_request', self.base, self.head, self.wrapper)
        self.guard = mock.Mock()
        self.guard.capabilities = {
            name: package.CapabilityEvidence(True, 'a'*40, True, None)
            for name in package.required_capabilities('entry', 'implement')}
        self.guard.revision = 'a'*40
        self.guard.validate_context.return_value = binding.ValidatedContract('b'*64, ('contract.txt',))
        self.guard.validate_scope.return_value = package.GuardValidation(True)
        self.environment = mock.patch.dict(os.environ)
        self.environment.start()
        self.addCleanup(self.environment.stop)
        for name in ('GITHUB_EVENT_NAME', 'GITHUB_SHA', 'GITHUB_EVENT_PATH'):
            os.environ.pop(name, None)

    def git(self, *args, input=None):
        return subprocess.run(['git', '-c', 'commit.gpgsign=false', *args], cwd=self.root,
                              input=input, text=True, capture_output=True, check=True).stdout.strip()

    def commit(self, message):
        self.git('add', '.')
        self.git('commit', '--quiet', '-m', message)
        return self.git('rev-parse', 'HEAD')

    def change(self, path, content):
        (self.root/path).write_text(content)
        return self.commit('fixture change')

    def merge(self, base, head, parents=None, tree=None):
        tree = tree or self.git('merge-tree', '--write-tree', base, head)
        args = ['commit-tree', tree]
        for parent in parents if parents is not None else (base, head):
            args += ['-p', parent]
        revision = self.git(*args, input='fixture integration\n')
        self.git('checkout', '--quiet', '--detach', revision)
        return revision

    def validate(self, **changes):
        args = dict(root=self.root, accepted=self.accepted, checker=self.accepted,
                    source_context=self.context, context={'opaque': 'guard-owned'},
                    evidence={'opaque': 'host-supplied'}, guard=self.guard)
        args.update(changes)
        return source.validate_source(**args)

    def rebase(self):
        self.git('checkout', '--quiet', '--detach', self.base)
        new_head = self.change('proposal.txt', 'task change\n')
        pairs = [dict(old=self.head, old_parent=self.accepted,
                      new=new_head, new_parent=self.base)]
        return dict(root=self.root, old_base=self.accepted, new_base=self.base,
                    old_head=self.head, new_head=new_head, correspondence=pairs,
                    protected_paths=['contract.txt'], guard=self.guard,
                    context={'opaque': 'guard-owned'}, evidence={})

    def test_distinct_accepted_base_head_wrapper_and_trees_are_retained(self):
        result = self.validate()
        self.assertTrue(result['source_valid'])
        self.assertFalse(result['execution_eligible'])
        self.assertIsNone(result['independent_review'])
        self.assertEqual([self.accepted, self.base, self.head, self.wrapper],
                         [result['snapshot'][key] for key in ('accepted', 'base', 'head', 'checkout')])
        self.assertNotEqual(result['snapshot']['head_tree'], result['snapshot']['checkout_tree'])
        self.assertEqual(self.accepted, result['snapshot']['checker'])
        snapshot, context, evidence = self.guard.validate_scope.call_args.args
        self.assertEqual(self.head, snapshot['head'])
        self.assertEqual({'opaque': 'guard-owned'}, context)
        self.assertEqual({'opaque': 'host-supplied'}, evidence)

    def test_unavailable_or_unverified_guard_never_calls_scope(self):
        result = self.validate(guard=None)
        self.assertFalse(result['source_valid'])
        for change in ('missing', 'unverified', 'live'):
            guard = copy.copy(self.guard)
            guard.validate_scope = mock.Mock()
            guard.capabilities = dict(self.guard.capabilities)
            if change == 'missing': guard.capabilities.pop('guard.scope-history')
            if change == 'unverified':
                guard.capabilities['guard.scope-history'] = package.CapabilityEvidence(True, 'a'*40, False, None)
            with self.subTest(change=change):
                self.assertFalse(self.validate(guard=guard, live=change == 'live')['source_valid'])
                guard.validate_scope.assert_not_called()

    def test_passing_merge_cannot_clear_failed_head_history_or_prerequisite_scope(self):
        for blocked in ('head', 'commit-revert', 'protected-prerequisite'):
            with self.subTest(blocked=blocked):
                self.guard.validate_scope.return_value = package.GuardValidation(False)
                result = self.validate(evidence={'failure': blocked})
                self.assertFalse(result['source_valid'])
                self.assertIn('guard-source-scope-blocked', result['blockers'])

    def test_false_malformed_or_exception_guard_context_blocks_before_scope(self):
        for verdict in (False, {'accepted': True}, package.GuardValidation(False), RuntimeError('credential-canary')):
            self.guard.validate_scope.reset_mock()
            self.guard.validate_context.side_effect = verdict if isinstance(verdict, Exception) else None
            self.guard.validate_context.return_value = verdict
            with self.subTest(verdict=type(verdict).__name__):
                result = self.validate()
                self.assertFalse(result['source_valid'])
                self.assertNotIn('credential-canary', json.dumps(result))
                self.guard.validate_scope.assert_not_called()

    def test_wrong_parents_forged_tree_unknown_commit_and_checker_drift_block(self):
        for kind in ('parents', 'tree', 'unknown', 'checker'):
            with self.subTest(kind=kind):
                self.git('checkout', '--quiet', '--detach', self.wrapper)
                context, checker = self.context, self.accepted
                if kind == 'parents':
                    wrapper = self.merge(self.base, self.head, [self.head, self.base])
                    context = self.context._replace(checkout=wrapper)
                elif kind == 'tree':
                    wrapper = self.merge(self.base, self.head, tree=self.git('rev-parse', self.head+'^{tree}'))
                    context = self.context._replace(checkout=wrapper)
                elif kind == 'unknown': context = self.context._replace(head='0'*40)
                else: checker = self.head[:8]
                self.assertFalse(self.validate(source_context=context, checker=checker)['source_valid'])

    def test_dirty_index_modes_assume_unchanged_and_unsafe_files_block(self):
        path = self.root/'proposal.txt'
        for kind in ('dirty', 'index', 'mode', 'assume', 'symlink', 'untracked', 'hardlink'):
            with self.subTest(kind=kind):
                self.git('checkout', '--quiet', '--detach', self.wrapper)
                if kind == 'mode': path.chmod(0o755)
                elif kind == 'symlink': path.unlink(); path.symlink_to('contract.txt')
                elif kind == 'hardlink': path.unlink(); os.link(self.root/'contract.txt', path)
                elif kind == 'untracked': (self.root/'extra').write_text('extra')
                else:
                    path.write_text('tampered\n')
                    if kind == 'index': self.git('add', 'proposal.txt'); path.write_text('task change\n')
                    if kind == 'assume': self.git('update-index', '--assume-unchanged', 'proposal.txt')
                self.assertFalse(self.validate()['source_valid'])
                self.git('update-index', '--no-assume-unchanged', 'proposal.txt')
                path.unlink()
                self.git('restore', '--staged', '--worktree', 'proposal.txt')
                if (self.root/'extra').exists(): (self.root/'extra').unlink()

    def test_post_guard_checkout_change_blocks_without_accepting_new_baseline(self):
        self.guard.validate_scope.side_effect = lambda *args: (
            (self.root/'proposal.txt').write_text('drift\n'), package.GuardValidation(True))[1]
        result = self.validate()
        self.assertFalse(result['source_valid'])
        self.assertEqual(self.wrapper, result['snapshot']['checkout'])

    def test_equivalent_rebase_uses_exact_pairs_patches_protected_content_and_tree(self):
        args = self.rebase()
        before = copy.deepcopy({key: value for key, value in args.items() if key != 'guard'})
        result = source.verify_rebase(**args)
        self.assertEqual('VERIFIED', result['status'])
        self.assertEqual(args['new_head'], result['fresh_review_required'])
        self.assertFalse(result['renewed_approval_required'])
        self.assertEqual(args['correspondence'], result['correspondence'])
        self.assertEqual(self.git('rev-parse', args['new_head']+'^{tree}'), result['result_tree'])
        self.assertEqual(before, {key: value for key, value in args.items() if key != 'guard'})
        self.assertFalse(result['execution_eligible'])

    def test_missing_duplicate_reordered_pairs_or_false_parents_block(self):
        args = self.rebase()
        pair = args['correspondence'][0]
        for pairs in ([], [pair, pair], [dict(pair, old_parent=self.base)],
                      [dict(pair, new_parent=self.accepted)], [dict(pair, old='f'*40)]):
            with self.subTest(pairs=pairs):
                self.assertEqual('BLOCKED', source.verify_rebase(**dict(args, correspondence=pairs))['status'])

    def test_changed_patch_protected_content_and_result_tree_require_renewal(self):
        args = self.rebase()
        for kind in ('patch', 'protected', 'tree'):
            self.git('checkout', '--quiet', '--detach', args['new_head'])
            if kind == 'patch': (self.root/'proposal.txt').write_text('different task\n')
            elif kind == 'protected': (self.root/'contract.txt').write_text('new approval required\n')
            else: (self.root/'extra.txt').write_text('hidden result change\n')
            new = self.commit('changed candidate')
            # A single rewritten commit names the new base as parent explicitly.
            tree = self.git('rev-parse', new+'^{tree}')
            new = self.git('commit-tree', tree, '-p', self.base, input='rewrite\n')
            pairs = [dict(args['correspondence'][0], new=new)]
            with self.subTest(kind=kind):
                result = source.verify_rebase(**dict(args, new_head=new, correspondence=pairs))
                self.assertEqual('BLOCKED', result['status'])
                self.assertTrue(result['renewed_approval_required'])
                self.assertEqual(new, result['fresh_review_required'])

    def test_overlap_even_matching_patch_requires_new_review_and_approval(self):
        args = self.rebase()
        self.git('checkout', '--quiet', '--detach', self.accepted)
        # Matching bytes after a base commit/revert still need the guard's history verdict.
        self.change('proposal.txt', 'temporary base overlap\n')
        self.git('rm', 'proposal.txt')
        overlap_base = self.commit('revert base overlap')
        new = self.change('proposal.txt', 'task change\n')
        pairs = [dict(args['correspondence'][0], new=new, new_parent=overlap_base)]
        self.guard.validate_scope.return_value = package.GuardValidation(False)
        result = source.verify_rebase(**dict(args, new_base=overlap_base, new_head=new, correspondence=pairs))
        # A matching final patch cannot clear the guard's intervening-history refusal.
        self.assertEqual('BLOCKED', result['status'])
        self.assertTrue(result['renewed_approval_required'])
        self.assertTrue(result['guard_history_required'])
        self.assertFalse(result['execution_eligible'])

    def test_partial_promisor_repository_blocks_before_any_source_lookup(self):
        for key, value in (('extensions.partialclone', 'origin'),
                           ('remote.origin.promisor', 'true'),
                           ('remote.origin.partialclonefilter', 'blob:none')):
            self.git('config', key, value)
            with self.subTest(key=key), mock.patch.object(ci, 'source_context') as lookup:
                self.assertFalse(self.validate()['source_valid'])
                lookup.assert_not_called()
            self.git('config', '--unset', key)
        promisor = self.root/'.git/objects/pack/fixture.promisor'
        promisor.write_text('')
        with mock.patch.object(ci, 'source_context') as lookup:
            self.assertFalse(self.validate()['source_valid'])
            lookup.assert_not_called()

    def test_rebase_cannot_select_subset_of_guard_protected_inventory(self):
        args = self.rebase()
        self.git('checkout', '--quiet', '--detach', self.accepted)
        base = self.change('policy.txt', 'changed protected prerequisite\n')
        new = self.change('proposal.txt', 'task change\n')
        pairs = [dict(args['correspondence'][0], new=new, new_parent=base)]
        self.guard.validate_context.return_value = binding.ValidatedContract(
            'b'*64, ('contract.txt', 'policy.txt'))
        result = source.verify_rebase(**dict(args, new_base=base, new_head=new, correspondence=pairs))
        self.assertEqual('BLOCKED', result['status'])
        self.assertTrue(result['renewed_approval_required'])
        self.guard.validate_scope.assert_not_called()

    def test_unavailable_rebase_guard_blocks_even_with_exact_local_equivalence(self):
        result = source.verify_rebase(**dict(self.rebase(), guard=None))
        self.assertEqual('BLOCKED', result['status'])
        self.assertEqual('rebase-guard-or-source-blocked', result['blocker'])
        self.assertFalse(result['execution_eligible'])

    def test_guard_revision_drift_blocks_before_context_or_scope(self):
        self.guard.revision = 'c'*40
        result = self.validate()
        self.assertFalse(result['source_valid'])
        self.assertIn('guard-source-revision-mismatch', result['blockers'])
        self.guard.validate_context.assert_not_called()
        self.guard.validate_scope.assert_not_called()

    def test_conflicting_rebase_needs_renewed_approval_and_exact_review(self):
        args = self.rebase()
        self.git('checkout', '--quiet', '--detach', self.accepted)
        base = self.change('proposal.txt', 'conflicting accepted update\n')
        new = self.change('proposal.txt', 'task change\n')
        pairs = [dict(args['correspondence'][0], new=new, new_parent=base)]
        result = source.verify_rebase(**dict(args, new_base=base, new_head=new, correspondence=pairs))
        self.assertEqual('BLOCKED', result['status'])
        self.assertTrue(result['renewed_approval_required'])
        self.assertEqual(new, result['fresh_review_required'])

    def test_invalid_rebase_identifiers_are_sanitized(self):
        result = source.verify_rebase(**dict(self.rebase(), new_head='credential-canary'))
        self.assertEqual('BLOCKED', result['status'])
        self.assertNotIn('credential-canary', json.dumps(result))
        self.assertIsNone(result['new_head'])

    def test_source_validation_is_inert_for_network_and_prepublication(self):
        with mock.patch('socket.create_connection', side_effect=AssertionError('network')), \
                mock.patch.object(source.publication, 'prepare', side_effect=AssertionError('preparation')):
            self.assertTrue(self.validate()['source_valid'])

    def test_publication_composes_existing_preparer_only_after_source_gate(self):
        request = dict(repository=str(self.root), source=self.accepted,
                       base_ref='refs/heads/main', base=self.base,
                       head_ref='refs/heads/topic', head=self.head)
        prepared = dict(status='PREPARED', source=self.accepted, base=self.base,
                        head=self.head, checkout='b'*40, tree='c'*40, check_exit=0)
        with mock.patch.object(source.publication, 'prepare', return_value=prepared) as prepare:
            result = source.prepare_candidate(self.validate(), request)
            self.assertEqual(prepared, result)
            prepare.assert_called_once_with(**request)
            prepare.reset_mock()
            self.assertEqual('BLOCKED', source.prepare_candidate(self.validate(guard=None), request)['status'])
            prepare.assert_not_called()

    def test_publication_drift_or_request_pin_change_does_not_prepare(self):
        request = dict(repository=str(self.root), source=self.accepted,
                       base_ref='refs/heads/main', base=self.head,
                       head_ref='refs/heads/topic', head=self.head)
        with mock.patch.object(source.publication, 'prepare') as prepare:
            self.assertEqual('BLOCKED', source.prepare_candidate(self.validate(), request)['status'])
            prepare.assert_not_called()


if __name__ == '__main__':
    unittest.main()
