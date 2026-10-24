# SPDX-License-Identifier: EUPL-1.2
import io
import os
import unittest
from unittest import mock
import entrypoints
from identity import identity


class EntrypointTests(unittest.TestCase):
    def launch(self, kind, **extra):
        events = []
        class Output(io.StringIO):
            def write(self, value):
                events.append(('output', value))
                return super().write(value)
        output = Output()
        env = dict(HERDR_BIN_PATH='/fake/herdr', HERDR_PANE_ID='w1:p2', HERDR_TAB_ID='w1:t3',
                   WHR_PANE_WORKSPACE='hello', SHELL='/bin/sh', **extra)
        with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(entrypoints.subprocess, 'run', side_effect=lambda argv, **kw: events.append(('herdr', argv))), mock.patch.object(entrypoints.os, 'execvp', side_effect=lambda exe, argv: events.append(('exec', exe, argv))), mock.patch.object(entrypoints.sys, 'stdout', output):
            entrypoints.launch(kind)
        return events, output.getvalue()

    def test_plugin_title_and_tab_label(self):
        for kind, title in [('workspace', identity('hello')[0]), ('host', '🟥 HOST')]:
            events, output = self.launch(kind)
            self.assertEqual([('herdr', ['/fake/herdr', 'pane', 'rename', 'w1:p2', title]),
                              ('herdr', ['/fake/herdr', 'tab', 'rename', 'w1:t3', title])], events[:2])
            self.assertEqual('', output)
            self.assertEqual(('exec', 'whr', ['whr', 'shell', '--local', 'hello']) if kind == 'workspace' else ('exec', '/bin/sh', ['/bin/sh', '-l']), events[-1])

    def test_plugin_background_optional_one_osc_before_exec(self):
        for kind, tint in [('workspace', identity('hello')[1]), ('host', '#3a0d0d')]:
            events, output = self.launch(kind, WHR_PANE_BACKGROUND='1')
            self.assertEqual('\x1b]11;' + tint + '\x1b\\', output)
            self.assertEqual('output', events[-2][0])
            self.assertEqual('exec', events[-1][0])
            self.assertEqual(1, sum(event[0] == 'output' for event in events))
            self.assertEqual('', self.launch(kind, WHR_PANE_BACKGROUND='0')[1])

    def test_invalid_name_or_context_starts_nothing(self):
        for update in [dict(WHR_PANE_WORKSPACE='Bad'), dict(HERDR_TAB_ID=''), dict(HERDR_PANE_ID='--help'), dict(WHR_PANE_BACKGROUND='yes')]:
            env = dict(HERDR_PANE_ID='w1:p2', HERDR_TAB_ID='w1:t3', WHR_PANE_WORKSPACE='hello')
            env.update(update)
            with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(entrypoints.subprocess, 'run') as run, mock.patch.object(entrypoints.os, 'execvp') as execute, self.assertRaises(ValueError):
                entrypoints.launch('workspace')
            run.assert_not_called()
            execute.assert_not_called()

    def test_metadata_failure_never_executes(self):
        import subprocess
        with mock.patch.dict(os.environ, dict(HERDR_PANE_ID='w1:p2', HERDR_TAB_ID='w1:t3', WHR_PANE_WORKSPACE='hello'), clear=True), mock.patch.object(entrypoints.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'herdr')), mock.patch.object(entrypoints.os, 'execvp') as execute:
            with self.assertRaises(subprocess.CalledProcessError):
                entrypoints.launch('workspace')
            execute.assert_not_called()

    def test_host_shell_validation_precedes_metadata_or_exec(self):
        for shell in ['relative-shell', '/bin/sh\n--x']:
            env = dict(HERDR_PANE_ID='w1:p1', HERDR_TAB_ID='w1:t1', SHELL=shell)
            with mock.patch.dict(os.environ, env, clear=True), mock.patch.object(entrypoints.subprocess, 'run') as run, mock.patch.object(entrypoints.os, 'execvp') as execute, self.assertRaises(ValueError):
                entrypoints.launch('host')
            run.assert_not_called()
            execute.assert_not_called()
