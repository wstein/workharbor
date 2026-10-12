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

    def adopt_fixture_policy(self, test_content=None):
        # Explicit local synthetic trust input, never authenticated adoption evidence.
        checker = self.root/'scripts/check_ci.py'
        checker.write_bytes(Path(ci.__file__).read_bytes())
        test = self.root/'scripts/test_check_ci.py'
        test.write_text(test_content or (
            'import unittest\nclass Probe(unittest.TestCase):\n'
            '    def test_probe(self): self.assertEqual(2, 1 + 1)\n'))
        self.git('add', '--', 'scripts/check_ci.py', 'scripts/test_check_ci.py')
        self.git('commit', '--quiet', '-m', 'test: synthetic reviewed operator policy')
        return self.git('rev-parse', 'HEAD')

    def test_operator_policy_requires_exact_trusted_source_and_frozen_baseline(self):
        policy = self.adopt_fixture_policy()
        self.assertTrue(ci.approved_operator_tooling(self.root, None, policy))
        self.assertFalse(ci.approved_documentation(self.root))
        for missing in (None, SOURCE, '0'*40):
            with self.subTest(policy=missing):
                self.assertFalse(ci.approved_operator_tooling(self.root, None, missing))
        self.commit('scripts/unknown_tool.py', 'pass\n')
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        self.git('checkout', '--quiet', '--detach', policy)
        self.commit('scripts/single_task.py', 'pass\n')
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        self.git('checkout', '--quiet', '--detach', policy)
        self.commit('scripts/test_check_ci.py', 'pass\n')
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))

    def test_operator_suite_requires_nonempty_strict_green_for_every_tool(self):
        for body, expected in ((
                'import unittest\nclass Probe(unittest.TestCase):\n'
                '    def test_probe(self): self.assertTrue(True)\n', True),
                ('import unittest\nclass Probe(unittest.TestCase):\n'
                 '    @unittest.skip("synthetic")\n    def test_probe(self): pass\n', False),
                ('import unittest\nclass Probe(unittest.TestCase):\n'
                 '    @unittest.expectedFailure\n    def test_probe(self): self.fail("synthetic")\n', False),
                ('# empty discovery\n', False),
                ('raise RuntimeError("synthetic import error")\n', False)):
            with self.subTest(expected=expected, body=body):
                self.git('checkout', '--quiet', '--detach', SOURCE)
                policy = self.adopt_fixture_policy(body)
                self.assertTrue(ci.approved_operator_tooling(self.root, None, policy))
                self.assertEqual(expected, ci.operator_suite(self.root))

    def test_checks_cannot_mutate_operator_content_or_change_pinned_head(self):
        policy = self.adopt_fixture_policy()
        def mutate(root):
            (root/'scripts/test_check_ci.py').write_text('pass\n')
            return True
        with mock.patch.object(ci, 'operator_suite', side_effect=mutate), \
                mock.patch.object(ci, 'run', side_effect=lambda argv, root: 2 if argv[0] == 'make' else 0), \
                mock.patch.object(ci, 'expected_t004_rejection', return_value=True):
            original_run = subprocess.run
            def commands(argv, *args, **kwargs):
                if argv[0] == 'git':
                    return original_run(argv, *args, **kwargs)
                return subprocess.CompletedProcess(argv, 0, '{}', '')
            with mock.patch.object(ci.subprocess, 'run', side_effect=commands):
                self.assertEqual(1, ci.check(self.root, policy_source=policy))

    def test_forged_merge_tree_and_wrong_parent_order_block(self):
        context = self.proposal()
        for parents, tree in (((context.head, context.base), None),
                              ((context.base, context.head, SOURCE), None),
                              (None, self.git('rev-parse', context.head + '^{tree}'))):
            with self.subTest(parents=parents, tree=tree):
                wrong = self.merge(context.base, context.head, parents, tree)
                with self.assertRaises(ValueError):
                    ci.source_context(self.root, *wrong)

    def test_invalid_implicit_event_cannot_fall_through_other_green_modes(self):
        context = self.proposal()
        path = Path(self.temporary.name)/'event.json'
        path.write_text('{}')
        for event in ('pull_request', 'workflow_dispatch', ''):
            with self.subTest(event=event), mock.patch.dict(os.environ, {
                    'GITHUB_EVENT_NAME': event, 'GITHUB_SHA': context.head,
                    'GITHUB_EVENT_PATH': str(path)}), \
                    mock.patch.object(ci, 'run', return_value=0) as run, \
                    mock.patch.object(ci, 'strict_suite', return_value=True) as strict, \
                    mock.patch.object(ci, 'approved_t003', return_value=True) as older:
                self.assertEqual(1, ci.check(self.root))
                run.assert_not_called()
                strict.assert_not_called()
                older.assert_not_called()

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

    def test_git_executable_config_and_environment_do_not_run_sentinels(self):
        context = self.proposal()
        sentinel = Path(self.temporary.name)/'executed'
        command = 'touch ' + str(sentinel)
        for key in ('filter.probe.clean', 'filter.probe.process', 'merge.probe.driver',
                    'diff.probe.textconv', 'diff.external', 'include.path'):
            with self.subTest(key=key):
                self.git('config', key, command)
                with self.assertRaises(ValueError):
                    ci.source_context(self.root, *context)
                self.assertFalse(sentinel.exists())
                self.git('config', '--unset', key)
        # fsmonitor is forcibly disabled before any config/index command; global
        # and environment-injected commands are ignored rather than inherited.
        self.git('config', 'core.fsmonitor', command)
        with mock.patch.dict(os.environ, {'GIT_CONFIG_COUNT': '1',
                'GIT_CONFIG_KEY_0': 'core.fsmonitor', 'GIT_CONFIG_VALUE_0': command,
                'GIT_DIR': str(Path(self.temporary.name)/'foreign-git')}):
            self.assertEqual(context, ci.source_context(self.root, *context))
        self.assertFalse(sentinel.exists())

    def test_workflow_requires_external_available_regular_policy_source(self):
        workflow = (ci.ROOT/'.github/workflows/check.yml').read_text()
        block = workflow.split("python3 - <<'PY'\n", 1)[1].split('          PY\n', 1)[0]
        import textwrap
        block = textwrap.dedent(block)
        self.root.rename(self.root.parent/'integration')
        self.root = self.root.parent/'integration'
        environment = dict(os.environ, GITHUB_WORKSPACE=str(self.root.parent))
        for pin in ('', 'HEAD', '0'*40):
            with self.subTest(pin=pin):
                result = subprocess.run([sys.executable, '-c', block], cwd=self.root.parent,
                                        env=dict(environment, WORKHARBOR_CI_POLICY_SOURCE=pin),
                                        capture_output=True)
                self.assertNotEqual(0, result.returncode)
                self.assertFalse((self.root.parent/'trusted-policy').exists())
        checker = self.root/'scripts/check_ci.py'
        checker.unlink()
        checker.symlink_to('../README.md')
        self.git('add', '--', 'scripts/check_ci.py')
        self.git('commit', '--quiet', '-m', 'test: synthetic unsafe policy source')
        unsafe = self.git('rev-parse', 'HEAD')
        result = subprocess.run([sys.executable, '-c', block], cwd=self.root.parent,
                                env=dict(environment, WORKHARBOR_CI_POLICY_SOURCE=unsafe),
                                capture_output=True)
        self.assertNotEqual(0, result.returncode)
        self.assertFalse((self.root.parent/'trusted-policy').exists())
        self.git('checkout', '--quiet', '--detach', SOURCE)
        # A supplied valid commit is distinct from the integration head.
        policy = self.adopt_fixture_policy()
        self.commit('docs/after-policy.md')
        result = subprocess.run([sys.executable, '-c', block], cwd=self.root.parent,
                                env=dict(environment, WORKHARBOR_CI_POLICY_SOURCE=policy),
                                capture_output=True)
        self.assertEqual(0, result.returncode)
        self.assertEqual(policy, subprocess.check_output(['git', 'rev-parse', 'HEAD'],
                         cwd=self.root.parent/'trusted-policy', text=True).strip())
        self.assertNotEqual(policy, self.git('rev-parse', 'HEAD'))

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
