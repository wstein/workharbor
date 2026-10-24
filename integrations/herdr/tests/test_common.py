# SPDX-License-Identifier: EUPL-1.2
import os
import unittest
from unittest import mock
from common import identifier, open_pane


class HerdrBoundaryTests(unittest.TestCase):
    def test_identifier_rejects_trailing_hostile_data(self):
        for value in ['w1:p1\n--x', 'w1:p1 trailing', '-x', '', None]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                identifier(value)

    def test_placement_specific_flags(self):
        env = dict(HERDR_WORKSPACE_ID='w1')
        for placement, expected in [('overlay', []), ('tab', ['--workspace', 'w1']), ('split', ['--target-pane', 'w1:p0'])]:
            with mock.patch.dict(os.environ, env, clear=True), mock.patch('common.herdr') as run:
                open_pane('workspace', placement, [], 'w1:p0')
                self.assertEqual(['plugin', 'pane', 'open', '--plugin', 'workharbor.local', '--entrypoint', 'workspace', '--placement', placement, '--focus', *expected], run.call_args.args[0])

    def test_invalid_used_context_refuses_before_herdr(self):
        for placement, workspace, target in [('tab', '-x', 'w1:p0'), ('split', 'w1', '-x'), ('split', 'w1', None)]:
            with mock.patch.dict(os.environ, dict(HERDR_WORKSPACE_ID=workspace), clear=True), mock.patch('common.herdr') as run, self.assertRaises(ValueError):
                open_pane('workspace', placement, [], target)
            run.assert_not_called()


    def test_unknown_placement_never_calls_herdr(self):
        with mock.patch('common.herdr') as run, self.assertRaises(ValueError):
            open_pane('workspace', 'unsupported', [], 'w1:p0')
        run.assert_not_called()
