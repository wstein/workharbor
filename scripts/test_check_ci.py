"""Acceptance outcomes and source identity; historical policy is superseded by ADR 0007."""
import importlib.util
import copy
import io
import json
import os
import py_compile
import signal
import shutil
import subprocess
import sys
import tempfile
import types
import unittest
from pathlib import Path
from unittest import mock

import task_package as package
import task_outcomes as outcomes
import test_task_package as package_tests

SPEC = importlib.util.spec_from_file_location('ci_source', Path(__file__).with_name('check_ci.py'))
ci = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ci)


class AcceptanceTests(unittest.TestCase):
    def test_nested_check_processes_stop_before_disposable_checkout_cleanup(self):
        # A real grandchild ignores TERM and inherits the check's output pipe.
        # Observe EOF inside the cleanup boundary, before removing its cwd.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            child = root/'child.py'
            child.write_text(
                'import os,signal,time\nfrom pathlib import Path\n'
                'signal.signal(signal.SIGTERM, signal.SIG_IGN)\n'
                'print("nested child ready", flush=True)\n'
                'Path("ready").write_text(str(os.getpid()))\n'
                'while True:\n    Path("heartbeat").write_text("alive")\n    time.sleep(.01)\n')
            parent = root/'parent.py'
            parent.write_text(
                'import os,subprocess,sys,time\nfrom pathlib import Path\n'
                'if os.getpid() != os.getpgrp(): os.setsid()\n'
                'Path("group").write_text(str(os.getpid()))\n'
                'subprocess.Popen([sys.executable, "child.py"])\n'
                'while not Path("ready").exists(): time.sleep(.01)\n'
                'if sys.argv[1] == "timeout": time.sleep(30)\n')
            for mode in ('timeout', 'success'):
                with self.subTest(mode=mode):
                    execution = root/mode
                    execution.mkdir()
                    shutil.copy(child, execution/'child.py')
                    shutil.copy(parent, execution/'parent.py')
                    code = (
                        'import runpy,shutil,subprocess,sys; from pathlib import Path; '
                        'ci=runpy.run_path(sys.argv[1]); root=Path(sys.argv[2]); '
                        'mode=sys.argv[3]\n'
                        'try: result=ci["run"]([sys.executable,"parent.py",mode],root,timeout=2)\n'
                        'except subprocess.TimeoutExpired:\n'
                        ' assert mode == "timeout"\n'
                        'else:\n'
                        ' assert mode == "success" and result == 0\n'
                        'assert root.is_dir() and (root/"ready").is_file()\n'
                        '# Observe actual process state at the root-removal boundary.\n'
                        'group=int((root/"group").read_text())\n'
                        'snapshot=subprocess.run(["ps","-axo","pgid=,stat="],'
                        'capture_output=True,text=True,check=True,timeout=2)\n'
                        'states=[state for pgid,state in '
                        '(line.split() for line in snapshot.stdout.splitlines()) if int(pgid)==group]\n'
                        'assert all(state.startswith("Z") for state in states),states\n'
                        'shutil.rmtree(root)\n'
                        'print("cleanup boundary reached",flush=True)\n')
                    try:
                        result = subprocess.run(
                            ci.python_argv('-c', code, str(Path(ci.__file__)), str(execution), mode),
                            capture_output=True, text=True, timeout=8)
                    finally:
                        # Keep a failing pre-fix regression from orphaning its probe.
                        if (execution/'group').exists():
                            try:
                                os.killpg(int((execution/'group').read_text()), signal.SIGKILL)
                            except ProcessLookupError:
                                pass
                    self.assertEqual(0, result.returncode, result.stderr)
                    self.assertIn('nested child ready', result.stdout)
                    self.assertIn('cleanup boundary reached', result.stdout)
                    self.assertFalse(execution.exists())

    def test_process_cleanup_observation_failure_retains_checkout(self):
        process = mock.Mock(pid=123456789)
        failure = subprocess.CalledProcessError(1, ['ps'])
        for stage in ('observation', 'leader wait'):
            with self.subTest(stage=stage):
                process.wait.side_effect = (subprocess.TimeoutExpired(['probe'], 10)
                                            if stage == 'leader wait' else None)
                with mock.patch.object(ci.os, 'killpg'), \
                        mock.patch.object(ci.subprocess, 'run', side_effect=failure):
                    with self.assertRaises(ci.ProcessCleanupError):
                        ci.drain_group(process)
        process.wait.side_effect = None
        # Signal permission alone says nothing about absence or live work.
        for states in ('', '123456789 Z\n', '123456789 R\n'):
            with self.subTest(states=states), \
                    mock.patch.object(ci.os, 'killpg', side_effect=PermissionError), \
                    mock.patch.object(ci.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, states)), \
                    mock.patch.object(ci.time, 'monotonic', side_effect=[0, 10, 10]):
                if states.endswith('R\n'):
                    with self.assertRaises(ci.ProcessCleanupError):
                        ci.drain_group(process)
                else:
                    self.assertIsNone(ci.drain_group(process))
        execution = None
        def fake_git(root, *args):
            if args[0] == 'rev-parse':
                return b'a' * 40 + b'\n'
            if args[:2] == ('worktree', 'add'):
                Path(args[-2]).mkdir()
            return b''
        try:
            with mock.patch.object(ci, 'git', side_effect=fake_git) as git_call, \
                    mock.patch.object(ci, 'clean_checkout'), \
                    mock.patch('sys.stderr', new_callable=io.StringIO):
                with self.assertRaises(ci.ProcessCleanupError):
                    with ci.baseline_execution(ci.ROOT) as execution:
                        raise ci.ProcessCleanupError('unavailable process observation')
                self.assertTrue(execution.is_dir())
                self.assertFalse(any(call.args[1:3] == ('worktree', 'remove') for call in git_call.call_args_list))
        finally:
            if execution is not None:
                shutil.rmtree(execution.parent)

    def run_cases(self, body, retained=()):
        namespace = {'unittest': unittest}
        exec(body, namespace)
        suite = unittest.TestLoader().loadTestsFromTestCase(namespace['Probe'])
        ids = [test.id() for test in ci.flatten(suite)]
        manifest = {'required_test_ids': ids, 'retained_assertion_failures': [
            {'id': ids[index], 'message': message} for index, message in retained]}
        result = unittest.TextTestRunner(stream=io.StringIO(), resultclass=ci.Outcomes).run(suite)
        return ids, manifest, result

    def test_exact_retained_assertion_is_qualified_but_still_raw_red(self):
        ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    def test_red(self): self.fail("known")', [(0, 'known')])
        self.assertFalse(result.wasSuccessful())
        self.assertEqual(1, len(result.failures))
        self.assertTrue(ci.evaluate(ids, manifest, result))

    def test_new_green_regressions_do_not_require_manifest_updates(self):
        ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    def test_old(self): pass\n    def test_new(self): pass')
        manifest['required_test_ids'] = ids[:1]
        self.assertTrue(ci.evaluate(ids, manifest, result))

    def test_missing_required_duplicate_and_empty_discovery_fail(self):
        ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    def test_green(self): pass')
        for discovery in ([], ids * 2, ['absent.test']):
            with self.subTest(discovery=discovery):
                self.assertFalse(ci.evaluate(discovery, manifest, result))
        manifest['required_test_ids'].append('missing.test')
        self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_new_failure_changed_message_unexpected_pass_and_nonassertion_fail(self):
        for line, retained in (('self.fail("new")', []), ('self.fail("changed")', [(0, 'known')]),
                               ('pass', [(0, 'known')]), ('raise RuntimeError("known")', [(0, 'known')]),
                               ('raise ValueError("known")', [])):
            with self.subTest(line=line):
                ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    def test_case(self): ' + line, retained)
                self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_skips_xfails_and_decorator_unexpected_success_fail(self):
        for decorator, line in (('@unittest.skip("reason")', 'pass'),
                                ('@unittest.expectedFailure', 'self.fail("known")'),
                                ('@unittest.expectedFailure', 'pass')):
            with self.subTest(decorator=decorator, line=line):
                ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    ' + decorator + '\n    def test_case(self): ' + line)
                self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_failed_subtest_cannot_impersonate_retained_parent_assertion(self):
        body = 'class Probe(unittest.TestCase):\n    def test_case(self):\n        with self.subTest(value=1): self.fail("known")'
        ids, manifest, result = self.run_cases(body, [(0, 'known')])
        self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_passing_subtests_and_teardown_failures_are_distinguished(self):
        ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    def test_case(self):\n        with self.subTest(value=1): pass')
        self.assertTrue(ci.evaluate(ids, manifest, result))
        ids, manifest, result = self.run_cases('class Probe(unittest.TestCase):\n    def test_case(self): self.fail("known")\n    def tearDown(self): self.fail("known")', [(0, 'known')])
        self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_lifecycle_assertions_cannot_impersonate_retained_test_body(self):
        for stage in ('setUp', 'tearDown'):
            body = ('class Probe(unittest.TestCase):\n    def test_case(self): pass\n'
                    '    def ' + stage + '(self): self.fail("known")')
            with self.subTest(stage=stage):
                ids, manifest, result = self.run_cases(body, [(0, 'known')])
                self.assertFalse(ci.evaluate(ids, manifest, result))
        body = ('class Probe(unittest.TestCase):\n'
                '    def setUp(self): self.addCleanup(self.fail, "known")\n'
                '    def test_case(self): pass')
        ids, manifest, result = self.run_cases(body, [(0, 'known')])
        self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_class_and_module_lifecycle_assertions_fail_acceptance(self):
        for stage in ('setUpClass', 'tearDownClass', 'setUpModule', 'tearDownModule'):
            with self.subTest(stage=stage):
                module = types.ModuleType('synthetic_ci_lifecycle')
                source = 'import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): pass\n'
                if stage.endswith('Class'):
                    source += '    @classmethod\n    def ' + stage + '(cls): raise AssertionError("known")\n'
                else:
                    source += 'def ' + stage + '(): raise AssertionError("known")\n'
                exec(source, module.__dict__)
                with mock.patch.dict(sys.modules, {module.__name__: module}):
                    suite = unittest.TestLoader().loadTestsFromModule(module)
                    ids = [test.id() for test in ci.flatten(suite)]
                    result = unittest.TextTestRunner(stream=io.StringIO(), resultclass=ci.Outcomes).run(suite)
                manifest = {'required_test_ids': ids, 'retained_assertion_failures': [{'id': ids[0], 'message': 'known'}]}
                self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_setup_alias_and_delegation_cannot_impersonate_test_dispatch(self):
        for setup in ('setUp = test_case', 'def setUp(self): self.test_case()',
                      'def setUp(self): self._callTestMethod(self.test_case)'):
            body = 'class Probe(unittest.TestCase):\n    def test_case(self): self.fail("known")\n    ' + setup
            with self.subTest(setup=setup):
                ids, manifest, result = self.run_cases(body, [(0, 'known')])
                self.assertFalse(ci.evaluate(ids, manifest, result))

    def test_manifest_rejects_duplicates_missing_rationale_and_unknown_fields(self):
        good = {'schema_version': 1, 'required_test_ids': ['probe.test'], 'retained_assertion_failures': [
            {'id': 'probe.test', 'message': 'known', 'reason': 'pending behavior', 'removal_condition': 'reviewed implementation'}]}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root/ci.MANIFEST
            path.parent.mkdir(parents=True)
            path.write_text(json.dumps(good))
            self.assertEqual(good, ci.load_manifest(root))
            variants = []
            for field, value in (('required_test_ids', []), ('required_test_ids', ['probe.test'] * 2),
                                 ('schema_version', 2), ('schema_version', True), ('schema_version', 1.0),
                                 ('retained_assertion_failures', good['retained_assertion_failures'] * 2)):
                variants.append(dict(good, **{field: value}))
            for field in ('id', 'message', 'reason', 'removal_condition'):
                case = dict(good['retained_assertion_failures'][0]); case[field] = ''
                variants.append(dict(good, retained_assertion_failures=[case]))
            variants.append(dict(good, unexpected=True))
            for value in variants:
                with self.subTest(value=value):
                    path.write_text(json.dumps(value))
                    with self.assertRaises(ValueError): ci.load_manifest(root)
            path.write_text('{"schema_version": 1, "schema_version": 1}')
            with self.assertRaises(ValueError): ci.load_manifest(root)

    def test_collection_includes_baseline_operator_and_direct_acceptance(self):
        # Check discovery in a child to avoid polluting unittest's module registry.
        code = ('import runpy,json; m=runpy.run_path("scripts/check_ci.py"); '
                'print(json.dumps([t.id() for t in m["flatten"](m["collect"](m["ROOT"]))]))')
        result = subprocess.run(ci.python_argv('-c', code), cwd=ci.ROOT, capture_output=True, text=True, check=True)
        ids = json.loads(result.stdout)
        for module in ('test_harness.', 'test_single_task.', 'test_single_task_state.', 'test_symphony.',
                       'test_check_ci.', 'test_prepare_publication.', 'test_approval_binding.',
                       'test_task_fixtures.', 'test_task_package.', 'test_task_handoff.',
                       'test_task_outcomes.', 'test_task_automation.'):
            self.assertTrue(any(name.startswith(module) for name in ids), module)
        for prefix in ('verify_t004_cli.CliBoundaryTests.', 'verify_t003_state.StatePrimitiveTests.'):
            self.assertEqual(12, sum(name.startswith(prefix) for name in ids))
        self.assertEqual(len(ids), len(set(ids)))

    def test_import_error_and_duplicate_load_tests_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'tests').mkdir(); (root/'scripts').mkdir()
            (root/'tests/test_broken.py').write_text('raise RuntimeError("broken discovery")\n')
            suite = unittest.TestLoader().discover(str(root/'tests'))
            ids = [test.id() for test in ci.flatten(suite)]
            result = unittest.TextTestRunner(stream=io.StringIO(), resultclass=ci.Outcomes).run(suite)
            self.assertFalse(ci.evaluate(ids, {'required_test_ids': ids, 'retained_assertion_failures': []}, result))

    def test_cli_source_alias_migrates_without_adoption_claim(self):
        with mock.patch.object(ci, 'check', return_value=0) as check:
            with mock.patch('sys.stderr', new_callable=io.StringIO) as stderr:
                self.assertEqual(0, ci.main(['--policy-source', 'a' * 40]))
                self.assertIn('Deprecated', stderr.getvalue())
                self.assertEqual('a' * 40, check.call_args.args[2])
            self.assertEqual(0, ci.main(['--source', 'b' * 40]))
            self.assertEqual('b' * 40, check.call_args.args[2])
        with mock.patch('sys.stderr', new_callable=io.StringIO), self.assertRaises(SystemExit):
            ci.main(['--source', 'a' * 40, '--policy-source', 'b' * 40])


class PackageAcceptanceTests(unittest.TestCase):
    """Disposable local ports prove CI integration, never authenticated approval."""
    git = package_tests.RepositoryPrimitiveTests.git

    def setUp(self):
        package_tests.PackageContractTests.setUp(self)
        (self.root/'tests').mkdir()
        self.probe('pass')
        self.input['execution']['build_provenance'] = 'f' * 64
        for name in ('spec', 'plan', 'execution'):
            self.input['approval_refs'][name] = 'sha256:' + 'a' * 64
        self.input['outcomes'][0]['cases'] = [dict(id='test_probe.Probe.test_case', expected='pass', fingerprint=None)]
        self.input['outcomes'][0]['tool_revision'] = outcomes.tool_revision()
        self.input['outcomes'][0]['input_revision'] = outcomes.snapshot(self.root)
        self.bind()
        self.guard = self.fake_guard()

    def probe(self, body):
        (self.root/'tests/test_probe.py').write_text(
            'import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): ' + body + '\n')

    def bind(self):
        self.value = package.build_package(self.input)
        self.approved_revision = package.package_revision_digest(self.value)
        self.approved_checks = copy.deepcopy(self.value['outcomes'])

    def fake_guard(self):
        class Guard:
            capabilities = {name: package.CapabilityEvidence(True, 'a' * 40, True, None)
                            for name in package.required_capabilities('validate', 'implement')}
            def validate_context(self, context, evidence):
                return package.GuardValidation(True)
            def compare_artifacts(self, approved, current, evidence):
                return 'identical'
            def validate_scope(self, snapshot, context, evidence):
                return package.GuardValidation(True)
            def validate_acceptance(self, context, evidence):
                return package.GuardValidation(True)
        return Guard()

    def invoke(self, **changes):
        arguments = dict(approved_revision=self.approved_revision, approved_checks=self.approved_checks,
                         guard=self.guard)
        arguments.update(changes)
        return ci.package_acceptance(self.value, self.snapshot, None, {}, **arguments)

    def test_approved_green_package_uses_shared_validator_and_runner(self):
        result = self.invoke()
        self.assertTrue(result['accepted'])
        self.assertTrue(result['validation']['structural_valid'])
        self.assertEqual([0], result['validation']['raw_exits'])
        self.assertTrue(result['outcomes']['accepted'])
        self.assertFalse(result['execution_eligible'])
        self.assertIsNone(result['authenticated_approval'])
        self.assertIsNone(result['independent_review'])
        self.assertIsNone(result['live_execution'])

    def test_missing_guard_blocks_before_matrix_execution(self):
        with mock.patch.dict(ci._outcomes, run_checks=mock.Mock()) as _:
            runner = ci._outcomes['run_checks']
            result = self.invoke(guard=None)
            self.assertFalse(result['accepted'])
            self.assertIn('guard-unavailable', {item['code'] for item in result['validation']['blockers']})
            self.assertFalse(result['execution_eligible'])
            runner.assert_not_called()

    def test_missing_changed_and_content_only_revision_pins_block(self):
        for revision in (None, '', 'f' * 64, self.value['package_digest']):
            with self.subTest(revision=revision), mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
                result = self.invoke(approved_revision=revision)
                self.assertFalse(result['accepted'])
                ci._outcomes['run_checks'].assert_not_called()

    def test_approval_reference_change_requires_new_raw_revision(self):
        before = self.value['package_digest']
        self.input['approval_refs']['spec'] = 'sha256:' + 'b' * 64
        self.value = package.build_package(self.input)
        self.assertEqual(before, self.value['package_digest'])
        with mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
            self.assertFalse(self.invoke()['accepted'])
            ci._outcomes['run_checks'].assert_not_called()

    def test_changed_approved_argv_deadline_cases_and_revisions_block(self):
        for field, replacement in (('argv', [sys.executable, '-I', '-B', '-c', 'raise RuntimeError()']),
                                   ('deadline_seconds', 11), ('tool_revision', 'sha256:' + 'a' * 64),
                                   ('input_revision', 'sha256:' + 'b' * 64), ('cases', [])):
            changed = copy.deepcopy(self.approved_checks)
            changed[0][field] = replacement
            with self.subTest(field=field), mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
                self.assertFalse(self.invoke(approved_checks=changed)['accepted'])
                ci._outcomes['run_checks'].assert_not_called()

    def test_package_self_assertions_and_missing_host_checks_do_not_authorize(self):
        for checks in (None, [], [dict(self.approved_checks[0], id='different-check')]):
            with self.subTest(checks=checks), mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
                self.assertFalse(self.invoke(approved_checks=checks)['accepted'])
                ci._outcomes['run_checks'].assert_not_called()

    def test_source_repository_and_shape_fail_before_execution(self):
        original = copy.deepcopy(self.input)
        for section, field, replacement, blocker in (
                ('source', 'accepted_commit', 'f' * 40, 'git-object-unavailable'),
                ('execution', 'build_provenance', None, 'execution-identity-unavailable'),
                ('approval_refs', 'execution', None, 'execution-approval-reference-missing')):
            self.input = copy.deepcopy(original)
            self.input[section][field] = replacement
            self.bind()
            with self.subTest(field=field), mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
                result = self.invoke()
                self.assertFalse(result['accepted'])
                self.assertIn(blocker, {item['code'] for item in result['validation']['blockers']})
                ci._outcomes['run_checks'].assert_not_called()
        self.value = dict(package.build_package(original), repository='other/repository')
        self.value.pop('package_digest')
        self.value = package.build_package(self.value)
        with mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
            result = self.invoke(approved_revision=package.package_revision_digest(self.value))
            self.assertFalse(result['accepted'])
            self.assertEqual([{'code': 'repository-mismatch'}], result['validation']['blockers'])
            ci._outcomes['run_checks'].assert_not_called()

    def test_guard_scope_rejection_and_capability_loss_block(self):
        for change in ('scope', 'capability'):
            guard = self.fake_guard()
            if change == 'scope':
                guard.validate_scope = lambda *args: package.GuardValidation(False)
            else:
                guard.capabilities = {}
            with self.subTest(change=change), mock.patch.dict(ci._outcomes, run_checks=mock.Mock()):
                self.assertFalse(self.invoke(guard=guard)['accepted'])
                ci._outcomes['run_checks'].assert_not_called()

    def test_changed_input_and_tool_bytes_fail_shared_execution(self):
        for change in ('input', 'tool'):
            self.input['outcomes'][0]['input_revision'] = outcomes.snapshot(self.root)
            self.input['outcomes'][0]['tool_revision'] = outcomes.tool_revision()
            if change == 'input':
                self.bind()
                (self.root/'changed.txt').write_text('changed')
            else:
                self.input['outcomes'][0]['tool_revision'] = 'sha256:' + 'f' * 64
                self.bind()
            with self.subTest(change=change):
                result = self.invoke()
                self.assertFalse(result['accepted'])
                self.assertEqual('revision_mismatch', result['outcomes']['checks'][0]['blocker'])

    def test_new_failure_skip_and_extra_case_remain_strict(self):
        for body in ('self.fail("new")', 'self.skipTest("skip")', 'pass\n    def test_extra(self): pass'):
            self.probe(body)
            self.input['outcomes'][0]['input_revision'] = outcomes.snapshot(self.root)
            self.bind()
            with self.subTest(body=body):
                self.assertFalse(self.invoke()['accepted'])

    def test_retained_package_assertion_requires_separate_matrix_approval(self):
        self.probe('self.fail("fixture-red")')
        check = self.input['outcomes'][0]
        check['input_revision'] = outcomes.snapshot(self.root)
        check['cases'][0].update(expected='assertion', fingerprint=outcomes.assertion_fingerprint(
            check['cases'][0]['id'], 'fixture-red'))
        check['raw_exit'] = 1
        self.input['approval_refs']['matrix'] = 'sha256:' + 'c' * 64
        self.bind()
        self.assertFalse(self.invoke()['accepted'])
        result = self.invoke(matrix_approved=True)
        self.assertTrue(result['accepted'])
        self.assertEqual([1], result['validation']['raw_exits'])
        self.assertFalse(result['execution_eligible'])

    def test_errors_are_sanitized_without_raw_argv_or_adapter_diagnostics(self):
        self.guard.validate_context = mock.Mock(side_effect=RuntimeError('secret-canary'))
        result = self.invoke()
        self.assertFalse(result['accepted'])
        self.assertNotIn('secret-canary', json.dumps(result))
        self.assertNotIn('argv', json.dumps(result))


class SourceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)/'repo'
        self.root.mkdir()
        self.git('init', '--quiet', '-b', 'main')
        self.git('config', 'user.name', 'Synthetic')
        self.git('config', 'user.email', 'fixture@example.invalid')
        (self.root/'guide.md').write_text('baseline\n')
        (self.root/'scripts').mkdir()
        (self.root/'scripts/check_ci.py').write_bytes(Path(ci.__file__).read_bytes())
        (self.root/'scripts/check_ci.py').chmod(Path(ci.__file__).stat().st_mode & 0o777)
        (self.root/'scripts/task_outcomes.py').write_bytes(Path(ci.__file__).with_name('task_outcomes.py').read_bytes())
        for name in ('task_package.py', 'single_task_state.py'):
            (self.root/'scripts'/name).write_bytes(Path(ci.__file__).with_name(name).read_bytes())
        self.git('add', '.')
        self.git('commit', '--quiet', '-m', 'test: baseline')
        self.base = self.git('rev-parse', 'HEAD')
        self.github = mock.patch.dict(os.environ)
        self.github.start(); self.addCleanup(self.github.stop)
        for name in ('GITHUB_EVENT_NAME', 'GITHUB_SHA', 'GITHUB_EVENT_PATH'):
            os.environ.pop(name, None)

    def git(self, *args, text=None):
        return subprocess.run(['git', '-c', 'commit.gpgsign=false', *args], cwd=self.root,
                              input=text, text=True, capture_output=True, check=True).stdout.strip()

    def commit(self, name, content='Proposal\n'):
        (self.root/name).write_text(content)
        self.git('add', '--', name); self.git('commit', '--quiet', '-m', 'test: revision')
        return self.git('rev-parse', 'HEAD')

    def merge(self, base, head, parents=None, tree=None):
        tree = tree or self.git('merge-tree', '--write-tree', base, head)
        args = ['commit-tree', tree]
        for parent in parents if parents is not None else (base, head): args += ['-p', parent]
        revision = self.git(*args, text='Synthetic integration\n')
        self.git('checkout', '--quiet', '--detach', revision)
        return ci.SourceContext('pull_request', base, head, revision)

    def proposal(self):
        head = self.commit('proposal.md')
        self.git('checkout', '--quiet', '--detach', self.base)
        base = self.commit('unrelated-base.md')
        return self.merge(base, head)

    def test_actual_merge_preserves_unrelated_base_advance(self):
        context = self.proposal()
        self.assertEqual(context, ci.source_context(self.root, *context))
        self.assertNotEqual(self.git('rev-parse', context.head + '^{tree}'), self.git('rev-parse', 'HEAD^{tree}'))

    def test_forged_merge_tree_and_wrong_parent_order_block(self):
        context = self.proposal()
        for parents, tree in (((context.head, context.base), None),
                              ((context.base, context.head, self.base), None),
                              (None, self.git('rev-parse', context.head + '^{tree}'))):
            with self.subTest(parents=parents, tree=tree):
                wrong = self.merge(context.base, context.head, parents, tree)
                with self.assertRaises(ValueError): ci.source_context(self.root, *wrong)

    def test_missing_objects_and_partial_context_block(self):
        for pins in (('local', None, None, None), ('local', '0' * 40, self.base, self.base)):
            with self.subTest(pins=pins), self.assertRaises(ValueError): ci.source_context(self.root, *pins)

    def test_stale_foreign_nonmain_and_unsupported_event_metadata_block(self):
        context = self.proposal()
        event = {'pull_request': {'base': {'sha': context.base, 'ref': 'main', 'repo': {'full_name': 'wstein/workharbor-nxt'}},
                                  'head': {'sha': context.head}}}
        path = Path(self.temporary.name)/'event.json'; path.write_text(json.dumps(event))
        os.environ.update(GITHUB_EVENT_NAME='pull_request', GITHUB_SHA=context.checkout, GITHUB_EVENT_PATH=str(path))
        self.assertEqual(context, ci.source_context(self.root, *context))
        for field, value in (('GITHUB_SHA', context.head), ('GITHUB_EVENT_NAME', 'workflow_dispatch')):
            with self.subTest(field=field), mock.patch.dict(os.environ, {field: value}), self.assertRaises(ValueError):
                ci.source_context(self.root, *context)
        for field, value in (('ref', 'topic'), ('repo', {'full_name': 'foreign/repository'}), ('sha', self.base)):
            changed = json.loads(json.dumps(event)); changed['pull_request']['base'][field] = value
            path.write_text(json.dumps(changed))
            with self.subTest(field=field), self.assertRaises(ValueError): ci.source_context(self.root, *context)

    def test_dirty_index_modes_untracked_and_assume_unchanged_block(self):
        context = self.proposal(); path = self.root/'proposal.md'
        for mutation in ('dirty', 'staged', 'mode', 'symlink', 'untracked', 'ignored', 'assume-unchanged'):
            with self.subTest(mutation=mutation):
                self.git('config', 'core.filemode', 'false')
                if mutation in ('untracked', 'ignored'):
                    (self.root/'extra.py').write_text('pass\n')
                    if mutation == 'ignored': (self.root/'.git/info/exclude').write_text('/extra.py\n')
                elif mutation == 'mode': path.chmod(0o755)
                elif mutation == 'symlink': path.unlink(); path.symlink_to('guide.md')
                else:
                    path.write_text('changed\n')
                    if mutation == 'staged': self.git('add', 'proposal.md'); path.write_text('Proposal\n')
                    elif mutation == 'assume-unchanged': self.git('update-index', '--assume-unchanged', 'proposal.md')
                with self.assertRaises((ValueError, subprocess.CalledProcessError)): ci.source_context(self.root, *context)
                self.git('update-index', '--no-assume-unchanged', 'proposal.md')
                self.git('restore', '--staged', '--worktree', 'proposal.md'); path.chmod(0o644)
                if (self.root/'extra.py').exists(): (self.root/'extra.py').unlink()

    def test_git_executable_config_and_environment_do_not_run_sentinels(self):
        context = self.proposal(); sentinel = Path(self.temporary.name)/'executed'
        for key in ('filter.probe.clean', 'filter.probe.process', 'merge.probe.driver',
                    'diff.probe.textconv', 'diff.external', 'include.path'):
            with self.subTest(key=key):
                self.git('config', key, 'touch ' + str(sentinel))
                with self.assertRaises(ValueError): ci.source_context(self.root, *context)
                self.assertFalse(sentinel.exists()); self.git('config', '--unset', key)
        self.git('config', 'core.fsmonitor', 'touch ' + str(sentinel))
        with mock.patch.dict(os.environ, {'GIT_CONFIG_COUNT': '1', 'GIT_CONFIG_KEY_0': 'core.fsmonitor',
                                         'GIT_CONFIG_VALUE_0': 'touch ' + str(sentinel), 'GIT_DIR': '/absent'}):
            self.assertEqual(context, ci.source_context(self.root, *context))
        self.assertFalse(sentinel.exists())

    def test_local_git_children_exclude_credentials_and_lazy_fetch(self):
        context = self.proposal()
        original = subprocess.run
        children = []
        def observe(argv, **kwargs):
            if argv[0] == 'git':
                children.append((argv, kwargs.get('env', {})))
            return original(argv, **kwargs)
        with mock.patch.dict(os.environ, {'GITHUB_TOKEN': 'secret-canary',
                                         'UNRELATED_HOST_SECRET': 'secret-canary',
                                         'PYTHONPATH': '/secret-canary'}), \
                mock.patch.object(ci.subprocess, 'run', side_effect=observe):
            self.assertEqual(context, ci.source_context(self.root, *context))
        self.assertTrue(children)
        for argv, environment in children:
            self.assertFalse('secret-canary' in environment.values(), 'host credential inherited')
            self.assertNotIn('PYTHONPATH', environment)
            self.assertIn('--no-lazy-fetch', argv)
            self.assertEqual('1', environment.get('GIT_NO_LAZY_FETCH'))
            self.assertEqual('', environment.get('GIT_ALLOW_PROTOCOL'))

    def test_git_objects_refuse_replacements_and_grafts(self):
        context = self.proposal()
        self.git('replace', context.head, context.base)
        with self.assertRaises(ValueError): ci.source_context(self.root, *context)
        self.git('replace', '-d', context.head)
        graft = self.root/'.git/info/grafts'
        for dangling in (False, True):
            if dangling: graft.symlink_to('missing')
            else: graft.write_text(context.checkout + '\n')
            with self.assertRaises(ValueError): ci.source_context(self.root, *context)
            graft.unlink()

    def test_source_pin_identifies_exact_code_without_adoption(self):
        ci.source_identity(self.root, self.base)
        for source in ('HEAD', '0' * 40):
            with self.subTest(source=source), self.assertRaises((ValueError, subprocess.CalledProcessError)):
                ci.source_identity(self.root, source)
        self.commit('scripts/check_ci.py', 'pass\n')
        with self.assertRaises(ValueError): ci.source_identity(self.root, self.base)

    def test_source_pin_binds_extracted_classifier_dependency(self):
        ci.source_identity(self.root, self.base)
        self.commit('scripts/task_outcomes.py', 'pass\n')
        with self.assertRaises(ValueError): ci.source_identity(self.root, self.base)

    def test_source_pin_binds_package_and_traversal_dependencies(self):
        for name in ('task_package.py', 'single_task_state.py'):
            with self.subTest(name=name):
                ci.source_identity(self.root, self.base)
                self.commit('scripts/' + name, 'pass\n')
                with self.assertRaises(ValueError): ci.source_identity(self.root, self.base)
                self.git('checkout', '--quiet', '--detach', self.base)

    def test_python_cache_is_external_for_current_and_child_imports_and_restored(self):
        before = (sys.pycache_prefix, sys.dont_write_bytecode)
        with ci.isolated_python_cache():
            prefix = Path(sys.pycache_prefix)
            result = subprocess.run(ci.python_argv('-c', 'import json,sys;print(json.dumps([sys.flags.isolated,sys.pycache_prefix,sys.dont_write_bytecode]))'),
                                    capture_output=True, text=True, check=True)
            self.assertEqual([1, str(prefix), True], json.loads(result.stdout))
        self.assertFalse(prefix.exists()); self.assertEqual(before, (sys.pycache_prefix, sys.dont_write_bytecode))

    def test_normal_cli_subprocess_retains_adjacent_module_imports(self):
        (self.root/'scripts/adjacent_module.py').write_text('VALUE = 7\n')
        (self.root/'scripts/probe.py').write_text('import adjacent_module\nprint(adjacent_module.VALUE)\n')
        with ci.isolated_python_cache():
            self.assertNotIn('PYTHONSAFEPATH', os.environ)
            result = subprocess.run([sys.executable, str(self.root/'scripts/probe.py')],
                                    cwd=self.root, capture_output=True, text=True)
        self.assertEqual((0, '7\n'), (result.returncode, result.stdout))

    def test_actual_cli_startup_rejects_hidden_imports_before_execution(self):
        sentinel = Path(self.temporary.name)/'executed'
        (self.root/'scripts/json.py').write_text('open(' + repr(str(sentinel)) + ',"w").write("executed")\n')
        for flags in (('-B',), ('-I', '-B')):
            result = subprocess.run([sys.executable, *flags, str(self.root/'scripts/check_ci.py')], cwd=self.root,
                                    capture_output=True, text=True)
            self.assertNotEqual(0, result.returncode); self.assertFalse(sentinel.exists())

    def test_execution_uses_disposable_same_revision_and_removes_generated_evidence(self):
        with ci.baseline_execution(self.root) as execution:
            self.assertNotEqual(self.root, execution)
            self.assertEqual(self.base, subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=execution, text=True).strip())
            (execution/'generated').write_text('private')
        self.assertFalse(execution.exists()); self.assertFalse((self.root/'generated').exists())
        self.assertEqual(self.base, ci.source_context(self.root).checkout)

    def test_invalid_source_blocks_before_any_check_runs(self):
        with mock.patch.object(ci, 'run') as run:
            self.assertEqual(1, ci.check(self.root, ('local', None, None, None)))
            run.assert_not_called()

    def test_command_timeouts_remain_finite_and_explicit(self):
        process = mock.Mock(pid=123456789)
        process.wait.return_value = 0
        with mock.patch.object(ci.subprocess, 'Popen', return_value=process), \
                mock.patch.object(ci, 'drain_group'):
            process.stdout = io.BytesIO()
            self.assertEqual(0, ci.run(['probe'], self.root))
            self.assertEqual(300, process.wait.call_args.kwargs['timeout'])
            process.stdout = io.BytesIO()
            self.assertEqual(0, ci.run(['suite'], self.root, timeout=600))
            self.assertEqual(600, process.wait.call_args.kwargs['timeout'])

    def test_aggregate_suite_timeout_is_bounded_and_fails_closed(self):
        original_run = ci.subprocess.run
        observed = []
        def commands(argv, *args, **kwargs):
            if argv[:2] == ['bash', '.specify/scripts/bash/check-prerequisites.sh']:
                return subprocess.CompletedProcess(argv, 0)
            return original_run(argv, *args, **kwargs)
        def checks(argv, root, **kwargs):
            observed.append(kwargs)
            if kwargs.get('timeout') == 600:
                raise subprocess.TimeoutExpired(argv, 600)
            return 0
        with mock.patch.object(ci, 'load_manifest', return_value={'retained_assertion_failures': []}), \
                mock.patch.object(ci, 'run', side_effect=checks), \
                mock.patch.object(ci.subprocess, 'run', side_effect=commands):
            self.assertEqual(1, ci.check(self.root))
        self.assertEqual({'timeout': 600}, observed[-1])
        self.assertTrue(all(not arguments for arguments in observed[:-1]))

    def test_post_check_source_drift_blocks(self):
        manifest = {'retained_assertion_failures': []}
        original_run = ci.subprocess.run
        def commands(argv, *args, **kwargs):
            if argv[:2] == ['bash', '.specify/scripts/bash/check-prerequisites.sh']:
                return subprocess.CompletedProcess(argv, 0)
            return original_run(argv, *args, **kwargs)
        for drift in (False, True):
            def successful_checks(argv, root, **kwargs):
                if drift:
                    (self.root/'guide.md').write_text('changed\n')
                return 0
            with self.subTest(drift=drift), mock.patch.object(ci, 'load_manifest', return_value=manifest), \
                    mock.patch.object(ci, 'run', side_effect=successful_checks), \
                    mock.patch.object(ci.subprocess, 'run', side_effect=commands):
                self.assertEqual(1 if drift else 0, ci.check(self.root))

    def test_workflow_checks_ordinary_merge_without_external_policy_source(self):
        workflow = (ci.ROOT/'.github/workflows/check.yml').read_text()
        self.assertIn('contents: read', workflow)
        self.assertIn('persist-credentials: false', workflow)
        self.assertIn('python3 -I -B scripts/check_ci.py', workflow)
        self.assertNotIn('WORKHARBOR_CI_POLICY_SOURCE', workflow)
        self.assertNotIn('continue-on-error', workflow)
        self.assertNotIn('pull_request_target', workflow)


if __name__ == '__main__':
    unittest.main()
