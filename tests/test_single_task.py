import json
import copy
import hashlib
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
import uuid
from contextlib import ExitStack
from pathlib import Path
from unittest import mock

SCRIPT = Path(__file__).resolve().parents[1]/'scripts/single_task.py'
FIXTURES = SCRIPT.parent.parent/'tests/fixtures/single-task'
GIT = shutil.which('git')


def canonical_bytes(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      ensure_ascii=False, allow_nan=False).encode('utf-8')


def digest(value):
    return hashlib.sha256(value).hexdigest()


def context_digest(context):
    value = copy.deepcopy(context)
    value['approval'].pop('context_digest', None)
    return digest(canonical_bytes(value))


class SingleTaskFixture:
    """Disposable local examples, not an implementation of the planned CLI.

    Only local Git plumbing is callable. A fresh subprocess environment admits
    no inherited credentials, configuration overrides, hooks or signing setup.
    The caller owns TemporaryDirectory lifetime; nothing touches the source repo.
    """

    def __init__(self, directory):
        if not GIT:
            raise RuntimeError('Git is required for fixture creation')
        self.directory = Path(directory)
        self.root = self.directory/'repo'
        self.root.mkdir()
        self.private = self.directory/'private'
        self.private.mkdir()
        self.hooks = self.private/'empty-hooks'
        self.hooks.mkdir()
        self.env = {
            'PATH': os.defpath,
            'HOME': str(self.private),
            'XDG_CONFIG_HOME': str(self.private),
            'GIT_CONFIG_NOSYSTEM': '1',
            'GIT_CONFIG_SYSTEM': os.devnull,
            'GIT_CONFIG_GLOBAL': os.devnull,
            'GIT_ATTR_NOSYSTEM': '1',
            'GIT_TEMPLATE_DIR': str(self.hooks),
            'GIT_ALLOW_PROTOCOL': '',
            'GIT_TERMINAL_PROMPT': '0',
            'GIT_AUTHOR_NAME': 'Synthetic Fixture',
            'GIT_AUTHOR_EMAIL': 'fixture@example.invalid',
            'GIT_COMMITTER_NAME': 'Synthetic Fixture',
            'GIT_COMMITTER_EMAIL': 'fixture@example.invalid',
            'LC_ALL': 'C',
        }
        self.commands = []
        self.outputs = []
        self.feature = self.root/'specs/synthetic'
        self.feature.mkdir(parents=True)
        for name in ('spec.md', 'plan.md', 'tasks.md'):
            shutil.copyfile(FIXTURES/name, self.feature/name)
        (self.root/'guide.md').write_text('Before\n', encoding='utf-8')
        self.git('init', '-b', 'main')
        self.git('add', '.')
        self.git('commit', '-m', 'test: synthetic baseline')
        self.baseline = self.git('rev-parse', 'HEAD').stdout.strip()
        self.reviewed_artifacts = {
            name: {'path': f'specs/synthetic/{name}', 'commit': self.baseline,
                   'sha256': digest((self.feature/name).read_bytes())}
            for name in ('spec.md', 'plan.md', 'tasks.md')
        }
        self.tasks_bytes = (self.feature/'tasks.md').read_bytes()
        self.dependency_table = self.tasks_bytes.decode('utf-8').split(
            '| Task | Prerequisites |', 1)[1]
        self.dependency_table = '| Task | Prerequisites |' + self.dependency_table.rstrip('\n')
        self.identities = {key: 'synthetic:' + key + ':' + uuid.uuid4().hex
                           for key in ('owner', 'approval', 'prerequisite', 'reviewer', 'review', 'attempt')}

    def git(self, *args):
        # No shell, transport, aliases, external diff or caller-supplied -c flags.
        if not args or args[0] not in {
                'init', 'add', 'commit', 'rev-parse', 'show', 'diff',
                'ls-tree', 'status', 'config'}:
            raise RuntimeError('Fixture permits local Git commands only')
        argv = [GIT, '-c', f'core.hooksPath={self.hooks}',
                '-c', 'commit.gpgSign=false', '-c', 'tag.gpgSign=false',
                '-c', 'core.fsmonitor=false', '-c', 'credential.helper=', *args]
        run = subprocess.run(argv, cwd=self.root, env=self.env,
                             capture_output=True, text=True, timeout=10, check=True)
        self.commands.append(argv)
        self.outputs.append(run.stdout + run.stderr)
        return run

    def template(self, name):
        return json.loads((FIXTURES/name).read_text(encoding='utf-8'))

    def manifest(self, paths, task):
        entries = []
        for path in sorted(paths):
            file = self.root/path
            if not file.exists():
                entries.append({'path': path, 'type': 'absent', 'mode': None, 'sha256': None})
                continue
            if file.is_symlink() or not file.is_file():
                raise ValueError('Fixture manifest requires regular files')
            content = file.read_bytes()
            if path == 'specs/synthetic/tasks.md':
                content = content.replace(f'- [x] {task}'.encode(), f'- [ ] {task}'.encode())
            entries.append({'path': path, 'type': 'regular',
                            'mode': '100755' if file.stat().st_mode & 0o111 else '100644',
                            'sha256': digest(content)})
        return {'schema_version': 1, 'entries': entries}

    def context(self, task='T001', prerequisite=None):
        value = self.template('context.json')
        value.update(task_id=task, reviewed_artifacts=copy.deepcopy(self.reviewed_artifacts),
                     dependencies=[] if task == 'T001' else ['T001'],
                     prerequisite_evidence=[] if prerequisite is None else [copy.deepcopy(prerequisite)],
                     allowed_paths=['guide.md'] if task == 'T001' else ['followup.md'])
        value['semantic_constraints'] = [f'Only write the synthetic output for {task}; preserve other tasks.']
        value['source_assertion'].update(sha256=digest(self.tasks_bytes),
                                         excerpt=self.dependency_table, owner_ref=self.identities['owner'])
        value['approval'].update(owner_ref=self.identities['owner'], evidence_ref=self.identities['approval'])
        value['approval']['context_digest'] = context_digest(value)
        return value

    def complete_prerequisite(self):
        (self.root/'guide.md').write_text('Synthetic guide\n', encoding='utf-8')
        (self.feature/'tasks.md').write_bytes(self.tasks_bytes.replace(b'[ ] T001', b'[x] T001'))
        self.git('add', '.')
        self.git('commit', '-m', 'test: synthetic prerequisite implementation')
        value = self.template('prerequisite.json')
        value['implementation_commit'] = self.git('rev-parse', 'HEAD').stdout.strip()
        value['content_manifest'] = self.manifest(['guide.md', 'specs/synthetic/tasks.md'], 'T001')
        check = ['git', 'diff', '--check']
        result = self.git(*check[1:])
        value['check_results'] = [{'id': 'local-diff', 'argv': check, 'cwd': '.',
                                   'exit': result.returncode,
                                   'content_manifest_digest': digest(canonical_bytes(value['content_manifest']))}]
        value['evidence_ref'] = self.identities['prerequisite']
        return value

    def review(self, context):
        value = self.template('review.json')
        manifest = self.manifest(context['allowed_paths'] + ['specs/synthetic/tasks.md'], context['task_id'])
        value.update(attempt_id=self.identities['attempt'], task_id=context['task_id'],
                     context_digest=context['approval']['context_digest'],
                     content_manifest_digest=digest(canonical_bytes(manifest)),
                     reviewer_ref=self.identities['reviewer'], evidence_ref=self.identities['review'])
        return value


class SingleTaskFixtureTests(unittest.TestCase):
    def fixture(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        return SingleTaskFixture(tmp.name)

    def test_two_task_feature(self):
        fixture = self.fixture()
        tasks = (fixture.feature/'tasks.md').read_text(encoding='utf-8')
        ids = re.findall(r'^- \[ \] (T\d{3}) ', tasks, re.MULTILINE)
        self.assertEqual(['T001', 'T002'], ids)
        rows = re.findall(r'^\| (T\d{3}) \| (.*?) \|$', tasks, re.MULTILINE)
        self.assertEqual([('T001', 'none'), ('T002', 'T001')], rows)
        for name in ('spec.md', 'plan.md', 'tasks.md'):
            self.assertEqual((FIXTURES/name).read_bytes(), (fixture.feature/name).read_bytes())
        self.assertEqual([], fixture.context()['dependencies'])
        self.assertEqual([], fixture.context()['prerequisite_evidence'])
        self.assertEqual('', fixture.git('status', '--porcelain').stdout)

    def test_runtime_context_prerequisite_review(self):
        fixture = self.fixture()
        initial = fixture.context()
        prerequisite = fixture.complete_prerequisite()
        context = fixture.context('T002', prerequisite)
        (fixture.root/'followup.md').write_text('Synthetic followup\n', encoding='utf-8')
        review = fixture.review(context)
        self.assertEqual('implement', context['mode'])
        self.assertEqual('T001', initial['task_id'])
        self.assertEqual(['T001'], context['dependencies'])
        self.assertEqual([prerequisite], context['prerequisite_evidence'])
        self.assertNotEqual(fixture.baseline, prerequisite['implementation_commit'])
        self.assertEqual('', fixture.git('diff', fixture.baseline, '--',
                                         'specs/synthetic/spec.md', 'specs/synthetic/plan.md').stdout)
        for artifact in context['reviewed_artifacts'].values():
            raw = fixture.git('show', artifact['commit'] + ':' + artifact['path']).stdout.encode('utf-8')
            self.assertEqual(digest(raw), artifact['sha256'])
            self.assertEqual(fixture.baseline, artifact['commit'])
        self.assertEqual(digest(fixture.tasks_bytes), context['source_assertion']['sha256'])
        self.assertEqual(fixture.dependency_table, context['source_assertion']['excerpt'])
        self.assertEqual(context_digest(context), review['context_digest'])
        self.assertEqual(fixture.identities['attempt'], review['attempt_id'])
        self.assertEqual(context['task_id'], review['task_id'])
        self.assertTrue(review['approved'])
        manifest = fixture.manifest(context['allowed_paths'] + ['specs/synthetic/tasks.md'], 'T002')
        self.assertEqual(digest(canonical_bytes(manifest)), review['content_manifest_digest'])
        for entry in prerequisite['content_manifest']['entries']:
            raw = fixture.git('show', prerequisite['implementation_commit'] + ':' + entry['path']).stdout.encode('utf-8')
            if entry['path'].endswith('/tasks.md'):
                self.assertIn(b'[x] T001', raw)
                self.assertIn(b'[ ] T002', raw)
                raw = raw.replace(b'[x] T001', b'[ ] T001')
            else:
                self.assertEqual(b'Synthetic guide\n', raw)
            self.assertEqual(digest(raw), entry['sha256'])
            mode = fixture.git('ls-tree', prerequisite['implementation_commit'], '--', entry['path']).stdout.split()[0]
            self.assertEqual(mode, entry['mode'])
        self.assertTrue(prerequisite['completed'])
        self.assertEqual('T001', prerequisite['task_id'])
        self.assertEqual(context['feature'], prerequisite['feature'])
        self.assertEqual(0, prerequisite['check_results'][0]['exit'])
        self.assertEqual(digest(canonical_bytes(prerequisite['content_manifest'])),
                         prerequisite['check_results'][0]['content_manifest_digest'])
        self.assertTrue(all(value.startswith('synthetic:') for value in fixture.identities.values()))
        self.assertNotIn('${', json.dumps([context, prerequisite, review]))
        original = review['content_manifest_digest']
        (fixture.root/'followup.md').write_text('Changed\n', encoding='utf-8')
        self.assertNotEqual(original, fixture.review(context)['content_manifest_digest'])
        context['allowed_paths'].append('changed.md')
        self.assertNotEqual(review['context_digest'], context_digest(context))

    def test_git_configuration_hooks_and_signing_are_isolated(self):
        with tempfile.TemporaryDirectory() as directory:
            hostile = Path(directory)
            hooks = hostile/'hooks'
            hooks.mkdir()
            marker = hostile/'hook-ran'
            hook = hooks/'pre-commit'
            hook.write_text('#!/bin/sh\ntouch "' + str(marker) + '"\nexit 99\n', encoding='utf-8')
            hook.chmod(0o755)
            config = hostile/'gitconfig'
            config.write_text(f'[core]\n hooksPath = {hooks}\n[commit]\n gpgSign = true\n'
                              '[gpg]\n program = /nonexistent/synthetic-signer\n[user]\n'
                              ' name = Hostile\n email = hostile@example.invalid\n', encoding='utf-8')
            env = {'HOME': str(hostile), 'XDG_CONFIG_HOME': str(hostile),
                   'GIT_CONFIG_GLOBAL': str(config), 'GIT_CONFIG_SYSTEM': str(config),
                   'GIT_TEMPLATE_DIR': str(hostile), 'GIT_CONFIG_COUNT': '1',
                   'GIT_CONFIG_KEY_0': 'commit.gpgSign', 'GIT_CONFIG_VALUE_0': 'true',
                   'GIT_AUTHOR_NAME': 'Hostile', 'GIT_DIR': str(hostile/'wrong.git')}
            with mock.patch.dict(os.environ, env):
                fixture = self.fixture()
                prerequisite = fixture.complete_prerequisite()
                self.assertEqual('false', fixture.git('config', '--get', 'commit.gpgSign').stdout.strip())
                self.assertEqual(str(fixture.hooks), fixture.git('config', '--get', 'core.hooksPath').stdout.strip())
                self.assertEqual('', fixture.git('config', '--global', '--list').stdout)
                self.assertEqual('', fixture.git('config', '--system', '--list').stdout)
                commit = fixture.git('show', '--format=raw', '--no-patch', prerequisite['implementation_commit']).stdout
                self.assertIn('Synthetic Fixture <fixture@example.invalid>', commit)
                self.assertNotIn('gpgsig', commit)
                self.assertNotIn('Hostile', commit)
                self.assertFalse(marker.exists())
                self.assertEqual(env['GIT_CONFIG_GLOBAL'], os.environ['GIT_CONFIG_GLOBAL'])
            self.assertTrue(hook.is_file())

    def test_tracker_credentials_are_removed_without_logging(self):
        credentials = {key: 'synthetic-secret-' + uuid.uuid4().hex for key in (
            'GITHUB_TOKEN', 'GH_TOKEN', 'LINEAR_API_KEY', 'SYMPHONY_API_KEY',
            'WORKHARBOR_TRACKER_TOKEN', 'UNRECOGNIZED_TRACKER_SECRET')}
        real_run = subprocess.run
        seen = []

        def capture(argv, **kwargs):
            seen.append(copy.deepcopy(kwargs['env']))
            self.assertFalse(set(credentials) & set(kwargs['env']))
            for value in credentials.values():
                self.assertNotIn(value, json.dumps([argv, kwargs['env']]))
            return real_run(argv, **kwargs)

        with mock.patch.dict(os.environ, credentials):
            parent = dict(os.environ)
            with mock.patch('subprocess.run', side_effect=capture):
                fixture = self.fixture()
                prerequisite = fixture.complete_prerequisite()
                context = fixture.context('T002', prerequisite)
                review = fixture.review(context)
            self.assertEqual(parent, dict(os.environ))
            self.assertGreater(len(seen), 5)
            evidence = json.dumps([fixture.commands, fixture.outputs, context, prerequisite, review])
            for key, value in credentials.items():
                self.assertNotIn(key, evidence)
                self.assertNotIn(value, evidence)

    def test_fixture_creation_rejects_network(self):
        real_popen = subprocess.Popen
        allowed = []
        rejected = []

        def deny(*args, **kwargs):
            rejected.append('python-network')
            raise RuntimeError('Synthetic test denies network')

        def local_only(argv, *args, **kwargs):
            private = Path(kwargs.get('env', {}).get('HOME', '/nonexistent'))
            prefix = [GIT, '-c', f'core.hooksPath={private / "empty-hooks"}',
                      '-c', 'commit.gpgSign=false', '-c', 'tag.gpgSign=false',
                      '-c', 'core.fsmonitor=false', '-c', 'credential.helper=']
            command = argv[len(prefix)] if isinstance(argv, list) and len(argv) > len(prefix) else None
            # Inspect every process launch, including subprocess.run's Popen.
            if (not isinstance(argv, list) or argv[:len(prefix)] != prefix or kwargs.get('shell')
                    or command not in {'init', 'add', 'commit', 'rev-parse', 'diff'}):
                rejected.append('subprocess-network')
                raise RuntimeError('Synthetic test denies network-capable subprocess')
            allowed.append(argv)
            return real_popen(argv, *args, **kwargs)

        with ExitStack() as stack:
            for target in ('socket.socket', 'socket.create_connection', 'socket.getaddrinfo',
                           'urllib.request.urlopen', 'http.client.HTTPConnection.connect',
                           'http.client.HTTPSConnection.connect', 'os.system', 'os.popen',
                           'os.execv', 'os.execve', 'os.spawnv', 'os.spawnve'):
                stack.enter_context(mock.patch(target, side_effect=deny))
            stack.enter_context(mock.patch('subprocess.Popen', side_effect=local_only))
            fixture = self.fixture()
            prerequisite = fixture.complete_prerequisite()
            fixture.review(fixture.context('T002', prerequisite))
            self.assertEqual([], rejected)
            self.assertGreater(len(allowed), 5)
            with self.assertRaisesRegex(RuntimeError, 'denies network'):
                socket.create_connection(('example.invalid', 443))
            for argv in (['curl', 'https://example.invalid'], ['git', 'fetch'],
                         [GIT, 'clone', 'https://example.invalid/repo'],
                         [sys.executable, '-c', 'pass']):
                with self.assertRaisesRegex(RuntimeError, 'denies network-capable'):
                    subprocess.run(argv, capture_output=True)
            with self.assertRaisesRegex(RuntimeError, 'local Git'):
                fixture.git('fetch', 'https://example.invalid/repo')
            self.assertEqual(['python-network'] + ['subprocess-network'] * 4, rejected)

    def test_protected_features_remain_unchanged(self):
        repo = SCRIPT.parent.parent
        fixture = self.fixture()
        self.assertFalse(fixture.root.is_relative_to(repo))
        prefixes = ('specs/001-workspace-lifecycle', 'specs/002-symphony-doc-smoke')
        state = repo/'.symphony/single-task.json'
        baseline = json.loads(state.read_text())['baseline'] if state.exists() else 'HEAD'
        # Read-only inventory of the source: include tracked bytes and directory
        # contents so additions/deletions are visible, not just HEAD tree objects.
        def inventory():
            values = {}
            for prefix in prefixes:
                root = repo/prefix
                self.assertTrue(root.is_dir())
                for path in sorted(root.rglob('*')):
                    self.assertFalse(path.is_symlink())
                    if path.is_file():
                        values[path.relative_to(repo).as_posix()] = path.read_bytes()
            return values

        def git(*args):
            return subprocess.check_output(
                [GIT, '-c', 'core.fsmonitor=false', *args], cwd=repo,
                env=fixture.env, timeout=10)

        before = inventory()
        trees = {prefix: git('rev-parse', baseline + ':' + prefix) for prefix in prefixes}
        baseline_files = git('ls-tree', '-r', '--name-only', baseline, '--', *prefixes).decode().splitlines()
        self.assertEqual(set(baseline_files), set(before))
        for path, content in before.items():
            self.assertEqual(git('show', baseline + ':' + path), content)
        fixture.complete_prerequisite()
        # Deliberate mutation is confined to disposable copies of protected data.
        for prefix in prefixes:
            shutil.copytree(repo/prefix, fixture.root/prefix)
            copy_spec = fixture.root/prefix/'spec.md'
            copy_spec.write_text('Disposable mutation\n', encoding='utf-8')
            self.assertNotEqual((repo/prefix/'spec.md').read_bytes(), copy_spec.read_bytes())
        self.assertEqual(before, inventory())
        for prefix, tree in trees.items():
            self.assertEqual(tree, git('rev-parse', 'HEAD:' + prefix))
        self.assertEqual(b'', git('diff', baseline, '--', *prefixes))
        self.assertEqual(b'', git('diff', '--cached', baseline, '--', *prefixes))


class SingleTaskTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.feature = self.root/'specs/demo'
        self.feature.mkdir(parents=True)
        for name in ('spec.md', 'plan.md'):
            (self.feature/name).write_text('Reviewed artifact\n')
        self.tasks = '- [ ] T001 Document guide in guide.md\n- [ ] T002 Other task in other.md\n'
        (self.feature/'tasks.md').write_text(self.tasks)
        (self.root/'.gitignore').write_text('.symphony/\n')
        (self.root/'guide.md').write_text('Before\n')
        self.git('init', '-b', 'main')
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'test: baseline')

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.root, capture_output=True, check=True)

    def cli(self, *args, ok=True):
        run = subprocess.run([sys.executable, str(SCRIPT), *args], cwd=self.root, capture_output=True, text=True)
        if ok:
            self.assertEqual(0, run.returncode, run.stderr)
        else:
            self.assertNotEqual(0, run.returncode)
        return run

    def begin(self, check=None):
        self.cli('begin', '--feature', 'demo', '--task', 'T001', '--allow', 'guide.md',
                 '--check', json.dumps(check or [sys.executable, '-c', 'pass']))

    def test_complete_and_retry_preserve_other_task(self):
        self.begin()
        (self.root/'guide.md').write_text('After\n')
        self.cli('complete')
        expected = self.tasks.replace('[ ] T001', '[x] T001')
        self.assertEqual(expected, (self.feature/'tasks.md').read_text())
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'docs: guide')
        self.cli('verify')
        self.cli('complete')
        self.assertEqual(expected, (self.feature/'tasks.md').read_text())
        self.cli('begin', '--feature', 'demo', '--task', 'T002', '--allow', 'guide.md',
                 '--check', '["true"]', ok=False)
        (self.root/'guide.md').write_text('Unauthorized rework\n')
        self.assertIn('rework', self.cli('verify', ok=False).stderr)

    def test_interruption_after_checkbox_before_phase_update(self):
        self.begin()
        (self.root/'guide.md').write_text('After\n')
        self.cli('verify')
        (self.feature/'tasks.md').write_text(self.tasks.replace('[ ] T001', '[x] T001'))
        state = json.loads((self.root/'.symphony/single-task.json').read_text())
        self.assertEqual('implement', state['phase'])
        self.cli('complete')
        state = json.loads((self.root/'.symphony/single-task.json').read_text())
        self.assertEqual('handoff-only', state['phase'])

    def test_reject_outside_scope_committed_and_untracked(self):
        self.begin()
        (self.root/'outside.md').write_text('Unexpected\n')
        self.assertIn('Out-of-scope', self.cli('verify', ok=False).stderr)
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'docs: unexpected')
        self.assertIn('Out-of-scope', self.cli('verify', ok=False).stderr)

    def test_reject_other_checkbox_and_selected_description(self):
        self.begin()
        path = self.feature/'tasks.md'
        for content in (self.tasks.replace('[ ] T002', '[x] T002'), self.tasks.replace('Document guide', 'Changed scope')):
            path.write_text(content)
            self.assertIn('checkbox', self.cli('verify', ok=False).stderr)

    def test_staged_violation_hidden_by_worktree_restore(self):
        self.begin()
        path = self.feature/'spec.md'
        original = path.read_bytes()
        path.write_text('Unauthorized staged change\n')
        self.git('add', 'specs/demo/spec.md')
        path.write_bytes(original)
        self.assertIn('Out-of-scope', self.cli('complete', ok=False).stderr)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_committed_violation_hidden_by_revert(self):
        self.begin()
        path = self.feature/'spec.md'
        original = path.read_bytes()
        for content in (b'Unauthorized committed change\n', original):
            path.write_bytes(content)
            self.git('add', '.')
            self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                     'commit', '-m', 'test: change then revert')
        self.assertIn('Out-of-scope', self.cli('complete', ok=False).stderr)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_staged_other_task_hidden_by_worktree_restore(self):
        self.begin()
        path = self.feature/'tasks.md'
        path.write_text(self.tasks.replace('[ ] T002', '[x] T002'))
        self.git('add', 'specs/demo/tasks.md')
        path.write_text(self.tasks)
        self.assertIn('checkbox', self.cli('complete', ok=False).stderr)
        self.assertEqual(self.tasks, path.read_text())

    def test_historical_other_task_hidden_by_revert(self):
        self.begin()
        path = self.feature/'tasks.md'
        for content in (self.tasks.replace('Other task', 'Unauthorized scope'), self.tasks):
            path.write_text(content)
            self.git('add', '.')
            self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                     'commit', '-m', 'test: task change then revert')
        self.assertIn('checkbox', self.cli('complete', ok=False).stderr)
        self.assertEqual(self.tasks, path.read_text())

    def test_mode_change_requires_rework_even_without_git_filemode(self):
        self.begin()
        self.cli('complete')
        self.git('config', 'core.filemode', 'false')
        (self.root/'guide.md').chmod(0o755)
        self.assertIn('rework', self.cli('verify', ok=False).stderr)

    def test_absent_file_differs_from_sentinel_contents(self):
        self.begin()
        (self.root/'guide.md').unlink()
        self.cli('complete')
        (self.root/'guide.md').write_bytes(b'<absent>')
        self.assertIn('rework', self.cli('verify', ok=False).stderr)

    def test_staged_symlink_hidden_by_worktree_restore(self):
        self.begin()
        path = self.root/'guide.md'
        path.unlink()
        path.symlink_to('specs/demo/spec.md')
        self.git('add', 'guide.md')
        path.unlink()
        path.write_text('Before\n')
        self.assertIn('Unsafe file type', self.cli('complete', ok=False).stderr)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_failed_acceptance_does_not_complete(self):
        self.begin([sys.executable, '-c', 'raise SystemExit(2)'])
        self.cli('complete', ok=False)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_early_completion_without_evidence_rejected(self):
        self.begin()
        (self.feature/'tasks.md').write_text(self.tasks.replace('[ ] T001', '[x] T001'))
        self.assertIn('evidence', self.cli('verify', ok=False).stderr)

    def test_acceptance_cannot_mutate_scope_or_complete(self):
        self.begin([sys.executable, '-c', "from pathlib import Path; Path('outside.md').write_text('unexpected')"])
        self.cli('complete', ok=False)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_dirty_checkout_and_unknown_task_rejected(self):
        self.cli('begin', '--feature', 'demo', '--task', 'T999', '--allow', 'guide.md', '--check', '["true"]', ok=False)
        (self.root/'guide.md').write_text('Existing work\n')
        run = self.cli('begin', '--feature', 'demo', '--task', 'T001', '--allow', 'guide.md', '--check', '["true"]', ok=False)
        self.assertIn('clean checkout', run.stderr)

    def test_traversal_and_symlink_rejected(self):
        self.cli('begin', '--feature', '../demo', '--task', 'T001', '--allow', 'guide.md', '--check', '["true"]', ok=False)
        self.cli('begin', '--feature', 'demo', '--task', 'T001', '--allow', '../outside', '--check', '["true"]', ok=False)
        self.begin()
        (self.root/'guide.md').unlink()
        (self.root/'guide.md').symlink_to(self.feature/'spec.md')
        self.cli('verify', ok=False)
