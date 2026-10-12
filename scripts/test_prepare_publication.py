"""Offline real-Git preparation tests, separate from the frozen task matrix."""
import contextlib
import importlib.util
import io
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

spec = importlib.util.spec_from_file_location(
    'prepublication', Path(__file__).with_name('prepare_publication.py'))
publication = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publication)

# A disposable trusted checker port. Actual policy remains owned by check_ci.py.
CHECKER = '''import argparse, os, subprocess, sys, time
from pathlib import Path
p = argparse.ArgumentParser()
for name in ('root', 'event', 'base', 'head', 'checkout', 'policy-source'):
    p.add_argument('--' + name, required=True)
a = p.parse_args()
r = Path(a.root)
def git(*args):
    return subprocess.check_output(['git', *args], cwd=r).decode().strip()
assert a.event == 'pull_request'
assert subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=Path(__file__).parents[1]).decode().strip() == a.policy_source
assert git('rev-parse', 'HEAD') == a.checkout
assert git('show', '-s', '--format=%P', 'HEAD') == a.base + ' ' + a.head
assert git('rev-parse', 'HEAD^{tree}') == git('merge-tree', '--write-tree', a.base, a.head)
assert not git('status', '--porcelain', '--untracked-files=all')
assert not any(name in os.environ for name in ('GITHUB_TOKEN', 'AWS_SECRET_ACCESS_KEY'))
assert Path(os.environ['HOME']).is_dir()
assert (r/'scripts/check_ci.py').read_bytes() == Path(__file__).read_bytes()
assert (r/'protected.txt').read_text() == 'baseline'
assert not (r/'protected.txt').is_symlink()
assert (r/'protected.txt').stat().st_mode & 0o777 == 0o644
if (r/'check-timeout').exists():
    time.sleep(20)
print('private-check-diagnostic', file=sys.stderr)
sys.exit(7 if (r/'check-fail').exists() else 0)
'''


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.repository = Path(self.temporary.name)/'repository'
        self.repository.mkdir()
        self.git('init', '--quiet', '-b', 'main')
        self.git('config', 'user.name', 'Test')
        self.git('config', 'user.email', 'test@invalid')
        (self.repository/'scripts').mkdir()
        (self.repository/'scripts/check_ci.py').write_text(CHECKER)
        (self.repository/'protected.txt').write_text('baseline')
        self.commit('base')
        self.base = self.git('rev-parse', 'HEAD')
        self.git('checkout', '--quiet', '-b', 'topic')
        (self.repository/'guide.md').write_text('proposal')
        self.commit('proposal')
        self.head = self.git('rev-parse', 'HEAD')
        self.git('checkout', '--quiet', 'main')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.repository,
                                       stderr=subprocess.DEVNULL).decode().strip()

    def commit(self, message):
        self.git('add', '.')
        self.git('commit', '--quiet', '-m', message)

    def prepare(self, **changes):
        options = dict(repository=str(self.repository), source=self.base,
                       base_ref='refs/heads/main', base=self.base,
                       head_ref='refs/heads/topic', head=self.head)
        options.update(changes)
        return publication.prepare(**options)

    def change_topic(self, path, content):
        self.git('checkout', '--quiet', 'topic')
        (self.repository/path).write_text(content)
        self.commit('change proposal')
        self.head = self.git('rev-parse', 'HEAD')
        self.git('checkout', '--quiet', 'main')

    def test_real_merge_is_repeatable_and_every_scratch_is_removed(self):
        created = []
        original = tempfile.TemporaryDirectory
        def scratch(*args, **kwargs):
            directory = original(*args, **kwargs)
            created.append(Path(directory.name))
            return directory
        with mock.patch.object(publication.tempfile, 'TemporaryDirectory', side_effect=scratch):
            first, second = self.prepare(), self.prepare()
        self.assertEqual('PREPARED', first['status'])
        self.assertEqual(0, first['check_exit'])
        self.assertEqual(first, second)
        self.assertTrue(created)
        self.assertTrue(all(not path.exists() for path in created))

    def test_base_and_head_advance_after_checks_are_blocked(self):
        original = publication.Commands.run
        for ref in ('refs/heads/main', 'refs/heads/topic'):
            with self.subTest(ref=ref):
                current = self.git('rev-parse', ref)
                advance = subprocess.check_output(
                    ['git', 'commit-tree', current + '^{tree}', '-p', current,
                     '-m', 'advance'], cwd=self.repository).decode().strip()
                def run(commands, argv, cwd, failure, **kwargs):
                    outcome = original(commands, argv, cwd, failure, **kwargs)
                    if len(argv) > 1 and argv[1].endswith('/scripts/check_ci.py'):
                        self.git('update-ref', ref, advance)
                    return outcome
                with mock.patch.object(publication.Commands, 'run', run):
                    result = self.prepare()
                self.assertEqual('BLOCKED', result['status'])
                self.assertEqual('remote_moved_after_check', result['blocker'])
                self.assertEqual(0, result['check_exit'])
                self.git('update-ref', ref, current)

    def test_initial_pin_mismatch_and_fetch_race_block(self):
        self.assertEqual('remote_moved_before_check', self.prepare(head='0' * 40)['blocker'])
        original = publication.remote_pins
        def race(*args):
            pins = original(*args)
            self.git('update-ref', 'refs/heads/topic', self.base)
            return pins
        with mock.patch.object(publication, 'remote_pins', side_effect=race):
            self.assertEqual('remote_moved_during_fetch', self.prepare()['blocker'])

    def test_trusted_checker_blocks_protected_change_and_proposal_checker(self):
        for path, content in (('protected.txt', 'changed'),
                              ('scripts/check_ci.py', 'raise SystemExit(0)')):
            with self.subTest(path=path):
                self.change_topic(path, content)
                result = self.prepare()
                self.assertEqual('BLOCKED', result['status'])
                self.assertEqual('check_failed', result['blocker'])
                self.assertNotEqual(0, result['check_exit'])
                self.change_topic(path, 'baseline' if path == 'protected.txt' else CHECKER)

    def test_unrelated_base_document_advance_delegates_success(self):
        (self.repository/'base.md').write_text('accepted base update')
        self.commit('advance accepted base')
        self.base = self.git('rev-parse', 'HEAD')
        self.assertEqual('PREPARED', self.prepare()['status'])

    def test_check_failure_missing_checker_and_old_interface_block(self):
        self.change_topic('check-fail', 'fail')
        result = self.prepare()
        self.assertEqual(('check_failed', 7), (result['blocker'], result['check_exit']))
        checker = self.repository/'scripts/check_ci.py'
        checker.unlink()
        self.commit('source without checker')
        source = self.git('rev-parse', 'HEAD')
        self.base = source
        self.assertEqual('checker_unavailable', self.prepare(source=source)['blocker'])
        checker.write_text('import sys\nraise SystemExit(2 if sys.argv[1:] else 0)\n')
        self.commit('old source checker')
        self.base = self.git('rev-parse', 'HEAD')
        result = self.prepare(source=self.git('rev-parse', 'HEAD'))
        self.assertEqual(('check_failed', 2), (result['blocker'], result['check_exit']))

    def test_missing_tools_and_timeout_fail_closed(self):
        with mock.patch.object(publication.subprocess, 'Popen', side_effect=FileNotFoundError):
            self.assertEqual('tool_unavailable', self.prepare()['blocker'])
        self.change_topic('check-timeout', 'sleep')
        self.assertEqual('timeout', self.prepare(timeout=0.8)['blocker'])

    def test_merge_conflict_and_unavailable_source_block_before_checks(self):
        (self.repository/'guide.md').write_text('conflicting accepted change')
        self.commit('conflicting base')
        self.base = self.git('rev-parse', 'HEAD')
        result = self.prepare()
        self.assertEqual(('BLOCKED', None), (result['status'], result['check_exit']))
        self.assertEqual('git_failed', result['blocker'])
        self.assertEqual('git_failed', self.prepare(source='0' * 40)['blocker'])

    def test_mode_changes_are_delegated_to_checker(self):
        self.git('checkout', '--quiet', 'topic')
        (self.repository/'protected.txt').chmod(0o755)
        self.commit('mode change')
        self.head = self.git('rev-parse', 'HEAD')
        self.git('checkout', '--quiet', 'main')
        self.assertEqual('check_failed', self.prepare()['blocker'])

    def test_live_dirty_index_untracked_symlink_and_history_are_preserved(self):
        (self.repository/'staged.txt').write_text('staged')
        self.git('add', 'staged.txt')
        (self.repository/'protected.txt').write_text('local dirty work')
        (self.repository/'untracked.txt').write_text('untracked')
        (self.repository/'link').symlink_to('untracked.txt')
        before = (self.git('status', '--porcelain'), self.git('ls-files', '--stage'),
                  self.git('show-ref'), self.git('reflog', '--all'))
        self.assertEqual('PREPARED', self.prepare()['status'])
        after = (self.git('status', '--porcelain'), self.git('ls-files', '--stage'),
                 self.git('show-ref'), self.git('reflog', '--all'))
        self.assertEqual(before, after)
        self.assertEqual('local dirty work', (self.repository/'protected.txt').read_text())
        self.assertTrue((self.repository/'link').is_symlink())

    def test_credentials_and_diagnostics_never_leave_command(self):
        output = io.StringIO()
        with mock.patch.dict(os.environ, GITHUB_TOKEN='sentinel-secret',
                             AWS_SECRET_ACCESS_KEY='sentinel-secret'), \
                contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            self.assertEqual('PREPARED', self.prepare()['status'])
        self.assertEqual('', output.getvalue())
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            code = publication.main(['--repository', 'https://user:sentinel-secret@host/repo'])
        self.assertEqual(3, code)
        self.assertNotIn('sentinel-secret', output.getvalue())
        self.assertNotIn('private-check-diagnostic', output.getvalue())
        self.assertEqual('BLOCKED', json.loads(output.getvalue())['status'])

    def test_unsafe_inputs_are_sanitized(self):
        for location in ('https://user:secret@host/repo', 'ssh://host/repo',
                         'https://host/repo?secret=x', '../relative', 'file:///tmp/repo'):
            with self.subTest(location=location):
                self.assertEqual('invalid_repository', self.prepare(repository=location)['blocker'])
        for changes in ({'source': 'HEAD'}, {'timeout': 0}, {'timeout': float('nan')},
                        {'base_ref': 'refs/heads/../bad'}, {'head_ref': '--upload-pack=bad'}):
            with self.subTest(changes=changes):
                self.assertEqual('BLOCKED', self.prepare(**changes)['status'])


if __name__ == '__main__':
    unittest.main()
