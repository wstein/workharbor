#!/usr/bin/env python3
"""Shared unittest classification and explicitly approved exact-package matrices.

Package data never authorizes execution. Callers supply the separately reviewed
checks, and (for test authoring only) a distinct matrix decision. Local decisions
are reported as supplied assertions, never authenticated owner evidence.
"""
import sys

if __name__ == '__main__' and not sys.flags.isolated:
    print('{"accepted":false,"blocker":"isolated_interpreter_required"}')
    raise SystemExit(4)

import argparse
import hashlib
import io
import json
import math
import os
import re
import signal
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

MAX_CHECKS = 100
MAX_CASES = 10000
MAX_INPUT = 1024 * 1024
MAX_DEADLINE = 300
DIGEST = re.compile(r'sha256:[0-9a-f]{64}\Z')
CHECK_KEYS = {'id', 'argv', 'deadline_seconds', 'tool_revision', 'input_revision', 'cases', 'raw_exit'}
CASE_KEYS = {'id', 'expected', 'fingerprint'}
_LOADED_TOOL_BYTES = Path(__file__).read_bytes()


def flatten(suite):
    for test in suite:
        yield from flatten(test) if isinstance(test, unittest.TestSuite) else (test,)


def validate_collection(ids, required, *, exact=False):
    """CI has a reviewed minimum; a package binds its complete ordered inventory."""
    if (not ids or any(not isinstance(item, str) or not item for item in ids)
            or len(ids) != len(set(ids)) or not set(required) <= set(ids)
            or (exact and list(ids) != list(required))):
        raise ValueError('invalid_collection')


class Outcomes(unittest.TextTestResult):
    """Record actual unittest dispatch stages, including aliased lifecycle code."""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.observed = []
        self.failed_subtests = []
        self._restores = {}
        self._assertions = {}
        self._lifecycle_errors = {}

    def startTest(self, test):
        super().startTest(test)
        # TestCase.run calls startTest before any lifecycle dispatch. Instrument
        # the four dispatch ports rather than guessing a stage from frame names
        # or method code shared by aliases. Custom run implementations which do
        # not use unittest's actual test dispatch cannot qualify a retained red.
        stack = []
        restores = []
        for name, stage in (('_callSetUp', 'setup'), ('_callTestMethod', 'test'),
                            ('_callTearDown', 'teardown'), ('_callCleanup', 'cleanup')):
            original = getattr(test, name)
            had = name in test.__dict__
            previous = test.__dict__.get(name)

            def invoke(*args, _original=original, _stage=stage, **kwargs):
                caller = sys._getframe(1).f_code
                body_dispatch = (_stage == 'test' and not stack
                                 and caller is unittest.TestCase.run.__code__)
                stack.append(_stage)
                try:
                    return _original(*args, **kwargs)
                except BaseException as error:
                    if _stage != 'test':
                        self._lifecycle_errors[id(error)] = error
                    if body_dispatch and id(error) not in self._lifecycle_errors:
                        method = getattr(test, getattr(test, '_testMethodName', ''), None)
                        code = getattr(method, '__code__', None)
                        trace = error.__traceback__
                        while trace is not None:
                            if trace.tb_frame.f_code is code:
                                self._assertions[id(error)] = error
                                break
                            trace = trace.tb_next
                    raise
                finally:
                    stack.pop()

            setattr(test, name, invoke)
            restores.append((name, had, previous))
        self._restores[id(test)] = restores

    def stopTest(self, test):
        for name, had, previous in self._restores.pop(id(test), ()):
            if had:
                setattr(test, name, previous)
            else:
                delattr(test, name)
        self._assertions.clear()
        self._lifecycle_errors.clear()
        super().stopTest(test)

    def addSuccess(self, test):
        super().addSuccess(test)
        self.observed.append((test.id(), 'pass', None))

    def addFailure(self, test, error):
        super().addFailure(test, error)
        body = self._assertions.pop(id(error[1]), None) is error[1]
        kind = 'assertion' if error[0] is AssertionError and body else 'other-failure'
        self.observed.append((test.id(), kind, str(error[1])))

    def addSubTest(self, test, subtest, error):
        super().addSubTest(test, subtest, error)
        if error is not None:
            self.failed_subtests.append(test.id())


def evaluate(ids, manifest, result):
    """Preserve repository minimum-ID and exact retained-message policy."""
    try:
        validate_collection(ids, manifest['required_test_ids'])
    except ValueError:
        return False
    expected = {case['id']: case['message'] for case in manifest['retained_assertion_failures']}
    observed = {name: (kind, message) for name, kind, message in result.observed}
    return (result.testsRun == len(ids) and len(result.observed) == len(ids)
            and set(observed) == set(ids)
            and not (result.errors or result.skipped or result.expectedFailures or result.unexpectedSuccesses
                     or result.failed_subtests)
            and all(observed[name] == ('assertion', expected[name]) if name in expected
                    else observed[name] == ('pass', None) for name in ids))


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False).encode('utf-8')


def digest(content):
    return 'sha256:' + hashlib.sha256(content).hexdigest()


def tool_revision():
    return digest(_LOADED_TOOL_BYTES)


def assertion_fingerprint(case_id, message):
    """Bound reviewed case/type/message identity; never expose exception text."""
    if (not isinstance(case_id, str) or not case_id or not isinstance(message, str)
            or len(message.encode('utf-8')) > 4096):
        raise ValueError('invalid_assertion_identity')
    return digest(canonical([case_id, 'AssertionError', message]))


def snapshot(root):
    """Bind all checkout bytes, paths and modes, including ignored/Git files.

    Reports and interpreter caches live outside this root. This detects lasting
    mutation; it is a content check, not a sandbox against hostile check code.
    """
    root = Path(root)
    if root.is_symlink() or not root.is_dir():
        raise ValueError('unsafe_root')
    entries = []
    for path in [root, *sorted(root.rglob('*'))]:
        metadata = path.lstat()
        mode = metadata.st_mode
        name = '.' if path == root else path.relative_to(root).as_posix()
        if stat.S_ISDIR(mode):
            entries.append([name, stat.S_IMODE(mode), 'directory'])
        elif stat.S_ISREG(mode) and metadata.st_nlink == 1:
            entries.append([name, stat.S_IMODE(mode), digest(path.read_bytes())])
        else:
            raise ValueError('unsafe_checkout_entry')
    return digest(canonical(entries))


def _identifier(value):
    return isinstance(value, str) and 0 < len(value.encode('utf-8')) <= 512 and all(32 <= ord(c) < 127 for c in value)


def _discovery(argv):
    if not isinstance(argv, list) or any(not isinstance(item, str) or len(item) > 512 for item in argv):
        raise ValueError('invalid_argv')
    if not argv or argv[0] not in ('python3', sys.executable):
        raise ValueError('unsupported_interpreter')
    args = argv[1:]
    if args[:2] == ['-I', '-B']:
        args = args[2:]
    elif args[:1] in (['-I'], ['-B']):
        args = args[1:]
    if (len(args) != 8 or args[:4] != ['-m', 'unittest', 'discover', '-s']
            or args[5] != '-p' or args[7] not in ('-v', '-q')):
        raise ValueError('unsupported_check_argv')
    directory, pattern = args[4], args[6]
    path = Path(directory)
    if (not directory or path.is_absolute() or '..' in path.parts or directory != path.as_posix()
            or not pattern or '/' in pattern or '\\' in pattern or not pattern.endswith('.py')):
        raise ValueError('unsafe_discovery')
    return directory, pattern


def validate_checks(checks):
    if not isinstance(checks, list) or not 0 < len(checks) <= MAX_CHECKS:
        raise ValueError('invalid_checks')
    ids, total = [], 0
    for check in checks:
        if not isinstance(check, dict) or set(check) != CHECK_KEYS or not _identifier(check['id']):
            raise ValueError('invalid_check')
        ids.append(check['id'])
        _discovery(check['argv'])
        deadline = check['deadline_seconds']
        if (type(deadline) not in (int, float) or not math.isfinite(deadline) or not 0 < deadline <= MAX_DEADLINE
                or any(not isinstance(check[key], str) or not DIGEST.fullmatch(check[key]) for key in ('tool_revision', 'input_revision'))):
            raise ValueError('invalid_check_bounds')
        cases = check['cases']
        if not isinstance(cases, list) or not cases:
            raise ValueError('invalid_cases')
        total += len(cases)
        if total > MAX_CASES:
            raise ValueError('case_limit')
        case_ids, reds = [], False
        for case in cases:
            if not isinstance(case, dict) or set(case) != CASE_KEYS or not _identifier(case['id']):
                raise ValueError('invalid_case')
            case_ids.append(case['id'])
            if case['expected'] == 'pass' and case['fingerprint'] is None:
                continue
            if (case['expected'] != 'assertion' or not isinstance(case['fingerprint'], str)
                    or not DIGEST.fullmatch(case['fingerprint'])):
                raise ValueError('invalid_expected_outcome')
            reds = True
        if len(case_ids) != len(set(case_ids)):
            raise ValueError('duplicate_case')
        if type(check['raw_exit']) is not int or check['raw_exit'] != int(reds):
            raise ValueError('invalid_raw_exit')
    if len(ids) != len(set(ids)):
        raise ValueError('duplicate_check')
    return checks


def checks_digest(checks):
    validate_checks(checks)
    return digest(canonical(checks))


def _child(check, root, report):
    if check['tool_revision'] != tool_revision():
        raise ValueError('tool_revision_mismatch')
    directory, pattern = _discovery(check['argv'])
    suite = unittest.TestLoader().discover(str(root/directory), pattern=pattern, top_level_dir=str(root/directory))
    ids = [case.id() for case in flatten(suite)]
    try:
        validate_collection(ids, [case['id'] for case in check['cases']], exact=True)
    except ValueError:
        report.write_bytes(canonical({'case_ids': [], 'blocker': 'invalid_collection'}))
        return 2
    result = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0, resultclass=Outcomes).run(suite)
    observed = []
    for name, kind, message in result.observed:
        fingerprint = None
        if kind == 'assertion':
            try:
                fingerprint = assertion_fingerprint(name, message)
            except ValueError:
                kind = 'other-failure'
        observed.append({'id': name, 'expected': kind, 'fingerprint': fingerprint})
    valid = (result.testsRun == len(ids) and len(observed) == len(ids)
             and not (result.errors or result.skipped or result.expectedFailures or result.unexpectedSuccesses or result.failed_subtests)
             and observed == check['cases'])
    report.write_bytes(canonical({'case_ids': ids, 'observed': observed, 'accepted': valid}))
    return 0 if result.wasSuccessful() else 1


def _load(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_INPUT:
        raise ValueError('invalid_input')
    def unique(pairs):
        value = {}
        for key, item in pairs:
            if key in value:
                raise ValueError('duplicate_key')
            value[key] = item
        return value
    return json.loads(path.read_bytes(), object_pairs_hook=unique)


def _execute(root, check):
    evidence = {'id': check['id'], 'raw_exit': None, 'runner_exit': None, 'case_ids': [], 'observed': [], 'accepted': False,
                'tool_revision': check['tool_revision'], 'input_revision': check['input_revision'], 'blocker': None}
    if (check['tool_revision'] != tool_revision() or digest(Path(__file__).read_bytes()) != tool_revision()
            or snapshot(root) != check['input_revision']):
        evidence['blocker'] = 'revision_mismatch'
        return evidence
    with tempfile.TemporaryDirectory(prefix='workharbor-outcomes-') as temporary:
        parent = Path(temporary)
        check_file, report = parent/'check.json', parent/'report.json'
        check_file.write_bytes(canonical(check))
        env = {'PATH': os.defpath, 'HOME': str(parent), 'TMPDIR': str(parent), 'LC_ALL': 'C',
               'PYTHONDONTWRITEBYTECODE': '1', 'PYTHONNOUSERSITE': '1'}
        argv = [sys.executable, '-I', '-B', str(Path(__file__).resolve()), '--child', str(check_file),
                '--root', str(root), '--report', str(report)]
        process = subprocess.Popen(argv, cwd=root, env=env, stdin=subprocess.DEVNULL,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
        try:
            process.wait(timeout=check['deadline_seconds'])
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
            evidence.update(raw_exit=process.returncode, runner_exit=process.returncode, blocker='timeout')
        else:
            evidence['raw_exit'] = evidence['runner_exit'] = process.returncode
            if report.exists():
                observed = _load(report)
                evidence['case_ids'] = observed.get('case_ids', [])
                evidence['observed'] = observed.get('observed', [])
                evidence['accepted'] = observed.get('accepted') is True and process.returncode == check['raw_exit']
                if observed.get('blocker') == 'invalid_collection':
                    evidence.update(raw_exit=None, blocker='invalid_collection')
            if not evidence['accepted'] and evidence['blocker'] is None:
                evidence['blocker'] = 'outcome_mismatch'
    if (snapshot(root) != check['input_revision'] or digest(Path(__file__).read_bytes()) != tool_revision()):
        evidence.update(accepted=False, blocker='mutating_check')
    return evidence


def run_checks(root, checks, *, approved_checks, matrix_approved=False):
    """Execute only separately supplied exact approved inputs; no guard copy."""
    result = {'accepted': False, 'matrix_digest': None, 'checks': [],
              'authenticated_approval': False, 'matrix_approval_supplied': matrix_approved is True, 'blocker': None}
    try:
        result['matrix_digest'] = checks_digest(checks)
        if canonical(checks) != canonical(validate_checks(approved_checks)):
            raise ValueError('approval_mismatch')
        if any(case['expected'] == 'assertion' for check in checks for case in check['cases']) and matrix_approved is not True:
            raise ValueError('matrix_approval_required')
        root = Path(root).absolute()
        for check in checks:
            evidence = _execute(root, check)
            result['checks'].append(evidence)
            if not evidence['accepted']:
                result['blocker'] = evidence['blocker']
                return result
        result['accepted'] = True
    except (OSError, ValueError, TypeError, UnicodeError, subprocess.SubprocessError):
        result['blocker'] = 'invalid_or_unavailable_outcomes'
    return result


def matrix_argv(root, matrix, checks, *, matrix_approved=False):
    """Portable zero-exit check argv to be explicitly approved by Feature 003."""
    argv = [sys.executable, '-I', '-B', str(Path(__file__).resolve()), '--matrix', str(Path(matrix).absolute()),
            '--root', str(Path(root).absolute()), '--approved-digest', checks_digest(checks)]
    if matrix_approved is True:
        argv.append('--matrix-approved')
    return argv


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError('invalid_arguments')


def main(argv=None):
    parser = Parser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--matrix', type=Path)
    parser.add_argument('--approved-digest')
    parser.add_argument('--matrix-approved', action='store_true')
    parser.add_argument('--child', type=Path, help=argparse.SUPPRESS)
    parser.add_argument('--report', type=Path, help=argparse.SUPPRESS)
    try:
        args = parser.parse_args(argv)
        if args.child and args.report and not args.matrix:
            return _child(_load(args.child), args.root, args.report)
        checks = _load(args.matrix)
        if checks_digest(checks) != args.approved_digest:
            raise ValueError('approval_mismatch')
        result = run_checks(args.root, checks, approved_checks=checks, matrix_approved=args.matrix_approved)
    except (OSError, ValueError, TypeError, UnicodeError):
        result = {'accepted': False, 'blocker': 'invalid_or_unavailable_outcomes', 'checks': [],
                  'authenticated_approval': False}
    print(canonical(result).decode('utf-8'))
    return 0 if result['accepted'] else 4


if __name__ == '__main__':
    raise SystemExit(main())
