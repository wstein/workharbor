"""Inert T001 inputs only; no TaskPackage validator or guard execution."""
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import socket
import subprocess
import tempfile
import unittest
from contextlib import ExitStack
from unittest.mock import patch


FIXTURES = Path(__file__).resolve().parents[1] / 'tests/fixtures/task-automation'
NAMES = ('documentation.json', 'test-authoring.json', 'implementation.json')
BASE = '5a2dda81f456ab485d46bc19374c4229e02487bd'
BLOBS = {
    'spec.md': '4bf3f49ed1cb3684f3cb54493ebfbbebff5ddef4',
    'plan.md': '123381f0a4546cad6dfab78c2321fa78a55ec243',
    'tasks.md': '62025b81219df310f50b03033a4ecd5366090dc7',
    'check_ci.py': '75a245de1adcbacedc7eebf634b06b35b3b3a635',
    'prepare_publication.py': '3a16da54dcbeac431955efc879d88912e2bed9d5',
    'approval_binding.py': 'caaa7267d4cfb2d82f20a3029a57f738f02304ac',
    'single_task.py': '4db2fec60dbc2b99b72ce2c5776eaabf8ac4b6f6',
    'single_task_state.py': '5a44b62723cb98568763331374d957e18f873133',
    'ci-acceptance.json': 'b58f59f3b19c5a0d8e76e4c12f0d916dd14686ef',
}
RETAINED = ('test_one_lock_owner', 'test_lock_released_after_exit',
            'test_transaction_rollback', 'test_append_only_events',
            'test_immutable_context')


def load_inputs(directory=FIXTURES):
    """Read inert JSON. This helper neither validates packages nor runs checks."""
    return [json.loads((directory / name).read_text(encoding='utf-8')) for name in NAMES]


class TaskFixtureTests(unittest.TestCase):
    def assert_shape(self, value, kind, outcomes, raw_exit):
        self.assertEqual({'fixture_schema', 'kind', 'task_id', 'issue',
                          'execution_eligible', 'scope', 'prerequisites',
                          'case_manifest', 'evidence', 'predecessor'}, set(value))
        self.assertIs(type(value['fixture_schema']), int)
        self.assertEqual(1, value['fixture_schema'])
        self.assertEqual(kind, value['kind'])
        manifest = value['case_manifest']
        self.assertEqual({'expected_raw_exit', 'cases'}, set(manifest))
        self.assertEqual(raw_exit, manifest['expected_raw_exit'])
        cases = manifest['cases']
        self.assertEqual(outcomes, [case['expected'] for case in cases])
        self.assertEqual(len(cases), len({case['id'] for case in cases}))
        for case in cases:
            self.assertTrue(case['id'].startswith('fixture.'))
            self.assertEqual({'id', 'expected', 'assertion'}, set(case))
            if case['expected'] == 'pass':
                self.assertIsNone(case['assertion'])
            else:
                self.assertEqual({'type': 'AssertionError',
                                  'message': 'synthetic pending behavior'}, case['assertion'])

    def test_documentation_shape(self):
        self.assert_shape(load_inputs()[0], 'documentation', ['pass'], 0)

    def test_test_authoring_shape(self):
        self.assert_shape(load_inputs()[1], 'test-authoring', ['pass', 'assertion'], 1)

    def test_implementation_shape(self):
        self.assert_shape(load_inputs()[2], 'implementation', ['pass', 'pass'], 0)

    def test_unique_synthetic_task_ids(self):
        values = load_inputs()
        self.assertEqual(3, len({value['task_id'] for value in values}))
        for value in values:
            self.assertTrue(value['task_id'].startswith('fixture:'))
            self.assertIsNone(value['issue'])

    def test_successor_preserves_case_inventory(self):
        documentation, authoring, implementation = load_inputs()
        self.assertIsNone(documentation['predecessor'])
        self.assertIsNone(authoring['predecessor'])
        self.assertEqual('test-authoring.json', implementation['predecessor'])
        before = authoring['case_manifest']['cases']
        after = implementation['case_manifest']['cases']
        self.assertEqual([case['id'] for case in before], [case['id'] for case in after])
        self.assertEqual(['pass', 'assertion'], [case['expected'] for case in before])
        self.assertEqual(['pass', 'pass'], [case['expected'] for case in after])

    def test_authority_evidence_absent(self):
        for value in load_inputs():
            self.assertIs(value['execution_eligible'], False)
            self.assertEqual({'authenticated_approval': None, 'independent_review': None,
                              'live_observation': None, 'observed_raw_exit': None,
                              'guard_capabilities': []}, value['evidence'])

    def test_prerequisites_are_unmet_data(self):
        for value in load_inputs():
            self.assertTrue(value['prerequisites'])
            for prerequisite in value['prerequisites']:
                self.assertEqual({'id', 'status', 'evidence'}, set(prerequisite))
                self.assertTrue(prerequisite['id'].startswith('fixture:'))
                self.assertIn(prerequisite['status'], ('unmet', 'unavailable'))
                self.assertIsNone(prerequisite['evidence'])

    def test_scope_is_synthetic(self):
        for value in load_inputs():
            self.assertTrue(value['scope'])
            for name in value['scope']:
                path = PurePosixPath(name)
                self.assertEqual('synthetic', path.parts[0])
                self.assertFalse(path.is_absolute())
                self.assertNotIn('..', path.parts)
                self.assertEqual(str(path), name)
                self.assertNotIn('specs/', name)

    def test_readme_records_source_inventory(self):
        readme = (FIXTURES / 'README.md').read_text(encoding='utf-8')
        for token in (BASE, *BLOBS.keys(), *BLOBS.values(),
                      '8f4debe5cd2ee2770f5963d292a9eb60af6270d7',
                      '20c17b3ac90a232643d7c3998498465a7008318a',
                      '8300295e47dc4b5b86da7cee7f0da99f8cc3bb70'):
            self.assertIn(token, readme)
        for name in RETAINED:
            self.assertIn('test_single_task_state.SingleTaskStateTests.' + name, readme)
        for module, count in (('test_harness', 5), ('test_single_task', 22),
                              ('test_single_task_state', 10), ('test_symphony', 4),
                              ('test_approval_binding', 25), ('test_check_ci', 29),
                              ('test_prepare_publication', 14),
                              ('verify_t003_state.StatePrimitiveTests', 12),
                              ('verify_t004_cli.CliBoundaryTests', 12)):
            self.assertIn(f'`{module}` | {count}', readme)
        self.assertIn('133 = 128 pass + five retained failures', readme)
        self.assertIn('41 = 36 pass + five failures', readme)

    def test_loading_is_read_only(self):
        with tempfile.TemporaryDirectory() as directory:
            repository = Path(directory)
            inputs = repository / 'fixtures'
            shutil.copytree(FIXTURES, inputs)
            (repository / 'unrelated.txt').write_text('preserve me', encoding='utf-8')
            def snapshot():
                return {str(path.relative_to(repository)):
                        None if path.is_dir() else path.read_bytes()
                        for path in repository.rglob('*')}
            before = snapshot()
            self.assertEqual(load_inputs(), load_inputs(inputs))
            self.assertEqual(before, snapshot())

    def test_loading_never_runs_prerequisites(self):
        with ExitStack() as stack:
            sentinels = [stack.enter_context(patch.object(module, name,
                         side_effect=AssertionError('process launch forbidden')))
                         for module, name in ((subprocess, 'Popen'), (os, 'system'),
                                              (os, 'fork'), (os, 'posix_spawn'),
                                              (os, 'posix_spawnp'))]
            self.assertEqual(3, len(load_inputs()))
            for sentinel in sentinels:
                sentinel.assert_not_called()

    def test_loading_never_uses_network(self):
        with ExitStack() as stack:
            sentinels = [stack.enter_context(patch.object(module, name,
                         side_effect=AssertionError('network forbidden')))
                         for module, name in ((socket, 'create_connection'),
                                              (socket, 'getaddrinfo'),
                                              (socket.socket, 'connect'),
                                              (socket.socket, 'connect_ex'))]
            self.assertEqual(3, len(load_inputs()))
            for sentinel in sentinels:
                sentinel.assert_not_called()


if __name__ == '__main__':
    unittest.main()
