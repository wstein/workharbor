"""T003 durable local journal fixtures; no provider or guard authority."""
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from contextlib import closing

import task_handoff as handoff


class ExclusiveOwnership:
    def __init__(self, state='exclusive', owner=None):
        self.state = state
        self.owner = owner

    def observe(self, repository, issue, owner):
        return handoff.OwnershipEvidence(repository, issue, self.owner or owner,
                                         self.state, 'fixture-verifier', 'fixture-observation')


class JournalTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.key = handoff.HandoffKey('fixture/repo', '17', 'a' * 64, 'guard-attempt')
        self.owner = handoff.Owner(str(self.root), 'fixture-host', 'fixture-process')

    def open(self, root=None, issue='17'):
        return handoff.open_journal(root or self.root, 'fixture/repo', issue)

    def child(self, code):
        return subprocess.run([sys.executable, '-B', '-c', code, str(self.root)],
                              env={**os.environ, 'PYTHONPATH': str(Path(__file__).parent)},
                              capture_output=True, text=True, timeout=10)

    def test_monotonic_keyed_events_and_reopen(self):
        with self.open() as journal:
            journal.create_record(self.key)
            self.assertEqual(1, journal.append(self.key, 'prepared', {'digest': 'a'}))
            self.assertEqual(2, journal.append(self.key, 'blocker', {'reason': 'fixture'}))
            other = handoff.HandoffKey('fixture/repo', '17', 'b' * 64, 'guard-attempt')
            journal.create_record(other)
            self.assertEqual(1, journal.append(other, 'prepared', {}))
        with self.open() as journal:
            self.assertEqual([1, 2], [event['sequence'] for event in journal.events(self.key)])
            self.assertEqual(3, journal.append(self.key, 'observed', {}))

    def test_intents_results_are_durable_idempotent_and_bound(self):
        with self.open() as journal:
            journal.create_record(self.key)
            first = journal.intent(self.key, 'publication', {'identity': 'exact', 'precondition': 'pin'})
            self.assertEqual(first, journal.intent(self.key, 'publication',
                                                  {'precondition': 'pin', 'identity': 'exact'}))
            with self.assertRaises(handoff.StateConflict):
                journal.intent(self.key, 'publication', {'identity': 'changed'})
            journal.result(self.key, 'publication', 'write-unknown', {'reason': 'timeout'})
        with self.open() as journal:
            self.assertEqual('write-unknown', journal.operation(self.key, 'publication')['status'])
            self.assertEqual('exact', journal.operation(self.key, 'publication')['intent']['identity'])
            journal.result(self.key, 'publication', 'succeeded', {'object': 'one'})
            count = len(journal.events(self.key))
            journal.result(self.key, 'publication', 'succeeded', {'object': 'one'})
            self.assertEqual(count, len(journal.events(self.key)))
            with self.assertRaises(handoff.StateConflict):
                journal.result(self.key, 'publication', 'write-unknown', {})
            with self.assertRaises(handoff.StateConflict):
                journal.result(self.key, 'missing-intent', 'succeeded', {})

    def test_rollback_cannot_commit_nested_append(self):
        with self.open() as journal:
            journal.create_record(self.key)
            with self.assertRaises(RuntimeError):
                with journal.transaction():
                    journal.append(self.key, 'discarded', {})
                    raise RuntimeError('fixture')
            self.assertEqual([], journal.events(self.key))

    def test_crash_after_intent_retains_it_and_releases_lock(self):
        result = self.child("""
import os, sys
from pathlib import Path
import task_handoff as h
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    key = h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt')
    j.create_record(key)
    j.intent(key, 'publication', {'identity': 'one'})
    os._exit(23)
""")
        self.assertEqual(23, result.returncode, result.stderr)
        with self.open() as journal:
            self.assertEqual('intent', journal.operation(self.key, 'publication')['status'])

    def test_crash_before_transaction_commit_retains_prior_evidence(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'prepared', {})
        result = self.child("""
import os, sys
from pathlib import Path
import task_handoff as h
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    with j.transaction():
        j.append(h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt'), 'lost', {})
        os._exit(24)
""")
        self.assertEqual(24, result.returncode, result.stderr)
        with self.open() as journal:
            self.assertEqual(['prepared'], [event['kind'] for event in journal.events(self.key)])

    def test_competing_process_cannot_open_same_issue(self):
        with self.open():
            result = self.child("""
import sys
from pathlib import Path
import task_handoff as h
try:
    with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17'):
        pass
except h.StateConflict:
    sys.exit(5)
""")
            self.assertEqual(5, result.returncode, result.stderr)
        with self.open():
            pass

    def test_issue_locks_are_separate(self):
        with self.open():
            with self.open(issue='18'):
                pass

    def test_ownership_requires_fresh_cross_host_port(self):
        with self.open() as journal:
            journal.claim_owner(self.owner)
            with self.assertRaises(handoff.StateConflict):
                journal.require_ownership(self.owner)
            journal.require_ownership(self.owner, ExclusiveOwnership())
            for state in ('unknown', 'conflict'):
                with self.subTest(state=state), self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(self.owner, ExclusiveOwnership(state))
            self.assertEqual(self.owner, journal.owner())
        with self.open() as journal:
            with self.assertRaises(handoff.StateConflict):
                journal.require_ownership(self.owner)
            self.assertEqual(self.owner, journal.owner())

    def test_competing_checkout_host_process_preserves_owner(self):
        with self.open() as journal:
            journal.claim_owner(self.owner)
            for other in (handoff.Owner(str(self.root), 'other-host', 'fixture-process'),
                          handoff.Owner(str(self.root), 'fixture-host', 'other-process')):
                with self.subTest(other=other), self.assertRaises(handoff.StateConflict):
                    journal.claim_owner(other)
                with self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(self.owner, ExclusiveOwnership(owner=other))
            self.assertEqual(self.owner, journal.owner())
        with tempfile.TemporaryDirectory() as other_root:
            with self.open(Path(other_root).resolve()) as journal:
                other = handoff.Owner(str(Path(other_root).resolve()), 'other-host', 'other-process')
                journal.claim_owner(other)
                with self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(other, ExclusiveOwnership(owner=self.owner))

    def test_guard_journal_is_untouched_and_handoff_is_ignored(self):
        guard = self.root / '.symphony/single-task/journal.sqlite3'
        guard.parent.mkdir(parents=True)
        guard.write_bytes(b'guard-owned-fixture')
        with self.open() as journal:
            journal.create_record(self.key)
        self.assertEqual(b'guard-owned-fixture', guard.read_bytes())
        repo = Path(__file__).resolve().parents[1]
        result = subprocess.run(['git', 'check-ignore', '.symphony/task-handoff/example/journal.sqlite3'],
                                cwd=repo, capture_output=True, text=True)
        self.assertEqual(0, result.returncode, result.stderr)

    def test_corrupt_or_unknown_schema_retained(self):
        with self.open():
            pass
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        for mutation in ('corrupt', 'version', 'trigger'):
            original = database.read_bytes()
            if mutation == 'corrupt':
                database.write_bytes(b'not sqlite')
            else:
                with closing(sqlite3.connect(database)) as connection:
                    connection.execute('PRAGMA user_version=99' if mutation == 'version'
                                       else 'DROP TRIGGER immutable_events_delete')
                    connection.commit()
            before = database.read_bytes()
            with self.subTest(mutation=mutation), self.assertRaises(handoff.InvalidState):
                with self.open():
                    pass
            self.assertEqual(before, database.read_bytes())
            database.write_bytes(original)

    def test_unsafe_paths_and_hardlinks_rejected(self):
        with self.open():
            pass
        directory = next(self.root.glob('.symphony/task-handoff/*'))
        database = directory / 'journal.sqlite3'
        copy = self.root / 'retained'
        database.rename(copy)
        database.symlink_to(copy)
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        database.unlink()
        os.link(copy, database)
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        database.unlink()
        copy.rename(database)
        with tempfile.TemporaryDirectory() as outside:
            directory.rename(self.root / 'retained-directory')
            directory.symlink_to(outside, target_is_directory=True)
            with self.assertRaises(handoff.InvalidState):
                with self.open():
                    pass
            self.assertEqual([], list(Path(outside).iterdir()))

    def test_missing_journal_does_not_invent_new_baseline(self):
        with self.open() as journal:
            journal.claim_owner(self.owner)
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        database.unlink()
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass

    def test_append_only_sql_and_reserved_operation_events(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'prepared', {})
            for kind in ('intent', 'result'):
                with self.assertRaises(handoff.InvalidState):
                    journal.append(self.key, kind, {})
            for statement in ('DELETE FROM events', "UPDATE events SET kind='changed'",
                              'DELETE FROM records'):
                with self.subTest(statement=statement), self.assertRaises(sqlite3.IntegrityError):
                    journal._db.execute(statement)
            self.assertEqual(['prepared'], [event['kind'] for event in journal.events(self.key)])

    def test_unknown_ownership_port_failure_preserves_identity(self):
        class Broken:
            def observe(self, *args):
                raise RuntimeError('untrusted diagnostic fixture')

        class Mismatched:
            def observe(self, repository, issue, owner):
                return handoff.OwnershipEvidence('fixture/other', issue, owner,
                                                 'exclusive', 'fixture-verifier', 'fixture-ref')

        with self.open() as journal:
            journal.claim_owner(self.owner)
            for port in (Broken(), Mismatched()):
                with self.subTest(port=type(port).__name__), self.assertRaises(handoff.StateConflict):
                    journal.require_ownership(self.owner, port)
            self.assertEqual(self.owner, journal.owner())

    def test_directory_durability_failure_rolls_back_event(self):
        with self.open() as journal:
            journal.create_record(self.key)
            with patch('task_handoff.os.fsync', side_effect=OSError('fixture fsync failure')):
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.append(self.key, 'uncommitted', {})
            with self.assertRaises(handoff.PersistenceFailure):
                journal.events(self.key)
        with self.open() as journal:
            self.assertEqual([], journal.events(self.key))

    def test_symlinked_ancestor_rejected(self):
        actual = self.root / 'actual'
        actual.mkdir()
        alias = self.root / 'alias'
        alias.symlink_to(actual, target_is_directory=True)
        child = actual / 'child'
        child.mkdir()
        with self.assertRaises(handoff.InvalidState):
            with self.open(alias / 'child'):
                pass
        self.assertEqual([], list(child.iterdir()))

    def test_large_uncommitted_crash_retains_only_committed_snapshot(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'prepared', {})
        result = self.child("""
import os, sys
from pathlib import Path
import task_handoff as h
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    j._db.execute('PRAGMA cache_size=5')
    with j.transaction():
        for i in range(48):
            j.append(h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt'),
                     'uncommitted', {'evidence': 'x'*65536})
        os._exit(25)
""")
        self.assertEqual(25, result.returncode, result.stderr)
        with self.open() as journal:
            self.assertEqual(['prepared'], [event['kind'] for event in journal.events(self.key)])

    def test_checkpoint_crashes_reopen_complete_old_or_new_state(self):
        with self.open() as journal:
            journal.create_record(self.key)
        for after in (False, True):
            result = self.child("""
import os, sys
from pathlib import Path
from unittest.mock import patch
import task_handoff as h
actual = os.replace
def interrupted(*args, **kwargs):
    if AFTER:
        actual(*args, **kwargs)
    os._exit(26)
with h.open_journal(Path(sys.argv[1]), 'fixture/repo', '17') as j:
    with patch('task_handoff.os.replace', side_effect=interrupted):
        j.append(h.HandoffKey('fixture/repo', '17', 'a'*64, 'guard-attempt'), 'checkpoint', {})
""".replace('AFTER', repr(after)))
            self.assertEqual(26, result.returncode, result.stderr)
            with self.open() as journal:
                self.assertEqual(int(after), len(journal.events(self.key)))
        directory = next(self.root.glob('.symphony/task-handoff/*'))
        retained = list(directory.glob('candidate-*.sqlite3'))
        self.assertEqual(1, len(retained))
        before = retained[0].read_bytes()
        with self.open() as journal:
            journal.append(self.key, 'later', {})
        self.assertEqual(before, retained[0].read_bytes())

    def test_directory_swap_cannot_write_outside_validated_descriptor(self):
        with self.open() as journal:
            journal.create_record(self.key)
        directory = next(self.root.glob('.symphony/task-handoff/*'))
        retained = self.root / 'retained-issue'
        with tempfile.TemporaryDirectory() as outside:
            outside = Path(outside).resolve()
            sentinel = outside / 'journal.sqlite3'
            sentinel.write_bytes(b'outside sentinel')
            actual = os.replace
            def swapped(*args, **kwargs):
                directory.rename(retained)
                directory.symlink_to(outside, target_is_directory=True)
                return actual(*args, **kwargs)
            with self.open() as journal:
                with patch('task_handoff.os.replace', side_effect=swapped):
                    with self.assertRaises(handoff.InvalidState):
                        journal.append(self.key, 'raced', {})
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.append(self.key, 'later', {})
            self.assertEqual(b'outside sentinel', sentinel.read_bytes())
            self.assertEqual(['journal.sqlite3'], [path.name for path in outside.iterdir()])
            self.assertTrue((retained / 'journal.sqlite3').is_file())

    def test_database_bound_checked_before_deserialization(self):
        with self.open():
            pass
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        with database.open('r+b') as stream:
            stream.truncate(handoff.MAX_DATABASE + 1)
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        self.assertEqual(handoff.MAX_DATABASE + 1, database.stat().st_size)

    def test_journal_growth_bound_rejects_without_truncating_evidence(self):
        with patch('task_handoff.MAX_DATABASE', 65536):
            with self.open() as journal:
                journal.create_record(self.key)
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.append(self.key, 'too-large', {'evidence': 'x' * 131072})
            with self.open() as journal:
                self.assertEqual([], journal.events(self.key))

    def test_replaced_or_modified_committed_file_not_overwritten(self):
        with self.open() as journal:
            journal.create_record(self.key)
            database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
            database.write_bytes(b'unknown replacement evidence')
            with self.assertRaises(handoff.InvalidState):
                journal.append(self.key, 'unsafe', {})
            self.assertEqual(b'unknown replacement evidence', database.read_bytes())

    def test_snapshot_replacement_during_load_is_preserved_and_rejected(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.append(self.key, 'initial', {})
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        with tempfile.TemporaryDirectory() as other_root:
            with self.open(Path(other_root).resolve()) as other:
                other.create_record(self.key)
                other.append(self.key, 'new-evidence', {})
            alternate = next(Path(other_root).glob('.symphony/task-handoff/*/journal.sqlite3')).read_bytes()
        for boundary in ('initialize', 'validate'):
            replacement = self.root / 'replacement.sqlite3'
            replacement.write_bytes(alternate)
            method = '__init__' if boundary == 'initialize' else '_validate'
            actual = getattr(handoff.Journal, method)
            def swapped(journal, *args):
                os.replace(replacement, database)
                return actual(journal, *args)
            with self.subTest(boundary=boundary), patch.object(handoff.Journal, method, swapped):
                with self.assertRaises(handoff.InvalidState):
                    with self.open():
                        pass
            self.assertEqual(alternate, database.read_bytes())

    def test_sqlite_snapshot_api_unavailable_fails_closed(self):
        class Unsupported:
            def close(self):
                pass
        with patch('task_handoff.sqlite3.connect', return_value=Unsupported()):
            with self.assertRaises(handoff.PersistenceFailure):
                with self.open():
                    pass
        self.assertEqual([], list(self.root.glob('.symphony/task-handoff/*/journal.sqlite3')))

    def test_key_scope_and_payload_validation(self):
        with self.open() as journal:
            with self.assertRaises(handoff.InvalidState):
                journal.create_record(handoff.HandoffKey('fixture/other', '17', 'a'*64, 'attempt'))
            journal.create_record(self.key)
            for value in ({'nan': float('nan')}, {'huge': 'x' * 1048576}):
                with self.assertRaises(handoff.InvalidState):
                    journal.append(self.key, 'observed', value)
            self.assertEqual([], journal.events(self.key))


if __name__ == '__main__':
    unittest.main()
