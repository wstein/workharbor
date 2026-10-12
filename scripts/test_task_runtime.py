"""T012 inert local runtime observations; synthetic Git/build bytes, never executed."""
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import prepare_symphony as runtime
import task_package as package


ROOT = Path(__file__).resolve().parents[1]


class RuntimeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.source = self.root / 'source'
        self.source.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.name', 'Offline Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        (self.source / 'runtime.ex').write_text('synthetic source, not Symphony\n')
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture: runtime source')
        self.revision = self.git('rev-parse', 'HEAD').decode().strip()
        self.tree = self.git('rev-parse', 'HEAD^{tree}').decode().strip()
        # Production requires the fixed upstream SHA. A clearly synthetic pin is
        # scoped to these disposable local algorithm tests, never a live claim.
        self.pin = patch.object(runtime, 'SYMPHONY_REVISION', self.revision)
        self.pin.start()
        self.addCleanup(self.pin.stop)
        self.artifact = self.root / 'runtime.bin'
        self.artifact.write_bytes(b'NOT EXECUTABLE: synthetic build bytes\n')
        metadata = dict(schema_version=1, symphony_revision=self.revision,
                        source_tree=self.tree, artifact_path='runtime.bin',
                        artifact_sha256=self.digest(self.artifact.read_bytes()))
        self.metadata = self.root / 'build.json'
        self.metadata.write_bytes(package.canonical_json(metadata))
        self.template = self.root / 'WORKFLOW.md'
        self.template.write_bytes((ROOT / 'WORKFLOW.md').read_bytes())
        config = json.loads(self.template.read_text().split('---', 2)[1])
        config['tracker']['provider']['repo'] = 'fixture/offline'
        config['workspace']['root'] = str(self.root / 'issues')
        self.workflow = self.root / 'WORKFLOW.local.md'
        self.workflow.write_text('---\n' + json.dumps(config) + '\n---\nFixture prompt.\n')
        self.settings = runtime.nonsecret_configuration(config)
        self.checks = [['python3', '-m', 'unittest', 'fixture_test']]
        self.actions = ['publish-pr', 'write-evidence', 'replace-labels']
        self.inputs = dict(source_root=self.source, artifact_root=self.root,
                           build_metadata_path='build.json', workflow_root=self.root,
                           workflow_path='WORKFLOW.local.md', template_path='WORKFLOW.md',
                           configuration=self.settings, guard_revision='b' * 40,
                           check_argv=self.checks, external_actions=self.actions)
        self.proposal = dict(context={'checks': [{'argv': self.checks[0]}]},
                             execution=dict(symphony_revision=self.revision,
                                            build_provenance=self.digest(self.metadata.read_bytes()),
                                            workflow_digest=self.digest(self.workflow.read_bytes()),
                                            template_digest=self.digest(self.template.read_bytes()),
                                            configuration_digest=self.digest(package.canonical_json(self.settings)),
                                            guard_revision='b' * 40, external_actions=self.actions))

    @staticmethod
    def digest(data):
        return hashlib.sha256(data).hexdigest()

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.source, check=True,
                              capture_output=True).stdout

    def observer(self, **changes):
        return runtime.RuntimeObserver(runtime.RuntimeInputs(**dict(self.inputs, **changes)))

    def observe(self, **changes):
        return self.observer(**changes).observe_effective(self.proposal, {})

    def test_exact_local_source_build_workflow_and_settings_are_observed(self):
        result = self.observe()
        self.assertTrue(result['local_verified'], result)
        self.assertFalse(result['execution_eligible'])
        self.assertFalse(result['activated'])
        self.assertIsNone(result['live_execution'])
        self.assertEqual([], result['blockers'])
        self.assertEqual(self.revision, result['execution']['symphony_revision'])
        self.assertEqual(self.proposal['execution']['configuration_digest'],
                         result['execution']['configuration_digest'])

    def test_configured_revision_without_actual_objects_is_insufficient(self):
        missing = self.root / 'missing'
        result = self.observe(source_root=missing)
        self.assertFalse(result['local_verified'])
        self.assertTrue(result['blockers'])

    def test_production_pin_cannot_be_replaced_by_matching_package_text(self):
        with patch.object(runtime, 'SYMPHONY_REVISION', 'be10a1b79df723d6d7612b5651c8522704dafb2e'):
            result = self.observe()
        self.assertFalse(result['local_verified'])

    def test_source_dirty_index_untracked_mode_and_wrong_head_block(self):
        for mutation in ('worktree', 'index', 'untracked', 'mode', 'head'):
            with self.subTest(mutation=mutation):
                path = self.source / 'runtime.ex'
                if mutation in ('worktree', 'index', 'head'):
                    path.write_text('drift\n')
                elif mutation == 'untracked':
                    (self.source / 'extra').write_text('drift\n')
                else:
                    path.chmod(0o755)
                    self.git('config', 'core.filemode', 'false')
                if mutation in ('index', 'head'):
                    self.git('add', '.')
                if mutation == 'head':
                    self.git('commit', '-qm', 'fixture: drift')
                self.assertFalse(self.observe()['local_verified'])
                # Fixture reset only; no task/source checkout is changed.
                self.git('reset', '--hard', self.revision)
                self.git('clean', '-fd')
                path.chmod(0o644)

    def test_artifact_bytes_and_provenance_metadata_drift_block(self):
        for path in (self.artifact, self.metadata):
            original = path.read_bytes()
            path.write_bytes(original + b' ')
            self.assertFalse(self.observe()['local_verified'])
            path.write_bytes(original)

    def test_build_claim_requires_matching_source_tree_and_artifact_digest(self):
        original = json.loads(self.metadata.read_bytes())
        for key, value in (('source_tree', 'a' * 40), ('artifact_sha256', 'a' * 64),
                           ('symphony_revision', 'a' * 40)):
            with self.subTest(key=key):
                altered = dict(original, **{key: value})
                self.metadata.write_bytes(package.canonical_json(altered))
                # Even approving a self-consistent metadata digest cannot replace
                # the actual locally observed source/artifact identity.
                proposal = copy.deepcopy(self.proposal)
                proposal['execution']['build_provenance'] = self.digest(self.metadata.read_bytes())
                self.assertFalse(self.observer().observe(proposal)['local_verified'])
        self.metadata.write_bytes(package.canonical_json(original))

    def test_workflow_template_configuration_guard_checks_and_actions_drift_block(self):
        for key in ('workflow_digest', 'template_digest', 'configuration_digest',
                    'build_provenance', 'guard_revision', 'symphony_revision'):
            with self.subTest(key=key):
                proposal = copy.deepcopy(self.proposal)
                proposal['execution'][key] = 'a' * len(proposal['execution'][key])
                self.assertFalse(self.observer().observe(proposal)['local_verified'])
        self.assertFalse(self.observe(check_argv=[['sh', '-c', 'secret-canary']])['local_verified'])
        self.assertFalse(self.observe(external_actions=list(reversed(self.actions)))['local_verified'])
        self.assertFalse(self.observe(guard_revision='c' * 40)['local_verified'])
        settings = dict(self.settings, max_turns=100)
        self.assertFalse(self.observe(configuration=settings)['local_verified'])

    def test_prompt_byte_drift_blocks_even_when_front_matter_is_unchanged(self):
        self.workflow.write_bytes(self.workflow.read_bytes() + b'Changed prompt.\n')
        self.assertFalse(self.observe()['local_verified'])

    def test_workflow_markers_must_be_exact_standalone_lines(self):
        original = self.workflow.read_bytes()
        malformed = (original.replace(b'---\n', b'---', 1),
                     original.replace(b'\n---\n', b'---\n', 1),
                     original.replace(b'---\n', b' ---\n', 1),
                     original.replace(b'\n---\n', b'\n---joined\n', 1))
        for workflow in malformed:
            with self.subTest(workflow_prefix=workflow[:8]):
                self.workflow.write_bytes(workflow)
                proposal = copy.deepcopy(self.proposal)
                proposal['execution']['workflow_digest'] = self.digest(workflow)
                result = self.observer().observe(proposal)
                self.assertFalse(result['local_verified'])
                self.assertEqual('runtime-workflow-invalid', result['blockers'][0]['code'])

    def test_workflow_exact_lf_crlf_markers_and_literal_marker_in_json(self):
        config = json.loads(self.workflow.read_text().split('---', 2)[1])
        config['hooks']['after_create'] += '\n# literal --- inside a JSON string\n'
        settings = runtime.nonsecret_configuration(config)
        for newline in ('\n', '\r\n'):
            workflow = ('---\n' + json.dumps(config, indent=2) + '\n---\nFixture prompt.\n')
            data = workflow.replace('\n', newline).encode()
            self.workflow.write_bytes(data)
            proposal = copy.deepcopy(self.proposal)
            proposal['execution']['workflow_digest'] = self.digest(data)
            proposal['execution']['configuration_digest'] = self.digest(package.canonical_json(settings))
            result = self.observer(configuration=settings).observe(proposal)
            self.assertTrue(result['local_verified'], result)

    def test_symlinks_hardlinks_traversal_and_nonregular_files_block(self):
        for path in (self.metadata, self.artifact, self.workflow, self.template):
            original = path.read_bytes()
            outside = self.root / 'outside'
            outside.write_bytes(original)
            path.unlink()
            path.symlink_to(outside)
            self.assertFalse(self.observe()['local_verified'])
            path.unlink()
            os.link(outside, path)
            self.assertFalse(self.observe()['local_verified'])
            path.unlink()
            os.mkfifo(path)
            self.assertFalse(self.observe()['local_verified'])
            path.unlink()
            path.write_bytes(original)
            outside.unlink()
        for name in ('../WORKFLOW.local.md', '/WORKFLOW.local.md', 'nested/../WORKFLOW.local.md'):
            self.assertFalse(self.observe(workflow_path=name)['local_verified'])

    def test_nonsecret_settings_are_a_strict_typed_allowlist(self):
        for changes in ({'token': 'secret-canary'}, {'max_turns': True},
                        {'repository': 'https://user:secret-canary@example.invalid/repo'}):
            settings = dict(self.settings, **changes)
            result = self.observe(configuration=settings)
            self.assertFalse(result['local_verified'])
            self.assertNotIn('secret-canary', json.dumps(result))
        config = json.loads(self.workflow.read_text().split('---', 2)[1])
        config['tracker']['provider']['token'] = 'secret-canary'
        with self.assertRaises(package.PackageError):
            runtime.nonsecret_configuration(config)

    def test_render_and_local_build_metadata_never_establish_live_behavior(self):
        result = self.observer().observe_effective(self.proposal, {}, live=True)
        self.assertTrue(result['local_verified'])
        self.assertFalse(result['live_verified'])
        self.assertIsNone(result['live_execution'])
        self.assertIn('runtime-live-observation-unavailable',
                      [item['code'] for item in result['blockers']])
        self.assertEqual({}, dict(self.observer().capabilities))

    def test_unbound_or_partial_host_evidence_never_establishes_live_behavior(self):
        result = self.observer().observe_effective(self.proposal,
                    {'runtime': {'binding_digest': 'a' * 64,
                                 'runtime_ref': 'sha256:' + 'b' * 64}}, live=True)
        self.assertFalse(result['live_verified'])
        self.assertIsNone(result['live_execution'])

    def test_host_observation_requires_complete_matching_binding_and_capabilities(self):
        # Trusted host injection is a separate port contract. These local
        # references are fixture assertions, not actual runtime/host evidence.
        binding = self.observe()['binding_digest']
        reference = 'sha256:' + 'd' * 64
        observation = runtime.RuntimeLiveObservation(binding, reference, reference, reference)
        capabilities = {name: package.CapabilityEvidence(True, 'e' * 40, True, reference)
                        for name in ('runtime.provenance', 'runtime.workflow',
                                     'runtime.configuration', 'runtime.host-isolation')}
        observer = runtime.RuntimeObserver(runtime.RuntimeInputs(**self.inputs),
                                           capabilities=capabilities)
        result = observer.observe_effective(self.proposal, {'runtime': observation}, live=True)
        self.assertTrue(result['live_verified'], result)
        self.assertEqual(reference, result['live_execution'])
        self.assertFalse(result['execution_eligible'])
        self.assertFalse(result['activated'])
        for field in ('binding_digest', 'runtime_ref', 'isolation_ref', 'secret_behavior_ref'):
            values = dict(binding_digest=binding, runtime_ref=reference, isolation_ref=reference,
                          secret_behavior_ref=reference)
            values[field] = 'a' * 64 if field == 'binding_digest' else None
            invalid = runtime.RuntimeLiveObservation(**values)
            result = observer.observe_effective(self.proposal, {'runtime': invalid}, live=True)
            self.assertFalse(result['live_verified'])
            self.assertIsNone(result['live_execution'])
        missing = self.observer().observe_effective(self.proposal, {'runtime': observation}, live=True)
        self.assertFalse(missing['live_verified'])
        capabilities_only = observer.observe_effective(self.proposal, {}, live=True)
        self.assertFalse(capabilities_only['live_verified'])

    def test_source_index_flags_and_object_replacements_block(self):
        self.git('update-index', '--assume-unchanged', 'runtime.ex')
        self.assertFalse(self.observe()['local_verified'])
        self.git('update-index', '--no-assume-unchanged', 'runtime.ex')
        self.git('update-index', '--skip-worktree', 'runtime.ex')
        self.assertFalse(self.observe()['local_verified'])
        self.git('update-index', '--no-skip-worktree', 'runtime.ex')
        self.git('update-ref', 'refs/replace/' + self.revision, self.revision)
        self.assertFalse(self.observe()['local_verified'])

    def test_clean_git_status_cannot_hide_hardlinked_source(self):
        os.link(self.source / 'runtime.ex', self.root / 'linked-source')
        self.assertEqual(b'', self.git('status', '--porcelain'))
        self.assertFalse(self.observe()['local_verified'])

    def test_mid_collection_drift_is_not_adopted(self):
        original = runtime._local_file
        count = 0
        def change_after_first_read(root, path, **kwargs):
            nonlocal count
            value = original(root, path, **kwargs)
            if path == 'WORKFLOW.local.md':
                count += 1
                if count == 1:
                    self.workflow.write_bytes(self.workflow.read_bytes() + b'drift\n')
            return value
        with patch.object(runtime, '_local_file', side_effect=change_after_first_read):
            result = self.observe()
        self.assertFalse(result['local_verified'])
        self.assertEqual('runtime-input-raced', result['blockers'][0]['code'])

    def test_default_missing_inputs_fail_closed_without_raw_diagnostics(self):
        result = runtime.RuntimeObserver().observe(self.proposal)
        self.assertFalse(result['local_verified'])
        self.assertIsNone(result['execution'])
        self.assertIsNone(result['binding_digest'])
        self.assertIsNone(result['live_execution'])
        self.assertTrue(result['blockers'])

    def test_no_runtime_artifact_check_or_network_execution_and_no_secret_output(self):
        original = subprocess.run
        def local_git_only(argv, **kwargs):
            self.assertEqual('git', argv[0])
            self.assertIn('--no-lazy-fetch', argv)
            self.assertFalse({'fetch', 'push', 'clone', 'checkout', 'commit'} & set(argv))
            self.assertNotIn('GITHUB_TOKEN', kwargs['env'])
            return original(argv, **kwargs)
        before = sorted(str(path.relative_to(self.root)) for path in self.root.rglob('*'))
        with patch.object(subprocess, 'run', side_effect=local_git_only), \
                patch.dict(os.environ, GITHUB_TOKEN='secret-canary'):
            result = self.observe()
        self.assertTrue(result['local_verified'], result)
        self.assertNotIn('secret-canary', json.dumps(result))
        self.assertEqual(before, sorted(str(path.relative_to(self.root)) for path in self.root.rglob('*')))


if __name__ == '__main__':
    unittest.main()
