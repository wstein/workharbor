"""Disposable offline helpers for Feature 005; never production authority.

The host constructs these objects in tests, not from submitted JSON. Their checks
confine supported helper operations and detect attempted live access; Python
patches are NOT a sandbox for hostile Python or arbitrary subprocesses. Future
exercise/coordinator code must opt into these boundaries and retain real guards.
"""
from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
import hashlib
import importlib.util
import os
from pathlib import Path, PurePosixPath
import socket
import stat
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import urllib.request


SOURCE_ROOT = Path(__file__).resolve().parents[1]
# Scoped accepted test-helper provenance, never a CI exemption or authority grant.
GUARD_FIXTURE_SHA256 = '32a08ad4b9e2d65bf4795bbe07b160ae38e0920d22bac0ed99db60c9c0836c4d'


def _guard_fixture_type():
    """Load only the fixed accepted fixture, never a candidate-selected plugin."""
    path = SOURCE_ROOT / 'tests/test_single_task.py'
    if path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1:
        raise RuntimeError('trusted fixture source unavailable')
    if hashlib.sha256(path.read_bytes()).hexdigest() != GUARD_FIXTURE_SHA256:
        raise RuntimeError('accepted fixture source changed; review required')
    spec = importlib.util.spec_from_file_location('_profile_guard_fixture', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.SingleTaskFixture


class FixtureBoundaryError(RuntimeError):
    """A supported fixture boundary rejected a non-fixture operation."""


class InjectedFixtureFault(RuntimeError):
    """A named, counted synthetic fault; no provider result is implied."""


class FaultHooks:
    """Count boundary observations and optionally fail at an exact occurrence."""

    def __init__(self):
        self.calls = []
        self._failures = {}

    def fail_on(self, name, occurrence=1):
        if not isinstance(name, str) or not name or type(occurrence) is not int or occurrence < 1:
            raise ValueError('invalid synthetic fault point')
        self._failures[name] = occurrence

    def hit(self, name):
        self.calls.append(name)
        if self._failures.get(name) == self.calls.count(name):
            raise InjectedFixtureFault('synthetic fault at reviewed boundary')


@dataclass(frozen=True)
class FixtureTarget:
    project: str
    profile: str
    task: str
    action: str
    route: str
    repository: str
    state: str


class SyntheticTime:
    def __init__(self, fixture):
        self.fixture = fixture
        self.value = 1800000000
        self.available = True

    def observe(self):
        self.fixture.assert_confined()
        self.fixture.faults.hit('time.observe')
        if not self.available:
            raise FixtureBoundaryError('synthetic clock unavailable')
        return self.value


class SyntheticAuthority:
    """Exact-target, host-injected OFFLINE decisions, not grant JSON parsing.

    Owner/policy/capability roles have independent stores. These observations do
    not implement production ProfileAuthority, assess eligibility or prove any
    real provider capability. A default/new authority has no decisions.
    """

    def __init__(self, fixture):
        self.fixture = fixture
        self._decisions = {role: set() for role in ('owner', 'policy', 'capability')}

    def attest(self, role, target):
        self.fixture.require_target(target)
        if role not in self._decisions:
            raise FixtureBoundaryError('unknown synthetic authority role')
        self._decisions[role].add(target)

    def observe(self, role, target):
        self.fixture.require_target(target)
        self.fixture.faults.hit('authority.observe')
        return role in self._decisions and target in self._decisions[role]

    def revoke(self, role, target):
        self.fixture.require_target(target)
        if role not in self._decisions:
            raise FixtureBoundaryError('unknown synthetic authority role')
        self._decisions[role].discard(target)


class SyntheticClaim:
    """In-memory fault fixture, NOT a durable or cross-process claim store."""

    def __init__(self, fixture):
        self.fixture = fixture
        self.generation = 0
        self.target = None
        self.executor = None

    def acquire(self, target, executor, expected_generation):
        self.fixture.require_target(target)
        self.fixture.faults.hit('claim.acquire')
        if type(expected_generation) is not int or expected_generation != self.generation:
            raise FixtureBoundaryError('synthetic generation conflict')
        if not isinstance(executor, str) or not executor.startswith('fixture:'):
            raise FixtureBoundaryError('fixture executor required')
        if self.target is not None:
            raise FixtureBoundaryError('synthetic claim already held')
        self.target, self.executor = target, executor
        self.generation += 1
        return self.generation


class SyntheticRuntime:
    """No transport or execution: explicit synthetic output with offline origin."""

    def __init__(self, fixture):
        self.fixture = fixture
        self.calls = []

    def emit(self, target, text):
        self.fixture.require_target(target)
        if not isinstance(text, str):
            raise FixtureBoundaryError('synthetic text required')
        self.fixture.faults.hit('runtime.emit')
        self.calls.append(target)
        return {'text': text, 'provenance': 'offline', 'live_verified': False}


class ProfileFixture:
    """Own fresh private roots; accept neither real roots nor injected live ports.

    Context/prerequisite/review inputs come from the existing accepted Git
    fixture. State is a sibling outside its repository. Environment creation is
    allowlist-based, with local Git configuration/hook/signing isolation.
    """

    def __init__(self):
        self._tmp = tempfile.TemporaryDirectory(prefix='workharbor-profile-fixture-')
        self.directory = Path(self._tmp.name).resolve()
        self.directory.chmod(0o700)
        self._closed = False
        try:
            self.guard = _guard_fixture_type()(self.directory)
            self.root = self.guard.root
            self.home = self.guard.private
            self.state = self.directory / 'state'
            self.state.mkdir(mode=0o700)
            self.root.chmod(0o700)
            self.home.chmod(0o700)
            self.env = dict(self.guard.env)
            self.env.update(TMPDIR=str(self.home), XDG_CACHE_HOME=str(self.home),
                            XDG_DATA_HOME=str(self.home))
            self.guard.env = dict(self.env)
            self.faults = FaultHooks()
            self.time = SyntheticTime(self)
            self.authority = SyntheticAuthority(self)
            self.claim = SyntheticClaim(self)
            self.runtime = SyntheticRuntime(self)
            self.denied_calls = []
            self._ports = {'time': self.time, 'authority': self.authority,
                           'claim': self.claim, 'runtime': self.runtime}
            self._git_config = (self.root / '.git/config').read_bytes()
            self._git_identity = ((self.root / '.git').stat().st_dev,
                                  (self.root / '.git').stat().st_ino)
            self._root_identities = {root: (root.stat().st_dev, root.stat().st_ino)
                for root in (self.directory, self.root, self.home, self.state)}
            self._protected = self._oracle_snapshot()
            self.assert_confined()
        except BaseException:
            self._tmp.cleanup()
            raise

    def __enter__(self):
        self.assert_confined()
        return self

    def __exit__(self, *exc):
        self.close()

    def close(self):
        self._closed = True
        self._tmp.cleanup()

    def _oracle_snapshot(self):
        paths = [self.guard.feature / name for name in ('spec.md', 'plan.md')]
        paths += sorted((SOURCE_ROOT / 'scripts').glob('*.py'))
        paths += [file for number in ('001', '002', '003', '004')
                  for directory in (SOURCE_ROOT / 'specs').glob(number + '-*')
                  for file in directory.rglob('*') if file.is_file()]
        return {path: (stat.S_IMODE(path.stat().st_mode),
                       hashlib.sha256(path.read_bytes()).hexdigest()) for path in paths}

    def assert_oracles_unchanged(self):
        for path, expected in self._protected.items():
            if path.is_symlink() or not path.is_file():
                raise FixtureBoundaryError('protected oracle changed')
            actual = (stat.S_IMODE(path.stat().st_mode),
                      hashlib.sha256(path.read_bytes()).hexdigest())
            if actual != expected:
                raise FixtureBoundaryError('protected oracle changed')

    def assert_confined(self):
        if self._closed:
            raise FixtureBoundaryError('fixture closed')
        for root in (self.directory, self.root, self.home, self.state):
            if root.is_symlink() or not root.is_dir() or root.resolve() != root:
                raise FixtureBoundaryError('fixture root alias or missing root')
            if ((root.stat().st_dev, root.stat().st_ino) != self._root_identities[root]):
                raise FixtureBoundaryError('fixture root replaced')
            if root.stat().st_uid != os.getuid() or stat.S_IMODE(root.stat().st_mode) != 0o700:
                raise FixtureBoundaryError('fixture root not private')
        if self.root.parent != self.directory or self.state.parent != self.directory:
            raise FixtureBoundaryError('fixture roots escaped')
        if self.root == SOURCE_ROOT or SOURCE_ROOT in self.root.parents:
            raise FixtureBoundaryError('real checkout refused')

    def require_roots(self, repository, state):
        self.assert_confined()
        # Compare spelling as well as resolution: even an equivalent alias is
        # rejected, rather than silently adopting caller-selected root paths.
        if (str(repository) != str(self.root) or str(state) != str(self.state)):
            raise FixtureBoundaryError('only owned fixture roots permitted')

    def target(self, action='begin', route='manual', task='T001', profile='fixture:profile'):
        target = FixtureTarget('fixture:project', profile, task, action, route,
                               str(self.root), str(self.state))
        self.require_target(target)
        return target

    def profile_record(self):
        """Inert exact-shape candidate data; no policy/capability/owner proof."""
        self.assert_confined()
        return {'schema_version': 1, 'project_id': 'fixture:project',
                'profile_id': 'fixture:profile', 'provider': 'fixture-provider',
                'backend': 'fixture-backend', 'agent': 'codex',
                'agent_version': 'fixture-1', 'authentication_kind': 'api-key',
                'execution_mode': 'direct-headless', 'adapter_revision': 'a' * 40,
                'runtime_revision': 'b' * 40, 'secret_ref': None,
                'policy_digest': '1' * 64, 'capability_digest': '2' * 64}

    def require_target(self, target):
        self.assert_confined()
        if type(target) is not FixtureTarget:
            raise FixtureBoundaryError('host-created synthetic target required')
        if not all(isinstance(getattr(target, field), str) for field in
                   ('project', 'profile', 'task', 'action', 'route', 'repository', 'state')):
            raise FixtureBoundaryError('invalid synthetic target fields')
        self.require_roots(target.repository, target.state)
        if (target.project != 'fixture:project' or not target.profile.startswith('fixture:')
                or target.task not in ('T001', 'T002')
                or target.route not in ('manual', 'orchestrated')
                or target.action not in ('begin', 'inspect', 'verify', 'complete',
                                         'record-commit', 'resume', 'rework', 'status',
                                         'lookup', 'transfer', 'ready', 'publish',
                                         'final-labels', 'provider-execute', 'safe-stop')):
            raise FixtureBoundaryError('non-fixture target refused')

    def require_ports(self, ports):
        self.assert_confined()
        if type(ports) is not dict or set(ports) != set(self._ports):
            raise FixtureBoundaryError('only constructed fixture ports permitted')
        if any(ports[name] is not port for name, port in self._ports.items()):
            raise FixtureBoundaryError('live or foreign port refused')
        return dict(self._ports)

    @property
    def ports(self):
        return dict(self._ports)

    def write(self, relative, content):
        self.assert_confined()
        path = PurePosixPath(relative)
        if (not isinstance(content, str) or relative not in ('guide.md', 'followup.md')
                or path.is_absolute() or '..' in path.parts):
            raise FixtureBoundaryError('outside approved synthetic output scope')
        output = self.root / relative
        if output.is_symlink() or (output.exists() and
                (not output.is_file() or output.stat().st_nlink != 1)):
            raise FixtureBoundaryError('unsafe fixture output alias')
        output.write_text(content, encoding='utf-8')

    def git(self, *args):
        self.assert_confined()
        # No caller-supplied config, transport, plugins, filenames or options.
        allowed = {('status', '--porcelain'), ('diff', '--check'),
                   ('rev-parse', 'HEAD'), ('add', 'guide.md'), ('add', 'followup.md'),
                   ('commit', '-m', 'test: synthetic output')}
        if args not in allowed:
            self._deny('git.unreviewed')
        metadata = self.root / '.git'
        if (metadata.is_symlink() or not metadata.is_dir()
                or (metadata.stat().st_dev, metadata.stat().st_ino) != self._git_identity):
            self._deny('git.metadata-indirection')
        for entry in metadata.rglob('*'):
            info = entry.lstat()
            if (stat.S_ISLNK(info.st_mode) or info.st_uid != os.getuid()
                    or not (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode))
                    or (stat.S_ISREG(info.st_mode) and info.st_nlink != 1)):
                self._deny('git.metadata-indirection')
        for name in ('commondir', 'gitdir', 'worktrees', 'objects/info/alternates',
                     'objects/info/http-alternates'):
            if (metadata / name).exists():
                self._deny('git.metadata-indirection')
        config = self.root / '.git/config'
        if (config.is_symlink() or not config.is_file() or config.stat().st_nlink != 1
                or config.read_bytes() != self._git_config
                or any(self.guard.hooks.iterdir())
                or (self.root / '.gitattributes').exists()
                or (self.root / '.gitattributes').is_symlink()
                or (self.root / '.git/info/attributes').exists()
                or (self.root / '.git/info/attributes').is_symlink()):
            self._deny('git.unreviewed-config')
        return self.guard.git(*args)

    def _deny(self, name):
        self.denied_calls.append(name)
        raise FixtureBoundaryError('live or unreviewed fixture operation refused')

    def credential(self, *args, **kwargs):
        self._deny('credential.discovery')

    def provider(self, *args, **kwargs):
        self._deny('provider.execute')

    def fallback(self, *args, **kwargs):
        self._deny('account-or-agent.fallback')

    @contextmanager
    def offline(self):
        """Fail attempted common in-process live access; never claim OS sandboxing.

        Common process launch entry points are denied in this scope. Reviewed local Git
        uses git() outside this scope with its isolated environment/protocol ban.
        Nested synthetic actions are allowed; global patches are process-local
        and intended for serial fixture tests, not concurrent application work.
        """
        self.assert_confined()
        with ExitStack() as stack:
            for obj, method, label in (
                (socket, 'create_connection', 'network.connect'),
                (socket, 'getaddrinfo', 'network.resolve'),
                (socket, 'gethostbyname', 'network.resolve'),
                (socket, 'gethostbyname_ex', 'network.resolve'),
                (socket, 'gethostbyaddr', 'network.resolve'),
                (socket.socket, 'connect', 'network.connect'),
                (socket.socket, 'connect_ex', 'network.connect'),
                (socket.socket, 'sendto', 'network.sendto'),
                (socket.socket, 'send', 'network.send'),
                (socket.socket, 'sendall', 'network.sendall'),
                (socket.socket, 'sendmsg', 'network.sendmsg'),
                (socket.socket, 'sendfile', 'network.sendfile'),
                (socket.socket, 'bind', 'network.bind'),
                (socket.socket, 'listen', 'network.listen'),
                (urllib.request, 'urlopen', 'network.urlopen'),
                (subprocess, 'Popen', 'process.spawn'),
                (os, 'system', 'process.system'),
                (os, 'fork', 'process.fork'),
                (os, 'posix_spawn', 'process.spawn'),
                (os, 'posix_spawnp', 'process.spawn'),
            ):
                if hasattr(obj, method):
                    stack.enter_context(patch.object(obj, method,
                        side_effect=lambda *a, _label=label, **k: self._deny(_label)))
            # Ambient credentials/config cannot leak to controlled fixture code.
            stack.enter_context(patch.dict(os.environ, self.env, clear=True))
            yield self


class T003Tests(unittest.TestCase):
    def test_isolated_fixture(self):
        source_bytes = (SOURCE_ROOT / 'README.md').read_bytes()
        with tempfile.TemporaryDirectory() as external:
            outside = Path(external)
            marker = outside / 'hook-ran'
            hooks = outside / 'hooks'
            hooks.mkdir()
            hook = hooks / 'pre-commit'
            hook.write_text('#!/bin/sh\ntouch "' + str(marker) + '"\nexit 99\n')
            hook.chmod(0o755)
            config = outside / 'gitconfig'
            config.write_text(f'[core]\n hooksPath = {hooks}\n[commit]\n gpgSign = true\n')
            hostile = {'HOME': str(outside), 'GIT_CONFIG_GLOBAL': str(config),
                       'GIT_DIR': str(outside / 'wrong.git'), 'GITHUB_TOKEN': 'synthetic-secret',
                       'ANTHROPIC_API_KEY': 'synthetic-secret', 'OPENAI_API_KEY': 'synthetic-secret'}
            with patch.dict(os.environ, hostile):
                with ProfileFixture() as fixture:
                    owned = fixture.directory
                    self.assertFalse(fixture.state.is_relative_to(fixture.root))
                    self.assertEqual('', fixture.git('status', '--porcelain').stdout)
                    self.assertEqual(['T001', 'T002'], [line.split()[3] for line in
                        (fixture.guard.feature / 'tasks.md').read_text().splitlines()
                        if line.startswith('- [ ] T')])
                    for root, state in ((SOURCE_ROOT, fixture.state),
                                        (fixture.root, outside),
                                        (str(fixture.root) + '/.', fixture.state)):
                        with self.assertRaises(FixtureBoundaryError):
                            fixture.require_roots(root, state)
                    for name in ('../escape', str(outside / 'escape'), 'specs/synthetic/spec.md'):
                        with self.assertRaises(FixtureBoundaryError):
                            fixture.write(name, 'forbidden')
                    fixture.write('guide.md', 'Synthetic change\n')
                    fixture.git('add', 'guide.md')
                    fixture.git('commit', '-m', 'test: synthetic output')
                    self.assertFalse(marker.exists())
                    local_config = fixture.root / '.git/config'
                    original_config = local_config.read_bytes()
                    local_config.write_bytes(original_config + b'\n[filter \"hostile\"]\n clean = false\n')
                    with self.assertRaises(FixtureBoundaryError):
                        fixture.git('add', 'guide.md')
                    local_config.write_bytes(original_config)
                    attributes = fixture.root / '.gitattributes'
                    attributes.write_text('guide.md filter=hostile\n')
                    with self.assertRaises(FixtureBoundaryError):
                        fixture.git('add', 'guide.md')
                    attributes.unlink()
                    self.assertNotEqual(fixture.guard.baseline, fixture.git('rev-parse', 'HEAD').stdout.strip())
                    context = fixture.guard.context()
                    self.assertEqual('T001', context['task_id'])
                    self.assertEqual([], context['dependencies'])
                    self.assertEqual(context['approval']['context_digest'],
                                     fixture.guard.review(context)['context_digest'])
                    self.assertTrue(fixture.guard.context('T002')['dependencies'])
                    with fixture.offline():
                        self.assertEqual(str(fixture.home), os.environ['HOME'])
                        self.assertNotIn('GITHUB_TOKEN', os.environ)
                        self.assertNotIn('ANTHROPIC_API_KEY', os.environ)
                        self.assertNotIn('OPENAI_API_KEY', os.environ)
                        self.assertNotIn('GIT_DIR', os.environ)
                    # A hostile symlink/hardlink never writes its external target.
                    output = fixture.root / 'followup.md'
                    sentinel = outside / 'sentinel'
                    sentinel.write_text('unchanged')
                    output.symlink_to(sentinel)
                    with self.assertRaises(FixtureBoundaryError):
                        fixture.write('followup.md', 'forbidden')
                    output.unlink()
                    os.link(sentinel, output)
                    with self.assertRaises(FixtureBoundaryError):
                        fixture.write('followup.md', 'forbidden')
                    output.unlink()
                    self.assertEqual('unchanged', sentinel.read_text())
                    spec = fixture.guard.feature / 'spec.md'
                    before = spec.read_bytes()
                    spec.write_bytes(before + b'corruption')
                    with self.assertRaises(FixtureBoundaryError):
                        fixture.assert_oracles_unchanged()
                    spec.write_bytes(before)
                    fixture.assert_oracles_unchanged()
                    with ProfileFixture() as other:
                        foreign_index = other.root / '.git/index'
                        index_bytes = foreign_index.read_bytes()
                        metadata = fixture.root / '.git'
                        saved = fixture.root / '.git-saved'
                        metadata.rename(saved)
                        try:
                            metadata.symlink_to(other.root / '.git', target_is_directory=True)
                            with self.assertRaises(FixtureBoundaryError):
                                fixture.git('add', 'guide.md')
                            metadata.unlink()
                            metadata.write_text('gitdir: ' + str(other.root / '.git') + '\n')
                            with self.assertRaises(FixtureBoundaryError):
                                fixture.git('add', 'guide.md')
                            metadata.unlink()
                        finally:
                            if metadata.is_symlink() or metadata.is_file():
                                metadata.unlink()
                            saved.rename(metadata)
                        pointer = metadata / 'commondir'
                        pointer.write_text(str(other.root / '.git') + '\n')
                        with self.assertRaises(FixtureBoundaryError):
                            fixture.git('add', 'guide.md')
                        pointer.unlink()
                        nested = metadata / 'objects/info/alternates'
                        nested.write_text(str(other.root / '.git/objects') + '\n')
                        with self.assertRaises(FixtureBoundaryError):
                            fixture.git('add', 'guide.md')
                        nested.unlink()
                        self.assertEqual(index_bytes, foreign_index.read_bytes())
                    self.assertEqual(source_bytes, (SOURCE_ROOT / 'README.md').read_bytes())
                self.assertFalse(owned.exists())
                with self.assertRaises(FixtureBoundaryError):
                    fixture.runtime.emit(fixture.target(), 'after close')

    def test_fixture_authority(self):
        from dataclasses import replace
        with ProfileFixture() as fixture, ProfileFixture() as other:
            target = fixture.target()
            record = fixture.profile_record()
            self.assertEqual(target.project, record['project_id'])
            self.assertEqual(target.profile, record['profile_id'])
            record['secret_ref'] = 'forged'
            self.assertIsNone(fixture.profile_record()['secret_ref'])
            self.assertFalse(fixture.authority.observe('owner', target))
            fixture.authority.attest('owner', target)
            self.assertTrue(fixture.authority.observe('owner', target))
            self.assertFalse(fixture.authority.observe('policy', target))
            self.assertFalse(fixture.authority.observe('capability', target))
            for role in ('policy', 'capability'):
                fixture.authority.attest(role, target)
                self.assertTrue(fixture.authority.observe(role, target))
            for changed in (replace(target, task='T002'), replace(target, action='complete'),
                            replace(target, route='orchestrated'),
                            replace(target, profile='fixture:other')):
                self.assertFalse(fixture.authority.observe('owner', changed))
            for forged in ({'owner_ref': 'owner', 'approved': True},
                           replace(target, repository=str(SOURCE_ROOT)), other.target()):
                with self.assertRaises(FixtureBoundaryError):
                    fixture.authority.observe('owner', forged)
            fixture.authority.revoke('owner', target)
            self.assertFalse(fixture.authority.observe('owner', target))
            self.assertEqual(1800000000, fixture.time.observe())
            fixture.time.available = False
            with self.assertRaises(FixtureBoundaryError):
                fixture.time.observe()
            self.assertEqual(1, fixture.claim.acquire(target, 'fixture:executor', 0))
            with self.assertRaises(FixtureBoundaryError):
                fixture.claim.acquire(target, 'fixture:competitor', 0)
            fixture.faults.fail_on('runtime.emit')
            with self.assertRaises(InjectedFixtureFault):
                fixture.runtime.emit(target, 'first')
            self.assertEqual([], fixture.runtime.calls)
            result = fixture.runtime.emit(target, 'explicit synthetic output')
            self.assertEqual({'text': 'explicit synthetic output', 'provenance': 'offline',
                              'live_verified': False}, result)
            self.assertEqual(2, fixture.faults.calls.count('runtime.emit'))

    def test_no_live_ports(self):
        with ProfileFixture() as fixture:
            self.assertEqual(fixture.ports, fixture.require_ports(fixture.ports))
            invoked = []
            class LiveRuntime:
                def emit(self, *args):
                    invoked.append(args)
                    raise AssertionError('live callback must not run')
            ports = fixture.ports
            ports['runtime'] = LiveRuntime()
            for submitted in (ports, {'live_verified': False},
                              {**fixture.ports, 'tracker': LiveRuntime()}):
                with self.assertRaises(FixtureBoundaryError):
                    fixture.require_ports(submitted)
            self.assertEqual([], invoked)
            with self.assertRaises(FixtureBoundaryError):
                fixture.git('fetch', 'https://example.invalid/repo.git')
            with fixture.offline():
                probes = [lambda: socket.create_connection(('example.invalid', 443)),
                          lambda: socket.getaddrinfo('example.invalid', 443),
                          lambda: urllib.request.urlopen('https://example.invalid'),
                          lambda: subprocess.run(['codex', '--version']),
                          lambda: os.system('claude --version'),
                          fixture.provider, fixture.credential, fixture.fallback]
                with socket.socket() as connection:
                    probes += [lambda: connection.connect(('127.0.0.1', 9)),
                               lambda: connection.connect_ex(('127.0.0.1', 9)),
                               lambda: connection.sendto(b'x', ('127.0.0.1', 9)),
                               lambda: connection.send(b'x'),
                               lambda: connection.sendall(b'x'),
                               lambda: connection.bind(('127.0.0.1', 0)),
                               lambda: connection.listen()]
                    for probe in probes:
                        with self.assertRaises(FixtureBoundaryError):
                            probe()
                # Both routes operate on injected synthetic ports without tracker,
                # provider, credentials or Symphony being installed or invoked.
                for route in ('manual', 'orchestrated'):
                    result = fixture.runtime.emit(fixture.target(route=route), 'offline')
                    self.assertEqual('offline', result['provenance'])
                    self.assertIs(result['live_verified'], False)
            self.assertEqual(16, len(fixture.denied_calls))
            self.assertIn('credential.discovery', fixture.denied_calls)
            self.assertIn('account-or-agent.fallback', fixture.denied_calls)
            fixture.assert_oracles_unchanged()


if __name__ == '__main__':
    unittest.main()
