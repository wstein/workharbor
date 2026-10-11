"""Verify only the legacy bootstrap entry contract, not T001 implementation."""
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[3]
CONTRACT = json.loads((REPO/'docs/development/bootstrap-t001.json').read_text())
GUARD = REPO/'scripts/single_task.py'


class T001BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        # Isolate Git configuration, credentials, hooks and signing for this
        # mechanical entry test. No fixture helper implementation is assumed.
        self.env = dict(PATH=os.environ['PATH'], HOME=str(self.root),
                        GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_TERMINAL_PROMPT='0', PYTHONDONTWRITEBYTECODE='1',
                        GIT_AUTHOR_NAME='Bootstrap Test', GIT_COMMITTER_NAME='Bootstrap Test',
                        GIT_AUTHOR_EMAIL='test@example.invalid',
                        GIT_COMMITTER_EMAIL='test@example.invalid')
        feature = self.root/'specs'/CONTRACT['feature']
        feature.mkdir(parents=True)
        for name in ('spec.md', 'plan.md', 'tasks.md'):
            (feature/name).write_bytes((REPO/'specs'/CONTRACT['feature']/name).read_bytes())
        self.tasks_path = feature/'tasks.md'
        self.before = self.tasks_path.read_bytes()
        (self.root/'.gitignore').write_text('.symphony/\n')
        self.git('init', '-b', 'issue-2-bootstrap-verification')
        self.git('config', 'core.hooksPath', os.devnull)
        self.git('config', 'commit.gpgsign', 'false')
        self.git('add', '.')
        self.git('commit', '-m', 'test: disposable entry baseline')

    def git(self, *args):
        return subprocess.run(['git', *args], cwd=self.root, env=self.env,
                              capture_output=True, check=True)

    def cli(self, *args):
        return subprocess.run([sys.executable, str(GUARD), *args], cwd=self.root,
                              env=self.env, capture_output=True, text=True)

    def begin(self):
        argv = ['begin', '--feature', CONTRACT['feature'], '--task', CONTRACT['task']]
        for path in CONTRACT['allowed_paths']:
            argv.extend(['--allow', path])
        for check in CONTRACT['checks']:
            argv.extend(['--check', json.dumps(check)])
        result = self.cli(*argv)
        self.assertEqual(0, result.returncode, result.stderr)

    def state(self):
        return json.loads((self.root/'.symphony/single-task.json').read_text())

    def test_entry_records_exact_scope_and_checks_without_running_them(self):
        self.begin()
        state = self.state()
        self.assertEqual('003-single-task-entry', state['feature'])
        self.assertEqual('T001', state['task'])
        self.assertEqual(sorted(CONTRACT['allowed_paths']), state['allowed'])
        self.assertEqual(CONTRACT['checks'], state['checks'])
        self.assertEqual([], state['results'])
        self.assertEqual('implement', state['phase'])
        self.assertEqual(self.before, self.tasks_path.read_bytes())
        self.assertEqual(b'', self.git('status', '--porcelain').stdout)

    def test_missing_t001_fixtures_block_completion(self):
        self.begin()
        result = self.cli('complete')
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Missing regular fixture:', result.stderr)
        self.assertEqual(self.before, self.tasks_path.read_bytes())
        self.assertEqual('implement', self.state()['phase'])

    def test_pending_or_missing_named_tests_cannot_pass_acceptance(self):
        # Presence of fixture files alone must not satisfy the direct named test
        # check. These inert files exercise preflight only, not T001 acceptance.
        for value in CONTRACT['allowed_paths'][:-1]:
            path = self.root/value
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('Synthetic preflight placeholder\n')
        tests = self.root/'tests/test_single_task.py'
        tests.write_text('import unittest\nclass SingleTaskFixtureTests(unittest.TestCase):\n    pass\n')
        result = subprocess.run(CONTRACT['checks'][0], cwd=self.root, env=self.env,
                                capture_output=True, text=True)
        self.assertNotEqual(0, result.returncode)
        self.assertEqual(self.before, self.tasks_path.read_bytes())

    def test_out_of_scope_change_blocks_before_acceptance(self):
        self.begin()
        (self.root/'outside.md').write_text('Not approved\n')
        result = self.cli('complete')
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Out-of-scope', result.stderr)
        self.assertEqual(self.before, self.tasks_path.read_bytes())

    def test_existing_state_is_preserved_on_reentry(self):
        self.begin()
        path = self.root/'.symphony/single-task.json'
        before = path.read_bytes()
        result = self.cli('begin', '--feature', CONTRACT['feature'], '--task', 'T001',
                          '--allow', 'tests/test_single_task.py', '--check', '["true"]')
        self.assertNotEqual(0, result.returncode)
        self.assertIn('Existing task state', result.stderr)
        self.assertEqual(before, path.read_bytes())
        self.assertEqual(self.before, self.tasks_path.read_bytes())


if __name__ == '__main__':
    unittest.main()
