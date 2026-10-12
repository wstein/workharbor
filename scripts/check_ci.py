#!/usr/bin/env python3
"""Temporary, content-bound T003 CI acceptance; all other trees require green."""
import hashlib
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SEALED_T003 = '586696f7cc1956d430647b918869fbf8caedd660'
T002_MERGE = '029a4ec83e1240b40f72eed004f513ff8fb8b5b0'
T002_IMPLEMENTATION = 'ac8bcb6f4d7c23682c0a66d7c2d7a31fd9dcb4f2'
CI_PATHS = {'.github/workflows/check.yml', 'scripts/check_ci.py',
            'docs/development/harness.md'}
PINS = {
    'docs/development/bootstrap-t003.json': 'b5cc969a053f461ccd2c0bcda68dac075d036adad6cebfc1e7c4b7689ee91ba6',
    'docs/development/bootstrap-t002.json': '597b2c2d31f90afd8118b0e593f9cf1b998ec1bfd7f21558e5cff45ab0596461',
    'scripts/verify_t003_state.py': '3bba2eeff6d6e1abe14ead36ce98c5f4e2a749fbb71f7d4c03012af9e6e78b0f',
    'scripts/verify_t002_red.py': 'c863cb1f16f5c0eda3c57631e24401612fd56abf49708dd294b389974a9cfffb',
    'scripts/single_task.py': '7c50b5170e31eed9c5c0dbeb047919c8845f6c266a32def55868a4ab9b863bc4',
    'specs/003-single-task-entry/tasks.md': 'c180e72fbb9e0fba51579629657cd24aa41808ddec79d8a58c191dd6d451c6ab',
}


def git(root, *args):
    return subprocess.run(['git', *args], cwd=root, capture_output=True, check=True).stdout


def approved_t003(root):
    try:
        for name, digest in PINS.items():
            path = root/name
            if path.is_symlink() or not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                return False
        git(root, 'merge-base', '--is-ancestor', T002_MERGE, SEALED_T003)
        git(root, 'merge-base', '--is-ancestor', SEALED_T003, 'HEAD')
        # Preserve the original prerequisite implementation and accepted blobs.
        for name in ('tests/test_single_task_state.py', 'specs/003-single-task-entry/tasks.md'):
            original = git(root, 'ls-tree', T002_IMPLEMENTATION, '--', name)
            if not original or original != git(root, 'ls-tree', T002_MERGE, '--', name):
                return False
        # Any successor implementation, marker, test or other content expires this gate.
        git(root, 'diff', '--quiet', SEALED_T003, '--', '.',
            *(f':(exclude){name}' for name in sorted(CI_PATHS)))
        untracked = git(root, 'ls-files', '--others', '--exclude-standard', '-z')
        return all(os.fsdecode(name) in CI_PATHS for name in untracked.split(b'\0') if name)
    except (OSError, subprocess.CalledProcessError):
        return False


def run(argv, root):
    print('+ ' + ' '.join(argv), flush=True)
    return subprocess.run(argv, cwd=root).returncode


def strict_suite(root):
    # unittest's successful exit alone also permits skips/xfail and empty discovery.
    suite = unittest.defaultTestLoader.discover(str(root/'tests'))
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    return (result.wasSuccessful() and result.testsRun > 0 and not result.skipped
            and not result.expectedFailures)


def check(root):
    scoped = approved_t003(root)
    print('CI mode: approved T003 phase' if scoped else 'CI mode: strict full green', flush=True)
    if scoped:
        for script in ('verify_t003_state.py', 'verify_t002_red.py'):
            if run([sys.executable, 'scripts/' + script], root):
                return 1
        if run([sys.executable, 'scripts/check_repo.py'], root) or run(['git', 'diff', '--check'], root):
            return 1
        for script in sorted((root/'.specify/scripts/bash').glob('*.sh')) + sorted((root/'scripts').glob('*.sh')):
            if run(['bash', '-n', str(script.relative_to(root))], root):
                return 1
        env = os.environ.copy()
        env.update(SPECIFY_FEATURE_DIRECTORY='specs/001-workspace-lifecycle', SPECIFY_FEATURE_NO_PERSIST='1')
        if subprocess.run(['bash', '.specify/scripts/bash/check-prerequisites.sh', '--json',
                           '--require-tasks', '--include-tasks'], cwd=root, env=env).returncode:
            return 1
    elif (root/'scripts/single_task_state.py').exists():
        if run([sys.executable, 'scripts/verify_t003_state.py'], root):
            return 1
    raw = run(['make', 'check', 'PYTHON=' + sys.executable], root)
    print(f'Raw make check exit: {raw}; full suite ' + ('remains RED (not implementation acceptance).' if scoped else 'must be GREEN.'), flush=True)
    if raw != (2 if scoped else 0):
        return 1
    return 0 if scoped or strict_suite(root) else 1


def self_test():
    class GateTests(unittest.TestCase):
        def test_approved_content_and_prerequisites(self):
            self.assertTrue(approved_t003(ROOT))

        def test_every_content_pin_refuses_changes_missing_files_and_symlinks(self):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                for name in PINS:
                    path = root/name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes((ROOT/name).read_bytes())
                with mock.patch(__name__ + '.git', return_value=b'approved blob'):
                    for name in PINS:
                        path = root/name
                        content = path.read_bytes()
                        for mutation in ('changed', 'missing', 'symlink'):
                            with self.subTest(path=name, mutation=mutation):
                                path.unlink()
                                if mutation == 'changed':
                                    path.write_bytes(content + b'\n')
                                elif mutation == 'symlink':
                                    path.symlink_to(ROOT/name)
                                self.assertFalse(approved_t003(root))
                                if path.is_symlink() or path.exists():
                                    path.unlink()
                                path.write_bytes(content)

        def test_unknown_history_successor_tree_and_untracked_code_refused(self):
            def source(*args):
                if args[1] == 'ls-files':
                    return b'scripts/single_task_context.py\0'
                return b'approved blob'
            with mock.patch(__name__ + '.git', side_effect=source):
                self.assertFalse(approved_t003(ROOT))
            for command in ('merge-base', 'ls-tree', 'diff', 'ls-files'):
                def fail(root, *args):
                    if args[0] == command:
                        raise subprocess.CalledProcessError(1, args)
                    return b'approved blob'
                with self.subTest(command=command), mock.patch(__name__ + '.git', side_effect=fail):
                    self.assertFalse(approved_t003(ROOT))

        def test_prerequisite_blob_mismatch_refused(self):
            def mismatch(root, *args):
                return b'wrong blob' if T002_MERGE in args else b'approved blob'
            with mock.patch(__name__ + '.git', side_effect=mismatch):
                self.assertFalse(approved_t003(ROOT))

        def test_strict_fallback_requires_green(self):
            with mock.patch(__name__ + '.approved_t003', return_value=False), \
                    mock.patch(__name__ + '.strict_suite', return_value=True):
                for raw in (0, 1, 2):
                    with self.subTest(raw=raw), mock.patch(__name__ + '.run', side_effect=[0, raw]):
                        self.assertEqual(0 if raw == 0 else 1, check(ROOT))
            with mock.patch(__name__ + '.approved_t003', return_value=False), \
                    mock.patch(__name__ + '.run', return_value=0), \
                    mock.patch(__name__ + '.strict_suite', return_value=False):
                self.assertEqual(1, check(ROOT))

        def test_strict_suite_refuses_empty_skipped_and_expected_failures(self):
            class Green(unittest.TestCase):
                def runTest(self):
                    pass
            class Skipped(Green):
                @unittest.skip('synthetic')
                def runTest(self):
                    pass
            class ExpectedFailure(Green):
                @unittest.expectedFailure
                def runTest(self):
                    self.fail('synthetic')
            for case, expected in ((None, False), (Green, True), (Skipped, False), (ExpectedFailure, False)):
                suite = unittest.TestSuite([] if case is None else [case()])
                with self.subTest(case=case), mock.patch.object(unittest.defaultTestLoader, 'discover', return_value=suite):
                    self.assertEqual(expected, strict_suite(ROOT))

        def test_scoped_checks_and_raw_red_required(self):
            with mock.patch(__name__ + '.approved_t003', return_value=True), \
                    mock.patch('subprocess.run', return_value=mock.Mock(returncode=0)):
                for raw in (0, 1, 2):
                    with self.subTest(raw=raw), mock.patch(__name__ + '.run', side_effect=lambda argv, root: raw if argv[0] == 'make' else 0):
                        self.assertEqual(0 if raw == 2 else 1, check(ROOT))
                for failing in ('verify_t003_state.py', 'verify_t002_red.py', 'check_repo.py', 'git', 'bash'):
                    with self.subTest(failing=failing), mock.patch(__name__ + '.run', side_effect=lambda argv, root: 1 if failing in ' '.join(argv) else 0):
                        self.assertEqual(1, check(ROOT))
                with mock.patch(__name__ + '.run', return_value=0), \
                        mock.patch('subprocess.run', return_value=mock.Mock(returncode=1)):
                    self.assertEqual(1, check(ROOT))
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(GateTests))
    return 0 if result.wasSuccessful() and result.testsRun == 7 else 1


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        raise SystemExit(self_test())
    if sys.argv[1:]:
        raise SystemExit('CI gate accepts no phase or scope override arguments')
    raise SystemExit(check(ROOT))
