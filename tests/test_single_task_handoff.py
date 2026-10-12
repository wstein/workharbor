"""Actual T023 public handoff freshness and durable acceptance identity."""
import json
from pathlib import Path
import subprocess
import sys
import unittest
import test_single_task_binding as fixtures
from test_single_task import canonical_bytes
import single_task_state as state


class HandoffFreshnessTests(unittest.TestCase):
    commit_current = fixtures.ImplementationBindingTests.commit_current

    def setUp(self):
        fixtures.ImplementationBindingTests.setUp(self)
        self.script = Path(state.__file__).with_name('single_task.py')
        self.review_path = self.fixture.private/'handoff-review.json'
        self.review_path.write_bytes(canonical_bytes(self.review))
        result = self.cli('record-commit', '--attempt', self.attempt_id, '--commit', self.commit)
        self.assertEqual(0, result.returncode, 'Real public commit binding is prerequisite')

    def cli(self, *args, injection=None):
        command = ([sys.executable, '-B', '-c', injection, str(self.script)] if injection else
                   [sys.executable, '-B', str(self.script)])
        return subprocess.run(command + list(args), cwd=self.root, env=self.fixture.env,
                              capture_output=True, text=True, timeout=20)

    def test_ready_record_binds_exact_fresh_acceptance(self):
        result = self.cli('resume', '--attempt', self.attempt_id, '--review', str(self.review_path))
        self.assertEqual(0, result.returncode)
        self.assertEqual('handoff-ready', json.loads(result.stdout)['progress'])
        with state.open_store(self.root, require_existing=True) as store:
            events = store.read_events(self.attempt_id)
            fresh = next(row for row in reversed(events) if row['kind'] == 'acceptance')
            ready = events[-1]['payload']
            self.assertEqual('handoff-verification', events[-1]['kind'])
            self.assertEqual(fresh['sequence'], ready['acceptance_sequence'])
            self.assertEqual(fresh['payload_digest'], ready['acceptance_digest'])
            self.assertEqual(fresh['payload']['verification_head'], ready['verification_head'])
            self.assertEqual(self.commit, ready['implementation_commit'])
            self.assertEqual(self.commit, state.implementation_binding(store, self.attempt_id)['implementation_commit'])

    def test_head_change_after_checks_and_binding_cannot_mark_ready(self):
        injection = r'''import pathlib,runpy,sys
sys.path.insert(0,str(pathlib.Path(sys.argv[1]).parent))
import single_task_state as state
import single_task_scope as scope
original=state.bind_implementation
def changed(*args,**kwargs):
    value=original(*args,**kwargs)
    scope.git(args[0],'-c','user.name=Synthetic Test','-c','user.email=test@example.invalid',
              '-c','commit.gpgSign=false','commit','--allow-empty','-m','test: post-check HEAD change')
    return value
state.bind_implementation=changed
sys.argv=sys.argv[1:]
runpy.run_path(sys.argv[0],run_name='__main__')
'''
        with state.open_store(self.root, require_existing=True) as store:
            before = store.read_events(self.attempt_id)
        result = self.cli('resume', '--attempt', self.attempt_id, '--review', str(self.review_path),
                          injection=injection)
        self.assertEqual(3, result.returncode, 'Unverified new HEAD must block handoff readiness')
        self.assertEqual('state_blocker', json.loads(result.stdout)['errors'][0]['code'])
        with state.open_store(self.root, require_existing=True) as store:
            events = store.read_events(self.attempt_id)
            self.assertEqual(before, events[:len(before)])
            self.assertEqual('completed-awaiting-commit', store.read_attempt(self.attempt_id)['progress'])
            self.assertFalse(any(row['kind'] == 'handoff-verification' for row in events))
            self.assertEqual(self.commit, state.implementation_binding(store, self.attempt_id)['implementation_commit'])
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_argument_failure_after_completion_stays_safe_and_stable(self):
        result = self.cli('status')
        self.assertEqual(2, result.returncode)
        value = json.loads(result.stdout)
        self.assertEqual('invalid_arguments', value['errors'][0]['code'])
        self.assertEqual('', result.stderr)
        self.assertEqual(self.after, self.tasks.read_bytes())
