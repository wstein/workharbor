"""Focused CI source tests, separate from the frozen 41-case bootstrap matrix."""
import importlib.util
import json
import os
import py_compile
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

    def test_policy_git_objects_refuse_replacements_and_grafts(self):
        policy = self.adopt_fixture_policy()
        replacement = self.commit('scripts/test_check_ci.py',
                                  (self.root/'scripts/test_check_ci.py').read_text() + '# unapproved marker\n')
        original = self.git('ls-tree', policy, '--', 'scripts/test_check_ci.py')
        self.git('replace', policy, replacement)
        self.assertNotEqual(original, self.git('ls-tree', policy, '--', 'scripts/test_check_ci.py'))
        with self.assertRaises(ValueError):
            ci.git(self.root, 'ls-tree', policy, '--', 'scripts/test_check_ci.py')
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        prepare_spec = importlib.util.spec_from_file_location('raw_preparation', ci.ROOT/'scripts/prepare_publication.py')
        preparation = importlib.util.module_from_spec(prepare_spec)
        prepare_spec.loader.exec_module(preparation)
        commands = preparation.Commands(Path(self.temporary.name), 30)
        with self.assertRaises(preparation.Blocked):
            commands.git(self.root, 'ls-tree', policy)
        binding_spec = importlib.util.spec_from_file_location('raw_binding', ci.ROOT/'scripts/approval_binding.py')
        binding = importlib.util.module_from_spec(binding_spec)
        sys.modules[binding_spec.name] = binding
        binding_spec.loader.exec_module(binding)
        with self.assertRaises(binding.BindingBlocked):
            binding._git(self.root, 'ls-tree', policy)
        self.git('replace', '-d', policy)
        graft = self.root/'.git/info/grafts'
        graft.write_text(policy + ' ' + SOURCE + '\n')
        with self.assertRaises(ValueError):
            ci.git(self.root, 'rev-list', policy)
        with self.assertRaises(binding.BindingBlocked):
            binding._git(self.root, 'rev-list', policy)
        with self.assertRaises(preparation.Blocked):
            commands.git(self.root, 'rev-list', policy)
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        graft.unlink()
        self.git('update-ref', 'refs/alternate-replacements/' + policy, replacement)
        with mock.patch.dict(os.environ, GIT_REPLACE_REF_BASE='refs/alternate-replacements',
                             GIT_NO_REPLACE_OBJECTS='0'):
            self.assertEqual(original, ci.git(self.root, 'ls-tree', policy, '--',
                                             'scripts/test_check_ci.py').decode().strip())
            with ci.isolated_python_cache():
                self.assertEqual('1', os.environ['GIT_NO_REPLACE_OBJECTS'])

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

    def test_post_adoption_base_allows_docs_but_revalidates_operator_and_protected_content(self):
        policy = self.adopt_fixture_policy()
        base = self.commit('docs/adopted-base-advance.md')
        self.git('checkout', '--quiet', '--detach', policy)
        head = self.commit('docs/post-adoption-proposal.md')
        context = self.merge(base, head)
        self.assertTrue(ci.approved_operator_tooling(self.root, context, policy))
        for name, mode in (('scripts/single_task.py', None),
                           ('scripts/test_check_ci.py', None),
                           ('scripts/check_ci.py', 0o755)):
            with self.subTest(path=name, mode=mode):
                self.git('checkout', '--quiet', '--detach', base)
                changed = self.commit(name, (self.root/name).read_text() + ('\n' if mode is None else ''), mode)
                context = self.merge(changed, head)
                self.assertEqual(context, ci.source_context(self.root, *context))
                self.assertFalse(ci.approved_operator_tooling(self.root, context, policy))

    def squash_policy(self, policy, parent=SOURCE):
        self.git('checkout', '--quiet', '--detach', parent)
        self.git('read-tree', '--reset', '-u', policy)
        self.git('commit', '--quiet', '-m', 'test: identical policy with rewritten history')
        return self.git('rev-parse', 'HEAD')

    def test_identical_nonancestor_squash_and_rebase_retain_policy_content(self):
        first = self.adopt_fixture_policy()
        policy = self.commit('scripts/test_check_ci.py', (self.root/'scripts/test_check_ci.py').read_text() + '\n')
        squashed = self.squash_policy(policy)
        self.assertNotEqual(policy, squashed)
        self.assertFalse(subprocess.run(['git', 'merge-base', '--is-ancestor', policy, squashed],
                                       cwd=self.root, capture_output=True).returncode == 0)
        self.assertTrue(ci.approved_operator_tooling(self.root, None, policy))
        self.git('checkout', '--quiet', '--detach', SOURCE)
        base = self.commit('docs/unrelated-before-rebase.md')
        self.git('cherry-pick', first, policy)
        head = self.git('rev-parse', 'HEAD')
        context = self.merge(base, head)
        self.assertTrue(ci.approved_operator_tooling(self.root, context, policy))

    def test_policy_states_cannot_rewind_mix_or_use_ambiguous_merge_history(self):
        first = self.adopt_fixture_policy()
        second = self.commit('scripts/test_check_ci.py', (self.root/'scripts/test_check_ci.py').read_text() + '\n')
        policy = self.commit('docs/development/harness.md', (self.root/'docs/development/harness.md').read_text() + '\n')
        self.git('checkout', '--quiet', '--detach', first)
        base = self.commit('docs/intermediate-policy-base.md')
        context = self.merge(base, policy)
        self.assertTrue(ci.approved_operator_tooling(self.root, context, policy))
        self.squash_policy(policy)
        self.git('revert', '--no-edit', second)
        self.git('cherry-pick', second)
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        self.squash_policy(policy)
        self.git('revert', '--no-edit', policy)
        self.git('cherry-pick', policy)
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        self.git('checkout', '--quiet', '--detach', first)
        side = self.commit('docs/side.md')
        self.git('checkout', '--quiet', '--detach', policy)
        self.git('merge', '--no-ff', '--no-edit', side)
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))

    def test_nonancestor_adopted_base_allows_unrelated_docs_and_blocks_protected_revert(self):
        policy = self.adopt_fixture_policy()
        adopted = self.squash_policy(policy)
        base = self.commit('docs/unrelated-adopted-base.md')
        head = self.commit('docs/nonancestor-proposal.md')
        context = self.merge(base, head)
        self.assertTrue(ci.approved_operator_tooling(self.root, context, policy))
        self.git('checkout', '--quiet', '--detach', adopted)
        bad = self.commit('scripts/single_task.py', 'pass\n')
        self.git('revert', '--no-edit', bad)
        context = self.merge(self.git('rev-parse', 'HEAD'), head)
        self.assertFalse(ci.approved_operator_tooling(self.root, context, policy))

    def test_main_promotion_requires_trusted_prefix_and_complete_approved_snapshot(self):
        policy = self.adopt_fixture_policy()
        main = 'd04f711e7b43f67bd6495cc4fae5ad40e73bab4c'
        context = self.merge(main, policy)
        self.assertTrue(ci.approved_operator_tooling(self.root, context, policy))
        # A trusted prefix is an exact object, not merely an identical final tree.
        self.git('checkout', '--quiet', '--detach', main)
        bad = self.commit('Makefile', 'unapproved\n')
        self.git('revert', '--no-edit', bad)
        context = self.merge(self.git('rev-parse', 'HEAD'), policy)
        self.assertEqual(self.git('rev-parse', policy + '^{tree}'), self.git('rev-parse', 'HEAD^{tree}'))
        self.assertFalse(ci.approved_operator_tooling(self.root, context, policy))
        self.git('checkout', '--quiet', '--detach', policy)
        bad = self.commit('Makefile', 'unapproved\n')
        self.git('revert', '--no-edit', bad)
        context = self.merge(main, self.git('rev-parse', 'HEAD'))
        self.assertFalse(ci.approved_operator_tooling(self.root, context, policy))
        self.git('checkout', '--quiet', '--detach', policy)
        head = self.commit('docs/extra-promotion-content.md')
        self.assertFalse(ci.approved_operator_tooling(self.root, self.merge(main, head), policy))
        forged = self.merge(main, policy, tree=self.git('rev-parse', main + '^{tree}'))
        self.assertFalse(ci.approved_operator_tooling(self.root, forged, policy))

    def test_main_promotion_rejects_nonmain_event_and_ignored_checkout(self):
        policy = self.adopt_fixture_policy()
        context = self.merge('d04f711e7b43f67bd6495cc4fae5ad40e73bab4c', policy)
        event = Path(self.temporary.name)/'promotion-event.json'
        repo = {'full_name': 'wstein/workharbor-nxt'}
        payload = {'pull_request': {'base': {'sha': context.base, 'repo': repo, 'ref': 'main'},
                                    'head': {'sha': context.head, 'repo': repo}}}
        event.write_text(json.dumps(payload))
        with mock.patch.dict(os.environ, {'GITHUB_EVENT_NAME': 'pull_request',
                'GITHUB_SHA': context.checkout, 'GITHUB_EVENT_PATH': str(event)}):
            self.assertTrue(ci.approved_operator_tooling(self.root, context, policy))
            payload['pull_request']['base']['ref'] = 'chore/issue-2-bootstrap-entry'
            event.write_text(json.dumps(payload))
            self.assertFalse(ci.approved_operator_tooling(self.root, context, policy))
        hidden = self.root/'ignored.py'
        hidden.write_text('pass\n')
        exclude = self.root/'.git/info/exclude'
        exclude.write_text(exclude.read_text() + '\n/ignored.py\n')
        self.assertFalse(ci.approved_operator_tooling(self.root, context, policy))

    def test_rewritten_history_cannot_hide_policy_or_contract_tampering(self):
        policy = self.adopt_fixture_policy()
        for name in ('scripts/check_ci.py', '.github/workflows/check.yml',
                     'scripts/test_check_ci.py', 'scripts/prepare_publication.py',
                     'specs/004-speckit-symphony-automation/contracts/cli.md',
                     'specs/004-speckit-symphony-automation/bounded-source-contract.md',
                     'Makefile', 'pyproject.toml', 'scripts/test_unknown.py'):
            with self.subTest(path=name):
                self.squash_policy(policy)
                bad = self.commit(name, 'unapproved scope/check/dependency content\n')
                self.git('revert', '--no-edit', bad)
                self.assertEqual('', self.git('diff', '--name-only', policy, 'HEAD'))
                self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))

    def test_rewritten_policy_mode_symlink_and_untracked_manifest_block(self):
        policy = self.adopt_fixture_policy()
        self.squash_policy(policy)
        self.commit('scripts/test_check_ci.py', (self.root/'scripts/test_check_ci.py').read_text(), 0o755)
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        self.squash_policy(policy)
        path = self.root/'scripts/test_check_ci.py'
        path.unlink()
        path.symlink_to('check_ci.py')
        self.git('add', '--', 'scripts/test_check_ci.py')
        self.git('commit', '--quiet', '-m', 'test: untrusted policy symlink')
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
        self.squash_policy(policy)
        (self.root/'scripts/test_untracked.py').write_text('pass\n')
        self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))

    def test_ignored_bytecode_and_source_cannot_execute_or_pass_the_boundary(self):
        policy = self.adopt_fixture_policy()
        sentinel = Path(self.temporary.name)/'executed'
        payload = Path(self.temporary.name)/'payload.py'
        payload.write_text('from pathlib import Path\nPath(' + repr(str(sentinel)) + ').write_text("executed")\n'
                           'import unittest\nclass Probe(unittest.TestCase):\n'
                           '    def test_probe(self): self.assertTrue(True)\n')
        cache = self.root/f'scripts/__pycache__/test_check_ci.{sys.implementation.cache_tag}.pyc'
        for name in (cache.relative_to(self.root).as_posix(), 'scripts/json.py', 'json.py', '.specify/feature.json'):
            with self.subTest(ignored=name):
                path = self.root/name
                path.parent.mkdir(parents=True, exist_ok=True)
                exclude = self.root/'.git/info/exclude'
                exclude.write_text(exclude.read_text() + '\n/' + name + '\n')
                if name.endswith('.pyc'):
                    py_compile.compile(str(payload), cfile=str(path), doraise=True,
                                       invalidation_mode=py_compile.PycInvalidationMode.UNCHECKED_HASH)
                else:
                    path.write_bytes(payload.read_bytes())
                self.assertEqual('', self.git('ls-files', '--others', '--exclude-standard'))
                self.assertFalse(ci.approved_operator_tooling(self.root, None, policy))
                self.assertFalse(ci.operator_suite(self.root))
                with mock.patch.object(ci, 'run', return_value=0) as run:
                    self.assertEqual(1, ci.check(self.root, policy_source=policy))
                    run.assert_not_called()
                self.assertFalse(sentinel.exists())
                path.unlink()

    def test_python_cache_is_external_for_current_and_child_imports_and_restored(self):
        before = (sys.pycache_prefix, sys.dont_write_bytecode,
                  os.environ.get('PYTHONPYCACHEPREFIX'), os.environ.get('PYTHONDONTWRITEBYTECODE'))
        with ci.isolated_python_cache():
            prefix = Path(sys.pycache_prefix)
            self.assertTrue(prefix.is_dir())
            self.assertFalse(prefix.is_relative_to(self.root))
            child = subprocess.run([sys.executable, '-c',
                                    'import json,sys;print(json.dumps([sys.pycache_prefix,sys.dont_write_bytecode]))'],
                                   check=True, text=True, capture_output=True)
            self.assertEqual([str(prefix), True], json.loads(child.stdout))
            isolated = subprocess.run(ci.python_argv('-c',
                'import json,sys;print(json.dumps([sys.flags.isolated,sys.pycache_prefix,sys.dont_write_bytecode]))'),
                check=True, text=True, capture_output=True)
            self.assertEqual([1, str(prefix), True], json.loads(isolated.stdout))
        self.assertFalse(prefix.exists())
        self.assertEqual(before, (sys.pycache_prefix, sys.dont_write_bytecode,
                                 os.environ.get('PYTHONPYCACHEPREFIX'), os.environ.get('PYTHONDONTWRITEBYTECODE')))

    def test_actual_cli_startup_rejects_hidden_imports_before_execution(self):
        policy = self.adopt_fixture_policy()
        sentinel = Path(self.temporary.name)/'startup-executed'
        hidden = self.root/'scripts/json.py'
        hidden.write_text('open(' + repr(str(sentinel)) + ',"w").write("executed")\nraise RuntimeError("injected")\n')
        exclude = self.root/'.git/info/exclude'
        exclude.write_text(exclude.read_text() + '\n/scripts/json.py\n')
        for flags in (('-B',), ('-I', '-B')):
            with self.subTest(flags=flags):
                result = subprocess.run([sys.executable, *flags, str(self.root/'scripts/check_ci.py'),
                                         '--root', str(self.root), '--policy-source', policy],
                                        capture_output=True, text=True)
                self.assertEqual(1, result.returncode)
                self.assertFalse(sentinel.exists())
        tool = Path(self.temporary.name)/'trusted-tool'
        tool.mkdir()
        (tool/'prepare_publication.py').write_bytes((ci.ROOT/'scripts/prepare_publication.py').read_bytes())
        (tool/'json.py').write_bytes(hidden.read_bytes())
        for flags in (('-B',), ('-I', '-B')):
            result = subprocess.run([sys.executable, *flags, str(tool/'prepare_publication.py')],
                                    capture_output=True, text=True)
            self.assertEqual(3, result.returncode)
            self.assertFalse(sentinel.exists())
        # Host Python injection must not propagate into checked subprocesses.
        poison = Path(self.temporary.name)/'poison'
        poison.mkdir()
        (poison/'sitecustomize.py').write_text(hidden.read_text())
        with mock.patch.dict(os.environ, {'PYTHONPATH': str(poison), 'PYTHONHOME': str(poison)}):
            with ci.isolated_python_cache():
                result = subprocess.run([sys.executable, '-c', 'print("safe")'], capture_output=True, text=True)
                self.assertEqual((0, 'safe\n'), (result.returncode, result.stdout))
        self.assertFalse(sentinel.exists())

    def test_frozen_evidence_is_generated_only_in_same_tree_disposable_checkout(self):
        context = self.proposal()
        original_tree = self.git('rev-parse', 'HEAD^{tree}')
        with ci.isolated_python_cache():
            with ci.baseline_execution(self.root) as execution:
                self.assertNotEqual(self.root, execution)
                self.assertEqual(context.checkout, subprocess.check_output(
                    ['git', 'rev-parse', 'HEAD'], cwd=execution, text=True).strip())
                code = ("import sys,unittest;sys.path.insert(0,'tests');import test_single_task_state as t;"
                        "r=unittest.TextTestRunner().run(unittest.TestSuite([t.SingleTaskStateTests('test_invalid_entry_preserves_tasks')]));"
                        "raise SystemExit(0 if r.wasSuccessful() else 1)")
                result = subprocess.run(ci.python_argv('-c', code), cwd=execution,
                                        capture_output=True, text=True)
                self.assertEqual(1, result.returncode)
                self.assertIn('structured JSON response absent', result.stderr)
                self.assertTrue((execution/'.symphony/t002-cli-evidence.jsonl').is_file())
        self.assertFalse(execution.exists())
        self.assertFalse((self.root/'.symphony').exists())
        self.assertEqual(original_tree, self.git('rev-parse', 'HEAD^{tree}'))
        self.assertEqual(context, ci.source_context(self.root, *context))

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
        block = workflow.split("python3 -I -B - <<'PY'\n", 1)[1].split('          PY\n', 1)[0]
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

    def test_workflow_fetches_the_approved_policy_outside_integration_history(self):
        policy = self.adopt_fixture_policy()
        self.git('branch', 'fixture/base', SOURCE)
        self.git('branch', 'fixture/policy', policy)
        remote = Path(self.temporary.name)/'remote.git'
        subprocess.run(['git', 'clone', '--quiet', '--bare', str(self.root), str(remote)], check=True)
        workspace = Path(self.temporary.name)/'fetched'
        workspace.mkdir()
        integration = workspace/'integration'
        subprocess.run(['git', 'clone', '--quiet', '--depth=1', '--single-branch', '--branch',
                        'fixture/base', remote.as_uri(), str(integration)], check=True)
        self.assertNotEqual(0, subprocess.run(['git', 'cat-file', '-e', policy], cwd=integration,
                                             capture_output=True).returncode)
        import textwrap
        block = textwrap.dedent((ci.ROOT/'.github/workflows/check.yml').read_text().split(
            "python3 -I -B - <<'PY'\n", 1)[1].split('          PY\n', 1)[0])
        result = subprocess.run([sys.executable, '-I', '-B', '-c', block], cwd=workspace,
                                env=dict(os.environ, GITHUB_WORKSPACE=str(workspace),
                                         WORKHARBOR_CI_POLICY_SOURCE=policy), capture_output=True)
        self.assertEqual(0, result.returncode)
        self.assertEqual(policy, subprocess.check_output(['git', 'rev-parse', 'HEAD'],
                         cwd=workspace/'trusted-policy', text=True).strip())
        self.assertEqual(SOURCE, subprocess.check_output(['git', 'rev-parse', 'HEAD'],
                         cwd=integration, text=True).strip())

    def test_integration_checks_execute_in_merge_worktree(self):
        context = self.proposal()
        roots = []
        def run(argv, root):
            roots.append(root)
            self.assertEqual(context.checkout, subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip())
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
        self.assertEqual(1, len(set(roots)))
        self.assertNotIn(self.root, roots)


if __name__ == '__main__':
    unittest.main()
