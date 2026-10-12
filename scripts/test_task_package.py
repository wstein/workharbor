"""T002 local data boundaries; no package/guard/execution authority."""
import copy
import hashlib
import math
import os
from pathlib import Path
import subprocess
import stat
import sys
import tempfile
import traceback
import unittest
from unittest.mock import patch

import task_package as package


class JsonPrimitiveTests(unittest.TestCase):
    def reject(self, data, code='invalid-json'):
        with self.assertRaises(package.PackageError) as raised:
            package.parse_json(data)
        self.assertEqual(code, raised.exception.code)
        self.assertNotIn('secret-canary', str(raised.exception))

    def test_strict_utf8_and_unicode(self):
        self.assertEqual({'name': 'Grüße'}, package.parse_json('{"name":"Grüße"}'.encode()))
        for value in (b'"\xff"', b'\xef\xbb\xbf{}', b'"\\ud800"', b'"\\udfff"'):
            with self.subTest(value=value):
                self.reject(value)

    def test_duplicate_keys_at_every_depth(self):
        for value in (b'{"secret-canary":1,"secret-canary":2}',
                      b'{"nested":[{"a":1,"a":2}]}'):
            self.reject(value)

    def test_nonfinite_including_overflow(self):
        for value in (b'NaN', b'Infinity', b'-Infinity', b'1e9999', b'{"a":[1e999]}'):
            self.reject(value)

    def test_malformed_trailing_and_nonbytes(self):
        for value in (b'', b'{secret-canary}', b'{} true', '{}', None):
            self.reject(value)

    def test_byte_limit_is_inclusive_and_never_truncates(self):
        self.assertEqual('x' * (package.MAX_JSON_BYTES - 2),
                         package.parse_json(b'"' + b'x' * (package.MAX_JSON_BYTES - 2) + b'"'))
        self.reject(b' ' * package.MAX_JSON_BYTES + b'{}', 'input-limit-exceeded')

    def test_deep_input_has_safe_failure(self):
        self.reject(b'[' * 2000 + b'0' + b']' * 2000)

    def test_exact_required_optional_keys(self):
        value = {'required': 1, 'optional': 2}
        self.assertIs(value, package.require_keys(value, {'required'}, {'optional'}))
        for value in ({'secret-canary': 1}, {}, [], {'required': 1, 'secret-canary': 2}):
            with self.subTest(value=value), self.assertRaises(package.PackageError) as raised:
                package.require_keys(value, {'required'}, {'optional'})
            self.assertEqual('invalid-object-keys', raised.exception.code)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_nested_keys_are_checked_explicitly_without_new_context_schema(self):
        value = package.parse_json(b'{"opaque":{"secret-canary":true}}')
        package.require_keys(value, {'opaque'})
        with self.assertRaises(package.PackageError):
            package.require_keys(value['opaque'], {'required'})

    def test_canonical_bytes(self):
        self.assertEqual(b'{"a":"Gr\xc3\xbc\xc3\x9fe","z":[1,true,null]}',
                         package.canonical_json({'z': [1, True, None], 'a': 'Grüße'}))

    def test_canonical_rejects_non_json_values(self):
        for value in (math.nan, math.inf, {'a': math.inf}, {1: 'key'}, ('tuple',),
                      {'a': '\ud800'}, object()):
            with self.subTest(kind=type(value)), self.assertRaises(package.PackageError):
                package.canonical_json(value)


class RepositoryPrimitiveTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve() / 'repository'
        self.root.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.name', 'Offline Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        (self.root / 'data').mkdir()
        (self.root / 'data/input.json').write_bytes(b'{"name":"safe"}')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')
        self.commit = self.git('rev-parse', 'HEAD').decode().strip()

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.root, check=True,
                              capture_output=True).stdout

    def test_regular_contained_read_and_json(self):
        self.assertEqual(b'{"name":"safe"}',
                         package.read_repository_file(self.root, 'data/input.json'))
        self.assertEqual({'name': 'safe'},
                         package.load_json(self.root, 'data/input.json', required={'name'}))
        with self.assertRaises(package.PackageError):
            package.load_json(self.root, 'data/input.json', required={'missing'})
        self.assertFalse((self.root / '.symphony').exists())

    def test_noncanonical_paths(self):
        for value in ('', '.', '..', '/data/input.json', './data/input.json',
                      'data//input.json', 'data/../input.json', 'data/input.json/',
                      'data\\input.json', 'data/\x00secret-canary', 'data/\nsecret-canary',
                      '.git/config', '.symphony/state', 'data/\ud800', None):
            with self.subTest(value=value), self.assertRaises(package.PackageError) as raised:
                package.read_repository_file(self.root, value)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_missing_directory_and_file(self):
        for value in ('absent/input.json', 'data/absent.json'):
            with self.subTest(value=value), self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, value)

    def test_symlink_files_and_parents_even_when_contained(self):
        (self.root / 'link.json').symlink_to('data/input.json')
        (self.root / 'parent').symlink_to('data', target_is_directory=True)
        for value in ('link.json', 'parent/input.json'):
            with self.subTest(value=value), self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, value)

    def test_root_and_ancestor_symlinks(self):
        link = self.root.parent / 'alias'
        link.symlink_to(self.root, target_is_directory=True)
        nested = self.root / 'data'
        for root in (link, link / 'data', Path('relative-root')):
            with self.subTest(root=root), self.assertRaises(package.PackageError):
                package.read_repository_file(root, 'input.json')
        self.assertTrue(nested.is_dir())

    def test_hardlink_directory_fifo_and_socket_rejected(self):
        os.link(self.root / 'data/input.json', self.root / 'hard.json')
        os.mkfifo(self.root / 'pipe')
        for value in ('hard.json', 'data/input.json', 'data', 'pipe'):
            with self.subTest(value=value), self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, value)
        # Native socket binding is denied in the host sandbox. Inject the same
        # nonregular stat mode at the type-check boundary without opening one.
        (self.root / 'socket').write_bytes(b'{}')
        original = package.os.stat
        def socket_metadata(name, *args, **kwargs):
            info = original(name, *args, **kwargs)
            if name == 'socket':
                values = list(info)
                values[0] = stat.S_IFSOCK | 0o600
                return os.stat_result(values)
            return info
        with patch.object(package.os, 'stat', side_effect=socket_metadata):
            with self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, 'socket')

    def test_oversized_file(self):
        (self.root / 'large').write_bytes(b'x' * (package.MAX_JSON_BYTES + 1))
        with self.assertRaises(package.PackageError) as raised:
            package.read_repository_file(self.root, 'large')
        self.assertEqual('input-limit-exceeded', raised.exception.code)

    def test_gitlink_and_descendants_rejected(self):
        self.git('update-index', '--add', '--cacheinfo', '160000,' + self.commit + ',submodule')
        (self.root / 'submodule').mkdir()
        (self.root / 'submodule/input.json').write_bytes(b'{}')
        with self.assertRaises(package.PackageError) as raised:
            package.read_repository_file(self.root, 'submodule/input.json')
        self.assertEqual('unsafe-repository-path', raised.exception.code)

    def test_literal_git_pathspec(self):
        (self.root / ':(glob)*.json').write_bytes(b'{}')
        self.assertEqual(b'{}', package.read_repository_file(self.root, ':(glob)*.json'))

    def test_file_replaced_between_stat_and_open(self):
        original = package.os.open
        def replaced(name, flags, *args, **kwargs):
            if name == 'input.json':
                old = self.root / 'data/input.json'
                old.rename(self.root / 'old.json')
                old.write_bytes(b'{"name":"race"}')
            return original(name, flags, *args, **kwargs)
        with patch.object(package.os, 'open', side_effect=replaced), self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_file_changed_during_read(self):
        original = package.os.read
        changed = False
        inode = (self.root / 'data/input.json').stat().st_ino
        def modified(fd, length):
            nonlocal changed
            data = original(fd, length)
            if not changed and os.fstat(fd).st_ino == inode:
                changed = True
                (self.root / 'data/input.json').write_bytes(b'{"name":"race"}')
            return data
        with patch.object(package.os, 'read', side_effect=modified), self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_parent_replaced_during_read(self):
        original = package.os.read
        changed = False
        inode = (self.root / 'data/input.json').stat().st_ino
        def replaced(fd, length):
            nonlocal changed
            data = original(fd, length)
            if not changed and os.fstat(fd).st_ino == inode:
                changed = True
                (self.root / 'data').rename(self.root / 'old-data')
                (self.root / 'data').mkdir()
                (self.root / 'data/input.json').write_bytes(data)
            return data
        with patch.object(package.os, 'read', side_effect=replaced), self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_file_changed_during_final_index_probe(self):
        original = package._git_index_entry
        probes = 0
        def modified(root, path):
            nonlocal probes
            entries = original(root, path)
            probes += 1
            if probes == 4:
                (self.root / 'data/input.json').write_bytes(b'{"name":"race"}')
            return entries
        with patch.object(package, '_git_index_entry', side_effect=modified):
            with self.assertRaises(package.PackageError):
                package.read_repository_file(self.root, 'data/input.json')

    def test_staged_symlink_mode_is_rejected_even_with_regular_worktree_file(self):
        blob = self.git('rev-parse', 'HEAD:data/input.json').decode().strip()
        self.git('update-index', '--cacheinfo', '120000,' + blob + ',data/input.json')
        with self.assertRaises(package.PackageError):
            package.read_repository_file(self.root, 'data/input.json')

    def test_raw_os_errors_do_not_escape_diagnostics(self):
        with patch.object(package.os, 'read', side_effect=OSError('secret-canary')):
            try:
                package.read_repository_file(self.root, 'data/input.json')
            except package.PackageError as error:
                self.assertNotIn('secret-canary', ''.join(traceback.format_exception(error)))
            else:
                self.fail('read should reject')

    def test_exact_local_commit_tree_and_blob(self):
        tree = self.git('rev-parse', 'HEAD^{tree}').decode().strip()
        blob = self.git('rev-parse', 'HEAD:data/input.json').decode().strip()
        for identity, kind in ((self.commit, 'commit'), (tree, 'tree'), (blob, 'blob')):
            self.assertEqual(identity, package.resolve_git_object(self.root, identity, expected_type=kind))
        with self.assertRaises(package.PackageError):
            package.resolve_git_object(self.root, blob)

    def test_nonfull_refs_missing_ids_and_revision_expressions(self):
        for value in ('HEAD', self.commit[:12], self.commit.upper(), self.commit + '^{commit}',
                      '0' * 40, '--secret-canary', None):
            with self.subTest(value=value), self.assertRaises(package.PackageError) as raised:
                package.resolve_git_object(self.root, value)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_inherited_git_environment_cannot_redefine_lookup(self):
        with patch.dict(os.environ, {'GIT_DIR': '/secret-canary/absent',
                                     'GIT_CONFIG_COUNT': '1', 'GIT_CONFIG_KEY_0': 'core.bare',
                                     'GIT_CONFIG_VALUE_0': 'true'}):
            self.assertEqual(self.commit, package.resolve_git_object(self.root, self.commit))

    def test_replacement_refs_and_grafts_rejected(self):
        self.git('replace', self.commit, self.commit)
        with self.assertRaises(package.PackageError):
            package.resolve_git_object(self.root, self.commit)
        self.git('replace', '-d', self.commit)
        (self.root / '.git/info/grafts').write_text(self.commit + '\n')
        with self.assertRaises(package.PackageError):
            package.resolve_git_object(self.root, self.commit)

    def test_git_lookup_disables_lazy_fetch_and_preserves_no_raw_errors(self):
        original = package.subprocess.run
        observed = []
        def local_only(argv, **kwargs):
            self.assertIn('--no-lazy-fetch', argv)
            self.assertEqual('1', kwargs['env']['GIT_NO_LAZY_FETCH'])
            self.assertEqual('', kwargs['env']['GIT_ALLOW_PROTOCOL'])
            self.assertNotIn('fetch', argv)
            observed.append(argv)
            return original(argv, **kwargs)
        with patch.object(package.subprocess, 'run', side_effect=local_only):
            self.assertEqual(self.commit, package.resolve_git_object(self.root, self.commit))
            with self.assertRaises(package.PackageError):
                package.resolve_git_object(self.root, '0' * 40)
        self.assertTrue(observed)
        with patch.object(package.subprocess, 'run', side_effect=OSError('secret-canary')):
            with self.assertRaises(package.PackageError) as raised:
                package.resolve_git_object(self.root, self.commit)
        self.assertNotIn('secret-canary', str(raised.exception))


class CapabilityPortTests(unittest.TestCase):
    def fixture_ports(self, required, *, live=False):
        from types import SimpleNamespace
        revision = 'a' * 40
        reference = 'sha256:' + 'b' * 64 if live else None
        ports = {}
        for category in ('guard', 'tracker', 'runtime'):
            ports[category] = SimpleNamespace(capabilities={
                name: package.CapabilityEvidence(True, revision, True, reference)
                for name in required if name.startswith(category + '.')})
        return ports

    def test_unavailable_defaults_keep_structural_success_ineligible(self):
        result = package.assess_capabilities('ready', 'implement')
        self.assertIs(result['structural_valid'], True)
        self.assertIs(result['execution_eligible'], False)
        self.assertIs(result['capabilities_satisfied'], False)
        self.assertEqual(list(package.required_capabilities('ready', 'implement')),
                         [item['capability'] for item in result['blockers']])
        self.assertTrue(all(item['code'] == 'capability-unavailable' for item in result['blockers']))
        self.assertTrue(all(item['implementation_revision'] is None for item in result['evidence']))
        self.assertTrue(all(item['live_observation_ref'] is None for item in result['evidence']))

    def test_requirements_are_action_and_mode_specific(self):
        entry = set(package.required_capabilities('entry', 'implement'))
        completion = set(package.required_capabilities('completion', 'implement'))
        self.assertIn('guard.context', entry)
        self.assertIn('guard.dependencies', entry)
        self.assertIn('guard.scope-history', entry)
        self.assertNotIn('guard.commit-binding', entry)
        self.assertIn('guard.commit-binding', completion)
        self.assertNotIn('tracker.safe-labels', entry | completion)
        ready = set(package.required_capabilities('ready', 'implement'))
        self.assertTrue(entry | completion <= ready)
        self.assertIn('runtime.provenance', ready)
        self.assertIn('tracker.approval-authentication', ready)
        self.assertIn('tracker.safe-labels', ready)
        rework = set(package.required_capabilities('entry', 'owner-authorized rework'))
        self.assertEqual(entry | {'guard.rework'}, rework)
        handoff = set(package.required_capabilities('handoff', 'handoff-only'))
        self.assertIn('tracker.pr-publication', handoff)
        self.assertIn('tracker.evidence', handoff)
        self.assertIn('guard.recovery', handoff)
        self.assertNotIn('guard.rework', handoff)

    def test_preview_uses_execution_requirements_without_running_ports(self):
        for action in ('prepare', 'validate', 'effective'):
            result = package.assess_capabilities(action, 'implement')
            self.assertIs(result['structural_valid'], True)
            self.assertIs(result['execution_eligible'], False)
            self.assertEqual(package.required_capabilities('entry', 'implement'),
                             tuple(result['required_capabilities']))

    def test_fixture_evidence_can_satisfy_only_offline_capability_assessment(self):
        required = package.required_capabilities('entry', 'implement')
        ports = self.fixture_ports(required)
        result = package.assess_capabilities('entry', 'implement', **ports)
        self.assertIs(result['capabilities_satisfied'], True)
        self.assertIs(result['execution_eligible'], False)
        self.assertEqual([], result['blockers'])
        self.assertTrue(all(item['live_observation_ref'] is None for item in result['evidence']))
        live = package.assess_capabilities('entry', 'implement', live=True, **ports)
        self.assertIs(live['capabilities_satisfied'], False)
        self.assertTrue(all(item['code'] == 'live-capability-unobserved' for item in live['blockers']))

    def test_live_evidence_does_not_grant_execution_or_authentication(self):
        required = package.required_capabilities('ready', 'implement')
        result = package.assess_capabilities('ready', 'implement', live=True,
                                            **self.fixture_ports(required, live=True))
        self.assertIs(result['capabilities_satisfied'], True)
        self.assertIs(result['execution_eligible'], False)
        self.assertEqual([], result['blockers'])
        self.assertIsNone(result['authenticated_approval'])
        self.assertIsNone(result['independent_review'])
        self.assertIsNone(result['live_execution'])

    def test_unavailable_and_unverified_evidence_and_immutable_defaults(self):
        from types import SimpleNamespace
        required = package.required_capabilities('entry', 'implement')
        for observation, code in (
                (package.CapabilityEvidence(False, None, None, None), 'capability-unavailable'),
                (package.CapabilityEvidence(True, None, True, None), 'capability-unverified'),
                (package.CapabilityEvidence(True, 'a' * 40, False, None), 'capability-unverified')):
            result = package.assess_capabilities('entry', 'implement',
                guard=SimpleNamespace(capabilities={name: observation for name in required}))
            self.assertTrue(all(item['code'] == code for item in result['blockers']))
        with self.assertRaises(TypeError):
            package.UnavailableGuard.capabilities['guard.context'] = observation

    def test_partial_or_malformed_evidence_fails_closed_and_does_not_echo(self):
        from types import SimpleNamespace
        required = package.required_capabilities('entry', 'implement')
        for evidence in ({'available': True},
                         package.CapabilityEvidence(1, 'a' * 40, True, None),
                         package.CapabilityEvidence(True, 'secret-canary', True, None),
                         package.CapabilityEvidence(True, 'a' * 40, 1, None),
                         package.CapabilityEvidence(True, 'a' * 40, True, 'secret-canary')):
            with self.subTest(kind=type(evidence)):
                result = package.assess_capabilities('entry', 'implement',
                    guard=SimpleNamespace(capabilities={name: evidence for name in required}))
                self.assertIs(result['capabilities_satisfied'], False)
                self.assertNotIn('secret-canary', str(result))

    def test_unavailable_operations_fail_closed_without_legacy_fallback(self):
        guard = package.UnavailableGuard()
        for method, arguments in (
                ('validate_context', ({'opaque': 'secret-canary'}, {})),
                ('compare_artifacts', ({}, {}, {})),
                ('validate_scope', ({}, {}, {})),
                ('validate_acceptance', ({}, {}))):
            with self.subTest(method=method), self.assertRaises(package.PackageError) as raised:
                getattr(guard, method)(*arguments)
            self.assertEqual('guard-unavailable', raised.exception.code)
        tracker = package.UnavailableTracker()
        for method, arguments in (('read_approval', ('secret-canary',)),
                                 ('read_issue', ({},)), ('find_pr', ({},)),
                                 ('publish_pr', ({},)), ('find_evidence', ({},)),
                                 ('write_evidence', ({},)), ('replace_labels', ({}, {}, {}))):
            with self.subTest(method=method), self.assertRaises(package.PackageError) as raised:
                getattr(tracker, method)(*arguments)
            self.assertEqual('tracker-unavailable', raised.exception.code)
        with self.assertRaises(package.PackageError) as raised:
            package.UnavailableRuntime().observe({'secret-canary': True})
        self.assertEqual('runtime-unavailable', raised.exception.code)

    def test_no_adapter_loading_from_task_data(self):
        from types import SimpleNamespace
        result = package.assess_capabilities('entry', 'implement',
                  guard=SimpleNamespace(capabilities={'adapter': 'secret-canary'}))
        self.assertIs(result['capabilities_satisfied'], False)
        self.assertNotIn('secret-canary', str(result))

    def test_port_failures_are_sanitized(self):
        class Broken:
            @property
            def capabilities(self):
                raise RuntimeError('secret-canary')
        result = package.assess_capabilities('ready', 'implement', guard=Broken())
        self.assertIs(result['capabilities_satisfied'], False)
        self.assertNotIn('secret-canary', str(result))

    def test_invalid_action_mode_or_live_flag_is_safe(self):
        for action, mode, live in (('secret-canary', 'implement', False),
                                   ('ready', 'secret-canary', False),
                                   ('ready', 'implement', 'secret-canary')):
            with self.subTest(action=action, mode=mode), self.assertRaises(package.PackageError) as raised:
                package.assess_capabilities(action, mode, live=live)
            self.assertEqual('invalid-capability-request', raised.exception.code)
            self.assertNotIn('secret-canary', str(raised.exception))


class PackageContractTests(unittest.TestCase):
    git = RepositoryPrimitiveTests.git

    def setUp(self):
        RepositoryPrimitiveTests.setUp(self)
        self.feature = '999-offline-fixture'
        directory = self.root / 'specs' / self.feature
        directory.mkdir(parents=True)
        for name in ('spec.md', 'plan.md', 'tasks.md'):
            (directory / name).write_text('synthetic ' + name + '\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'synthetic artifacts')
        self.commit = self.git('rev-parse', 'HEAD').decode().strip()
        tree = self.git('rev-parse', 'HEAD^{tree}').decode().strip()
        self.input = dict(schema_version=1, repository='fixture/offline', issue=None,
            feature=self.feature, task_id='fixture:documentation', mode='implement',
            context={'opaque_guard_context': [1, {'unchanged': True}],
                     'approval': {'context_digest': 'guard-owned-self-digest'}},
            source=dict(accepted_commit=self.commit, accepted_tree=tree,
                        proposal_commit=self.commit, proposal_tree=tree, proposal_commits=[],
                        artifact_revisions=[dict(path=f'specs/{self.feature}/{name}', commit=self.commit,
                            sha256=hashlib.sha256((directory / name).read_bytes()).hexdigest())
                            for name in ('spec.md', 'plan.md', 'tasks.md')],
                        equivalence_evidence_digest=None),
            execution=dict(symphony_revision='be10a1b79df723d6d7612b5651c8522704dafb2e',
                           build_provenance=None, workflow_digest='a' * 64,
                           template_digest='b' * 64, configuration_digest='c' * 64,
                           guard_revision=self.commit, capabilities={}, external_actions=[],
                           publication_scope=None),
            outcomes=[dict(id='fixture-documentation',
                argv=[sys.executable, '-I', '-B', '-m', 'unittest', 'discover',
                      '-s', 'tests', '-p', 'test_*.py', '-v'], deadline_seconds=10,
                tool_revision='sha256:' + 'd' * 64, input_revision='sha256:' + 'e' * 64,
                cases=[dict(id='fixture.documentation', expected='pass', fingerprint=None)], raw_exit=0)],
            approval_refs=dict(spec=None, plan=None, execution=None, matrix=None, rework=None))
        self.snapshot = dict(repository='fixture/offline', root=self.root,
                             approved=object(), current=object())

    def validator(self, checks):
        self.assertEqual(self.input['outcomes'], checks)
        return checks

    def test_build_is_canonical_detached_and_deterministic(self):
        value = package.build_package(self.input)
        original_context = copy.deepcopy(self.input['context'])
        self.assertEqual(value, package.build_package(copy.deepcopy(self.input)))
        data = package.package_bytes(value)
        self.assertFalse(data.endswith(b'\n'))
        self.assertEqual(data, package.canonical_json(value))
        self.assertEqual(original_context, value['context'])
        self.input['context']['opaque_guard_context'].append('mutated')
        self.assertEqual(original_context, value['context'])

    def test_identity_excludes_approval_refs_and_retains_separate_raw_revision(self):
        before = package.build_package(self.input)
        self.input['approval_refs']['spec'] = 'sha256:' + 'a' * 64
        after = package.build_package(self.input)
        self.assertEqual(before['package_digest'], after['package_digest'])
        self.assertNotEqual(package.package_revision_digest(before), package.package_revision_digest(after))
        self.assertIsNone(before['approval_refs']['spec'])

    def test_every_immutable_field_and_check_order_changes_identity(self):
        before = package.build_package(self.input)['package_digest']
        for field, replacement in (('mode', 'handoff-only'), ('task_id', 'fixture:other'),
                                   ('context', {'different_opaque_context': True})):
            changed = copy.deepcopy(self.input)
            changed[field] = replacement
            self.assertNotEqual(before, package.build_package(changed)['package_digest'])
        changed = copy.deepcopy(self.input)
        changed['outcomes'].append(copy.deepcopy(changed['outcomes'][0]))
        changed['outcomes'][1]['id'] = 'second'
        self.assertNotEqual(package.build_package(changed)['package_digest'],
            package.build_package(dict(changed, outcomes=list(reversed(changed['outcomes']))))['package_digest'])

    def test_transient_output_and_attempt_metadata_is_not_package_identity(self):
        first = package.build_package(self.input, timestamp='first', output_path='/first', attempt_id='first')
        second = package.build_package(self.input, timestamp='second', output_path='/second', attempt_id='second')
        self.assertEqual(package.package_bytes(first), package.package_bytes(second))

    def test_unknown_missing_version_and_tampered_digest_rejected(self):
        variants = []
        changed = copy.deepcopy(self.input)
        changed['secret-canary'] = True
        variants.append(changed)
        changed = copy.deepcopy(self.input)
        del changed['source']
        variants.append(changed)
        variants.append(dict(self.input, schema_version=True))
        variants.append(dict(self.input, schema_version=2))
        for value in variants:
            with self.subTest(keys=list(value)), self.assertRaises(package.PackageError):
                package.build_package(value)
        sealed = package.build_package(self.input)
        sealed['context'] = {'changed': True}
        with self.assertRaises(package.PackageError):
            package.package_bytes(sealed)

    def test_nested_source_execution_and_approval_keys_rejected(self):
        for section in ('source', 'execution', 'approval_refs'):
            changed = copy.deepcopy(self.input)
            changed[section]['secret-canary'] = True
            with self.subTest(section=section), self.assertRaises(package.PackageError) as raised:
                package.build_package(changed)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_missing_nested_fields_artifact_inventory_and_credential_refs_rejected(self):
        variants = []
        for section, field in (('source', 'accepted_tree'), ('execution', 'guard_revision'),
                               ('approval_refs', 'plan')):
            changed = copy.deepcopy(self.input)
            del changed[section][field]
            variants.append(changed)
        changed = copy.deepcopy(self.input)
        changed['source']['artifact_revisions'][1] = copy.deepcopy(changed['source']['artifact_revisions'][0])
        variants.append(changed)
        changed = copy.deepcopy(self.input)
        changed['approval_refs']['spec'] = 'https://secret-canary@github.com/owner/repo/pull/1?token=secret-canary'
        variants.append(changed)
        for value in variants:
            with self.subTest(section=list(value)), self.assertRaises(package.PackageError) as raised:
                package.build_package(value)
            self.assertNotIn('secret-canary', str(raised.exception))

    def test_guard_raw_errors_and_invalid_results_do_not_escape(self):
        for raw_result in (None, True, package.GuardValidation(True, ('secret-canary',))):
            class Guard(package.UnavailableGuard):
                def validate_context(self, context, evidence):
                    return raw_result
            result = package.validate(package.build_package(self.input), self.snapshot, {}, {}, guard=Guard())
            self.assertIs(result['execution_eligible'], False)
            self.assertIn('guard-unavailable', [item['code'] for item in result['blockers']])
            self.assertNotIn('secret-canary', str(result))

    def test_issue_repository_and_required_reference_actions_are_distinct(self):
        changed = copy.deepcopy(self.input)
        changed['issue'] = {'repository': 'fixture/offline', 'number': 7}
        changed['task_id'] = 'T002'
        self.assertEqual(7, package.build_package(changed)['issue']['number'])
        changed['issue']['repository'] = 'other/repository'
        with self.assertRaises(package.PackageError):
            package.build_package(changed)
        for number in (0, True, -1):
            changed['issue'] = {'repository': 'fixture/offline', 'number': number}
            with self.subTest(number=number), self.assertRaises(package.PackageError):
                package.build_package(changed)
        changed = copy.deepcopy(self.input)
        changed['approval_refs'] = {'spec': 'sha256:' + 'a' * 64}
        with self.assertRaises(package.PackageError):
            package.build_package(changed)

    def test_default_validation_is_structural_ineligible_and_side_effect_free(self):
        value = package.build_package(self.input)
        before = {path: path.read_bytes() for path in self.root.rglob('*') if path.is_file()}
        result = package.validate(value, self.snapshot, {}, {})
        self.assertIs(result['structural_valid'], True)
        self.assertIs(result['execution_eligible'], False)
        self.assertIsNone(result['authenticated_approval'])
        self.assertIsNone(result['independent_review'])
        self.assertIsNone(result['live_execution'])
        self.assertEqual([], result['raw_exits'])
        self.assertIn('guard-unavailable', [item['code'] for item in result['blockers']])
        self.assertIn('outcome-validator-unavailable', [item['code'] for item in result['blockers']])
        self.assertEqual(before, {path: path.read_bytes() for path in self.root.rglob('*') if path.is_file()})

    def test_trusted_repository_mismatch_and_source_tree_missing_ids_block(self):
        value = package.build_package(self.input)
        result = package.validate(value, dict(self.snapshot, repository='other/repo'), {}, {})
        self.assertIs(result['structural_valid'], False)
        self.assertIn('repository-mismatch', [item['code'] for item in result['blockers']])
        for key, identity in (('accepted_commit', '0' * 40), ('proposal_tree', self.commit)):
            changed = copy.deepcopy(self.input)
            changed['source'][key] = identity
            result = package.validate(package.build_package(changed), self.snapshot, {}, {})
            self.assertIs(result['structural_valid'], False)

    def test_distinct_artifact_revisions_checked_locally_without_guard_schema(self):
        changed = copy.deepcopy(self.input)
        changed['source']['artifact_revisions'][0]['sha256'] = '0' * 64
        result = package.validate(package.build_package(changed), self.snapshot, {}, {})
        self.assertIs(result['structural_valid'], False)
        self.assertIn('artifact-identity-mismatch', [item['code'] for item in result['blockers']])

    def test_injected_guard_owns_context_comparison_scope_and_acceptance(self):
        calls = []
        test = self
        class Guard:
            capabilities = {}
            def validate_context(self, context, evidence):
                test.assertEqual(test.input['context'], context)
                calls.append('context')
                return package.GuardValidation(True)
            def compare_artifacts(self, approved, current, evidence):
                test.assertIs(test.snapshot['approved'], approved)
                test.assertIs(test.snapshot['current'], current)
                calls.append('comparison')
                return 'identical'
            def validate_scope(self, snapshot, context, evidence):
                calls.append('scope')
                return package.GuardValidation(True)
            def validate_acceptance(self, context, evidence):
                calls.append('acceptance')
                return package.GuardValidation(True)
        self.input['approval_refs'] = {key: 'sha256:' + str(index) * 64
            for index, key in enumerate(self.input['approval_refs'])}
        value = package.build_package(self.input)
        result = package.validate(value, self.snapshot, {}, {}, guard=Guard(), outcome_validator=self.validator)
        self.assertEqual(['context', 'comparison', 'scope', 'acceptance'], calls)
        self.assertIs(result['structural_valid'], True)
        self.assertIs(result['execution_eligible'], False)
        self.assertEqual('supplied-local', result['approval_assurance'])
        self.assertIsNone(result['authenticated_approval'])

    def test_unmet_prerequisites_are_reported_without_running_them(self):
        class Guard(package.UnavailableGuard):
            def validate_context(self, context, evidence):
                return package.GuardValidation(False, ('fixture:missing-dependency',))
        result = package.validate(package.build_package(self.input), self.snapshot, {}, {}, guard=Guard())
        self.assertEqual(['fixture:missing-dependency'], result['unmet_prerequisites'])
        self.assertIs(result['execution_eligible'], False)

    def test_outcome_validator_is_delegated_without_execution_or_raw_error(self):
        value = package.build_package(self.input)
        result = package.validate(value, self.snapshot, {}, {}, outcome_validator=self.validator)
        self.assertNotIn('outcome-validator-unavailable', [item['code'] for item in result['blockers']])
        def broken(_checks):
            raise ValueError('secret-canary')
        result = package.validate(value, self.snapshot, {}, {}, outcome_validator=broken)
        self.assertIn('invalid-outcomes', [item['code'] for item in result['blockers']])
        self.assertNotIn('secret-canary', str(result))

    def test_renewal_and_ambiguous_guard_comparisons_block_without_normalization(self):
        for comparison in ('renewal-required', 'ambiguous', 'secret-canary'):
            class Guard(package.UnavailableGuard):
                def validate_context(self, context, evidence):
                    return package.GuardValidation(True)
                def compare_artifacts(self, approved, current, evidence):
                    return comparison
            result = package.validate(package.build_package(self.input), self.snapshot, {}, {}, guard=Guard())
            self.assertIn('guard-comparison-blocked', [item['code'] for item in result['blockers']])
            self.assertNotIn('secret-canary', str(result))

    def test_assertion_matrix_and_rework_need_their_own_reference(self):
        changed = copy.deepcopy(self.input)
        changed['outcomes'][0]['cases'][0].update(expected='assertion', fingerprint='sha256:' + 'f' * 64)
        changed['outcomes'][0]['raw_exit'] = 1
        result = package.validate(package.build_package(changed), self.snapshot, {}, {}, outcome_validator=lambda value: value)
        self.assertIn('matrix-approval-reference-missing', [item['code'] for item in result['blockers']])
        changed['mode'] = 'owner-authorized rework'
        result = package.validate(package.build_package(changed), self.snapshot, {}, {})
        self.assertIn('rework-approval-reference-missing', [item['code'] for item in result['blockers']])


if __name__ == '__main__':
    unittest.main()
