import json
import copy
import hashlib
import os
import re
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import unittest
import uuid
from contextlib import ExitStack, closing
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
        self.directory = Path(directory).resolve()
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
            'PYTHONDONTWRITEBYTECODE': '1',
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
            if path == self.feature.relative_to(self.root).as_posix() + '/tasks.md':
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
        manifest = self.manifest(context['allowed_paths'] + [f'specs/{context["feature"]}/tasks.md'], context['task_id'])
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
        self.fixture = SingleTaskFixture(self.tmp.name)
        self.root = self.fixture.root
        self.feature = self.root/'specs/demo'
        self.fixture.feature.rename(self.feature)
        self.fixture.feature = self.feature
        for name in ('spec.md', 'plan.md'):
            (self.feature/name).write_text('Reviewed artifact\n')
        self.tasks = ('- [ ] T001 Document guide in guide.md\n- [ ] T002 Other task in other.md\n'
                      '\n## Dependencies\n\n| Task | Prerequisites |\n| --- | --- |\n'
                      '| T001 | none |\n| T002 | T001 |\n')
        (self.feature/'tasks.md').write_text(self.tasks)
        (self.root/'.gitignore').write_text('.symphony/\n')
        (self.root/'guide.md').write_text('Before\n')
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'test: baseline')
        self.reviewed_commit = self.git('rev-parse', 'HEAD').stdout.decode().strip()
        self.reviewed_bytes = {name: (self.feature/name).read_bytes() for name in ('spec.md', 'plan.md', 'tasks.md')}
        self.attempt = None

    def git(self, *args):
        return subprocess.run(['git', '-c', f'core.hooksPath={self.fixture.hooks}',
                               '-c', 'core.fsmonitor=false', '-c', 'credential.helper=', *args],
                              cwd=self.root, env=self.fixture.env, capture_output=True, check=True, timeout=10)

    def cli(self, *args, ok=True):
        if args[0] in ('verify', 'complete') and self.attempt is not None:
            path = self.fixture.private/'review.json'
            paths = self.entry_context['allowed_paths'] + ['specs/demo/tasks.md']
            if all(not (self.root/item).is_symlink() and
                   (not (self.root/item).exists() or (self.root/item).is_file()) for item in paths):
                self.prepare_review()
            # Unsafe mutations use the legitimate review retained before the
            # mutation, so the CLI must reject the actual file-type change.
            args = (*args, '--attempt', self.attempt, '--review', str(path))
        run = subprocess.run([sys.executable, '-B', str(SCRIPT), *args], cwd=self.root,
                             env=self.fixture.env, capture_output=True, text=True, timeout=10)
        if ok:
            self.assertEqual(0, run.returncode, run.stderr)
        elif ok is False:
            self.assertNotEqual(0, run.returncode)
            if args[0] in ('verify', 'complete'):
                self.assertNotEqual('command_unavailable', json.loads(run.stdout)['errors'][0]['code'],
                                    'Retained completion/retry scenario requires T017/T023')
        return run

    def entry(self, check=None, **changes):
        context = self.fixture.context()
        context.update(feature='demo', reviewed_artifacts={name: {'path': f'specs/demo/{name}',
                       'commit': self.reviewed_commit, 'sha256': digest(content)}
                       for name, content in self.reviewed_bytes.items()})
        context['source_assertion'].update(path='specs/demo/tasks.md', sha256=digest(self.reviewed_bytes['tasks.md']),
                                           excerpt=self.tasks.split('\n\n')[-1].rstrip('\n'))
        context['checks'] = [{'id': 'local-check', 'argv': check or [sys.executable, '-c', 'pass'],
                              'cwd': '.', 'timeout_seconds': 10, 'expected_exit': 0}]
        context.update(changes)
        context['approval']['context_digest'] = context_digest(context)
        path = self.fixture.private/'entry-context.json'
        path.write_bytes(canonical_bytes(context))
        self.entry_context = context
        return path

    def begin(self, check=None):
        result = self.cli('begin', '--context', str(self.entry(check)))
        self.attempt = json.loads(result.stdout)['attempt_id']
        self.prepare_review()

    def prepare_review(self):
        review = self.fixture.review(self.entry_context)
        review['attempt_id'] = self.attempt
        (self.fixture.private/'review.json').write_bytes(canonical_bytes(review))

    def test_complete_and_retry_preserve_other_task(self):
        self.begin()
        (self.root/'guide.md').write_text('After\n')
        self.cli('complete')
        expected = self.tasks.replace('[ ] T001', '[x] T001')
        self.assertEqual(expected, (self.feature/'tasks.md').read_text())
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'docs: guide')
        bound = self.cli('record-commit', '--attempt', self.attempt, '--commit',
                         self.git('rev-parse', 'HEAD').stdout.decode().strip(), ok=None)
        self.assertEqual(0, bound.returncode, 'T023 must bind the same completed implementation commit')
        self.cli('verify')
        self.cli('complete')
        self.assertEqual(expected, (self.feature/'tasks.md').read_text())
        self.cli('begin', '--feature', 'demo', '--task', 'T002', '--allow', 'guide.md',
                 '--check', '["true"]', ok=False)
        (self.root/'guide.md').write_text('Unauthorized rework\n')
        self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])

    def test_interruption_after_checkbox_before_phase_update(self):
        self.begin()
        (self.root/'guide.md').write_text('After\n')
        self.cli('verify')
        (self.feature/'tasks.md').write_text(self.tasks.replace('[ ] T001', '[x] T001'))
        # Prepare a real interrupted intent using the same publication API;
        # recovery belongs to T021/T023 and must remain an explicit body blocker.
        sys.path.insert(0, str(SCRIPT.parent))
        import single_task_state as backend
        (self.feature/'tasks.md').write_text(self.tasks)
        flush = os.fsync
        def interrupt(fd):
            if stat.S_ISDIR(os.fstat(fd).st_mode) and b'[x] T001' in (self.feature/'tasks.md').read_bytes():
                raise OSError('Synthetic interrupted publication')
            return flush(fd)
        with backend.open_store(self.root, require_existing=True, writable_existing=True) as store:
            with mock.patch.object(backend.os, 'fsync', side_effect=interrupt):
                with self.assertRaises(backend.PersistenceFailure):
                    backend.publish_completion(self.root, store, self.attempt)
            self.assertEqual('completion-prepared', store.read_attempt(self.attempt)['progress'])
        completed = (self.feature/'tasks.md').read_bytes()
        result = self.cli('resume', '--attempt', self.attempt, '--review', str(self.fixture.private/'review.json'), ok=None)
        self.assertEqual(0, result.returncode, 'T023 must recover the same interrupted completion')
        self.assertEqual(completed, (self.feature/'tasks.md').read_bytes())
        self.assertEqual('handoff-only', json.loads(result.stdout)['mode'])

    def test_reject_outside_scope_committed_and_untracked(self):
        self.begin()
        (self.root/'outside.md').write_text('Unexpected\n')
        self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-m', 'docs: unexpected')
        self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])

    def test_reject_other_checkbox_and_selected_description(self):
        self.begin()
        path = self.feature/'tasks.md'
        for content in (self.tasks.replace('[ ] T002', '[x] T002'), self.tasks.replace('Document guide', 'Changed scope')):
            path.write_text(content)
            self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])

    def test_staged_violation_hidden_by_worktree_restore(self):
        self.begin()
        path = self.feature/'spec.md'
        original = path.read_bytes()
        path.write_text('Unauthorized staged change\n')
        self.git('add', 'specs/demo/spec.md')
        path.write_bytes(original)
        self.assertEqual('scope_blocker', json.loads(self.cli('complete', ok=False).stdout)['errors'][0]['code'])
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
        self.assertEqual('scope_blocker', json.loads(self.cli('complete', ok=False).stdout)['errors'][0]['code'])
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_staged_other_task_hidden_by_worktree_restore(self):
        self.begin()
        path = self.feature/'tasks.md'
        path.write_text(self.tasks.replace('[ ] T002', '[x] T002'))
        self.git('add', 'specs/demo/tasks.md')
        path.write_text(self.tasks)
        self.assertEqual('scope_blocker', json.loads(self.cli('complete', ok=False).stdout)['errors'][0]['code'])
        self.assertEqual(self.tasks, path.read_text())

    def test_historical_other_task_hidden_by_revert(self):
        self.begin()
        path = self.feature/'tasks.md'
        for content in (self.tasks.replace('Other task', 'Unauthorized scope'), self.tasks):
            path.write_text(content)
            self.git('add', '.')
            self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                     'commit', '-m', 'test: task change then revert')
        self.assertEqual('scope_blocker', json.loads(self.cli('complete', ok=False).stdout)['errors'][0]['code'])
        self.assertEqual(self.tasks, path.read_text())

    def test_mode_change_requires_rework_even_without_git_filemode(self):
        self.begin()
        self.cli('complete')
        self.git('config', 'core.filemode', 'false')
        (self.root/'guide.md').chmod(0o755)
        self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])

    def test_absent_file_differs_from_sentinel_contents(self):
        self.begin()
        (self.root/'guide.md').unlink()
        self.cli('complete')
        (self.root/'guide.md').write_bytes(b'<absent>')
        self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])

    def test_staged_symlink_hidden_by_worktree_restore(self):
        self.begin()
        path = self.root/'guide.md'
        path.unlink()
        path.symlink_to('specs/demo/spec.md')
        self.git('add', 'guide.md')
        path.unlink()
        path.write_text('Before\n')
        self.assertEqual('scope_blocker', json.loads(self.cli('complete', ok=False).stdout)['errors'][0]['code'])
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_failed_acceptance_does_not_complete(self):
        self.begin([sys.executable, '-c', 'raise SystemExit(2)'])
        self.cli('complete', ok=False)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_early_completion_without_evidence_rejected(self):
        self.begin()
        (self.feature/'tasks.md').write_text(self.tasks.replace('[ ] T001', '[x] T001'))
        self.assertEqual('scope_blocker', json.loads(self.cli('verify', ok=False).stdout)['errors'][0]['code'])

    def test_acceptance_cannot_mutate_scope_or_complete(self):
        self.begin([sys.executable, '-c', "from pathlib import Path; Path('outside.md').write_text('unexpected')"])
        self.cli('complete', ok=False)
        self.assertEqual(self.tasks, (self.feature/'tasks.md').read_text())

    def test_dirty_checkout_and_unknown_task_rejected(self):
        result = self.cli('begin', '--context', str(self.entry(task_id='T999')), ok=False)
        self.assertEqual('scope_context', json.loads(result.stdout)['errors'][0]['code'])
        (self.root/'guide.md').write_text('Existing work\n')
        run = self.cli('begin', '--context', str(self.entry()), ok=False)
        self.assertEqual('dirty_baseline', json.loads(run.stdout)['errors'][0]['code'])

    def test_traversal_and_symlink_rejected(self):
        self.cli('begin', '--context', str(self.entry(feature='../demo')), ok=False)
        self.cli('begin', '--context', str(self.entry(allowed_paths=['../outside'])), ok=False)
        self.begin()
        (self.root/'guide.md').unlink()
        (self.root/'guide.md').symlink_to(self.feature/'spec.md')
        self.cli('verify', ok=False)


class SingleTaskAcceptanceTests(unittest.TestCase):
    """Proposed T012 public CLI contracts; no task entry/completion authority."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.fixture = SingleTaskFixture(temporary.name)
        self.root = self.fixture.root
        self.private = self.fixture.private
        self.tasks = self.fixture.feature/'tasks.md'
        (self.root/'.gitignore').write_text('.symphony/\n', encoding='utf-8')
        (self.root/'checks').mkdir()
        (self.root/'checks/.keep').write_bytes(b'')
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: prepare acceptance fixture')
        baseline = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        for artifact in self.fixture.reviewed_artifacts.values():
            artifact['commit'] = baseline
        self.context = self.fixture.context()
        self.attempt = None
        self.trace = self.private/'trace.jsonl'
        self.review_path = self.private/'review.json'

    def cli(self, command, *args):
        run = subprocess.run([sys.executable, '-B', str(SCRIPT), command, *args],
                             cwd=self.root, env=self.fixture.env, capture_output=True,
                             text=True, timeout=15)
        value = json.loads(run.stdout)
        self.assertEqual(1, value['schema_version'])
        self.assertIs(False, value['owner_identity_authenticated'])
        self.assertIs(False, value['live_dispatch_verified'])
        self.assertIsInstance(value['errors'], list)
        return run, value

    def expect(self, run, exit_code):
        self.assertEqual(exit_code, run.returncode,
                         'T012 ' + self._testMethodName + ' requires its public CLI contract')

    def check(self, code='pass', *, check_id='local-check', cwd='.', timeout=2):
        return {'id': check_id, 'argv': [sys.executable, '-B', '-c', code],
                'cwd': cwd, 'timeout_seconds': timeout, 'expected_exit': 0}

    def begin(self, checks=None, allowed=None):
        if checks is not None:
            self.context['checks'] = checks
        if allowed is not None:
            self.context['allowed_paths'] = allowed
        self.context['approval']['context_digest'] = context_digest(self.context)
        path = self.private/'context.json'
        path.write_bytes(canonical_bytes(self.context))
        run, value = self.cli('begin', '--context', str(path))
        self.assertEqual(0, run.returncode, 'T012 fixture entry must be valid before behavior assertions')
        self.attempt = value['attempt_id']
        self.assertTrue(self.attempt)
        self.assertEqual('implement', value['mode'])
        self.assertEqual('active', value['progress'])
        self.before_tasks = self.tasks.read_bytes()
        self.before_attempt, self.before_events = self.journal()
        self.review()

    def journal(self):
        import sqlite3
        with closing(sqlite3.connect(self.root/'.symphony/single-task/journal.sqlite3')) as db:
            attempts = db.execute('SELECT * FROM attempts ORDER BY attempt_id').fetchall()
            events = db.execute('SELECT * FROM events ORDER BY attempt_id,sequence').fetchall()
        return attempts, events

    def content(self):
        values = {}
        for path in self.context['allowed_paths'] + ['specs/synthetic/tasks.md']:
            file = self.root/path
            values[path] = None if not file.exists() else (file.read_bytes(), file.stat().st_mode & 0o777)
        return values

    def review(self, **changes):
        value = self.fixture.review(self.context)
        value['attempt_id'] = self.attempt
        value.update(changes)
        self.review_path.write_bytes(canonical_bytes(value))
        return value

    def invoke(self, command='verify', review=True):
        args = ['--attempt', self.attempt]
        if review:
            args += ['--review', str(self.review_path)]
        return self.cli(command, *args)

    def blocked(self, exit_code=3, command='verify'):
        before = self.tasks.read_bytes()
        run, value = self.invoke(command)
        self.assertEqual(before, self.tasks.read_bytes())
        self.assertFalse(self.trace.exists())
        attempts, events = self.journal()
        self.assertEqual(self.before_attempt, attempts)
        self.assertEqual(self.before_events, events[:len(self.before_events)])
        self.assertNotEqual('handoff-only', value['mode'])
        self.assertNotIn('completed', str(value['progress']))
        self.assertNotEqual('passed', value['local_verification'])
        self.expect(run, exit_code)
        self.assertTrue(value['errors'])
        self.assertNotEqual('command_unavailable', value['errors'][0]['code'])
        return run, value

    def tracing_check(self, token='check', *, cwd='.', extra=''):
        code = ('import json,os,sys; from pathlib import Path; '
                f'p=Path({str(self.trace)!r}); '
                f'p.open("a").write(json.dumps([{token!r},os.getcwd(),sys.argv[1:]])+"\\n"); ' + extra)
        return self.check(code, check_id=token, cwd=cwd)

    def trace_rows(self):
        return [json.loads(row) for row in self.trace.read_text().splitlines()]

    def test_inspect_identities(self):
        self.begin()
        (self.root/'guide.md').write_bytes(b'Permitted change\n')
        run, value = self.invoke('inspect', review=False)
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 0)
        self.assertEqual(self.attempt, value['attempt_id'])
        self.assertEqual('T001', value['task_id'])
        self.assertEqual(context_digest(self.context), value['context_digest'])
        expected = self.fixture.manifest(['guide.md', 'specs/synthetic/tasks.md'], 'T001')
        self.assertEqual(expected, value['content_manifest'])
        self.assertEqual(digest(canonical_bytes(expected)), value['content_manifest_digest'])
        self.assertNotEqual('passed', value['local_verification'])

    def test_inspect_canonical_manifest(self):
        # The expected digest uses literal independently assembled entries/bytes.
        self.fixture.git('config', 'core.precomposeUnicode', 'false')
        paths = ['a\u0301.md', 'é.bin', '脚本.sh', 'missing.md']
        for path, content in zip(paths, (b'Combining\n', b'\x00\xff\n', b'#!/bin/sh\n')):
            (self.root/path).write_bytes(content)
        (self.root/'脚本.sh').chmod(0o755)
        self.tasks.write_bytes(self.tasks.read_bytes().replace(b'\n', b'\r\n'))
        self.fixture.git('add', '.')
        self.fixture.git('commit', '-m', 'test: canonical byte fixture')
        revision = self.fixture.git('rev-parse', 'HEAD').stdout.strip()
        for name, artifact in self.context['reviewed_artifacts'].items():
            artifact.update(commit=revision, sha256=digest((self.fixture.feature/name).read_bytes()))
        self.context['source_assertion']['sha256'] = digest(self.tasks.read_bytes())
        self.begin(allowed=paths)
        run, value = self.invoke('inspect', review=False)
        self.expect(run, 0)
        expected = {'schema_version': 1, 'entries': [
            {'path': path, 'type': 'absent' if path == 'missing.md' else 'regular',
             'mode': None if path == 'missing.md' else '100755' if path == '脚本.sh' else '100644',
             'sha256': None if path == 'missing.md' else digest((self.root/path).read_bytes())}
            for path in sorted(paths + ['specs/synthetic/tasks.md'])]}
        encoded = json.dumps(expected, sort_keys=True, separators=(',', ':'),
                             ensure_ascii=False, allow_nan=False).encode('utf-8')
        self.assertEqual(expected, value['content_manifest'])
        self.assertEqual(hashlib.sha256(encoded).hexdigest(), value['content_manifest_digest'])
        self.assertIn('a\u0301.md'.encode(), encoded)
        self.assertIn(b'\r\n', self.tasks.read_bytes())

    def test_inspect_does_not_execute_checks(self):
        self.begin([self.tracing_check()])
        run, value = self.invoke('inspect', review=False)
        self.assertFalse(self.trace.exists())
        self.assertEqual(self.before_events, self.journal()[1])
        self.expect(run, 0)
        self.assertNotEqual('passed', value['local_verification'])

    def test_inspect_preserves_content_and_events(self):
        self.begin()
        (self.root/'guide.md').write_bytes(b'Edited\n')
        content, journal = self.content(), self.journal()
        index = (self.root/'.git/index').read_bytes()
        run, _ = self.invoke('inspect', review=False)
        self.assertEqual(content, self.content())
        self.assertEqual(journal, self.journal())
        self.assertEqual(index, (self.root/'.git/index').read_bytes())
        self.expect(run, 0)

    def test_inspect_reflects_current_content(self):
        self.begin()
        run, first = self.invoke('inspect', review=False)
        self.expect(run, 0)
        (self.root/'guide.md').write_bytes(b'Second snapshot\n')
        run, second = self.invoke('inspect', review=False)
        self.expect(run, 0)
        self.assertNotEqual(first['content_manifest_digest'], second['content_manifest_digest'])
        expected = self.fixture.manifest(['guide.md', 'specs/synthetic/tasks.md'], 'T001')
        self.assertEqual(expected, second['content_manifest'])

    def test_inspect_rejects_invalid_scope(self):
        self.begin([self.tracing_check()])
        (self.root/'outside.md').write_bytes(b'Outside\n')
        run, value = self.invoke('inspect', review=False)
        self.assertFalse(self.trace.exists())
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 3)
        self.assertIn('outside.md', value['errors'][0]['affected_paths'])

    def test_inspect_respects_worktree_lock(self):
        import selectors
        self.begin([self.tracing_check()])
        lock = self.root/'.symphony/single-task/lock'
        code = ('import fcntl,sys; f=open(sys.argv[1],"r+"); '
                'fcntl.flock(f,fcntl.LOCK_EX); print("ready",flush=True); sys.stdin.read()')
        child = subprocess.Popen([sys.executable, '-B', '-c', code, str(lock)],
                                 env=self.fixture.env, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE, text=True)
        try:
            with selectors.DefaultSelector() as ready:
                ready.register(child.stdout, selectors.EVENT_READ)
                self.assertTrue(ready.select(5), 'Lock holder did not signal readiness')
                self.assertEqual('ready\n', child.stdout.readline())
            run, value = self.invoke('inspect', review=False)
            self.assertFalse(self.trace.exists())
            self.assertEqual(self.before_tasks, self.tasks.read_bytes())
            self.assertEqual((self.before_attempt, self.before_events), self.journal())
            self.expect(run, 5)
            self.assertEqual('concurrent_attempt', value['errors'][0]['code'])
        finally:
            child.communicate('', timeout=5)

    def test_acceptance_ordered_argv(self):
        self.begin([self.tracing_check('first'), self.tracing_check('second')])
        run, _ = self.invoke()
        self.expect(run, 0)
        self.assertEqual(['first', 'second'], [row[0] for row in self.trace_rows()])
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())

    def test_acceptance_literal_metacharacters(self):
        check = self.tracing_check()
        literal = ['space argument', '"quoted"', ';', '$(touch shell-sentinel)', '`touch shell-sentinel`']
        check['argv'] += literal
        self.begin([check])
        run, _ = self.invoke()
        self.expect(run, 0)
        self.assertEqual(literal, self.trace_rows()[0][2])
        self.assertFalse((self.root/'shell-sentinel').exists())

    def test_acceptance_validated_cwd(self):
        self.begin([self.tracing_check(cwd='checks')])
        run, _ = self.invoke()
        self.expect(run, 0)
        self.assertEqual(str(self.root/'checks'), self.trace_rows()[0][1])

    def test_acceptance_missing_executable(self):
        check = self.check()
        check['argv'] = [str(self.private/'definitely-missing-executable')]
        self.begin([check])
        self.blocked(4, 'complete')

    def test_acceptance_nonzero_exit(self):
        self.begin([self.check('raise SystemExit(17)')])
        self.blocked(4, 'complete')

    def test_acceptance_timeout(self):
        self.begin([self.check('import time; time.sleep(30)', timeout=0.1)])
        run, value = self.blocked(4, 'complete')
        self.assertIn('timeout', json.dumps(value).lower())

    def test_acceptance_missing_required_check(self):
        missing = self.check(check_id='missing')
        missing['argv'] = [str(self.private/'missing-second-executable')]
        self.begin([self.tracing_check('first'), missing])
        run, _ = self.invoke('complete')
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 4)
        self.assertEqual(['first'], [row[0] for row in self.trace_rows()])

    def test_acceptance_does_not_reuse_old_success(self):
        control = self.private/'fail'
        self.begin([self.tracing_check(extra=f'raise SystemExit(17 if Path({str(control)!r}).exists() else 0)')])
        run, _ = self.invoke()
        self.expect(run, 0)
        control.write_bytes(b'fail')
        run, _ = self.invoke('complete')
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 4)
        self.assertEqual(2, len(self.trace_rows()))

    def test_acceptance_safe_failure_evidence(self):
        secret = 'synthetic-output-secret-' + uuid.uuid4().hex
        output = self.private/'synthetic-output.txt'
        output.write_text(secret, encoding='utf-8')
        code = (f'from pathlib import Path; import sys; s=Path({str(output)!r}).read_text(); '
                'print(s); print(s,file=sys.stderr); raise SystemExit(17)')
        self.begin([self.check(code)])
        run, _ = self.invoke('complete')
        # Output-specific data lives outside the reviewed argv/context.
        self.assertNotIn(secret, run.stdout + run.stderr)
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 4)
        for path in (self.root/'.symphony/single-task').rglob('*'):
            if path.is_file() and path.name != 'lock':
                self.assertNotIn(secret.encode(), path.read_bytes())

    def test_acceptance_local_only_envelope(self):
        self.begin()
        run, value = self.invoke()
        self.expect(run, 0)
        self.assertEqual('passed', value['local_verification'])
        self.assertEqual('implement', value['mode'])
        self.assertEqual([], value['errors'])
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())

    def test_review_duplicate_keys_rejected(self):
        self.begin([self.tracing_check()])
        content = self.review_path.read_bytes()
        self.review_path.write_bytes(content.replace(b'"approved":true', b'"approved":false,"approved":true'))
        self.blocked()

    def test_review_nonfinite_json_rejected(self):
        self.begin([self.tracing_check()])
        self.review_path.write_bytes(self.review_path.read_bytes().replace(b'"schema_version":1', b'"schema_version":NaN'))
        self.blocked()

    def test_review_invalid_utf8_rejected(self):
        self.begin([self.tracing_check()])
        self.review_path.write_bytes(b'{"reviewer_ref":"\xff"}')
        self.blocked()

    def test_rejected_review_retains_safe_blocker(self):
        self.begin([self.tracing_check()])
        self.review(approved=False)
        self.blocked()
        with closing(__import__('sqlite3').connect(self.root/'.symphony/single-task/journal.sqlite3')) as db:
            kind, payload = db.execute('SELECT kind,payload FROM events ORDER BY sequence DESC LIMIT 1').fetchone()
        self.assertEqual('blocker', kind)
        self.assertEqual({'code': 'scope_blocker'}, json.loads(payload))

    def test_review_missing(self):
        self.begin([self.tracing_check()])
        run, value = self.invoke(review=False)
        self.assertFalse(self.trace.exists())
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 2)
        self.assertEqual('invalid_arguments', value['errors'][0]['code'])

    def test_review_not_approved(self):
        self.begin([self.tracing_check()])
        self.review(approved=False)
        self.blocked()

    def test_review_wrong_attempt(self):
        self.begin([self.tracing_check()])
        self.review(attempt_id='different-existing-identity')
        self.blocked()

    def test_review_wrong_task(self):
        self.begin([self.tracing_check()])
        self.review(task_id='T002')
        self.blocked()

    def test_review_wrong_context_digest(self):
        self.begin([self.tracing_check()])
        self.review(context_digest='0'*64)
        self.blocked()

    def test_review_wrong_manifest_digest(self):
        self.begin([self.tracing_check()])
        self.review(content_manifest_digest='0'*64)
        self.blocked()

    def test_review_stale_bytes(self):
        self.begin([self.tracing_check()])
        (self.root/'guide.md').write_bytes(b'Stale review\n')
        self.blocked()

    def test_review_stale_mode(self):
        self.begin([self.tracing_check()])
        self.fixture.git('config', 'core.filemode', 'false')
        (self.root/'guide.md').chmod(0o755)
        self.blocked()

    def test_review_stale_absence(self):
        self.begin([self.tracing_check()], allowed=['missing.md'])
        (self.root/'missing.md').write_bytes(b'<absent>')
        self.blocked()

    def mutated_check(self, code):
        self.begin([self.check(code)])
        run, value = self.invoke('complete')
        self.expect(run, 4)
        self.assertNotEqual('handoff-only', value['mode'])
        self.assertNotIn('completed', str(value['progress']))
        self.assertEqual(self.before_attempt, self.journal()[0])
        return value

    def test_check_mutates_allowed_content(self):
        self.mutated_check("from pathlib import Path; Path('guide.md').write_bytes(b'Changed during check')")
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.assertEqual(b'Changed during check', (self.root/'guide.md').read_bytes())

    def test_check_mutates_task_content(self):
        self.mutated_check("from pathlib import Path; p=Path('specs/synthetic/tasks.md'); p.write_bytes(p.read_bytes().replace(b'[ ] T002',b'[x] T002'))")
        self.assertEqual(self.before_tasks.replace(b'[ ] T002', b'[x] T002'), self.tasks.read_bytes())

    def test_check_mutates_outside_scope(self):
        self.mutated_check("from pathlib import Path; Path('outside.md').write_bytes(b'Outside')")
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.assertEqual(b'Outside', (self.root/'outside.md').read_bytes())

    def test_check_mutates_allowed_mode(self):
        self.fixture.git('config', 'core.filemode', 'false')
        self.mutated_check("from pathlib import Path; Path('guide.md').chmod(0o755)")
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.assertTrue((self.root/'guide.md').stat().st_mode & 0o111)

    def test_check_removes_allowed_content(self):
        self.mutated_check("from pathlib import Path; Path('guide.md').unlink()")
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.assertFalse((self.root/'guide.md').exists())

    def test_verify_preserves_pending_marker(self):
        self.begin([self.tracing_check()])
        run, value = self.invoke()
        self.assertEqual(self.before_tasks, self.tasks.read_bytes())
        self.expect(run, 0)
        self.assertEqual('implement', value['mode'])
        self.assertEqual(['check'], [row[0] for row in self.trace_rows()])
        self.assertGreater(len(self.journal()[1]), len(self.before_events))

    def test_complete_selected_checkbox_only(self):
        self.begin()
        (self.root/'guide.md').write_bytes(b'Accepted change\n')
        self.review()
        run, value = self.invoke('complete')
        self.expect(run, 0)
        self.assertEqual(self.before_tasks.replace(b'[ ] T001', b'[x] T001', 1), self.tasks.read_bytes())
        self.assertEqual('handoff-only', value['mode'])
        self.assertEqual('completed-awaiting-commit', value['progress'])

    def test_complete_reruns_after_verify(self):
        self.begin([self.tracing_check()])
        run, _ = self.invoke()
        self.expect(run, 0)
        run, _ = self.invoke('complete')
        self.expect(run, 0)
        self.assertEqual(['check', 'check'], [row[0] for row in self.trace_rows()])

    def test_complete_failed_check_preserves_marker(self):
        self.begin([self.check('raise SystemExit(9)')])
        self.blocked(4, 'complete')

    def test_complete_idempotent_same_attempt(self):
        self.begin()
        run, first = self.invoke('complete')
        self.expect(run, 0)
        expected = self.tasks.read_bytes()
        run, second = self.invoke('complete')
        self.expect(run, 0)
        self.assertEqual(expected, self.tasks.read_bytes())
        self.assertEqual(self.attempt, first['attempt_id'])
        self.assertEqual(self.attempt, second['attempt_id'])
        self.assertEqual(1, len(self.journal()[0]))
        self.assertEqual('handoff-only', second['mode'])
        self.assertEqual('completed-awaiting-commit', second['progress'])

    def test_complete_preserves_immutable_entry_mode(self):
        self.begin()
        run, _ = self.invoke('complete')
        self.expect(run, 0)
        attempts, events = self.journal()
        self.assertEqual(self.before_attempt, attempts)
        self.assertEqual(self.before_events, events[:len(self.before_events)])
        run, value = self.cli('status', '--attempt', self.attempt)
        self.expect(run, 0)
        self.assertEqual('handoff-only', value['mode'])
        self.assertEqual('completed-awaiting-commit', value['progress'])

    def test_inspect_after_completion_is_read_only(self):
        self.begin()
        review = self.review()
        run, _ = self.invoke('complete')
        self.expect(run, 0)
        content, journal = self.content(), self.journal()
        run, value = self.invoke('inspect', review=False)
        self.assertEqual(content, self.content())
        self.assertEqual(journal, self.journal())
        self.expect(run, 0)
        self.assertEqual(review['content_manifest_digest'], value['content_manifest_digest'])

    def test_complete_rejects_manual_marker(self):
        self.begin([self.tracing_check()])
        self.tasks.write_bytes(self.before_tasks.replace(b'[ ] T001', b'[x] T001', 1))
        self.review()
        self.blocked(3, 'complete')
