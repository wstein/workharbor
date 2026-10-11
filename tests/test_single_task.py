import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1]/'scripts/single_task.py'


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
