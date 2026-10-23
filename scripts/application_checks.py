# SPDX-License-Identifier: EUPL-1.2
"""Fixed application gates; manifest data supplies test identities, never argv."""
import json
import hashlib
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
TAILWIND_METADATA = '''Tailwind.profiles()
|> Enum.map(fn {profile, _} ->
  version = Tailwind.configured_version(profile)
  url = Tailwind.default_base_url()
    |> String.replace("$version", version)
    |> String.replace("$target", Tailwind.configured_target(profile))
  %{path: Tailwind.bin_path(version), url: url}
end)
|> Enum.uniq()
|> Jason.encode!()
|> then(&File.write!(System.fetch_env!("WORKHARBOR_TAILWIND_METADATA"), &1, [:exclusive]))'''

# Official v4.3.3 sha256sums.txt, pinned for every published native artifact.
TAILWIND_SHA256 = {
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-arm64':
        '55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195',
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-arm64-musl':
        '71ea4be79c9de9827545682df3e040053fb535d37c71ed2cfdedf9385a0868e0',
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-x64':
        'dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a',
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-x64-musl':
        'a04d34ceacc8f52cbe8920ad846cdeb61d3d0021dba32db0d1f77c9d9fad7a6c',
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-macos-arm64':
        'cdf646702987a743464dff4d9c60fd4480d1c1e73dd819a9a67f1078815dce9d',
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-macos-x64':
        '7922e0953f2110c05976e3bf58f14e643d90427575e766b7d433f5f80cbee7e1',
    'https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-windows-x64.exe':
        'e0e260ce048014e9268f6237ff18f8ccf02cef521cbd0ae04e82c2cdf7aa3955',
}



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
            or any(not re.fullmatch(r'\d+\.\d+\.\d+(?:-otp-\d+)?', value)
                   for name, value in pins.items() if name != 'python')
            or not re.fullmatch(r'\d+\.\d+(?:\.\d+)?', pins['python'])):
        raise ValueError('Incomplete or invalid pinned tool versions')
    return pins


def check_applications(root, inventory, run):
    """Execute fixed gates with process ownership supplied by check_ci.run."""
    root = Path(root).resolve()
    validate_inventory(inventory)
    pins = tool_pins(root)
    # The harness is development tooling: .tool-versions names a minimum CPython series.
    floor = tuple(map(int, pins['python'].split('.')))
    if sys.version_info[:len(floor)] < floor:
        raise ValueError('Python ' + pins['python'] + ' or newer required by .tool-versions')
    tools = {}
    for name in ('go', 'gofmt', 'mix', 'elixir', 'erl', 'node', 'npm', 'curl'):
        tools[name] = shutil.which(name)
        if not tools[name]:
            remedy = 'install curl before checking' if name == 'curl' else 'install .tool-versions before checking'
            raise ValueError('Missing required tool: ' + name + '; ' + remedy)
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
        def download(url, destination, cwd):
            execute([tools['curl'], '--disable', '--fail', '--location', '--proto', '=https',
                     '--proto-redir', '=https', '--retry', '2', '--retry-delay', '1',
                     '--retry-max-time', '60', '--max-time', '60', '--output', str(destination), url], cwd)
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
        # npm refuses loading the same physical file for user and global config.
        # Distinct private empty files preserve isolation without inherited defaults.
        for kind in ('user', 'global'):
            config = cache/('npm-' + kind + '.conf')
            descriptor = os.open(config, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            os.close(descriptor)
            env['npm_config_' + kind + 'config'] = str(config)
        execute([tools['npm'], 'ci', '--ignore-scripts', '--no-audit', '--no-fund',
                 '--registry=https://registry.npmjs.org'], root/'apps/host_agent/tools/devcontainer')
        for module in GO_MODULES:
            cwd = root/'apps'/module
            if execute([tools['gofmt'], '-l', '.'], cwd).read_bytes():
                raise ValueError('Unformatted Go source: ' + module)
            execute([tools['go'], 'mod', 'download'], cwd)
            execute([tools['go'], 'vet', './...'], cwd)
            if module == 'host_agent':
                # Compile the real-Docker acceptance entrypoints without running them.
                execute([tools['go'], 'vet', '-tags', 'docker_integration', './internal/providers/docker/'], cwd)
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
        # Mix's HTTP client rejects GitHub's large redirect headers. Download
        # with the isolated curl process, then let Mix verify the same pinned
        # SHA-512 before installing the local file in its private MIX_HOME.
        rebar = cache/'rebar3'
        download(REBAR_URL, rebar, cwd)
        execute([tools['mix'], 'local.rebar', 'rebar3', str(rebar), '--sha512', REBAR_SHA512, '--force'], cwd)
        execute([tools['mix'], 'deps.get', '--check-locked'], cwd)
        execute([tools['mix'], 'format', '--check-formatted'], cwd)
        execute([tools['mix'], 'compile', '--warnings-as-errors'], cwd)
        # Query the locked package instead of duplicating its platform/version
        # selection. Preinstall the same binaries in the fresh build directory;
        # assets.setup still runs and verifies them through --if-missing.
        metadata = cache/'tailwind-metadata.json'
        env['WORKHARBOR_TAILWIND_METADATA'] = str(metadata)
        try:
            execute([tools['mix'], 'run', '--no-start', '--no-compile', '-e', TAILWIND_METADATA], cwd)
        finally:
            env.pop('WORKHARBOR_TAILWIND_METADATA', None)
        try:
            assets = json.loads(metadata.read_text(), object_pairs_hook=unique_object)
        except (OSError, UnicodeError, ValueError) as error:
            raise ValueError('Invalid Tailwind installation metadata') from error
        if not isinstance(assets, list) or not assets:
            raise ValueError('Missing Tailwind installation metadata')
        for asset in assets:
            if (not isinstance(asset, dict) or set(asset) != {'path', 'url'}
                    or not all(isinstance(value, str) for value in asset.values())
                    or not re.fullmatch(r'https://github\.com/tailwindlabs/tailwindcss/releases/download/'
                                        r'v[0-9][0-9A-Za-z.+-]*/tailwindcss-[a-z0-9_.-]+', asset['url'])):
                raise ValueError('Invalid Tailwind installation metadata')
            expected = TAILWIND_SHA256.get(asset['url'])
            if expected is None:
                raise ValueError('Tailwind artifact has no reviewed SHA-256 pin')
            destination = Path(asset['path'])
            if not destination.is_absolute() or not destination.resolve().is_relative_to(cwd/'_build'):
                raise ValueError('Tailwind destination must remain inside the disposable build directory')
            destination.parent.mkdir(parents=True, exist_ok=True)
            download(asset['url'], destination, cwd)
            with destination.open('rb') as artifact:
                actual = hashlib.file_digest(artifact, 'sha256').hexdigest()
            if actual != expected:
                raise ValueError('Tailwind artifact SHA-256 mismatch')
            destination.chmod(0o755)
        execute([tools['mix'], 'assets.setup'], cwd)
        execute([tools['mix'], 'assets.build'], cwd)
        events = cache/'exunit.jsonl'
        env['WORKHARBOR_EXUNIT_EVENTS'] = str(events)
        execute([tools['mix'], 'test', '--warnings-as-errors', '--formatter', 'ExUnit.CLIFormatter',
                 '--formatter', 'WorkHarbor.AcceptanceFormatter'], cwd)
        count = validate_exunit_events(read_events(events), inventory['control_plane'])
        print(f'Application control_plane: {count} ExUnit tests passed; all required IDs present.', flush=True)
    return True
