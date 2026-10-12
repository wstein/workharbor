#!/usr/bin/env python3
"""Operator-reviewed T002 expected-red gate, not a green implementation claim."""
import contextlib
import io
import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def flatten(suite):
    for test in suite:
        if isinstance(test, unittest.TestSuite):
            yield from flatten(test)
        else:
            yield test


class Outcomes(unittest.TestResult):
    def __init__(self):
        super().__init__()
        self.observed = []

    def addSuccess(self, test):
        super().addSuccess(test)
        self.observed.append((test.id(), 'pass', None))

    def addFailure(self, test, error):
        super().addFailure(test, error)
        # A setup/cleanup failure is never a test's expected red assertion.
        frames = []
        trace = error[2]
        while trace:
            frames.append(trace.tb_frame.f_code)
            trace = trace.tb_next
        method = getattr(test, test._testMethodName)
        intended = method.__func__.__code__ in frames
        self.observed.append((test.id(), 'assertion' if intended else 'setup-failure', str(error[1])))

    def addError(self, test, error):
        super().addError(test, error)
        self.observed.append((test.id(), 'error', None))

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        self.observed.append((test.id(), 'skip', None))

    def addExpectedFailure(self, test, error):
        super().addExpectedFailure(test, error)
        self.observed.append((test.id(), 'xfail', None))

    def addUnexpectedSuccess(self, test):
        super().addUnexpectedSuccess(test)
        self.observed.append((test.id(), 'unexpected-success', None))


def evaluate(cases, baseline, red):
    expected = set(baseline) | set(red)
    ids = [case[0] for case in cases]
    if len(ids) != len(set(ids)) or set(ids) != expected:
        return False
    return all((kind == 'pass' and message is None) if name in baseline else
               (kind == 'assertion' and message == red[name])
               for name, kind, message in cases)


def self_test():
    baseline, red = {'base'}, {'red': 'approved assertion'}
    good = [('base', 'pass', None), ('red', 'assertion', 'approved assertion')]
    assert evaluate(good, baseline, red)
    for kind in ('pass', 'error', 'skip', 'xfail', 'unexpected-success', 'setup-failure'):
        assert not evaluate([good[0], ('red', kind, 'approved assertion')], baseline, red)
    for bad in (good[:1], good + [good[1]], good + [('extra', 'pass', None)],
                [('base', 'assertion', 'regression'), good[1]],
                [good[0], ('red', 'assertion', 'wrong failure')]):
        assert not evaluate(bad, baseline, red)
    class AssertionCase(unittest.TestCase):
        def test_red(self):
            self.fail('approved assertion')
    class SetupCase(AssertionCase):
        def setUp(self):
            self.fail('approved assertion')
    for case, expected in ((AssertionCase, 'assertion'), (SetupCase, 'setup-failure')):
        result = Outcomes()
        case('test_red').run(result)
        assert result.observed[0][1:] == (expected, 'approved assertion')
    print('T002 verifier self-test passed (expected red, invalid outcomes and setup rejection).')


def main():
    if sys.argv[1:] == ['--self-test']:
        self_test()
        return 0
    if sys.argv[1:]:
        raise ValueError('No task-selection or scope override arguments are permitted')
    contract = json.loads((ROOT/'docs/development/bootstrap-t002.json').read_text())
    baseline = set(contract['baseline_test_ids'])
    red = {case['id']: case['failure'] for case in contract['red_cases']}
    if len(red) != 10 or len(baseline) != 31 or baseline & set(red):
        raise ValueError('Invalid reviewed case matrix')
    sys.path.insert(0, str(ROOT/'tests'))
    suite = unittest.defaultTestLoader.discover(str(ROOT/'tests'))
    tests = list(flatten(suite))
    ids = [test.id() for test in tests]
    if len(ids) != len(set(ids)) or set(ids) != baseline | set(red):
        print('T002 blocked: missing/extra/duplicate tests or import failure; expected 31 baseline + 10 red cases.')
        return 1
    result = Outcomes()
    # Never publish arbitrary exception text, test output or secret-bearing argv.
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        unittest.TestSuite(tests).run(result)
    accepted = evaluate(result.observed, baseline, red)
    report = {'schema_version': 1, 'task': 'T002', 'expected_red_verified': accepted,
              'implementation_verified': False, 'tests_run': result.testsRun,
              'outcomes': [{'id': name, 'outcome': kind,
                            'matches_approved_failure': name in red and kind == 'assertion' and message == red[name]}
                           for name, kind, message in result.observed]}
    print(json.dumps(report, indent=2))
    return 0 if accepted and result.testsRun == 41 else 1


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (ValueError, OSError, KeyError, TypeError):
        raise SystemExit('T002 verifier blocked: invalid contract or local inputs')
