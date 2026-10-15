# SPDX-License-Identifier: EUPL-1.2
"""Fixed application gates; manifest data supplies test identities, never argv."""
import json
import os
import re
import shutil
import sys
import tempfile
from pathlib import Path

GO_MODULES = ('whr', 'host_agent', 'api_client')
MODULE_PREFIX = 'github.com/wstein/workharbor-nxt/apps/'
HEX_VERSION = '2.5.2'
REBAR_URL = 'https://github.com/erlang/rebar3/releases/download/3.27.3/rebar3'
REBAR_SHA512 = ('30410ea3222f52f0af90e6ba7df483d9a1e3452dc6ed6f60f372008fc017dbd8a'
                'e22c45e1359302796f2dbb805bbcfce53bf5d08fdd43885203b0ae015ff0516')


def validate_inventory(value):
    if not isinstance(value, dict) or set(value) != {*GO_MODULES, 'control_plane'}:
        raise ValueError('Application inventory requires exactly the three Go modules and control_plane')
    for module, ids in value.items():
        if (not isinstance(ids, list) or (not ids and module != 'api_client')
                or any(not isinstance(identity, str) or not identity.strip() for identity in ids)
                or len(ids) != len(set(ids))):
            raise ValueError('Application required IDs must be unique and critical suites nonempty')
        for identity in ids:
            pattern = (re.escape(MODULE_PREFIX + module) + r'/[^\s]+/Test[^\s]+'
                       if module in GO_MODULES else r'WorkHarbor[A-Za-z0-9_.]*/test [^\r\n]+')
            # A module-root Go package can also own a required behavior test.
            if module in GO_MODULES and identity.startswith(MODULE_PREFIX + module + '/Test'):
                pattern = re.escape(MODULE_PREFIX + module) + r'/Test[^\s]+'
            if not re.fullmatch(pattern, identity):
                raise ValueError('Invalid application required test identity')


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('Duplicate application event key')
        result[key] = value
    return result


def read_events(path):
    try:
        events = [json.loads(line, object_pairs_hook=unique_object) for line in path.read_text().splitlines()]
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ValueError('Missing or malformed application events') from error
    if not events or any(not isinstance(event, dict) for event in events):
        raise ValueError('Empty or malformed application events')
    return events


def validate_go_events(events, required):
    packages, running, passed, no_tests = {}, set(), set(), set()
    for event in events:
        action, package, test = event.get('Action'), event.get('Package'), event.get('Test')
        if action == 'build-output':
            continue
        if action == 'build-fail' or not isinstance(package, str) or not package:
            raise ValueError('Invalid or failed Go build/test event')
        if test is not None and (not isinstance(test, str) or not test):
            raise ValueError('Invalid Go test identity')
        identity = (package, test)
        if action == 'start' and not test:
            if package in packages:
                raise ValueError('Duplicate Go package start')
            packages[package] = 'running'
        elif package not in packages or packages[package] != 'running':
            raise ValueError('Go event outside a running package')
        elif action == 'run' and test:
            if identity in running or identity in passed:
                raise ValueError('Duplicate Go test dispatch')
            running.add(identity)
        elif action == 'pass' and test:
            if identity not in running:
                raise ValueError('Go pass without test dispatch')
            running.remove(identity)
            passed.add(identity)
        elif action in ('pass', 'skip') and not test:
            if any(test_package == package for test_package, _ in running):
                raise ValueError('Incomplete Go test events')
            if action == 'skip' and (package not in no_tests or any(test_package == package for test_package, _ in passed)):
                raise ValueError('Go package skipped tests')
            packages[package] = action
        elif action == 'output':
            if not test and '[no test files]' in event.get('Output', ''):
                no_tests.add(package)
        elif action in ('pause', 'cont') and test and identity in running:
            continue
        else:
            raise ValueError('Failed, skipped or unknown Go test event')
    passed_ids = {package + '/' + test for package, test in passed}
    if (not packages or running or 'running' in packages.values() or not set(required) <= passed_ids):
        raise ValueError('Missing required or incomplete Go test outcomes')
    return len(passed)


def validate_exunit_events(events, required):
    if (not events or events[0] != {'event': 'suite_started'}
            or events[-1] != {'event': 'suite_finished'}):
        raise ValueError('Incomplete ExUnit suite events')
    passed = set()
    for event in events[1:-1]:
        if (set(event) != {'event', 'id', 'status'} or event['event'] != 'test_finished'
                or not isinstance(event['id'], str) or not event['id'] or event['status'] != 'passed'
                or event['id'] in passed):
            raise ValueError('Failed, skipped, excluded, duplicate or malformed ExUnit outcome')
        passed.add(event['id'])
    if not passed or not set(required) <= passed:
        raise ValueError('Missing required ExUnit test outcomes')
    return len(passed)


def tool_pins(root):
    try:
        rows = [line.split() for line in (root/'.tool-versions').read_text().splitlines() if line.strip()]
        pins = dict(rows)
    except (OSError, ValueError) as error:
        raise ValueError('Invalid .tool-versions') from error
    if (len(rows) != len(pins) or set(pins) != {'erlang', 'elixir', 'golang', 'nodejs', 'python'}
            or any(not re.fullmatch(r'\d+\.\d+\.\d+(?:-otp-\d+)?', value) for value in pins.values())):
        raise ValueError('Incomplete or invalid pinned tool versions')
    return pins


def check_applications(root, inventory, run):
    """Execute fixed gates with process ownership supplied by check_ci.run."""
    root = Path(root).resolve()
    validate_inventory(inventory)
    pins = tool_pins(root)
    if '.'.join(map(str, sys.version_info[:3])) != pins['python']:
        raise ValueError('Python does not match .tool-versions')
    tools = {}
    for name in ('go', 'gofmt', 'mix', 'elixir', 'erl', 'node', 'npm'):
        tools[name] = shutil.which(name)
        if not tools[name]:
            raise ValueError('Missing required tool: ' + name + '; install .tool-versions before checking')
    with tempfile.TemporaryDirectory(prefix='workharbor-app-checks-') as directory:
        cache = Path(directory)
        # No inherited GOFLAGS, Mix/Hex mirrors, BEAM flags, server activation or
        # developer credential/config paths. Build intermediates/deps remain in
        # the disposable source tree for Phoenix's generated asset-relative paths.
        env = {key: os.environ[key] for key in ('PATH', 'LANG', 'LC_ALL', 'TMPDIR') if key in os.environ}
        env.update(HOME=str(cache/'home'), MIX_HOME=str(cache/'mix'), HEX_HOME=str(cache/'hex'),
                   MIX_ARCHIVES=str(cache/'mix/archives'), GOCACHE=str(cache/'go/build'),
                   GOMODCACHE=str(cache/'go/mod'), GOENV='off', GOTOOLCHAIN='local',
                   GOWORK=str(root/'go.work'), MIX_ENV='test',
                   GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                   GIT_TERMINAL_PROMPT='0', GIT_CONFIG_COUNT='1', GIT_CONFIG_KEY_0='core.hooksPath',
                   GIT_CONFIG_VALUE_0=os.devnull)
        for name in ('HOME', 'MIX_HOME', 'HEX_HOME', 'MIX_ARCHIVES', 'GOCACHE', 'GOMODCACHE'):
            Path(env[name]).mkdir(parents=True, exist_ok=True)
        serial = 0
        def execute(argv, cwd=root):
            nonlocal serial
            serial += 1
            output = cache/('output-' + str(serial))
            if run(argv, cwd, env=env, output_path=output) != 0:
                raise ValueError('Application command failed: ' + ' '.join(argv))
            return output
        versions = [
            ([tools['go'], 'version'], lambda value: value.startswith('go version go' + pins['golang'] + ' ')),
            ([tools['elixir'], '--version'], lambda value: bool(re.search(
                r'^Elixir ' + re.escape(pins['elixir'].split('-otp-')[0]) + r' \(compiled with Erlang/OTP '
                + re.escape(pins['erlang'].split('.')[0]) + r'\)$', value, re.M))),
            ([tools['erl'], '-noshell', '-eval',
              '{ok,V}=file:read_file(filename:join([code:root_dir(),"releases",erlang:system_info(otp_release),"OTP_VERSION"])),io:put_chars(V),halt().'],
             lambda value: value.strip() == pins['erlang']),
            ([tools['node'], '--version'], lambda value: value.strip() == 'v' + pins['nodejs']),
        ]
        for argv, matches in versions:
            if not matches(execute(argv).read_text()):
                raise ValueError('Tool version does not match .tool-versions: ' + argv[0])
        env['npm_config_cache'] = str(cache/'npm')
        env['npm_config_userconfig'] = os.devnull
        env['npm_config_globalconfig'] = os.devnull
        execute([tools['npm'], 'ci', '--ignore-scripts', '--no-audit', '--no-fund',
                 '--registry=https://registry.npmjs.org'], root/'apps/host_agent/tools/devcontainer')
        for module in GO_MODULES:
            cwd = root/'apps'/module
            if execute([tools['gofmt'], '-l', '.'], cwd).read_bytes():
                raise ValueError('Unformatted Go source: ' + module)
            execute([tools['go'], 'mod', 'download'], cwd)
            execute([tools['go'], 'vet', './...'], cwd)
            output = cache/('build-' + module)
            output.mkdir()
            argv = ([tools['go'], 'build', './...'] if module == 'api_client' else
                    [tools['go'], 'build', '-o', str(output) + '/', './...'])
            execute(argv, cwd)
            events = execute([tools['go'], 'test', '-json', '-count=1', './...'], cwd)
            count = validate_go_events(read_events(events), inventory[module])
            print(f'Application {module}: {count} Go tests passed; all required IDs present.', flush=True)
        cwd = root/'apps/control_plane'
        execute([tools['mix'], 'local.hex', HEX_VERSION, '--force'], cwd)
        execute([tools['mix'], 'local.rebar', 'rebar3', REBAR_URL, '--sha512', REBAR_SHA512, '--force'], cwd)
        execute([tools['mix'], 'deps.get', '--check-locked'], cwd)
        execute([tools['mix'], 'format', '--check-formatted'], cwd)
        execute([tools['mix'], 'compile', '--warnings-as-errors'], cwd)
        execute([tools['mix'], 'assets.setup'], cwd)
        execute([tools['mix'], 'assets.build'], cwd)
        events = cache/'exunit.jsonl'
        env['WORKHARBOR_EXUNIT_EVENTS'] = str(events)
        execute([tools['mix'], 'test', '--warnings-as-errors', '--formatter', 'ExUnit.CLIFormatter',
                 '--formatter', 'WorkHarbor.AcceptanceFormatter'], cwd)
        count = validate_exunit_events(read_events(events), inventory['control_plane'])
        print(f'Application control_plane: {count} ExUnit tests passed; all required IDs present.', flush=True)
    return True
