"""Shared exact-case packages; fixture matrices confer no live authority."""
import copy
import hashlib
import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SPEC = importlib.util.spec_from_file_location('task_outcomes_under_test', Path(__file__).with_name('task_outcomes.py'))
outcomes = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(outcomes)


class PackageOutcomeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)/'checkout'
        self.root.mkdir()
        (self.root/'tests').mkdir()
        self.source = self.root/'tests/test_probe.py'
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): pass\n')

    def check(self, *, cases=None, raw_exit=0, deadline=10):
        return {'id': 'fixture-check', 'argv': [sys.executable, '-m', 'unittest', 'discover',
                '-s', 'tests', '-p', 'test_*.py', '-v'], 'deadline_seconds': deadline,
                'tool_revision': outcomes.tool_revision(), 'input_revision': outcomes.snapshot(self.root),
                'cases': cases or [{'id': 'test_probe.Probe.test_case', 'expected': 'pass', 'fingerprint': None}],
                'raw_exit': raw_exit}

    def run_check(self, check, *, approved=None, matrix_approved=False):
        return outcomes.run_checks(self.root, [check], approved_checks=approved or [check],
                                   matrix_approved=matrix_approved)

    def test_all_three_inert_fixtures_use_same_exact_runner(self):
        fixture_root = Path(__file__).resolve().parents[1]/'tests/fixtures/task-automation'
        for kind in ('documentation', 'test-authoring', 'implementation'):
            fixture = json.loads((fixture_root/(kind + '.json')).read_text())
            body = 'import unittest\nclass Probe(unittest.TestCase):\n'
            cases = []
            for index, case in enumerate(fixture['case_manifest']['cases']):
                message = case['assertion']['message'] if case['assertion'] else None
                body += '    def test_' + str(index) + '(self): ' + ('self.fail(' + repr(message) + ')' if message else 'pass') + '\n'
                cases.append({'id': case['id'], 'expected': case['expected'], 'fingerprint':
                              outcomes.assertion_fingerprint(case['id'], message) if message else None})
            mapping = {f'test_{index}': case['id'] for index, case in enumerate(cases)}
            body += '    def id(self): return ' + repr(mapping) + '[self._testMethodName]\n'
            self.source.write_text(body)
            check = self.check(cases=cases, raw_exit=fixture['case_manifest']['expected_raw_exit'])
            with self.subTest(kind=kind):
                result = self.run_check(check, matrix_approved=kind == 'test-authoring')
                self.assertTrue(result['accepted'], result)
                self.assertEqual(check['raw_exit'], result['checks'][0]['raw_exit'])
                self.assertEqual([case['id'] for case in cases], result['checks'][0]['case_ids'])
                self.assertEqual(outcomes.checks_digest([check]), result['matrix_digest'])
                self.assertFalse(result['authenticated_approval'])

    def test_execution_requires_separate_exact_argv_approval(self):
        check = self.check()
        changed = copy.deepcopy(check); changed['argv'][-1] = '-q'
        with mock.patch.object(outcomes.subprocess, 'Popen') as launch:
            result = self.run_check(changed, approved=[check])
        self.assertFalse(result['accepted']); launch.assert_not_called()

    def test_assertions_need_distinct_reviewed_matrix_and_exact_fingerprint(self):
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): self.fail("reviewed")\n')
        case = {'id': 'test_probe.Probe.test_case', 'expected': 'assertion',
                'fingerprint': outcomes.assertion_fingerprint('test_probe.Probe.test_case', 'reviewed')}
        check = self.check(cases=[case], raw_exit=1)
        self.assertFalse(self.run_check(check)['accepted'])
        self.assertTrue(self.run_check(check, matrix_approved=True)['accepted'])
        case['fingerprint'] = outcomes.assertion_fingerprint(case['id'], 'changed')
        self.assertFalse(self.run_check(check, matrix_approved=True)['accepted'])

    def test_missing_extra_duplicate_empty_and_reordered_case_inventory_rejected(self):
        check = self.check()
        variants = [[], check['cases'] * 2, [dict(check['cases'][0], id='missing')],
                    check['cases'] + [dict(check['cases'][0], id='extra')]]
        for cases in variants:
            with self.subTest(cases=cases):
                candidate = dict(check, cases=cases)
                self.assertFalse(self.run_check(candidate)['accepted'])
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_a(self): pass\n    def test_b(self): pass\n')
        cases = [{'id': 'test_probe.Probe.test_' + name, 'expected': 'pass', 'fingerprint': None} for name in ('b', 'a')]
        self.assertFalse(self.run_check(self.check(cases=cases))['accepted'])

    def test_duplicate_empty_check_ids_and_limits_rejected(self):
        check = self.check()
        for checks in ([], [check] * 2, [dict(check, id='')], [dict(check, id=str(i)) for i in range(101)]):
            with self.subTest(size=len(checks)), self.assertRaises(ValueError):
                outcomes.validate_checks(checks)
        with self.assertRaises(ValueError):
            outcomes.validate_checks([dict(check, cases=[dict(check['cases'][0], id=str(i)) for i in range(10001)])])

    def test_invalid_deadlines_argv_revisions_raw_exits_and_fingerprints_rejected(self):
        check = self.check()
        for key, values in {'deadline_seconds': [0, -1, True, float('inf'), 301],
                            'argv': [[], ['sh', '-c', 'true'], [sys.executable, '-m', 'unittest', 'discover', '-s', '../escape']],
                            'tool_revision': ['HEAD'], 'input_revision': [''], 'raw_exit': [True, 2]}.items():
            for value in values:
                with self.subTest(key=key, value=value), self.assertRaises(ValueError):
                    outcomes.validate_checks([dict(check, **{key: value})])
        for case in (dict(check['cases'][0], fingerprint='unsafe'),
                     dict(check['cases'][0], expected='assertion'), dict(check['cases'][0], expected='xfail')):
            with self.assertRaises(ValueError): outcomes.validate_checks([dict(check, cases=[case])])

    def test_setup_import_cleanup_subtest_skip_xfail_and_unexpected_outcomes_rejected(self):
        bodies = ['raise ImportError("private detail")',
                  'class Probe(unittest.TestCase):\n    def setUp(self): self.fail("known")\n    def test_case(self): pass',
                  'class Probe(unittest.TestCase):\n    def test_case(self): self.addCleanup(self.fail,"known")',
                  'class Probe(unittest.TestCase):\n    def test_case(self):\n        with self.subTest(value=1): self.fail("known")',
                  'class Probe(unittest.TestCase):\n    @unittest.skip("known")\n    def test_case(self): pass',
                  'class Probe(unittest.TestCase):\n    @unittest.expectedFailure\n    def test_case(self): self.fail("known")',
                  'class Probe(unittest.TestCase):\n    @unittest.expectedFailure\n    def test_case(self): pass',
                  'class Probe(unittest.TestCase):\n    def test_case(self): raise RuntimeError("known")',
                  'class Probe(unittest.TestCase):\n    def test_case(self): self.fail("regression")']
        for body in bodies:
            self.source.write_text('import unittest\n' + body + '\n')
            with self.subTest(body=body): self.assertFalse(self.run_check(self.check())['accepted'])
        case = {'id': 'test_probe.Probe.test_case', 'expected': 'assertion',
                'fingerprint': outcomes.assertion_fingerprint('test_probe.Probe.test_case', 'known')}
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): pass\n')
        self.assertFalse(self.run_check(self.check(cases=[case], raw_exit=1), matrix_approved=True)['accepted'])

    def test_load_tests_duplicate_inventory_rejected_before_body_runs(self):
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): raise RuntimeError("body ran")\n'
                               'def load_tests(loader, tests, pattern):\n    tests.addTests(loader.loadTestsFromTestCase(Probe))\n    return tests\n')
        result = self.run_check(self.check())
        self.assertFalse(result['accepted'])
        self.assertIsNone(result['checks'][0]['raw_exit'])

    def test_timeout_kills_process_group_and_reports_no_success(self):
        self.source.write_text('import unittest,time\nclass Probe(unittest.TestCase):\n    def test_case(self): time.sleep(30)\n')
        result = self.run_check(self.check(deadline=0.1))
        self.assertFalse(result['accepted'])
        self.assertEqual('timeout', result['checks'][0]['blocker'])
        self.assertIsInstance(result['checks'][0]['raw_exit'], int)

    def test_mutation_and_tool_or_input_revision_drift_block(self):
        check = self.check()
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): pass\n# drift\n')
        with mock.patch.object(outcomes.subprocess, 'Popen') as launch:
            self.assertFalse(self.run_check(check)['accepted'])
        launch.assert_not_called()
        self.source.write_text('import unittest\nfrom pathlib import Path\nclass Probe(unittest.TestCase):\n'
                               '    def test_case(self): Path("generated").write_text("changed")\n')
        self.assertFalse(self.run_check(self.check())['accepted'])
        check = self.check(); check['tool_revision'] = 'sha256:' + '0' * 64
        self.assertFalse(self.run_check(check)['accepted'])

    def test_no_credentials_output_or_raw_assertion_message_in_evidence(self):
        self.source.write_text('import unittest,os\nclass Probe(unittest.TestCase):\n    def test_case(self):\n'
                               '        self.assertNotIn("GITHUB_TOKEN", os.environ)\n'
                               '        print("sensitive output")\n        self.fail("sensitive output")\n')
        with mock.patch.dict(os.environ, {'GITHUB_TOKEN': 'credential-value'}):
            result = self.run_check(self.check())
        rendered = json.dumps(result)
        self.assertNotIn('sensitive output', rendered)
        self.assertNotIn('credential-value', rendered)
        self.assertEqual(1, result['checks'][0]['raw_exit'])

    def test_fingerprint_binds_case_type_and_bounded_assertion_identity(self):
        value = outcomes.assertion_fingerprint('case.one', 'known')
        self.assertRegex(value, r'^sha256:[0-9a-f]{64}$')
        self.assertNotEqual(value, outcomes.assertion_fingerprint('case.two', 'known'))
        self.assertNotEqual(value, outcomes.assertion_fingerprint('case.one', 'different'))
        with self.assertRaises(ValueError): outcomes.assertion_fingerprint('case.one', 'x' * 4097)

    def test_approved_matrix_entry_returns_zero_preserving_raw_red_and_rejects_digest_drift(self):
        self.source.write_text('import unittest\nclass Probe(unittest.TestCase):\n    def test_case(self): self.fail("known")\n')
        case = {'id': 'test_probe.Probe.test_case', 'expected': 'assertion',
                'fingerprint': outcomes.assertion_fingerprint('test_probe.Probe.test_case', 'known')}
        checks = [self.check(cases=[case], raw_exit=1)]
        matrix = Path(self.temporary.name)/'matrix.json'; matrix.write_text(json.dumps(checks))
        argv = outcomes.matrix_argv(self.root, matrix, checks, matrix_approved=True)
        result = subprocess.run(argv, capture_output=True, text=True)
        self.assertEqual(0, result.returncode, result.stderr)
        evidence = json.loads(result.stdout)
        self.assertEqual(1, evidence['checks'][0]['raw_exit'])
        self.assertFalse(evidence['authenticated_approval'])
        matrix.write_text(json.dumps([dict(checks[0], id='different')]))
        result = subprocess.run(argv, capture_output=True, text=True)
        self.assertEqual(4, result.returncode)
        self.assertNotIn('known', result.stdout + result.stderr)


class InvocationStageTests(unittest.TestCase):
    def classify(self, source):
        namespace = {'unittest': unittest}
        exec(source, namespace)
        suite = unittest.TestLoader().loadTestsFromTestCase(namespace['Probe'])
        result = unittest.TextTestRunner(stream=io.StringIO(), resultclass=outcomes.Outcomes).run(suite)
        return result.observed

    def test_body_method_aliased_to_lifecycle_name_keeps_actual_body_stage(self):
        observed = self.classify('class Probe(unittest.TestCase):\n    def helper(self): self.fail("known")\n'
                                 '    test_case = helper\n    def setUp(self): pass\n    tearDown = setUp\n')
        self.assertEqual('assertion', observed[0][1])

    def test_nested_dispatch_from_setup_and_cleanup_cannot_claim_body(self):
        for hook in ('def setUp(self): self._callTestMethod(self.test_case)',
                     'def setUp(self): self.addCleanup(self._callTestMethod, self.test_case)',
                     'setUp = test_case'):
            source = 'class Probe(unittest.TestCase):\n    def test_case(self): self.fail("known")\n    ' + hook
            with self.subTest(hook=hook):
                observed = self.classify(source)
                self.assertTrue(any(kind == 'other-failure' for _, kind, _ in observed))

    def test_overridden_dispatch_failure_without_body_is_not_body_assertion(self):
        observed = self.classify('class Probe(unittest.TestCase):\n    def test_case(self): pass\n'
                                 '    def _callTestMethod(self, method): self.fail("known")')
        self.assertEqual('other-failure', observed[0][1])

    def test_bound_dispatch_name_is_not_required_for_real_body(self):
        observed = self.classify('class Probe(unittest.TestCase):\n    def test_case(self): self.fail("known")\n'
                                 '    def _callTestMethod(self, method): method()')
        self.assertEqual('assertion', observed[0][1])


if __name__ == '__main__':
    unittest.main()
