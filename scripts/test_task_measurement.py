"""Synthetic administration records; no live timing or efficiency evidence."""
from dataclasses import replace
from datetime import datetime, timedelta, timezone
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch

import task_handoff as handoff


EVIDENCE = 'sha256:' + 'e' * 64
KINDS = ('preparation', 'approval', 'check', 'configuration', 'handoff',
         'handoff-repair', 'implementation', 'computation', 'waiting')


def observation(sample=1, task='004/T001', categories=('documentation',), *, retry=False):
    start = datetime(2026, 10, 12, tzinfo=timezone.utc) + timedelta(days=sample)
    attempts = ('initial', 'retry') if retry else ('initial',)
    intervals = []
    for index, kind in enumerate(KINDS):
        def timestamp(offset):
            return (start + timedelta(seconds=offset)).isoformat().replace('+00:00', 'Z')
        intervals.append(handoff.AdministrationInterval(
            timestamp(index * 10), timestamp((index + 1) * 10), kind,
            attempts[-1] if retry and index >= 5 else attempts[0], EVIDENCE))
    return handoff.AdministrationObservation(
        sample=sample, task=task, categories=categories,
        package_digest='a' * 64, source_revision='b' * 40,
        runtime_revision='sha256:' + 'c' * 64,
        tool_revisions={'python': 'version:3.14.8', 'git': 'd' * 40},
        started_at=intervals[0].started_at, ended_at=intervals[-1].ended_at,
        active_administration_seconds=60, intervals=tuple(intervals),
        attempts=attempts, evidence_ref=EVIDENCE)


class ObservationTests(unittest.TestCase):
    def test_retains_individual_revisions_intervals_exclusions_and_retry_chain(self):
        value = observation(categories=('implementation', 'interrupted-retry'), retry=True)
        self.assertIs(value, handoff.validate_administration_observation(value))
        self.assertEqual(KINDS, tuple(item.activity for item in value.intervals))
        self.assertEqual(('initial', 'retry'), value.attempts)
        self.assertIn(b'"version:3.14.8"', value.encoded())
        self.assertIn(b'"waiting"', value.encoded())

    def test_all_administration_counts_and_only_three_exclusions_are_subtracted(self):
        value = observation()
        # Six administration intervals count, including preparation, active
        # approval/check/configuration work, handoff and its repair.
        for seconds in (0, 10, 20, 30, 40, 50, 70, 90):
            with self.subTest(seconds=seconds), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(
                    replace(value, active_administration_seconds=seconds))
        for activity in ('approval-wait', 'check-computation', 'other-excluded'):
            intervals = (replace(value.intervals[0], activity=activity),) + value.intervals[1:]
            with self.subTest(activity=activity), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(replace(value, intervals=intervals))

    def test_complete_timeline_rejects_missing_selective_overlap_and_outside_intervals(self):
        value = observation()
        cases = (
            value.intervals[1:], value.intervals[:-1],
            value.intervals[:2] + value.intervals[3:],
            value.intervals[:6] + value.intervals[7:],  # omitted exclusion
            (value.intervals[0],) + value.intervals,
            tuple(reversed(value.intervals)),
            (replace(value.intervals[0], ended_at=value.intervals[0].started_at),)
            + value.intervals[1:],
            (replace(value.intervals[0], started_at='2026-10-12T00:00:00Z'),)
            + value.intervals[1:])
        for intervals in cases:
            with self.subTest(intervals=intervals), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(replace(value, intervals=intervals))
        for change in (dict(started_at=value.intervals[1].started_at),
                       dict(ended_at=value.intervals[-2].ended_at)):
            with self.subTest(change=change), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(replace(value, **change))

    def test_bad_identity_pins_timing_numbers_and_evidence_are_rejected(self):
        value = observation()
        cases = (
            dict(sample=True), dict(sample=0), dict(task=''), dict(categories=()),
            dict(categories=('documentation', 'documentation')),
            dict(package_digest='main'), dict(source_revision='HEAD'),
            dict(runtime_revision='latest'), dict(tool_revisions={}),
            dict(tool_revisions={'python': 'latest'}),
            dict(tool_revisions={'python': 'version:>=3.11'}),
            dict(tool_revisions={'': 'version:3.14.8'}),
            dict(started_at='2026-13-01T00:00:00Z'),
            dict(started_at='2026-10-13T00:00:00'),
            dict(active_administration_seconds=True),
            dict(active_administration_seconds=-1),
            dict(active_administration_seconds=float('nan')),
            dict(active_administration_seconds=float('inf')),
            dict(evidence_ref=''), dict(attempts=()), dict(attempts=('initial', 'initial')))
        for change in cases:
            with self.subTest(change=change), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(replace(value, **change))
        for change in (dict(evidence_ref=''), dict(attempt_id='not-recorded')):
            intervals = (replace(value.intervals[0], **change),) + value.intervals[1:]
            with self.subTest(change=change), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(replace(value, intervals=intervals))

    def test_subsecond_intervals_are_counted_without_rounding_away_work(self):
        interval = handoff.AdministrationInterval('2026-10-12T00:00:00Z',
            '2026-10-12T00:00:00.125Z', 'preparation', 'initial', EVIDENCE)
        value = replace(observation(), started_at=interval.started_at, ended_at=interval.ended_at,
                        intervals=(interval,), active_administration_seconds=0.125)
        handoff.validate_administration_observation(value)
        with self.assertRaises(handoff.InvalidState):
            handoff.validate_administration_observation(
                replace(value, active_administration_seconds=0))

    def test_retry_relationships_require_all_ordered_attempts_and_category(self):
        value = observation(categories=('implementation', 'interrupted-retry'), retry=True)
        cases = (dict(attempts=('initial',)), dict(attempts=('initial', 'unused', 'retry')),
                 dict(attempts=('retry', 'initial')), dict(categories=('implementation',)))
        for change in cases:
            with self.subTest(change=change), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(replace(value, **change))
        with self.assertRaises(handoff.InvalidState):
            handoff.validate_administration_observation(
                observation(categories=('documentation', 'interrupted-retry')))

    def test_bad_objects_fail_as_validation_errors(self):
        for value in (None, {}, replace(observation(), intervals=(None,)),
                      replace(observation(), tool_revisions=[]),
                      replace(observation(), categories='documentation')):
            with self.subTest(value=value), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_observation(value)


class SampleTests(unittest.TestCase):
    def setUp(self):
        self.values = tuple(observation(index, f'004/T{index:03}', categories, retry=retry)
            for index, categories, retry in (
                (1, ('documentation',), False), (2, ('implementation',), False),
                (3, ('test-authoring',), False),
                (4, ('implementation', 'interrupted-retry'), True),
                (5, ('documentation',), False)))
        self.roster = tuple((item.sample, item.task) for item in self.values)

    def test_five_consecutive_individual_records_include_required_categories(self):
        self.assertEqual(self.values,
            handoff.validate_administration_sample(self.values, self.roster))

    def test_missing_extra_selected_duplicate_or_reordered_samples_rejected(self):
        for values in (self.values[:-1], self.values[1:], self.values + self.values[:1],
                       self.values[:2] + self.values[3:], tuple(reversed(self.values)),
                       self.values[:4] + self.values[:1],
                       self.values[:4] + (replace(self.values[4], task='004/T099'),)):
            with self.subTest(values=values), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_sample(values, self.roster)

    def test_caller_supplies_complete_consecutive_roster_and_category_coverage(self):
        for roster in ((), self.roster[:-1], self.roster + ((6, '004/T006'),),
                       ((0, '004/T000'),) + self.roster[1:],
                       self.roster[:4] + ((7, '004/T005'),),
                       self.roster[:4] + ((5, '004/T001'),)):
            with self.subTest(roster=roster), self.assertRaises(handoff.InvalidState):
                handoff.validate_administration_sample(self.values, roster)
        values = tuple(observation(index, f'004/T{index:03}') for index in range(1, 6))
        with self.assertRaises(handoff.InvalidState):
            handoff.validate_administration_sample(values, self.roster)

    def test_validation_does_not_turn_a_target_miss_into_missing_data(self):
        value = self.values[0]
        interval = replace(value.intervals[0], ended_at='2026-10-13T00:11:00Z')
        missed = replace(value, ended_at=interval.ended_at, intervals=(interval,),
                         active_administration_seconds=660)
        values = (missed,) + self.values[1:]
        self.assertEqual(660,
            handoff.validate_administration_sample(values, self.roster)[0]
            .active_administration_seconds)


class CaptureTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.value = observation()
        self.key = handoff.HandoffKey('fixture/repo', '17', 'a' * 64, 'initial')

    def open(self):
        return handoff.open_journal(self.root, 'fixture/repo', '17')

    def test_capture_reopen_and_repeat_keep_one_exact_observation(self):
        with self.open() as journal:
            journal.create_record(self.key)
            journal.capture_administration(self.key, self.value)
            journal.capture_administration(self.key, self.value)
            self.assertEqual(1, len(journal.events(self.key)))
        with self.open() as journal:
            self.assertEqual((self.value,), journal.administration_observations(self.key))
            journal.capture_administration(self.key, self.value)
            self.assertEqual(1, len(journal.events(self.key)))

    def test_invalid_conflicting_and_wrong_attempt_or_package_capture_retains_evidence(self):
        with self.open() as journal:
            journal.create_record(self.key)
            for value in (replace(self.value, package_digest='f' * 64),
                          replace(self.value, active_administration_seconds=0),
                          replace(self.value, attempts=('other',))):
                with self.subTest(value=value), self.assertRaises(handoff.InvalidState):
                    journal.capture_administration(self.key, value)
            self.assertEqual([], journal.events(self.key))
            journal.capture_administration(self.key, self.value)
            for change in (dict(task='004/T099'), dict(sample=2), dict(source_revision='f' * 40)):
                with self.subTest(change=change), self.assertRaises(handoff.StateConflict):
                    journal.capture_administration(self.key, replace(self.value, **change))
            self.assertEqual((self.value,), journal.administration_observations(self.key))

    def test_reserved_event_cannot_bypass_capture_validation(self):
        with self.open() as journal:
            journal.create_record(self.key)
            with self.assertRaises(handoff.InvalidState):
                journal.append(self.key, 'administration-observation', {'invented': True})
            self.assertEqual([], journal.events(self.key))

    def test_retained_invalid_observation_blocks_reopen_without_repair(self):
        with self.open() as journal:
            journal.create_record(self.key)
            # Model unknown/corrupt persisted evidence through the internal
            # primitive, bypassing the public capture API only in this fixture.
            journal._append(self.key, 'administration-observation', {'invented': True})
        database = next(self.root.glob('.symphony/task-handoff/*/journal.sqlite3'))
        before = database.read_bytes()
        with self.assertRaises(handoff.InvalidState):
            with self.open():
                pass
        self.assertEqual(before, database.read_bytes())

    def test_persistence_failure_retains_no_partial_measurement(self):
        with self.open() as journal:
            journal.create_record(self.key)
            with patch('task_handoff.os.fsync', side_effect=OSError('fixture failure')):
                with self.assertRaises(handoff.PersistenceFailure):
                    journal.capture_administration(self.key, self.value)
        with self.open() as journal:
            self.assertEqual((), journal.administration_observations(self.key))


if __name__ == '__main__':
    unittest.main()
