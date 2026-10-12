#!/usr/bin/env python3
"""Proposed T004 gate: direct CLI greens plus an exact retained-red matrix.

No implementation, dispatch, owner authentication or prerequisite acceptance.
The unchanged authored scenarios remain collected, including the five that need
T010's validated begin. Never substitute this gate for the final green suite.
"""
import contextlib
import hashlib
import io
import json
import os
import selectors
import signal
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'tests'))
sys.path.insert(0, str(ROOT/'scripts'))
from test_single_task import SingleTaskFixture, canonical_bytes
from verify_t002_red import Outcomes as BaseOutcomes, evaluate, flatten
import single_task_state as backend


def file_digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Outcomes(BaseOutcomes):
    def addSubTest(self, test, subtest, error):
        super().addSubTest(test, subtest, error)
        if error is not None and not any(case[0] == test.id() for case in self.observed):
            # Preserve failed subtest parents in the safe report; never accept
            # them as a precise retained-red assertion or a green direct case.
            self.observed.append((test.id(), 'subtest-failure', None))


class CliBoundaryTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.fixture = SingleTaskFixture(temp.name)
        self.root = self.fixture.root
        (self.root/'.gitignore').write_text('.symphony/\n', encoding='utf-8')
        self.fixture.git('add', '.gitignore')
        self.fixture.git('commit', '-m', 'test: ignore local state')
        self.tasks = self.root/'specs/synthetic/tasks.md'
        self.before = self.tasks.read_bytes()
        self.index = self.fixture.git('ls-tree', 'HEAD', '--', 'specs/synthetic/tasks.md').stdout
        self.context = self.root/'.symphony/input.json'
        self.context.parent.mkdir()
        self.context.write_bytes(canonical_bytes(self.fixture.context()))
        self.script = ROOT/'scripts/single_task.py'

    def tearDown(self):
        self.assertEqual(self.before, self.tasks.read_bytes())
        self.assertEqual(self.index, self.fixture.git(
            'ls-tree', 'HEAD', '--', 'specs/synthetic/tasks.md').stdout)
        self.assertEqual('', self.fixture.git('diff', '--cached').stdout)

    def cli(self, *args, injection=None):
        argv = [sys.executable, str(self.script), *args]
        if injection:
            argv = [sys.executable, '-c', injection, str(self.script), *args]
        return subprocess.run(argv, cwd=self.root, env=self.fixture.env,
                              capture_output=True, text=True, timeout=8)

    def response(self, result, exit_code, *, code=None, legacy=False):
        self.assertEqual(exit_code, result.returncode)
        value = json.loads(result.stdout)
        self.assertIsInstance(value, dict)
        self.assertIs(type(value['schema_version']), int)
        self.assertEqual(1, value['schema_version'])
        for field in ('attempt_id', 'mode', 'progress', 'local_verification'):
            self.assertIn(field, value)
        self.assertTrue(value['attempt_id'] is None or isinstance(value['attempt_id'], str))
        self.assertIn(value['mode'], (None, 'implement', 'owner-authorized rework', 'handoff-only'))
        self.assertTrue(value['progress'] is None or isinstance(value['progress'], str))
        self.assertIn(value['local_verification'], ('not-run', 'passed', 'failed'))
        self.assertIs(False, value['owner_identity_authenticated'])
        self.assertIs(False, value['live_dispatch_verified'])
        self.assertIsInstance(value['errors'], list)
        if exit_code == 0:
            self.assertEqual([], value['errors'])
        else:
            self.assertTrue(value['errors'])
        for error in value['errors']:
            self.assertTrue(error['code'])
            self.assertTrue(error['message'])
            self.assertIsInstance(error['affected_paths'], list)
            self.assertIsInstance(error['affected_ids'], list)
            self.assertTrue(all(isinstance(item, str) for item in
                                error['affected_paths'] + error['affected_ids']))
        if code:
            self.assertEqual(code, value['errors'][0]['code'])
        if not legacy:
            self.assertEqual('', result.stderr)
        return value

    def legacy_begin(self, check=None):
        return self.cli('begin', '--feature', 'synthetic', '--task', 'T001',
                        '--allow', 'guide.md', '--check', json.dumps(
                            check or [sys.executable, '-c', 'pass']))

    def seed(self):
        with backend.open_store(self.root) as store:
            store.create_attempt(attempt_id='synthetic-attempt', context=self.fixture.context(),
                                 baseline=self.fixture.git('rev-parse', 'HEAD').stdout.strip(),
                                 tasks_bytes=self.before, entry_mode='implement')

    def commands(self):
        return [
            ['begin', '--context', str(self.context)],
            ['rework', '--context', str(self.context)],
            ['inspect', '--attempt', 'synthetic-attempt'],
            ['verify', '--attempt', 'synthetic-attempt', '--review', str(self.context)],
            ['complete', '--attempt', 'synthetic-attempt', '--review', str(self.context)],
            ['record-commit', '--attempt', 'synthetic-attempt', '--commit', '0'*40],
            ['resume', '--attempt', 'synthetic-attempt', '--review', str(self.context)],
            ['status', '--attempt', 'synthetic-attempt'],
        ]

    def test_legacy_success_envelope(self):
        value = self.response(self.legacy_begin(), 0, legacy=True)
        self.assertIsNone(value['attempt_id'])
        self.assertEqual('implement', value['mode'])
        self.assertEqual('legacy-active', value['progress'])
        self.assertEqual('not-run', value['local_verification'])
        self.assertFalse((self.root/'.symphony/single-task/journal.sqlite3').exists())
        value = self.response(self.cli('verify'), 0, legacy=True)
        self.assertEqual('passed', value['local_verification'])
        self.assertEqual('legacy-active', value['progress'])

    def test_argument_errors_are_safe_and_stable(self):
        secret = 'synthetic-T004-secret-argv'
        for argv in ([], ['unknown-command', secret], ['begin', '--unknown', secret],
                     ['status'], ['begin', '--context'], ['status', '--attempt', 'safe', secret]):
            with self.subTest(argv=argv):
                first = self.cli(*argv)
                value = self.response(first, 2, code='invalid_arguments')
                second = self.cli(*argv)
                repeated = self.response(second, 2, code='invalid_arguments')
                self.assertEqual(value, repeated)
                self.assertNotIn(secret, first.stdout + first.stderr)
                self.assertNotIn('unknown-command', first.stdout + first.stderr)

    def test_deferred_commands_do_not_enter_or_execute(self):
        marker = self.root/'.symphony/prior-evidence'
        marker.write_bytes(b'keep prior evidence')
        for argv in self.commands():
            with self.subTest(command=argv[0]):
                value = self.response(self.cli(*argv), 2, code='command_unavailable')
                self.assertEqual('not-run', value['local_verification'])
                self.assertIsNone(value['attempt_id'])
                self.assertIsNone(value['mode'])
                self.assertIsNone(value['progress'])
                self.assertFalse((self.root/'.symphony/single-task/journal.sqlite3').exists())
                self.assertFalse((self.root/'.symphony/single-task/contexts').exists())
                self.assertFalse((self.root/'.symphony/single-task.json').exists())
                self.assertEqual(b'keep prior evidence', marker.read_bytes())

    def test_malformed_context_is_not_echoed(self):
        secret = 'synthetic-T004-secret-context'
        content = canonical_bytes({'unknown': secret, 'checks': [{'argv': [secret]}]})
        self.context.write_bytes(content)
        result = self.cli('begin', '--context', str(self.context))
        self.response(result, 2, code='command_unavailable')
        self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertNotIn(content.decode(), result.stdout + result.stderr)
        self.assertEqual(content, self.context.read_bytes())

    def test_scope_blocker_exit_three(self):
        self.response(self.legacy_begin(), 0, legacy=True)
        (self.root/'outside.md').write_text('out of scope\n', encoding='utf-8')
        result = self.cli('verify')
        self.response(result, 3, code='scope_blocker', legacy=True)
        self.assertIn('Out-of-scope', result.stderr)

    def test_acceptance_exit_four_does_not_echo_output_or_argv(self):
        secret = 'synthetic-T004-secret-check'
        check = [sys.executable, '-c', f'import sys; print({secret!r}); print({secret!r}, file=sys.stderr); sys.exit(7)']
        self.response(self.legacy_begin(check), 0, legacy=True)
        result = self.cli('verify')
        value = self.response(result, 4, code='acceptance_failed', legacy=True)
        self.assertEqual('failed', value['local_verification'])
        self.assertNotIn(secret, result.stdout + result.stderr)
        self.assertNotIn(check[2], result.stdout + result.stderr)
        saved = json.loads((self.root/'.symphony/single-task.json').read_bytes())
        self.assertIn('last_failure', saved)
        self.assertNotIn(secret, json.dumps(saved['last_failure']))

    def test_persistence_exit_six_is_safe(self):
        injection = '''import os, runpy, sys
def failed(*a, **k): raise OSError('synthetic-T004-secret-persistence')
os.fsync = failed
sys.argv = sys.argv[1:]
sys.path.insert(0, str(__import__('pathlib').Path(sys.argv[0]).parent))
runpy.run_path(sys.argv[0], run_name='__main__')
'''
        result = self.cli('begin', '--context', str(self.context), injection=injection)
        self.response(result, 6, code='persistence_failure')
        self.assertNotIn('synthetic-T004-secret-persistence', result.stdout + result.stderr)
        self.assertFalse((self.root/'.symphony/single-task.json').exists())

    def test_all_commands_share_nonblocking_store_lock(self):
        self.seed()
        journal = self.root/'.symphony/single-task/journal.sqlite3'
        before = journal.read_bytes()
        with backend.open_store(self.root):
            for argv in self.commands() + [['verify'], ['complete'],
                    ['begin', '--feature', 'synthetic', '--task', 'T001',
                     '--allow', 'guide.md', '--check', '["true"]']]:
                with self.subTest(command=argv[0]):
                    self.response(self.cli(*argv), 5, code='concurrent_attempt')
        self.assertEqual(before, journal.read_bytes())
        self.assertFalse((self.root/'.symphony/single-task.json').exists())

    def test_lock_released_after_cli_error(self):
        self.response(self.cli('begin', '--context', str(self.context)), 2,
                      code='command_unavailable')
        # A separate CLI process and the real primitive can each reacquire it.
        self.response(self.cli('begin', '--context', str(self.context)), 2,
                      code='command_unavailable')
        with backend.open_store(self.root):
            pass

    def test_unsafe_lock_paths_rejected_without_external_writes(self):
        for component in ('.symphony', '.symphony/single-task', '.symphony/single-task/lock'):
            with self.subTest(component=component), tempfile.TemporaryDirectory() as temp:
                fixture = SingleTaskFixture(temp)
                path = fixture.root/component
                path.parent.mkdir(parents=True, exist_ok=True)
                outside = fixture.private/'external'
                if component.endswith('/lock'):
                    outside.write_bytes(b'keep external bytes')
                    path.symlink_to(outside)
                else:
                    outside.mkdir()
                    path.symlink_to(outside, target_is_directory=True)
                result = subprocess.run([sys.executable, str(self.script), 'begin',
                                         '--context', 'unused.json'], cwd=fixture.root,
                                        env=fixture.env, capture_output=True, text=True, timeout=8)
                self.response(result, 3, code='state_blocker')
                if outside.is_file():
                    self.assertEqual(b'keep external bytes', outside.read_bytes())
                else:
                    self.assertEqual([], list(outside.iterdir()))
                self.assertEqual(fixture.tasks_bytes, (fixture.feature/'tasks.md').read_bytes())

    def test_mixed_legacy_and_sqlite_is_never_adopted(self):
        self.seed()
        state = self.root/'.symphony/single-task.json'
        state.write_bytes(b'{"retained_legacy":true}\n')
        journal = self.root/'.symphony/single-task/journal.sqlite3'
        before = journal.read_bytes()
        for argv in (['verify'], ['complete'], ['status', '--attempt', 'synthetic-attempt']):
            self.response(self.cli(*argv), 3, code='state_blocker')
            self.assertEqual(before, journal.read_bytes())
            self.assertEqual(b'{"retained_legacy":true}\n', state.read_bytes())

    def test_legacy_acceptance_holds_lock_against_all_commands(self):
        ready = self.root/'.symphony/ready'
        release = self.root/'.symphony/release'
        os.mkfifo(ready)
        os.mkfifo(release)
        check = [sys.executable, '-c',
                 'import pathlib,sys; pathlib.Path(sys.argv[1]).write_text("ready\\n"); '
                 'open(sys.argv[2]).read(1)', str(ready), str(release)]
        self.response(self.legacy_begin(check), 0, legacy=True)
        read_fd = os.open(ready, os.O_RDONLY | os.O_NONBLOCK)
        process = subprocess.Popen([sys.executable, str(self.script), 'verify'],
                                   cwd=self.root, env=self.fixture.env, text=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   start_new_session=True)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(read_fd, selectors.EVENT_READ)
                self.assertTrue(selector.select(5), 'Acceptance did not signal readiness')
            self.assertEqual(b'ready\n', os.read(read_fd, 100))
            legacy = self.root/'.symphony/single-task.json'
            before = legacy.read_bytes()
            for argv in self.commands() + [['verify'], ['complete']]:
                self.response(self.cli(*argv), 5, code='concurrent_attempt')
                self.assertEqual(before, legacy.read_bytes())
            # Nonblocking write avoids hanging if the checker died unexpectedly.
            with os.fdopen(os.open(release, os.O_WRONLY | os.O_NONBLOCK), 'w') as stream:
                stream.write('x')
            stdout, stderr = process.communicate(timeout=8)
            self.response(subprocess.CompletedProcess([], process.returncode, stdout, stderr),
                          0, legacy=True)
        finally:
            os.close(read_fd)
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
            process.communicate(timeout=5)


def validate_inventory(tests, expected):
    ids = [test.id() for test in tests]
    return len(ids) == len(set(ids)) and set(ids) == set(expected)


def self_test():
    baseline, red = {'green'}, {'retained': "'usage:' not found in ''"}
    good = [('green', 'pass', None), ('retained', 'assertion', red['retained'])]
    assert evaluate(good, baseline, red)
    for kind in ('pass', 'error', 'skip', 'xfail', 'unexpected-success', 'setup-failure', 'subtest-failure'):
        assert not evaluate([good[0], ('retained', kind, red['retained'])], baseline, red)
    for kind in ('error', 'skip', 'xfail', 'unexpected-success', 'setup-failure', 'subtest-failure', 'assertion'):
        assert not evaluate([('green', kind, None), good[1]], baseline, red)
    for bad in (good[:1], good + [good[1]], good + [('extra', 'pass', None)],
                [('green', 'assertion', 'regression'), good[1]],
                [good[0], ('retained', 'assertion', 'old usage failure')]):
        assert not evaluate(bad, baseline, red)
    class Broken(unittest.TestCase):
        def setUp(self):
            self.fail(red['retained'])
        def test_body(self):
            pass
    observed = Outcomes()
    Broken('test_body').run(observed)
    assert observed.observed[0][1] == 'setup-failure'
    class BrokenSubtest(unittest.TestCase):
        def test_body(self):
            with self.subTest():
                self.fail(red['retained'])
    observed = Outcomes()
    BrokenSubtest('test_body').run(observed)
    assert observed.observed[0][1] == 'subtest-failure'
    suite = list(flatten(unittest.defaultTestLoader.loadTestsFromTestCase(CliBoundaryTests)))
    ids = [test.id() for test in suite]
    assert validate_inventory(suite, ids)
    assert not validate_inventory(suite[:-1], ids)
    assert not validate_inventory(suite + [suite[0]], ids)
    assert not validate_inventory(suite, ids + ['unittest.loader._FailedTest.import_error'])
    print('T004 verifier self-test passed: inventory, baseline regression, retained-red, setup/error/skip/xfail rejection.')


def main():
    if sys.argv[1:] == ['--self-test']:
        self_test()
        return 0
    if sys.argv[1:]:
        raise ValueError('No scope or matrix overrides are permitted')
    contract = json.loads((ROOT/'docs/development/bootstrap-t004.json').read_text())
    for path, expected in contract['frozen_files'].items():
        file = ROOT/path
        if file.is_symlink() or file_digest(file) != expected:
            raise ValueError('Frozen acceptance inputs changed')
    expected_green = set(contract['baseline_test_ids']) | set(contract['authored_green_ids'])
    red = {case['id']: case['failure'] for case in contract['retained_red_cases']}
    direct = contract['direct_green_ids']
    if (len(contract['baseline_test_ids']) != 31 or len(contract['authored_green_ids']) != 5
            or len(red) != 5 or len(direct) != 12 or expected_green & set(red)):
        raise ValueError('Invalid T004 matrix')
    discovered = list(flatten(unittest.defaultTestLoader.discover(str(ROOT/'tests'))))
    direct_tests = list(flatten(unittest.defaultTestLoader.loadTestsFromTestCase(CliBoundaryTests)))
    # __main__ IDs are stable only after the script runs; the contract uses them.
    if (not validate_inventory(discovered, expected_green | set(red))
            or not validate_inventory(direct_tests, direct)):
        raise ValueError('Missing/extra/duplicate cases or import failure')
    result = Outcomes()
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        unittest.TestSuite(discovered + direct_tests).run(result)
    accepted = evaluate(result.observed, expected_green | set(direct), red)
    report = {'schema_version': 1, 'task': 'T004', 'proposal_matrix_verified': accepted,
              'validated_entry_implemented': False,
              'prerequisite_owner_accepted': contract['prerequisite']['owner_accepted'],
              'owner_identity_authenticated': False, 'live_dispatch_verified': False,
              'tests_run': result.testsRun, 'full_suite_green': False,
              'outcomes': [{'id': name, 'outcome': kind,
                            'matches_retained_red': name in red and kind == 'assertion' and message == red[name]}
                           for name, kind, message in result.observed]}
    print(json.dumps(report, indent=2))
    return 0 if accepted and result.testsRun == 53 else 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, KeyError, TypeError, ImportError):
        raise SystemExit('T004 verifier blocked: invalid contract, frozen inputs or case inventory')
