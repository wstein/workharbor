# SPDX-License-Identifier: EUPL-1.2
"""Real script/exec/PTY tests. Every whr/Herdr invocation is an isolated fake."""
import errno
import json
import os
from pathlib import Path
import pty
import select
import subprocess
import sys
import tempfile
import time
import tomllib
import unittest

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]


class IntegrationTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.log = self.root / 'calls.jsonl'
        # Fakes deliberately use a fixed interpreter and fixed executable paths.
        prelude = '#!' + sys.executable + '\nimport json, os, subprocess, sys\n'
        prelude += "def record(value):\n    with open(os.environ['FAKE_LOG'], 'a') as out: out.write(json.dumps(value) + '\\n')\n"
        whr = prelude + '''record({'tool': 'whr', 'argv': sys.argv[1:]})
if sys.argv[1:] == ['ls', '--local', '--json']:
    print(json.dumps({'schema_version': 1, 'command': 'workspace.ls', 'ok': True, 'exit_code': 0,
        'result': {'workspaces': [{'name': 'hello', 'state': 'running', 'image': 'not-for-display'},
                                  {'name': 'stopped-workspace', 'state': 'exited'},
                                  {'name': 'UPPER', 'state': 'running'}]}}))
    sys.exit(int(os.environ.get('FAKE_DISCOVERY_STATUS', '0')))
elif sys.argv[1:] == ['shell', '--local', 'hello']:
    print('FIXTURE-SHELL', flush=True)
    sys.exit(int(os.environ.get('FAKE_SHELL_STATUS', '7')))
else:
    sys.exit(64)
'''
        herdr = prelude + '''record({'tool': 'herdr', 'argv': sys.argv[1:]})
if os.environ.get('FAKE_HERDR_STATUS'):
    sys.exit(int(os.environ['FAKE_HERDR_STATUS']))
if sys.argv[1:4] == ['plugin', 'pane', 'open']:
    args = sys.argv[1:]
    placement = args[args.index('--placement') + 1]
    workspace, target = '--workspace' in args, '--target-pane' in args
    if placement == 'overlay' and (workspace or target):
        sys.exit('overlay and popup plugin panes target the active pane')
    if placement == 'tab' and target:
        sys.exit('tab plugin panes support workspace_id but not target_pane_id or direction')
    if placement == 'split' and (workspace or not target):
        sys.exit('split and zoomed plugin panes target an existing pane; use target_pane_id')
    entry = args[args.index('--entrypoint') + 1]
    env = dict(os.environ, HERDR_PANE_ID='w1:p9', HERDR_TAB_ID='w1:t9')
    for i, arg in enumerate(args):
        if arg == '--env':
            key, value = args[i+1].split('=', 1)
            env[key] = value
    if entry == 'workspace':
        result = subprocess.run([sys.executable, '-B', os.environ['PLUGIN_ENTRY'], 'workspace'], env=env, capture_output=True)
        record({'child_status': result.returncode, 'child_output': result.stdout.decode()})
'''
        for name, source in [('whr', whr), ('herdr', herdr)]:
            path = self.root / name
            path.write_text(source)
            path.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.root) + os.pathsep + os.defpath,
                        HERDR_BIN_PATH=str(self.root / 'herdr'), HERDR_PANE_ID='w1:p0', HERDR_TAB_ID='w1:t0',
                        HERDR_WORKSPACE_ID='w1', WHR_PANE_TARGET='w1:p0', WHR_PANE_WORKSPACE='hello',
                        WHR_PICKER_PLACEMENT='split', WHR_PANE_BACKGROUND='0', TERM='xterm-256color',
                        PYTHONDONTWRITEBYTECODE='1', FAKE_LOG=str(self.log), PLUGIN_ENTRY=str(ROOT / 'entrypoints.py'))

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def terminal(self, script, arguments, key=None):
        master, slave = pty.openpty()
        child = subprocess.Popen([sys.executable, '-B', str(ROOT / script), *arguments],
                                 env=self.env, stdin=slave, stdout=slave, stderr=slave)
        os.close(slave)
        output = bytearray()
        try:
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                ready, _, _ = select.select([master], [], [], 0.1)
                if ready:
                    try:
                        chunk = os.read(master, 65536)
                    except OSError as error:
                        if error.errno == errno.EIO:
                            break
                        raise
                    if not chunk:
                        break
                    output += chunk
                    if key is not None and b'hello' in output:
                        os.write(master, key)
                        key = None
                elif child.poll() is not None:
                    break
            else:
                self.fail('isolated fake PTY timed out')
            return child.wait(timeout=2), bytes(output)
        finally:
            if child.poll() is None:
                child.kill()
                child.wait()
            os.close(master)

    def test_plugin_end_to_end_with_fakes(self):
        self.env['WHR_PANE_BACKGROUND'] = '1'
        status, output = self.terminal('picker.py', [], b'\r')
        self.assertEqual(0, status, output)
        self.assertIn(b'hello', output)
        self.assertNotIn(b'not-for-display', output)
        self.assertNotIn(b'stopped-workspace', output)
        self.assertNotIn(b'UPPER', output)
        calls = self.calls()
        self.assertEqual([['ls', '--local', '--json'], ['shell', '--local', 'hello']],
                         [call['argv'] for call in calls if call.get('tool') == 'whr'])
        metadata = [call['argv'] for call in calls if call.get('tool') == 'herdr' and call['argv'][0] in ('pane', 'tab')]
        self.assertEqual([['pane', 'rename', 'w1:p9', '🟡 ws:hello'], ['tab', 'rename', 'w1:t9', '🟡 ws:hello']], metadata)
        child = next(call for call in calls if 'child_status' in call)
        self.assertEqual(7, child['child_status'])
        self.assertEqual('\x1b]11;#383000\x1b\\FIXTURE-SHELL\n', child['child_output'])

    def test_native_pty_exec_preserves_shell_status_and_cancel_opens_nothing(self):
        status, output = self.terminal('entrypoints.py', ['workspace'])
        self.assertEqual(7, status)
        self.assertIn(b'FIXTURE-SHELL', output)
        self.assertNotIn(b'\x1b]11;', output)
        self.log.unlink()
        status, output = self.terminal('picker.py', [], b'q')
        self.assertEqual(0, status, output)
        self.assertEqual([dict(tool='whr', argv=['ls', '--local', '--json'])], self.calls())

    def test_stale_workspace_exit_remains_whr_responsibility(self):
        self.env['FAKE_SHELL_STATUS'] = '68'
        status, _ = self.terminal('entrypoints.py', ['workspace'])
        self.assertEqual(68, status)
        self.assertEqual([['shell', '--local', 'hello']], [call['argv'] for call in self.calls() if call.get('tool') == 'whr'])

    def test_plugin_absence_leaves_local_shell_usable(self):
        # Static dependency guard; actual no-Herdr process behavior is covered by
        # whr's independent TestLocalShellDoesNotStartMultiplexer, not these fakes.
        for path in (REPO / 'apps/whr').rglob('*.go'):
            self.assertNotIn('integrations/herdr', path.read_text())
        self.assertNotIn('integrations/herdr', (REPO / 'apps/whr/go.mod').read_text())

    def test_plugin_does_not_join_required_repo_checks(self):
        for name in ['Makefile', '.github/workflows/check.yml', 'scripts/check_ci.py', 'docs/development/ci-acceptance.json']:
            self.assertNotIn('integrations/herdr', (REPO / name).read_text())
        manifest = tomllib.loads((ROOT / 'herdr-plugin.toml').read_text())
        self.assertEqual('workharbor.local', manifest['id'])
        self.assertEqual(['linux', 'macos'], manifest['platforms'])
        self.assertEqual({'picker', 'workspace', 'host'}, {pane['id'] for pane in manifest['panes']})
        self.assertEqual('overlay', next(pane['placement'] for pane in manifest['panes'] if pane['id'] == 'picker'))
        self.assertFalse({'startup', 'events', 'build'} & set(manifest))
        for entry in manifest['panes'] + manifest['actions']:
            self.assertEqual(['python3', '-B'], entry['command'][:2])
            self.assertTrue((ROOT / entry['command'][2]).is_file())

    def test_manifest_actions_open_fixed_entries(self):
        manifest = tomllib.loads((ROOT / 'herdr-plugin.toml').read_text())
        for action in manifest['actions']:
            with self.subTest(action=action['id']):
                if self.log.exists():
                    self.log.unlink()
                argv = [sys.executable, *action['command'][1:]]
                result = subprocess.run(argv, cwd=ROOT, env=self.env, capture_output=True)
                self.assertEqual(0, result.returncode, result.stderr)
                calls = self.calls()
                self.assertEqual(1, len(calls))
                args = calls[0]['argv']
                picker = action['id'].startswith('pick-')
                self.assertEqual('picker' if picker else 'host', args[args.index('--entrypoint') + 1])
                self.assertEqual('overlay' if picker else 'tab', args[args.index('--placement') + 1])
                self.assertIn('WHR_PANE_BACKGROUND=' + ('1' if action['id'].endswith('tinted') else '0'), args)


    def test_real_subprocess_metadata_exit_failure_stops_exec(self):
        self.env['FAKE_HERDR_STATUS'] = '1'
        status, output = self.terminal('entrypoints.py', ['workspace'])
        self.assertEqual(68, status, output)
        self.assertFalse(any(call.get('tool') == 'whr' for call in self.calls()))

    def test_real_subprocess_discovery_nonzero_with_valid_json_stops_picker(self):
        self.env['FAKE_DISCOVERY_STATUS'] = '68'
        status, output = self.terminal('picker.py', [], b'\r')
        self.assertEqual(68, status, output)
        self.assertEqual([dict(tool='whr', argv=['ls', '--local', '--json'])], self.calls())


    def test_fake_enforces_herdr_093_placement_contract(self):
        cases = [
            ('overlay', ['--workspace', 'w1'], 'overlay and popup plugin panes target the active pane'),
            ('overlay', ['--workspace', 'w1', '--target-pane', 'w1:p0'], 'overlay and popup plugin panes target the active pane'),
            ('tab', ['--workspace', 'w1', '--target-pane', 'w1:p0'], 'tab plugin panes support workspace_id but not target_pane_id or direction'),
            ('split', ['--workspace', 'w1', '--target-pane', 'w1:p0'], 'split and zoomed plugin panes target an existing pane; use target_pane_id')]
        for placement, flags, message in cases:
            with self.subTest(placement=placement, flags=flags):
                result = subprocess.run([str(self.root / 'herdr'), 'plugin', 'pane', 'open', '--plugin', 'workharbor.local', '--entrypoint', 'host', '--placement', placement, *flags], env=self.env, capture_output=True)
                self.assertEqual(1, result.returncode)
                self.assertEqual(message, result.stderr.decode().strip())


    def test_standalone_runner_refuses_a_skipped_suite(self):
        import check as runner
        from contextlib import redirect_stderr
        from io import StringIO
        from unittest import mock
        class SkippedFixture(unittest.TestCase):
            @unittest.skip('controlled fixture')
            def runTest(self):
                pass
        suite = unittest.TestSuite([SkippedFixture()])
        with mock.patch.object(runner.unittest.defaultTestLoader, 'discover', return_value=suite), mock.patch.object(runner, 'check_palette', return_value=0) as palette, redirect_stderr(StringIO()):
            self.assertEqual(1, runner.main())
        palette.assert_not_called()
