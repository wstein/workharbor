# SPDX-License-Identifier: EUPL-1.2
import io
import json
import os
import subprocess
import unittest
from unittest import mock
import picker
import actions


def envelope(records):
    return dict(schema_version=1, command='workspace.ls', ok=True, exit_code=0, result=dict(workspaces=records))


class PickerTests(unittest.TestCase):
    def test_picker_uses_approved_cli_fields(self):
        class Record(dict):
            def get(self, key, default=None):
                if key not in ('name', 'state'):
                    raise AssertionError('record field outside FR-018')
                return super().get(key, default)
        records = [Record(name='hello', state='running', image='not-for-display'), Record(name='stale', state='exited')]
        for state in ['created', 'paused', 'restarting', 'removing', 'stopped', 'exited', 'dead', 'unknown', None, 1, 'Running']:
            records.append(Record(name='other', state=state))
        records.append(Record(name='missing'))
        self.assertEqual(['hello'], picker.names_from_result(envelope(records)))
        with mock.patch.object(picker.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, json.dumps(envelope(records)).encode())), mock.patch.dict(os.environ, {}, clear=True):
            self.assertEqual(['hello'], picker.discover())
            argv = picker.subprocess.run.call_args.args[0]
            self.assertEqual(['whr', 'ls', '--local', '--json'], argv)

    def test_picker_rejects_invalid_name(self):
        invalid = ['UPPER', '-x', 'a_', 'a.b', 'a' * 65, 'x\n', '$(touch marker)', None, 1]
        self.assertEqual([], picker.names_from_result(envelope([dict(name=n, state='running') for n in invalid])))
        for document in [{}, envelope({}), dict(envelope([]), ok=False), dict(envelope([]), schema_version=2)]:
            with self.assertRaises(ValueError):
                picker.names_from_result(document)

    def run_picker(self, selection):
        env = dict(HERDR_BIN_PATH='/fake/herdr', HERDR_WORKSPACE_ID='w1', WHR_PANE_TARGET='w1:p0', WHR_PICKER_PLACEMENT='split', WHR_PANE_BACKGROUND='1')
        with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(picker, 'discover', return_value=['hello']), mock.patch.object(picker, 'choose', return_value=selection), mock.patch('common.subprocess.run') as run:
            picker.run()
            return run.call_args_list

    def test_picker_cancel_opens_nothing(self):
        self.assertEqual([], self.run_picker(None))
        with mock.patch.object(picker, 'discover', return_value=[]), mock.patch.object(picker, 'choose') as choose, mock.patch('common.subprocess.run') as run, mock.patch('sys.stderr', io.StringIO()):
            picker.run()
            choose.assert_not_called()
            run.assert_not_called()

    def test_picker_passes_selection_as_data(self):
        calls = self.run_picker('hello')
        self.assertEqual(1, len(calls))
        self.assertEqual(['/fake/herdr', 'plugin', 'pane', 'open', '--plugin', 'workharbor.local', '--entrypoint', 'workspace', '--placement', 'split', '--focus', '--target-pane', 'w1:p0', '--env', 'WHR_PANE_WORKSPACE=hello', '--env', 'WHR_PANE_BACKGROUND=1'], calls[0].args[0])
        with self.assertRaises(ValueError):
            self.run_picker('foreign')
        with self.assertRaises(ValueError):
            self.run_picker('hello; true')

    def test_discovery_failure_and_duplicate_keys_refuse(self):
        for payload in [b'[]', b'{"ok":true,"ok":false}', b'not json']:
            with mock.patch.object(picker.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, payload)), self.assertRaises(ValueError):
                picker.discover()
        with mock.patch.object(picker.subprocess, 'run', side_effect=subprocess.CalledProcessError(68, 'whr')), self.assertRaises(subprocess.CalledProcessError):
            picker.discover()

    def test_action_opens_overlay_and_retains_original_split_target(self):
        env = dict(HERDR_BIN_PATH='/fake/herdr', HERDR_WORKSPACE_ID='w1', HERDR_PANE_ID='w1:p0')
        with mock.patch.dict(os.environ, env, clear=True), mock.patch('common.subprocess.run') as run:
            actions.open_action('picker', 'split', True)
            self.assertEqual(['/fake/herdr', 'plugin', 'pane', 'open', '--plugin', 'workharbor.local', '--entrypoint', 'picker', '--placement', 'overlay', '--focus', '--env', 'WHR_PICKER_PLACEMENT=split', '--env', 'WHR_PANE_TARGET=w1:p0', '--env', 'WHR_PANE_BACKGROUND=1'], run.call_args.args[0])

    def test_picker_validates_target_before_discovery_for_tab_and_split(self):
        for placement, target in [('tab', '-x'), ('split', '-x'), ('split', '')]:
            env = dict(WHR_PICKER_PLACEMENT=placement, WHR_PANE_TARGET=target)
            with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(picker, 'discover', return_value=[]) as discover, mock.patch('common.herdr') as open_call, self.assertRaises(ValueError):
                picker.run()
            discover.assert_not_called()
            open_call.assert_not_called()

    def test_protocol_command_exit_code_and_duplicate_record_name(self):
        for update in [dict(command='workspace.shell'), dict(exit_code=68), dict(exit_code=False)]:
            with self.subTest(update=update), self.assertRaises(ValueError):
                picker.names_from_result(dict(envelope([dict(name='hello', state='running')]), **update))
        valid = json.dumps(envelope([dict(name='hello', state='running')]))
        duplicate = valid.replace('"name": "hello"', '"name": "invalid", "name": "hello"')
        with mock.patch.object(picker.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, duplicate.encode())), self.assertRaises(ValueError):
            picker.discover()
