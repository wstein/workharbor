import importlib.util
import json
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('checks', ROOT / 'scripts/check_repo.py')
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)

VECTORS = ROOT / 'contracts/workspace/vectors.json'
SCHEMA = ROOT / 'contracts/workspace/workharbor.schema.json'
BASE = {'schema_version': 1, 'devcontainer_path': '.devcontainer/devcontainer.json',
        'projects_path': 'projects', 'bundles': []}
V2_FIELDS = {'name': 'my-ws', 'start_dir': '.', 'human_shell': 'fish', 'console_mode': 'herdr',
             'shortcuts': 'on', 'project': {'url': 'https://f.example/o/a.git', 'path': 'projects/a'}}


def raw(document):
    return json.dumps(document).encode()


class WorkspaceManifestVectorTests(unittest.TestCase):
    def cases(self):
        return json.loads(VECTORS.read_text(encoding='utf-8'))['cases']

    def test_workspace_manifest_vectors(self):
        cases = self.cases()
        self.assertGreaterEqual(len(cases), 70)
        ids = [case['id'] for case in cases]
        self.assertEqual(len(ids), len(set(ids)))
        for case in cases:
            with self.subTest(case=case['id']):
                self.assertEqual(case['expect_codes'], checks.manifest_codes(checks.vector_bytes(case)))

    def test_vector_kinds_cover_raw_byte_and_size_cases(self):
        kinds = {key for case in self.cases() for key in case if key in
                 ('document', 'bytes_hex', 'raw_text', 'generate_bytes')}
        self.assertEqual({'document', 'bytes_hex', 'raw_text', 'generate_bytes'}, kinds)
        sizes = {len(checks.vector_bytes(c)) for c in self.cases() if 'generate_bytes' in c}
        self.assertEqual({65537}, sizes)
        self.assertEqual([], checks.manifest_codes(b' ' * 0 + raw(BASE) + b' ' * (65536 - len(raw(BASE)))))

    def test_schema_version_is_type_exact(self):
        for text in ('true', '1.0', '1e0', '1E0', '"1"', 'null', '[1]', '2.0', '1.5', '3', '0', '-1', '01'):
            with self.subTest(version=text):
                body = '{"schema_version":%s,"devcontainer_path":"d","projects_path":"p","bundles":[]}' % text
                self.assertNotEqual([], checks.manifest_codes(body.encode()))
        self.assertEqual(['manifest.schema'], checks.manifest_codes(raw(dict(BASE, schema_version=True))))

    def test_example_manifests_still_valid(self):
        examples = sorted((ROOT / 'examples/workspace').glob('*.json'))
        self.assertTrue(examples)
        for path in examples:
            with self.subTest(path=path.name):
                self.assertEqual([], checks.manifest_codes(path.read_bytes()))
        self.assertEqual([], checks.check(ROOT))

    def test_version1_rejects_version2_fields(self):
        for field, value in V2_FIELDS.items():
            with self.subTest(field=field):
                self.assertEqual(['manifest.schema'], checks.manifest_codes(raw(dict(BASE, **{field: value}))))
                self.assertEqual([], checks.manifest_codes(raw(dict(BASE, schema_version=2, **{field: value}))))

    def test_version2_defaults_documented(self):
        properties = json.loads(SCHEMA.read_text(encoding='utf-8'))['$defs']['v2']['properties']
        self.assertEqual({'start_dir': '.', 'human_shell': 'fish', 'console_mode': 'herdr', 'shortcuts': 'on'},
                         {name: properties[name].get('default') for name in
                          ('start_dir', 'human_shell', 'console_mode', 'shortcuts')})
        for name in ('name', 'project'):
            self.assertNotIn('default', properties[name])
        self.assertEqual([], checks.manifest_codes(raw(dict(BASE, schema_version=2))))

    def test_version2_has_no_resolver_or_agent_shell_field(self):
        properties = json.loads(SCHEMA.read_text(encoding='utf-8'))['$defs']['v2']['properties']
        self.assertFalse({'agent_shell', 'shell', 'bundles_attach'} & set(properties))
        self.assertEqual(['manifest.schema'], checks.manifest_codes(raw(dict(BASE, schema_version=2, agent_shell='bash'))))

    def test_repository_check_runs_vectors_and_reports_disagreement(self):
        self.assertEqual([], checks.vector_errors(ROOT))
        bad = {'id': 'x', 'document': BASE, 'expect_codes': ['manifest.schema']}
        self.assertTrue(checks.vector_case_errors([bad]))

    def test_hostile_bytes(self):
        deep = b'[' * 129 + b']' * 129
        self.assertEqual(['manifest.syntax'], checks.manifest_codes(deep))
        self.assertEqual(['manifest.syntax'], checks.manifest_codes(b'[' * 5000 + b']' * 5000))
        self.assertEqual(['manifest.syntax'], checks.manifest_codes(b'{"schema_version":NaN}'))
        self.assertEqual(['manifest.duplicate_key'], checks.manifest_codes(b'{"a":{"b":1,"b":2}}'))
        self.assertEqual(['manifest.encoding'], checks.manifest_codes(b'\xef\xbb\xbf{}'))


if __name__ == '__main__':
    unittest.main()
