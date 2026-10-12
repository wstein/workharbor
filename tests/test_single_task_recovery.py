"""Direct T021 recovery controls; public command wiring belongs to T023."""
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest import mock
import test_single_task_completion as fixtures
import single_task_state as state
import single_task_acceptance as acceptance


class CompletionRecoveryTests(unittest.TestCase):
    def setUp(self):
        fixtures.CompletionPublicationTests.setUp(self)

    def owner(self):
        function = getattr(state, 'recover_completion', None)
        self.assertTrue(callable(function), 'T021 owning crash recovery is missing')
        return function

    def accept(self):
        return fixtures.CompletionPublicationTests.accept(self)

    def recover(self):
        with state.open_store(self.root, require_existing=True, writable_existing=True) as store:
            before = store.read_events(self.attempt_id)
            result = self.owner()(self.root, store, self.attempt_id, self.review)
            after = store.read_events(self.attempt_id)
            self.assertEqual(before, after[:len(before)])
            self.assertEqual(self.attempt_id, result['attempt_id'])
            self.assertEqual('completed-awaiting-commit', result['progress'])
            self.assertEqual(1, sum(e['kind'] == 'completion-intent' for e in after))
            self.assertEqual(1, sum(e['kind'] == 'completion' for e in after))
            self.assertGreater(sum(e['kind'] == 'check-result' for e in after),
                               sum(e['kind'] == 'check-result' for e in before))
            return after

    def test_active_reruns_checks_and_completes_same_attempt(self):
        self.recover()
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_sealed_pre_intent_reruns_checks(self):
        self.accept()
        self.recover()
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_changed_sealed_pre_intent_blocks(self):
        self.accept()
        (self.root/'guide.md').write_bytes(b'Changed sealed content')
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises((state.InvalidState, ValueError)):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_manual_marker_without_intent_blocks_before_checks(self):
        self.tasks.write_bytes(self.after)
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))

    def prepare(self, replaced=False):
        self.accept()
        with state.open_store(self.root) as store:
            replace = state.os.replace
            def interrupt(*args, **kwargs):
                if replaced:
                    replace(*args, **kwargs)
                raise OSError('Injected interruption')
            with mock.patch.object(state.os, 'replace', side_effect=interrupt):
                with self.assertRaises(state.PersistenceFailure):
                    state.publish_completion(self.root, store, self.attempt_id)

    def test_prepared_before_replace_reruns_and_finishes_original_intent(self):
        self.prepare()
        self.recover()
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_prepared_after_replace_reruns_and_finishes_original_intent(self):
        self.prepare(replaced=True)
        self.recover()
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_prepared_failed_check_does_not_publish(self):
        self.prepare()
        self.fail_control.write_bytes(b'fail')
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises(acceptance.AcceptanceFailure):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id)[:len(events)])
            self.assertEqual('completion-prepared', store.read_attempt(self.attempt_id)['progress'])
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_missing_later_intent_is_corruption(self):
        with state.open_store(self.root) as store:
            store.append_event(self.attempt_id, 'fixture-inconsistent-progress', {},
                               progress='completion-prepared')
            events = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_real_crash_after_temporary_fsync_recovers_without_scope_orphan(self):
        self.accept()
        script = r"""import os,pathlib,stat,sys
sys.path.insert(0,sys.argv[1])
import single_task_state as state
flush=state.os.fsync
def crash(fd):
    flush(fd)
    link=os.readlink('/proc/self/fd/'+str(fd)) if sys.platform.startswith('linux') else None
    info=os.fstat(fd)
    if stat.S_ISREG(info.st_mode) and info.st_size==int(sys.argv[4]):
        os._exit(91)
state.os.fsync=crash
with state.open_store(pathlib.Path(sys.argv[2]),require_existing=True,writable_existing=True) as store:
    state.publish_completion(pathlib.Path(sys.argv[2]),store,sys.argv[3])
"""
        result = subprocess.run([sys.executable, '-B', '-c', script,
                                 str(Path(state.__file__).parent), str(self.root), self.attempt_id,
                                 str(len(self.after))], capture_output=True, timeout=15)
        self.assertEqual(91, result.returncode, 'Actual process must crash after temporary bytes are flushed')
        self.assertEqual(self.before, self.tasks.read_bytes())
        self.assertEqual([], list(self.tasks.parent.glob('.single-task-*')))
        temporary = list((self.root/'.symphony/single-task/publication').iterdir())
        self.assertEqual(1, len(temporary))
        self.assertEqual(self.after, temporary[0].read_bytes())
        self.recover()
        self.assertEqual(self.after, self.tasks.read_bytes())
        self.assertFalse(temporary[0].exists())

    def test_prepared_stale_review_blocks_without_publication(self):
        self.prepare()
        self.review['content_manifest_digest'] = '0' * 64
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_prepared_unexpected_private_temporary_is_retained(self):
        self.prepare()
        path = self.root/'.symphony/single-task/publication'/('0' * 64)
        path.write_bytes(b'Unbound evidence')
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(b'Unbound evidence', path.read_bytes())
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_repeated_recovery_reruns_without_duplicate_transition(self):
        self.recover()
        self.recover()
        self.assertEqual(self.after, self.tasks.read_bytes())

    def test_corrupt_checkbox_phase_blocks_before_checks_and_keeps_evidence(self):
        self.prepare()
        with state.open_store(self.root) as store:
            store.append_event(self.attempt_id, 'checkbox-applied',
                               {'schema_version': 1, 'intent_digest': '0' * 64,
                                'tasks_sha256': '0' * 64}, progress='checkbox-applied')
            events = store.read_events(self.attempt_id)
        with state.open_store(self.root) as store:
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_claimed_applied_phase_with_pre_bytes_blocks(self):
        self.prepare()
        with state.open_store(self.root) as store:
            intent = next(row for row in store.read_events(self.attempt_id)
                          if row['kind'] == 'completion-intent')
            store.append_event(self.attempt_id, 'checkbox-applied',
                               {'schema_version': 1, 'intent_digest': intent['payload_digest'],
                                'tasks_sha256': intent['payload']['tasks_after_sha256']},
                               progress='checkbox-applied')
            events = store.read_events(self.attempt_id)
            with self.assertRaises(state.InvalidState):
                self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(self.before, self.tasks.read_bytes())

    def test_publication_file_swapped_to_fifo_rejects_without_blocking(self):
        self.prepare()
        original_open = state.os.open
        swapped = []
        def swap(name, flags, *args, **kwargs):
            if type(name) is str and len(name) == 64 and not flags & os.O_WRONLY:
                os.unlink(name, dir_fd=kwargs['dir_fd'])
                os.mkfifo(name, dir_fd=kwargs['dir_fd'])
                swapped.append(name)
            return original_open(name, flags, *args, **kwargs)
        with state.open_store(self.root) as store:
            events = store.read_events(self.attempt_id)
            with mock.patch.object(state.os, 'open', side_effect=swap):
                with self.assertRaises(state.InvalidState):
                    self.owner()(self.root, store, self.attempt_id, self.review)
            self.assertEqual(events, store.read_events(self.attempt_id))
        self.assertEqual(1, len(swapped))
        self.assertEqual(self.before, self.tasks.read_bytes())
