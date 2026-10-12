"""Focused CI source tests, separate from the frozen 41-case bootstrap matrix."""
import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SPEC = importlib.util.spec_from_file_location('ci_source', Path(__file__).with_name('check_ci.py'))
ci = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ci)
SOURCE = '670ef69e733b8d5d34b5660af4999218f613b3db'


class SourceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)/'repo'
        subprocess.run(['git', 'clone', '--quiet', '--shared', str(ci.ROOT), str(self.root)], check=True)
        self.git('checkout', '--quiet', '--detach', SOURCE)
        self.git('config', 'user.name', 'Synthetic')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.github = mock.patch.dict(os.environ)
        self.github.start()
        self.addCleanup(self.github.stop)
        for name in ('GITHUB_EVENT_NAME', 'GITHUB_SHA', 'GITHUB_EVENT_PATH'):
            os.environ.pop(name, None)

    def git(self, *args, text=None):
        return subprocess.run(['git', '-c', 'commit.gpgsign=false', *args], cwd=self.root,
                              input=text, text=True, capture_output=True, check=True).stdout.strip()

    def commit(self, name, content='Proposal\n', mode=None):
        path = self.root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        if mode is not None:
            path.chmod(mode)
        self.git('add', '--', name)
        self.git('commit', '--quiet', '-m', 'test: synthetic revision')
        return self.git('rev-parse', 'HEAD')

    def merge(self, base, head, parents=None, tree=None):
        if tree is None:
            tree = self.git('merge-tree', '--write-tree', base, head)
        args = ['commit-tree', tree]
        for parent in parents if parents is not None else (base, head):
            args += ['-p', parent]
        revision = self.git(*args, text='Synthetic integration\n')
        self.git('checkout', '--quiet', '--detach', revision)
        return ci.SourceContext('pull_request', base, head, revision)

    def proposal(self):
        head = self.commit('docs/proposal.md')
        self.git('checkout', '--quiet', '--detach', SOURCE)
        base = self.commit('docs/unrelated-base.md')
        return self.merge(base, head)

    def test_actual_merge_preserves_unrelated_base_advance(self):
        context = self.proposal()
        self.assertNotEqual(self.git('rev-parse', context.head + '^{tree}'),
                            self.git('rev-parse', context.checkout + '^{tree}'))
        self.assertEqual(context, ci.source_context(self.root, *context))
        self.assertTrue(ci.approved_documentation(self.root, context))

    def test_protected_base_advance_and_head_scope_changes_block(self):
        for name in ('scripts/single_task.py', 'specs/003-single-task-entry/tasks.md',
                     'docs/development/bootstrap-t003.json', 'tests/test_single_task_state.py'):
            with self.subTest(path=name):
                self.git('checkout', '--quiet', '--detach', SOURCE)
                head = self.commit('docs/proposal.md')
                self.git('checkout', '--quiet', '--detach', SOURCE)
                base = self.commit(name, (self.root/name).read_text() + '\n')
                context = self.merge(base, head)
                self.assertEqual(context, ci.source_context(self.root, *context))
                self.assertFalse(ci.approved_documentation(self.root, context))
        self.git('checkout', '--quiet', '--detach', SOURCE)
        self.commit('scripts/check_ci.py', 'pass\n')
        self.assertFalse(ci.approved_documentation(self.root))

    def test_out_of_scope_and_unsafe_mode_history_cannot_hide_in_revert(self):
        for name, mode in (('scripts/single_task.py', None), ('docs/proposal.md', 0o755)):
            with self.subTest(path=name):
                self.git('checkout', '--quiet', '--detach', SOURCE)
                if name == 'docs/proposal.md':
                    before = self.commit(name)
                    bad = self.commit(name, mode=mode)
                else:
                    before = SOURCE
                    bad = self.commit(name, 'pass\n')
                self.git('revert', '--no-edit', bad)
                self.assertEqual('', self.git('diff', '--name-only', before, 'HEAD'))
                self.assertFalse(ci.approved_documentation(self.root))

    def test_unknown_accepted_identity_and_missing_objects_block(self):
        context = self.proposal()
        with mock.patch.object(ci, 'DOCUMENTATION_POLICY_SOURCE', '0'*40):
            self.assertFalse(ci.approved_documentation(self.root, context))
        with self.assertRaises(ValueError):
            ci.source_context(self.root, *context._replace(head='0'*40))
        with self.assertRaises(ValueError):
            ci.source_context(self.root, *context._replace(head='HEAD'))

    def test_forged_merge_tree_and_wrong_parent_order_block(self):
        context = self.proposal()
        for parents, tree in (((context.head, context.base), None),
                              ((context.base, context.head, SOURCE), None),
                              (None, self.git('rev-parse', context.head + '^{tree}'))):
            with self.subTest(parents=parents, tree=tree):
                wrong = self.merge(context.base, context.head, parents, tree)
                with self.assertRaises(ValueError):
                    ci.source_context(self.root, *wrong)

    def test_stale_and_foreign_event_metadata_block(self):
        context = self.proposal()
        event = {'pull_request': {
            'base': {'sha': context.base, 'repo': {'full_name': 'wstein/workharbor-nxt'}},
            'head': {'sha': context.head, 'repo': {'full_name': 'wstein/workharbor-nxt'}}}}
        path = Path(self.temporary.name)/'event.json'
        path.write_text(json.dumps(event))
        os.environ.update(GITHUB_EVENT_NAME='pull_request', GITHUB_SHA=context.checkout,
                          GITHUB_EVENT_PATH=str(path))
        self.assertEqual(context, ci.source_context(self.root, *context))
        for key, value in (('GITHUB_SHA', context.head), ('GITHUB_EVENT_NAME', 'workflow_dispatch')):
            with self.subTest(key=key), mock.patch.dict(os.environ, {key: value}):
                with self.assertRaises(ValueError):
                    ci.source_context(self.root, *context)
        event['pull_request']['head']['sha'] = SOURCE
        path.write_text(json.dumps(event))
        with self.assertRaises(ValueError):
            ci.source_context(self.root, *context)
        event['pull_request']['head']['sha'] = context.head
        event['pull_request']['head']['repo']['full_name'] = 'foreign/repo'
        path.write_text(json.dumps(event))
        with self.assertRaises(ValueError):
            ci.source_context(self.root, *context)

    def test_dirty_index_modes_untracked_and_assume_unchanged_block(self):
        context = self.proposal()
        path = self.root/'docs/proposal.md'
        for mutation in ('dirty', 'staged', 'mode', 'symlink', 'untracked', 'assume-unchanged'):
            with self.subTest(mutation=mutation):
                self.git('config', 'core.filemode', 'false')
                if mutation == 'untracked':
                    (self.root/'docs/untracked.md').write_text('untracked\n')
                elif mutation == 'mode':
                    path.chmod(0o755)
                elif mutation == 'symlink':
                    path.unlink()
                    path.symlink_to('unrelated-base.md')
                else:
                    path.write_text('changed\n')
                    if mutation == 'staged':
                        self.git('add', '--', 'docs/proposal.md')
                        path.write_text('Proposal\n')
                    elif mutation == 'assume-unchanged':
                        self.git('update-index', '--assume-unchanged', 'docs/proposal.md')
                with self.assertRaises(ValueError):
                    ci.source_context(self.root, *context)
                self.git('update-index', '--no-assume-unchanged', 'docs/proposal.md')
                self.git('restore', '--staged', '--worktree', '--', 'docs/proposal.md')
                path.chmod(0o644)
                extra = self.root/'docs/untracked.md'
                if extra.exists():
                    extra.unlink()

    def test_integration_checks_execute_in_merge_worktree(self):
        context = self.proposal()
        roots = []
        def run(argv, root):
            roots.append(root)
            self.assertEqual(context.checkout, self.git('rev-parse', 'HEAD'))
            self.assertTrue((root/'docs/unrelated-base.md').is_file())
            return 2 if argv[0] == 'make' else 0
        rejection = subprocess.CompletedProcess([], 1, '{}', '')
        original_run = subprocess.run
        def commands(argv, *args, **kwargs):
            if argv[0] == 'git':
                return original_run(argv, *args, **kwargs)
            return rejection if 'scripts/verify_t004_cli.py' in argv else subprocess.CompletedProcess(argv, 0)
        with mock.patch.object(ci, 'run', side_effect=run), \
                mock.patch.object(ci, 'expected_t004_rejection', return_value=True), \
                mock.patch.object(ci.subprocess, 'run', side_effect=commands):
            self.assertEqual(0, ci.check(self.root, context))
        self.assertTrue(roots)
        self.assertEqual({self.root}, set(roots))


if __name__ == '__main__':
    unittest.main()
