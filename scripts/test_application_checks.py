"""Application acceptance negatives use recorded events, never installed host tools."""
import copy
import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock


class ApplicationChecksTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location('application_checks', Path(__file__).with_name('application_checks.py'))
        self.apps = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.apps)
        self.inventory = {
            'whr': ['github.com/wstein/workharbor-nxt/apps/whr/cmd/whr/TestScaffoldRefusesProductCommands'],
            'host_agent': ['github.com/wstein/workharbor-nxt/apps/host_agent/cmd/workharbor-agent/TestScaffoldRefusesProductCommands'],
            'api_client': [],
            'control_plane': ['WorkHarborWeb.PageControllerTest/test GET /'],
        }

    def go_events(self, module='whr'):
        identity = self.inventory[module][0]
        package, name = identity.rsplit('/', 1)
        return [dict(Action='start', Package=package), dict(Action='run', Package=package, Test=name),
                dict(Action='pass', Package=package, Test=name), dict(Action='pass', Package=package)]

    def exunit_events(self):
        return [dict(event='suite_started'), dict(event='test_finished', id=self.inventory['control_plane'][0], status='passed'),
                dict(event='suite_finished')]

    def test_inventory_rejects_missing_duplicate_empty_critical_and_command_fields(self):
        self.apps.validate_inventory(self.inventory)
        variants = [dict(self.inventory, shell=['touch marker']), dict(self.inventory, whr=[]),
                    dict(self.inventory, control_plane=[]), dict(self.inventory, api_client=['wrong/TestX'])]
        missing = dict(self.inventory); del missing['host_agent']; variants.append(missing)
        duplicate = copy.deepcopy(self.inventory); duplicate['whr'] *= 2; variants.append(duplicate)
        for value in variants:
            with self.subTest(value=value), self.assertRaises(ValueError): self.apps.validate_inventory(value)

    def test_go_missing_skip_fail_duplicate_and_incomplete_events_block(self):
        good = self.go_events()
        self.apps.validate_go_events(good, self.inventory['whr'])
        variants = [[], good[:1] + good[3:], good[:-1], good + [good[2]],
                    good + [dict(Action='fail', Package='additional/package', Test='TestRegression')],
                    good + [dict(Action='skip', Package='additional/package', Test='TestOptional')]]
        for value in variants:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.apps.validate_go_events(value, self.inventory['whr'])

    def test_concurrent_nested_packages_keep_exact_test_ownership(self):
        parent, child = 'module/pkg', 'module/pkg/child'
        prefix = [dict(Action='start', Package=parent), dict(Action='start', Package=child),
                  dict(Action='run', Package=child, Test='TestChild')]
        parent_end = [dict(Action='output', Package=parent, Output='[no test files]'), dict(Action='skip', Package=parent)]
        child_end = [dict(Action='pass', Package=child, Test='TestChild'), dict(Action='pass', Package=child)]
        for ending in (parent_end + child_end, child_end + parent_end):
            with self.subTest(ending=ending):
                self.assertEqual(1, self.apps.validate_go_events(prefix + ending, [child + '/TestChild']))

    def test_go_doc_only_library_still_rejects_unexpected_bad_tests(self):
        package = 'github.com/wstein/workharbor-nxt/apps/api_client'
        self.apps.validate_go_events([dict(Action='start', Package=package), dict(Action='output', Package=package, Output='[no test files]'), dict(Action='skip', Package=package)], [])
        for action in ('skip', 'fail'):
            events = [dict(Action='start', Package=package), dict(Action=action, Package=package, Test='TestNew'),
                      dict(Action='pass', Package=package)]
            with self.subTest(action=action), self.assertRaises(ValueError): self.apps.validate_go_events(events, [])

    def test_exunit_missing_skipped_excluded_failed_duplicate_and_incomplete_block(self):
        good = self.exunit_events()
        self.apps.validate_exunit_events(good, self.inventory['control_plane'])
        variants = [[], [good[0], good[2]], good[:-1], good + [good[1]]]
        for status in ('skipped', 'excluded', 'failed', 'invalid'):
            variants.append([good[0], dict(good[1], status=status), good[2]])
            variants.append([good[0], good[1], dict(event='test_finished', id='Extra/test regression', status=status), good[2]])
        for value in variants:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.apps.validate_exunit_events(value, self.inventory['control_plane'])

    def test_malformed_event_bytes_never_qualify(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'events'
            for content in (b'', b'not JSON\n', b'{"Action":"pass","Action":"skip"}\n', b'[]\n', b'\xff\n'):
                path.write_bytes(content)
                with self.subTest(content=content), self.assertRaises(ValueError): self.apps.read_events(path)

    def exercise(self, *, missing=None, fail=None, formatted=False, wrong_version=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)/'source'; root.mkdir()
            (root/'.tool-versions').write_text('erlang 29.1.1\nelixir 1.20.4-otp-29\ngolang 1.27.2\nnodejs 26.11.1\npython 3.14.8\n')
            observed = []
            def execute(argv, cwd, *, env, output_path):
                observed.append((argv, cwd, env))
                command = tuple(argv)
                if fail and fail(command): return 1
                name = Path(argv[0]).name
                data = b''
                if command[1:] == ('version',): data = b'go version go1.27.2 fixture/fixture\n'
                elif command[1:] == ('--version',):
                    data = (b'Elixir 1.20.4 (compiled with Erlang/OTP 29)\n' if name == 'elixir' else b'v26.11.1\n')
                    if wrong_version: data = b'wrong version\n'
                elif name == 'erl': data = b'29.1.1\n'
                elif command[1:] == ('-l', '.') and formatted: data = b'cmd/main.go\n'
                elif command[1:3] == ('test', '-json'):
                    module = Path(cwd).name
                    events = self.go_events(module) if module != 'api_client' else [
                        dict(Action='start', Package='library'), dict(Action='output', Package='library', Output='[no test files]'), dict(Action='skip', Package='library')]
                    data = ''.join(json.dumps(event) + '\n' for event in events).encode()
                elif command[1:2] == ('test',):
                    Path(env['WORKHARBOR_EXUNIT_EVENTS']).write_text(''.join(json.dumps(e) + '\n' for e in self.exunit_events()))
                Path(output_path).write_bytes(data)
                return 0
            def which(name): return None if name == missing else '/fixture/bin/' + name
            with mock.patch.object(self.apps.shutil, 'which', side_effect=which), \
                    mock.patch.object(self.apps.sys, 'version_info', (3, 14, 8)):
                self.apps.check_applications(root, self.inventory, execute)
            return observed, root

    def test_missing_tool_blocks_before_any_application_command(self):
        for name in ('go', 'gofmt', 'mix', 'elixir', 'erl', 'node'):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'Missing required tool'): self.exercise(missing=name)

    def test_wrong_tool_version_unformatted_source_and_failed_gates_block(self):
        for options in (dict(wrong_version=True), dict(formatted=True),
                        dict(fail=lambda argv: 'vet' in argv), dict(fail=lambda argv: 'build' in argv),
                        dict(fail=lambda argv: 'assets.build' in argv), dict(fail=lambda argv: 'deps.get' in argv),
                        dict(fail=lambda argv: 'test' in argv), dict(fail=lambda argv: 'format' in argv)):
            with self.subTest(options=options), self.assertRaises(ValueError): self.exercise(**options)

    def test_fixed_gates_locked_setup_external_caches_and_all_modules(self):
        observed, root = self.exercise()
        commands = [argv[1:] for argv, _, _ in observed]
        self.assertIn(['local.hex', '2.5.2', '--force'], commands)
        self.assertIn(['local.rebar', 'rebar3', self.apps.REBAR_URL, '--sha512', self.apps.REBAR_SHA512, '--force'], commands)
        self.assertIn(['deps.get', '--check-locked'], commands)
        self.assertIn(['format', '--check-formatted'], commands)
        self.assertIn(['compile', '--warnings-as-errors'], commands)
        self.assertIn(['assets.setup'], commands)
        self.assertIn(['assets.build'], commands)
        self.assertEqual(3, commands.count(['test', '-json', '-count=1', './...']))
        self.assertEqual(3, commands.count(['vet', './...']))
        self.assertIn(['build', './...'], commands)
        self.assertEqual(2, sum(argv[:2] == ['build', '-o'] for argv in commands))
        for _, _, env in observed:
            for name in ('GOCACHE', 'GOMODCACHE', 'MIX_HOME', 'HEX_HOME', 'MIX_ARCHIVES', 'HOME'):
                self.assertFalse(Path(env[name]).is_relative_to(root))
            self.assertEqual('off', env['GOENV'])
            self.assertEqual('local', env['GOTOOLCHAIN'])
            self.assertNotIn('ERL_AFLAGS', env)
            self.assertNotIn('PHX_SERVER', env)

    def test_pinned_companion_setup_precedes_host_tests_and_refuses_scripts(self):
        observed, root = self.exercise()
        setup = [(i, argv, cwd, env) for i, (argv, cwd, env) in enumerate(observed)
                 if Path(argv[0]).name == 'npm' and argv[1:2] == ['ci']]
        self.assertEqual(1, len(setup))
        index, argv, cwd, env = setup[0]
        self.assertEqual(['ci', '--ignore-scripts', '--no-audit', '--no-fund',
                          '--registry=https://registry.npmjs.org'], argv[1:])
        self.assertEqual(root.resolve()/'apps/host_agent/tools/devcontainer', cwd)
        self.assertFalse(Path(env['npm_config_cache']).is_relative_to(root))
        host_test = next(i for i, (argv, cwd, _) in enumerate(observed)
                         if Path(cwd).name == 'host_agent' and argv[1:3] == ['test', '-json'])
        self.assertLess(index, host_test)
        with self.assertRaisesRegex(ValueError, 'Missing required tool'):
            self.exercise(missing='npm')
        with self.assertRaises(ValueError):
            self.exercise(fail=lambda argv: Path(argv[0]).name == 'npm' and 'ci' in argv)

    def test_ci_and_local_share_the_application_gate(self):
        root = Path(__file__).resolve().parents[1]
        make = (root/'Makefile').read_text()
        workflow = (root/'.github/workflows/check.yml').read_text()
        self.assertIn('--applications-only', make)
        self.assertIn('python3 -I -B scripts/check_ci.py', workflow)
        self.assertIn('  bootstrap:', workflow)  # Preserve the inherited required-check identity.
        for version in ('29.1.1', '1.20.4', '1.27.2', '26.11.1', '3.14.8'):
            self.assertIn(version, workflow)
        self.assertNotIn('continue-on-error', workflow)
