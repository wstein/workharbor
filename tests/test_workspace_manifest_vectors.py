import importlib.util
import copy
import json
import os
import shutil
import subprocess
import tempfile
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
        self.assertEqual(checks.MIN_VECTOR_CASES, len(cases))
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
        self.assertEqual({65536, 65537}, sizes)
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

    def test_old_workspace_error_messages_are_unchanged(self):
        expected = {
            'require schema_version 1': ('workspace example: require schema_version 1', [dict(BASE, schema_version=3), [1]]),
            'fields': ('workspace example: unexpected or missing fields; update the reviewed contract',
                       [dict(BASE, extra=1), {k: v for k, v in BASE.items() if k != 'bundles'}]),
            'path': ('workspace example: unsafe projects_path; use a relative path without traversal',
                     [dict(BASE, projects_path='../x'), dict(BASE, projects_path=3)]),
            'bundles': ('workspace example: attachments are deferred; retain the empty bundle list',
                        [dict(BASE, bundles=[1]), dict(BASE, bundles='x')]),
        }
        for name, (message, documents) in expected.items():
            for document in documents:
                with self.subTest(name=name, document=document):
                    self.assertEqual([message], checks.workspace_errors(document))

    def make_root(self, files):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        # Isolated from the ambient Git configuration and HOME of whoever runs the tests.
        home = root.parent / (root.name + '-home')
        home.mkdir()
        self.addCleanup(shutil.rmtree, home, True)
        self.git_env = dict(os.environ, HOME=str(home), GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM='1')
        subprocess.run(['git', 'init', str(root)], check=True, capture_output=True, env=self.git_env)
        for name, content in files.items():
            (root / name).parent.mkdir(parents=True, exist_ok=True)
            (root / name).write_bytes(content)
        subprocess.run(['git', 'add', '.'], cwd=root, check=True, capture_output=True, env=self.git_env)
        return root

    def test_check_survives_depth_bomb_example_and_reports_once(self):
        deep = b'[' * 100000 + b']' * 100000
        root = self.make_root({checks.WORKSPACE_MANIFEST: deep})
        errors = [e for e in checks.check(root) if 'workharbor.json' in e and 'required artifact' not in e]
        self.assertEqual(1, len(errors), errors)

    def test_check_validates_nested_example_manifests(self):
        bad = raw(dict(BASE, projects_path='../x'))
        root = self.make_root({checks.WORKSPACE_MANIFEST: raw(BASE),
                               'examples/workspace/admitted-image/workharbor.json': bad,
                               'examples/workspace/admitted-image/.devcontainer/devcontainer.json': b'{"image":"x"}'})
        errors = checks.check(root)
        self.assertTrue(any('admitted-image/workharbor.json' in e for e in errors), errors)
        self.assertFalse(any('devcontainer.json' in e for e in errors), errors)

    def test_check_reports_a_symlinked_example_manifest(self):
        root = self.make_root({checks.WORKSPACE_MANIFEST: raw(BASE),
                               'examples/workspace/real/workharbor.json': raw(BASE)})
        link = root / 'examples/workspace/linked/workharbor.json'
        link.parent.mkdir(parents=True)
        link.symlink_to('../real/workharbor.json')
        errors = checks.check(root)
        self.assertTrue(any('linked/workharbor.json' in e and 'symbolic link' in e for e in errors), errors)
        dangling = root / 'examples/workspace/dangling/workharbor.json'
        dangling.parent.mkdir(parents=True)
        dangling.symlink_to('nowhere.json')
        self.assertTrue(any('dangling/workharbor.json' in e and 'symbolic link' in e for e in checks.check(root)))

    def test_check_reports_a_symlinked_example_directory(self):
        root = self.make_root({checks.WORKSPACE_MANIFEST: raw(BASE),
                               'elsewhere/workharbor.json': raw(dict(BASE, projects_path='../x'))})
        (root / 'examples/workspace/dir').symlink_to(root / 'elsewhere')
        errors = checks.check(root)
        self.assertTrue(any('examples/workspace/dir' in e and 'symbolic link' in e for e in errors), errors)

    def test_examples_stay_on_the_schema_version_the_resolver_accepts(self):
        v2 = raw(dict(BASE, schema_version=2))
        root = self.make_root({checks.WORKSPACE_MANIFEST: raw(BASE),
                               'examples/workspace/v2/workharbor.json': v2})
        errors = checks.check(root)
        self.assertTrue(any('v2/workharbor.json' in e and 'schema_version 1' in e for e in errors), errors)
        self.assertEqual([], checks.manifest_codes(v2))
        root = self.make_root({checks.WORKSPACE_MANIFEST: v2})
        self.assertTrue(any('schema_version 1' in e for e in checks.check(root)))

    def test_check_runs_vectors_and_requires_them(self):
        root = self.make_root({checks.WORKSPACE_MANIFEST: raw(BASE)})
        self.assertTrue(any('vectors.json' in e for e in checks.check(root)))
        original = checks.vector_errors
        checks.vector_errors = lambda _root: ['sentinel vector error']
        try:
            self.assertIn('sentinel vector error', checks.check(root))
        finally:
            checks.vector_errors = original

    def test_vectors_fewer_than_pinned_count_fail(self):
        cases = self.cases()
        root = self.make_root({'contracts/workspace/vectors.json': json.dumps({'cases': cases[:5]}).encode()})
        self.assertTrue(any('fewer' in e for e in checks.vector_errors(root)))
        root = self.make_root({'contracts/workspace/vectors.json': json.dumps({'cases': cases}).encode()})
        self.assertEqual([], checks.vector_errors(root))

    def test_duplicate_vector_ids_rejected(self):
        case = {'id': 'x', 'document': BASE, 'expect_codes': []}
        self.assertTrue(any('duplicate' in e for e in checks.vector_case_errors([case, case])))

    def test_check_follows_the_schema_file(self):
        schema = json.loads(SCHEMA.read_text(encoding='utf-8'))

        def codes(mutate, document):
            changed = copy.deepcopy(schema)
            mutate(changed)
            return checks.manifest_codes(raw(document), changed)

        v2 = dict(BASE, schema_version=2)
        # const of a version
        self.assertEqual([], codes(lambda s: None, v2))
        self.assertEqual(['manifest.schema'], codes(lambda s: s['$defs']['v2']['properties']['schema_version'].update(const=3), v2))
        # required list
        self.assertEqual([], codes(lambda s: s['$defs']['v1'].update(required=['schema_version']),
                                   {'schema_version': 1}))
        self.assertEqual(['manifest.schema'], codes(lambda s: s['$defs']['v1']['required'].append('name'), BASE))
        # additionalProperties
        self.assertEqual([], codes(lambda s: s['$defs']['v1'].update(additionalProperties=True), dict(BASE, extra=1)))
        # maxItems of bundles
        self.assertEqual([], codes(lambda s: s['$defs']['v1']['properties']['bundles'].update(maxItems=1),
                                   dict(BASE, bundles=[1])))
        # enum and name pattern
        self.assertEqual([], codes(lambda s: s['$defs']['v2']['properties']['human_shell']['enum'].append('tcsh'),
                                   dict(v2, human_shell='tcsh')))
        self.assertEqual([], codes(lambda s: s['$defs']['v2']['properties']['name'].update(pattern='^X$'), dict(v2, name='X')))
        # oneOf: removing version 2 makes it unknown
        self.assertEqual(['manifest.schema'], codes(lambda s: s['oneOf'].pop(), v2))

    def test_catalogue_order_of_codes(self):
        document = dict(BASE, schema_version=2, devcontainer_path='/abs', bundles=[1], name='My')
        self.assertEqual(['manifest.schema', 'manifest.path_invalid', 'manifest.bundles_deferred'],
                         checks.manifest_codes(raw(document)))


if __name__ == '__main__':
    unittest.main()
