"""Independent failure-path controls for frozen selective migration vectors."""
# SPDX-License-Identifier: EUPL-1.2
import copy
import hashlib
import importlib.util
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]


class GoVectorTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('go_vectors', ROOT/'scripts/generate_go_vectors.py')
        self.tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.tool)
        self.manifest, self.schema, self.vectors = self.tool.load_contract(ROOT)

    def copied(self, parent):
        root = Path(parent)/'source'
        shutil.copytree(ROOT/'contracts/migration', root/'contracts/migration')
        (root/'scripts').mkdir()
        shutil.copyfile(ROOT/'scripts/generate_go_vectors.py', root/'scripts/generate_go_vectors.py')
        return root

    def case(self, identity):
        return next(case for case in self.vectors['cases'] if case['id'] == identity)

    def test_all_frozen_cases_reproduce_byte_for_byte_without_writes(self):
        paths = [ROOT/'scripts/generate_go_vectors.py', *(ROOT/'contracts/migration').rglob('*')]
        before = {str(p): p.read_bytes() for p in paths if p.is_file()}
        self.assertGreaterEqual(self.tool.verify(ROOT), 80)
        self.assertEqual(before, {name: Path(name).read_bytes() for name in before})

    def test_hand_inspected_unicode_html_boolean_and_zero_anchors(self):
        anchors = {
            'json.negative-zero': ('30', '5feceb66ffc86f38d952786c6d696c79c2dbc239dd4e91b46729d73a27fb57e9'),
            'json.supplementary-order': ('7b22ee8080223a312c22f0908080223a327d', '8706b5798a29d65739b3d1bbb1d009f87d005c71c8130c5353c4990c15ddfec8'),
            'json.line-separators': ('22e280a8e280a922', 'e3a0e2262ac7790f4df36b522a8fbdd4ee3370d568eb15dc25c5f1775f14ed6c'),
            'json.html': ('223c3e262f22', '396adbc953af00557241e2c859a735e8a11b03279d475359cf45dffed8746492'),
            'json.boolean-integer': ('7b2262223a747275652c2269223a317d', '1dc08b312fcdfc60ec48be27868b868ea4846bcb42e831b21f5db31162f31c63'),
        }
        for identity, (raw_hex, digest) in anchors.items():
            with self.subTest(identity=identity):
                expected = self.case(identity)['source_expected']
                self.assertEqual(bytes.fromhex(raw_hex), self.tool.expand_bytes(expected['canonical']))
                self.assertEqual(digest, expected['digest'])
                self.assertEqual(expected, self.case(identity)['product_expected'])

    def test_source_and_product_domains_remain_distinct(self):
        identities = ['json.depth-129', 'canonical.depth-129', 'digest.depth-129',
                      'path.surrogate', 'path.dot-integer', 'path.normal-dot-integer']
        for identity in identities:
            with self.subTest(identity=identity):
                case = self.case(identity)
                self.assertEqual('ok', case['source_expected']['status'])
                self.assertEqual('reject', case['product_expected']['status'])
                self.assertNotEqual('parity', case['decision'])
        self.assertEqual('5155e7af4f6c5b0853aa65153bf170836c773666b83ae909760596f07154c950',
                         self.case('json.digits-4300')['source_expected']['digest'])
        for identity in ('json.depth-128', 'canonical.depth-128', 'digest.depth-128'):
            self.assertEqual('ok', self.case(identity)['product_expected']['status'])

    def test_bigints_reach_canonical_and_digest_oracles_before_rejection(self):
        with self.tool.oracle_environment():
            oracles = self.tool.load_oracles(ROOT, self.manifest)
            for operation in ('canonical-bytes', 'canonical-digest'):
                for sign in ('positive', 'negative'):
                    case = self.case(('canonical' if operation == 'canonical-bytes' else 'digest') + '.4301-' + sign)
                    name = operation.replace('-', '_')
                    original = oracles['json'][name]
                    seen = []
                    def observe(value):
                        seen.append(value)
                        return original(value)
                    with mock.patch.dict(oracles['json'], {name: observe}):
                        self.assertEqual({'status': 'reject', 'code': 'invalid-json'}, self.tool.evaluate(case, oracles))
                    self.assertEqual(1, len(seen))
                    self.assertIs(type(seen[0]), int)
                    self.assertEqual(10**4300, abs(seen[0]))
            for identity in ('canonical.4300-positive', 'canonical.4300-negative',
                             'digest.4300-positive', 'digest.4300-negative'):
                self.assertEqual('ok', self.tool.evaluate(self.case(identity), oracles)['status'])

    def test_byte_size_precedence_and_canonicalization_without_byte_cap(self):
        self.assertEqual('ok', self.case('json.bytes-max')['product_expected']['status'])
        self.assertEqual({'status': 'reject', 'code': 'input-limit-exceeded'},
                         self.case('json.bytes-over-malformed')['source_expected'])
        expected = self.case('canonical.no-byte-cap')['product_expected']
        self.assertEqual('ok', expected['status'])
        self.assertGreater(expected['canonical']['length'], 1024*1024)

    def test_schema_rejects_unknown_fields_types_options_ids_and_operations(self):
        variants = []
        value = copy.deepcopy(self.vectors); value['unknown'] = True; variants.append(value)
        value = copy.deepcopy(self.vectors); value['schema_version'] = True; variants.append(value)
        value = copy.deepcopy(self.vectors); value['cases'][0]['operation'] = 'exec'; variants.append(value)
        value = copy.deepcopy(self.vectors); value['cases'][0]['input']['unknown'] = 'secret'; variants.append(value)
        value = copy.deepcopy(self.vectors); value['cases'][0]['source_expected']['status'] = 'maybe'; variants.append(value)
        for value in variants:
            with self.subTest(value=str(value)[:80]), self.assertRaises(self.tool.ContractError):
                self.tool.validate(value, self.schema)
        value = copy.deepcopy(self.vectors); value['cases'].append(copy.deepcopy(value['cases'][0]))
        with self.assertRaises(self.tool.ContractError): self.tool.validate_vectors(value, self.schema)
        value = copy.deepcopy(self.vectors); value['cases'][0]['dot'] = {'kind': 'boolean', 'value': True}
        with self.assertRaises(self.tool.ContractError): self.tool.validate_vectors(value, self.schema)

    def test_strict_fixture_loader_rejects_duplicate_keys_constants_utf8_and_size(self):
        for raw in (b'{"x":1,"x":2}', b'{"x":NaN}', b'"\xff"', b'[]'*5000000):
            with self.subTest(length=len(raw)), self.assertRaises(self.tool.ContractError):
                self.tool.strict_json(raw)

    def test_compact_inputs_check_length_hash_and_expansion_bounds(self):
        spec = self.tool.encode_bytes(b'0' + b' '*1048575)
        self.assertLess(len(json.dumps(spec)), 500)
        self.assertEqual(1048576, len(self.tool.expand_bytes(spec)))
        for field, bad in [('length', 3), ('sha256', '0'*64), ('count', 10**12)]:
            value = dict(spec); value[field] = bad
            with self.subTest(field=field), self.assertRaises(self.tool.ContractError): self.tool.expand_bytes(value)
        with self.assertRaises(self.tool.ContractError): self.tool.materialize({'kind': 'power10', 'exponent': 10**9, 'negative': False})
        with self.assertRaises(self.tool.ContractError): self.tool.materialize({'kind': 'string', 'codepoints': [0x110000]})
        with self.assertRaises(self.tool.ContractError): self.tool.materialize({'kind': 'integer', 'decimal': 'secret-canary'})
        with self.assertRaises(self.tool.ContractError):
            self.tool.materialize({'kind': 'list', 'items': [{'kind': 'repeat-string', 'codepoint': 120, 'count': 1048577}]*3})

    def test_native_construction_failure_is_not_attributed_to_source(self):
        case = copy.deepcopy(self.case('canonical.4301-positive'))
        case['input'] = {'kind': 'power10', 'exponent': 10**9, 'negative': False}
        with self.tool.oracle_environment():
            oracles = self.tool.load_oracles(ROOT, self.manifest)
            with mock.patch.dict(oracles['json'], {'canonical_bytes': mock.Mock()}) as unused:
                with self.assertRaises(self.tool.ContractError): self.tool.evaluate(case, oracles)
                oracles['json']['canonical_bytes'].assert_not_called()

    def test_snapshot_manifest_tool_and_schema_tampering_are_rejected(self):
        for relative in ('contracts/migration/oracle/provider_profiles.py',
                         'contracts/migration/oracle/single_task_context.py',
                         'scripts/generate_go_vectors.py', 'contracts/migration/vectors.schema.json'):
            with self.subTest(path=relative), tempfile.TemporaryDirectory() as parent:
                root = self.copied(parent); path = root/relative
                path.write_bytes(path.read_bytes() + b' ')
                with self.assertRaises(self.tool.ContractError): self.tool.verify(root)
        with tempfile.TemporaryDirectory() as parent:
            root = self.copied(parent); path = root/'contracts/migration/manifest.json'
            manifest = json.loads(path.read_text()); manifest['sources'][0]['sha256'] = '0'*64
            path.write_text(json.dumps(manifest))
            with self.assertRaises(self.tool.ContractError): self.tool.verify(root)

    def test_oracle_namespace_contains_only_selected_behavior(self):
        oracles = self.tool.load_oracles(ROOT, self.manifest)
        self.assertNotIn('require_profiles', oracles['json'])
        self.assertNotIn('read_artifact', oracles['json'])
        self.assertNotIn('validate_context', oracles['path'])
        self.assertNotIn('reviewed_bytes', oracles['path'])
        self.assertNotIn('os', oracles['json'])
        self.assertNotIn('Path', oracles['path'])

    def test_default_cli_is_offline_read_only_and_reports_runtime_evidence(self):
        script = ROOT/'scripts/generate_go_vectors.py'
        result = subprocess.run([sys.executable, '-I', '-B', str(script)], capture_output=True, check=False)
        self.assertEqual(0, result.returncode, result.stderr.decode())
        evidence = json.loads(result.stdout)
        self.assertGreaterEqual(evidence['verified_cases'], 80)
        self.assertRegex(evidence['runtime']['executable_sha256'], '^[0-9a-f]{64}$')
        self.assertNotIn('executable_sha256', self.manifest['runtime'])
        result = subprocess.run([sys.executable, '-B', str(script)], capture_output=True, check=False)
        self.assertNotEqual(0, result.returncode)
        self.assertIn(b'-I -B', result.stderr)

    def test_candidate_generation_never_overwrites_accepted_or_existing_files(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.copied(parent)
            accepted = root/'contracts/migration/vectors.json'; before = accepted.read_bytes()
            with self.assertRaises(self.tool.ContractError): self.tool.write_candidate(root, accepted)
            alias = Path(parent)/'alias'; alias.symlink_to(root, target_is_directory=True)
            with self.assertRaises(self.tool.ContractError): self.tool.write_candidate(root, alias/'contracts/migration/vectors.json')
            candidate = Path(parent)/'candidate.json'
            self.tool.write_candidate(root, candidate)
            self.assertEqual(before, candidate.read_bytes())
            with self.assertRaises(self.tool.ContractError): self.tool.write_candidate(root, candidate)
            self.assertEqual(before, accepted.read_bytes())

    def test_changed_expected_bytes_fail_verification_and_only_external_candidate_repairs(self):
        with tempfile.TemporaryDirectory() as parent:
            root = self.copied(parent); accepted = root/'contracts/migration/vectors.json'
            value = json.loads(accepted.read_text()); value['cases'][0]['source_expected']['digest'] = '0'*64; value['cases'][0]['product_expected']['digest'] = '0'*64
            accepted.write_bytes(self.tool.render(value)); changed = accepted.read_bytes()
            with self.assertRaises(self.tool.ContractError): self.tool.verify(root)
            candidate = Path(parent)/'candidate.json'; self.tool.write_candidate(root, candidate)
            self.assertNotEqual(changed, candidate.read_bytes())
            self.assertEqual(changed, accepted.read_bytes())

    def test_runtime_limits_are_explicit_and_restored(self):
        digits, recursion = sys.get_int_max_str_digits(), sys.getrecursionlimit()
        try:
            sys.set_int_max_str_digits(0); sys.setrecursionlimit(1500)
            with self.tool.oracle_environment():
                self.assertEqual(4300, sys.get_int_max_str_digits())
                self.assertEqual(1000, sys.getrecursionlimit())
            self.assertEqual(0, sys.get_int_max_str_digits())
            self.assertEqual(1500, sys.getrecursionlimit())
        finally:
            sys.set_int_max_str_digits(digits); sys.setrecursionlimit(recursion)
        with mock.patch.object(self.tool.sys, 'version_info', (3, 13, 0)):
            with self.assertRaises(self.tool.ContractError):
                with self.tool.oracle_environment(): pass


if __name__ == '__main__':
    unittest.main()
