"""Offline T007 CLI; fixtures and local Git only, no execution permission."""
import contextlib
import copy
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import unittest
from unittest import mock

import task_automation as automation
import task_outcomes as outcomes
import task_package as package
import test_task_package as package_tests


class OfflineAutomationTests(unittest.TestCase):
    git = package_tests.RepositoryPrimitiveTests.git

    def setUp(self):
        package_tests.PackageContractTests.setUp(self)
        self.git('remote', 'add', 'origin', 'https://github.com/fixture/offline.git')
        self.input['outcomes'][0]['tool_revision'] = outcomes.tool_revision()
        self.input['outcomes'][0]['input_revision'] = outcomes.snapshot(self.root)
        self.save()

    def save(self, value=None, name='input.json'):
        (self.root / name).write_bytes(package.canonical_json(self.input if value is None else value))
        return name

    def invoke(self, *argv):
        output, errors = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
            code = automation.main(list(argv), root=self.root, repository='fixture/offline')
        self.assertEqual('', errors.getvalue())
        return code, json.loads(output.getvalue())

    def prepare(self):
        return self.invoke('prepare', '--input', 'input.json', '--output', 'output')

    def artifact(self, result):
        return 'output/' + result['artifacts']['package']

    def test_prepare_is_deterministic_and_reuses_exact_bytes(self):
        code, first = self.prepare()
        self.assertEqual(0, code)
        path = self.root / self.artifact(first)
        before = (path.read_bytes(), path.stat().st_mtime_ns)
        code, second = self.prepare()
        self.assertEqual(0, code)
        self.assertFalse(first['reused'])
        self.assertTrue(second['reused'])
        self.assertEqual(first['package_digest'], second['package_digest'])
        self.assertEqual(before, (path.read_bytes(), path.stat().st_mtime_ns))
        self.assertEqual(1, len(list((self.root / 'output').iterdir())))

    def test_approval_reference_change_creates_new_serialization_without_overwrite(self):
        _, first = self.prepare()
        original = (self.root / self.artifact(first)).read_bytes()
        self.input['approval_refs']['spec'] = 'sha256:' + 'a' * 64
        self.save()
        code, second = self.prepare()
        self.assertEqual(0, code)
        self.assertEqual(first['package_digest'], second['package_digest'])
        self.assertNotEqual(first['revision_digest'], second['revision_digest'])
        self.assertEqual(original, (self.root / self.artifact(first)).read_bytes())
        self.assertEqual(2, len(list((self.root / 'output').iterdir())))

    def test_changed_immutable_input_creates_new_package(self):
        _, first = self.prepare()
        self.input['task_id'] = 'fixture:other'
        self.save()
        code, second = self.prepare()
        self.assertEqual(0, code)
        self.assertNotEqual(first['package_digest'], second['package_digest'])

    def test_validate_offline_reports_blockers_without_eligibility(self):
        _, first = self.prepare()
        code, result = self.invoke('validate', '--package', self.artifact(first), '--offline')
        self.assertEqual(3, code)
        self.assertTrue(result['structural_valid'])
        self.assertFalse(result['execution_eligible'])
        self.assertIn('guard-unavailable', {item['code'] for item in result['blockers']})
        for field in ('authenticated_approval', 'independent_review', 'live_execution'):
            self.assertIsNone(result[field])
        self.assertEqual([], result['raw_exits'])

    def test_effective_preview_has_expected_identities_and_missing_capabilities(self):
        _, first = self.prepare()
        code, result = self.invoke('effective', '--package', self.artifact(first), '--offline')
        self.assertEqual(0, code)
        self.assertEqual('previewed', result['progress'])
        self.assertFalse(result['execution_eligible'])
        self.assertEqual('implement', result['requested_mode'])
        self.assertEqual('implement', result['effective_mode'])
        self.assertEqual(self.input['source'], result['effective']['source'])
        self.assertIsNone(result['effective']['execution']['build_provenance'])
        self.assertFalse(result['capabilities']['capabilities_satisfied'])
        self.assertEqual(3, result['transport_retry_limit'])
        self.assertEqual(0, result['transport_attempts'])

    def test_three_fixture_mvp_uses_same_prepare_validate_effective_flow(self):
        fixture_root = Path(__file__).resolve().parents[1] / 'tests/fixtures/task-automation'
        for kind in ('documentation', 'test-authoring', 'implementation'):
            with self.subTest(kind=kind):
                fixture = json.loads((fixture_root / (kind + '.json')).read_text())
                value = copy.deepcopy(self.input)
                value['task_id'] = fixture['task_id']
                value['context'] = {'fixture_input': fixture}
                check = value['outcomes'][0]
                check['raw_exit'] = fixture['case_manifest']['expected_raw_exit']
                check['cases'] = [dict(id=case['id'], expected=case['expected'], fingerprint=
                    None if case['assertion'] is None else outcomes.assertion_fingerprint(
                        case['id'], case['assertion']['message']))
                    for case in fixture['case_manifest']['cases']]
                self.save(value)
                code, prepared = self.prepare()
                self.assertEqual(0, code)
                self.assertEqual([], prepared['raw_exits'])
                self.assertEqual(3, self.invoke('validate', '--package', self.artifact(prepared), '--offline')[0])
                self.assertEqual(0, self.invoke('effective', '--package', self.artifact(prepared), '--offline')[0])
                sealed = json.loads((self.root / self.artifact(prepared)).read_bytes())
                self.assertEqual(fixture, sealed['context']['fixture_input'])
                self.assertIsNone(sealed['approval_refs']['matrix'])

    def test_all_offline_commands_deny_network_execution_and_live_ports(self):
        original = package.subprocess.run
        def local_git_only(argv, **kwargs):
            self.assertEqual('git', argv[0])
            self.assertIn('--no-lazy-fetch', argv)
            self.assertFalse({'fetch', 'push', 'clone', 'checkout', 'commit'} & set(argv))
            return original(argv, **kwargs)
        with contextlib.ExitStack() as stack:
            stack.enter_context(mock.patch.object(package.subprocess, 'run', side_effect=local_git_only))
            denied = [stack.enter_context(mock.patch.object(module, name,
                side_effect=AssertionError('forbidden offline operation')))
                for module, name in ((socket, 'create_connection'), (socket, 'getaddrinfo'),
                                     (socket.socket, 'connect'), (os, 'system'),
                                     (outcomes, 'run_checks'), (outcomes, '_execute'),
                                     (package.UnavailableTracker, 'read_approval'),
                                     (package.UnavailableTracker, 'read_issue'),
                                     (package.UnavailableRuntime, 'observe'))]
            code, first = self.prepare()
            self.assertEqual(0, code)
            self.invoke('validate', '--package', self.artifact(first), '--offline')
            self.invoke('effective', '--package', self.artifact(first), '--offline')
            for sentinel in denied:
                sentinel.assert_not_called()
        self.assertFalse((self.root / '.symphony').exists())

    def test_isolated_subprocess_cli_uses_local_trusted_repository_identity(self):
        command = [sys.executable, '-I', '-B', str(Path(automation.__file__)),
                   'prepare', '--input', 'input.json', '--output', 'output']
        process = subprocess.run(command, cwd=self.root, capture_output=True)
        self.assertEqual(0, process.returncode, process.stderr)
        self.assertEqual(b'', process.stderr)
        self.assertFalse(json.loads(process.stdout)['execution_eligible'])

    def test_repository_mismatch_blocks_before_output(self):
        self.input['repository'] = 'other/repository'
        self.save()
        code, result = self.prepare()
        self.assertEqual(3, code)
        self.assertIn('repository-mismatch', {item['code'] for item in result['blockers']})
        self.assertFalse((self.root / 'output').exists())

    def test_unavailable_source_blocks_before_output(self):
        self.input['source']['accepted_commit'] = 'f' * 40
        self.save()
        code, _ = self.prepare()
        self.assertEqual(3, code)
        self.assertFalse((self.root / 'output').exists())

    def test_invalid_arguments_never_echo_input(self):
        for argv in ((), ('ready', '--token', 'secret-canary'),
                     ('validate', '--package', 'secret-canary'),
                     ('effective', '--package', 'input.json', '--offline', '--live')):
            with self.subTest(argv=argv):
                code, result = self.invoke(*argv)
                self.assertEqual(2, code)
                self.assertNotIn('secret-canary', json.dumps(result))

    def test_malformed_duplicate_json_and_unknown_fields_fail_safely(self):
        for data in (b'{secret-canary}', b'{"schema_version":1,"schema_version":1}',
                     package.canonical_json(dict(self.input, secret_canary='do-not-echo'))):
            (self.root / 'input.json').write_bytes(data)
            code, result = self.prepare()
            self.assertEqual(2, code)
            self.assertNotIn('canary', json.dumps(result))
            self.assertFalse((self.root / 'output').exists())

    def test_input_limit_blocks_without_truncation(self):
        (self.root / 'input.json').write_bytes(b' ' * (1024 * 1024) + b'{}')
        self.assertEqual(2, self.prepare()[0])
        self.assertFalse((self.root / 'output').exists())

    def test_check_case_and_deadline_bounds_never_execute(self):
        variants = []
        checks = [dict(copy.deepcopy(self.input['outcomes'][0]), id=f'check-{i}') for i in range(101)]
        variants.append(checks)
        large = copy.deepcopy(self.input['outcomes'])
        large[0]['cases'] = [dict(id=f'case-{i}', expected='pass', fingerprint=None) for i in range(10001)]
        variants.append(large)
        for deadline in (0, -1, 301):
            checks = copy.deepcopy(self.input['outcomes'])
            checks[0]['deadline_seconds'] = deadline
            variants.append(checks)
        with mock.patch.object(outcomes, 'run_checks', side_effect=AssertionError('never run')) as run:
            for checks in variants:
                self.save(dict(self.input, outcomes=checks))
                self.assertEqual(4, self.prepare()[0])
            run.assert_not_called()
        self.assertFalse((self.root / 'output').exists())

    def test_rejects_unreviewed_interpreter_or_check_argv(self):
        for argv in (['sh', '-c', 'secret-canary'], [sys.executable, '-m', 'unittest']):
            value = copy.deepcopy(self.input)
            value['outcomes'][0]['argv'] = argv
            self.save(value)
            code, result = self.prepare()
            self.assertEqual(4, code)
            self.assertNotIn('secret-canary', json.dumps(result))

    def test_input_escape_symlink_hardlink_and_output_escape_block(self):
        (self.root / 'link.json').symlink_to(self.root / 'input.json')
        os.link(self.root / 'input.json', self.root / 'hard.json')
        for name in ('../input.json', str(self.root.parent / 'input.json'), 'link.json', 'hard.json'):
            self.assertEqual(3, self.invoke('prepare', '--input', name, '--output', 'output')[0])
        (self.root / 'hard.json').unlink()
        for name in ('../escape', '.git/output', '.symphony/output'):
            self.assertEqual(3, self.invoke('prepare', '--input', 'input.json', '--output', name)[0])

    def test_output_symlink_never_writes_outside(self):
        outside = self.root.parent / 'outside'
        outside.mkdir()
        (self.root / 'output').symlink_to(outside, target_is_directory=True)
        self.assertEqual(3, self.prepare()[0])
        self.assertEqual([], list(outside.iterdir()))

    def test_existing_different_bytes_are_ownership_conflict_without_overwrite(self):
        _, first = self.prepare()
        path = self.root / self.artifact(first)
        path.write_bytes(b'preserve-corrupt-evidence')
        code, result = self.prepare()
        self.assertEqual(5, code)
        self.assertEqual(b'preserve-corrupt-evidence', path.read_bytes())
        self.assertIn('output-conflict', {item['code'] for item in result['blockers']})

    def test_incomplete_previous_output_is_preserved(self):
        _, first = self.prepare()
        directory = (self.root / self.artifact(first)).parent
        (directory / 'summary.json').unlink()
        before = (directory / 'package.json').read_bytes()
        self.assertEqual(6, self.prepare()[0])
        self.assertEqual(before, (directory / 'package.json').read_bytes())
        self.assertFalse((directory / 'summary.json').exists())

    def test_output_file_symlink_and_hardlink_block_reuse(self):
        _, first = self.prepare()
        path = self.root / self.artifact(first)
        original = path.read_bytes()
        elsewhere = self.root / 'retained.json'
        path.rename(elsewhere)
        path.symlink_to(elsewhere)
        self.assertEqual(3, self.prepare()[0])
        path.unlink()
        os.link(elsewhere, path)
        self.assertEqual(3, self.prepare()[0])
        self.assertEqual(original, elsewhere.read_bytes())

    def test_persistence_failure_is_sanitized_and_never_reported_success(self):
        with mock.patch.object(package.os, 'fsync', side_effect=OSError('secret-canary')):
            code, result = self.prepare()
        self.assertEqual(6, code)
        self.assertNotIn('secret-canary', json.dumps(result))

    def test_summary_does_not_echo_context_argv_or_raw_output(self):
        self.input['context'] = {'opaque': 'context-canary'}
        self.save()
        _, result = self.prepare()
        summary = (self.root / 'output' / result['artifacts']['summary']).read_text()
        for text in (summary, json.dumps(result)):
            self.assertNotIn('context-canary', text)
            self.assertNotIn(sys.executable, text)
            self.assertNotIn('unittest', text)
        self.assertEqual('prepared', result['progress'])

    def test_local_git_children_receive_no_credentials_or_interpreter_settings(self):
        original = package.subprocess.run
        def inspect(argv, **kwargs):
            self.assertNotIn('GITHUB_TOKEN', kwargs['env'])
            self.assertNotIn('AWS_SECRET_ACCESS_KEY', kwargs['env'])
            self.assertNotIn('PYTHONPATH', kwargs['env'])
            return original(argv, **kwargs)
        with mock.patch.dict(os.environ, GITHUB_TOKEN='secret-canary',
                             AWS_SECRET_ACCESS_KEY='secret-canary', PYTHONPATH='secret-canary'), \
                mock.patch.object(package.subprocess, 'run', side_effect=inspect):
            code, result = self.prepare()
        self.assertEqual(0, code)
        self.assertNotIn('secret-canary', json.dumps(result))

    def test_output_staged_gitlink_and_ancestor_block_before_mkdir(self):
        for path, output in (('output', 'output'), ('boundary', 'boundary/output')):
            with self.subTest(path=path):
                self.git('update-index', '--add', '--cacheinfo', '160000', self.commit, path)
                self.assertEqual(3, self.invoke('prepare', '--input', 'input.json', '--output', output)[0])
                self.assertFalse((self.root / path).exists())
                self.git('update-index', '--force-remove', path)

    def test_new_output_hardlink_race_never_reports_success(self):
        original = package.os.write
        external = self.root.parent / 'outside-link'
        def link_during_write(descriptor, data):
            written = original(descriptor, data)
            paths = list((self.root / 'output').glob('*/package.json'))
            if paths and not external.exists():
                os.link(paths[0], external)
            return written
        with mock.patch.object(package.os, 'write', side_effect=link_during_write):
            code, _ = self.prepare()
        self.assertEqual(3, code)
        self.assertTrue(external.exists())

    def test_new_output_content_race_never_reports_success(self):
        original = package.os.write
        def tamper_during_write(descriptor, data):
            written = original(descriptor, data)
            paths = list((self.root / 'output').glob('*/package.json'))
            if paths:
                content = paths[0].read_bytes()
                paths[0].write_bytes(b'!' + content[1:])
            return written
        with mock.patch.object(package.os, 'write', side_effect=tamper_during_write):
            code, _ = self.prepare()
        self.assertEqual(5, code)

    def test_earlier_artifact_tamper_during_summary_write_blocks_final_snapshot(self):
        original = package.os.write
        def tamper_earlier_file(descriptor, data):
            written = original(descriptor, data)
            if list((self.root / 'output').glob('*/summary.json')):
                path = next((self.root / 'output').glob('*/package.json'))
                path.write_bytes(b'concurrent-tamper')
            return written
        with mock.patch.object(package.os, 'write', side_effect=tamper_earlier_file):
            self.assertEqual(5, self.prepare()[0])

    def test_near_limit_effective_response_has_bounded_safe_failure(self):
        value = copy.deepcopy(self.input)
        value['execution']['capabilities'] = {'entry': []}
        base_size = len(package.package_bytes(package.build_package(value)))
        def name(index):
            prefix = ''.join(chr(97 + (index // (26 ** power)) % 26) for power in range(4))
            return 'guard.' + prefix + 'x' * 60
        count = (package.MAX_JSON_BYTES - 100 - base_size) // (len(name(0)) + 3)
        value['execution']['capabilities']['entry'] = [name(index) for index in range(count)]
        sealed = package.build_package(value)
        data = package.package_bytes(sealed)
        self.assertGreater(len(data), package.MAX_JSON_BYTES - 500)
        self.assertLessEqual(len(data), package.MAX_JSON_BYTES)
        (self.root / 'sealed.json').write_bytes(data)
        code, result = self.invoke('effective', '--package', 'sealed.json', '--offline')
        self.assertEqual(2, code)
        self.assertEqual([{'code': 'response-limit-exceeded'}], result['blockers'])
        self.assertFalse(result['execution_eligible'])


if __name__ == '__main__':
    unittest.main()
