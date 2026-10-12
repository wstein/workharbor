"""Bounded local JSON and repository primitives; no execution or guard authority.

Readers reuse Feature 003's no-follow traversal/type checks without opening or
modifying its state. Returned bytes are a checked snapshot, not a filesystem lock
or a hostile-code sandbox. Callers must revalidate inputs at their action boundary.
"""
from contextlib import contextmanager
from dataclasses import dataclass
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess
from types import MappingProxyType
from typing import Mapping, Protocol

import single_task_state as state

MAX_JSON_BYTES = 1024 * 1024


class PackageError(ValueError):
    """Fixed diagnostic identifier; raw input/OS/Git errors never enter messages."""

    def __init__(self, code):
        self.code = code
        super().__init__(code)


def _json_value(value):
    if value is None or type(value) in (str, bool, int):
        if isinstance(value, str):
            value.encode('utf-8', errors='strict')
    elif type(value) is float:
        if not math.isfinite(value):
            raise ValueError
    elif type(value) is list:
        for child in value:
            _json_value(child)
    elif type(value) is dict:
        for key, child in value.items():
            if type(key) is not str:
                raise ValueError
            key.encode('utf-8', errors='strict')
            _json_value(child)
    else:
        raise ValueError


def parse_json(data):
    """Decode at most 1 MiB of strict UTF-8 JSON without relaxing JSON numbers."""
    if type(data) is not bytes:
        raise PackageError('invalid-json')
    if len(data) > MAX_JSON_BYTES:
        raise PackageError('input-limit-exceeded')
    def pairs(entries):
        result = {}
        for key, value in entries:
            if key in result:
                raise ValueError
            result[key] = value
        return result
    def constant(_value):
        raise ValueError
    try:
        value = json.loads(data.decode('utf-8', errors='strict'),
                           object_pairs_hook=pairs, parse_constant=constant)
        _json_value(value)
        return value
    except (ValueError, TypeError, UnicodeError, RecursionError):
        raise PackageError('invalid-json') from None


def require_keys(value, required, optional=()):
    """Check an explicitly supplied object shape; nested/opaque schemas stay owned by callers."""
    if (type(value) is not dict or not set(required) <= value.keys()
            or not value.keys() <= set(required) | set(optional)):
        raise PackageError('invalid-object-keys')
    return value


def canonical_json(value):
    """Canonical JSON bytes using the existing guard's serialization primitive."""
    try:
        _json_value(value)
        data = state._canonical(value)
    except (ValueError, TypeError, UnicodeError, RecursionError, state.InvalidState):
        raise PackageError('invalid-json') from None
    if len(data) > MAX_JSON_BYTES:
        raise PackageError('input-limit-exceeded')
    return data


def _path_parts(value):
    if (type(value) is not str or not value or len(value) > 4096
            or '\\' in value or any(ord(char) < 32 or ord(char) == 127 for char in value)):
        raise PackageError('unsafe-repository-path')
    try:
        value.encode('utf-8', errors='strict')
    except UnicodeError:
        raise PackageError('unsafe-repository-path') from None
    parts = value.split('/')
    if (len(parts) > 64 or any(part in ('', '.', '..', '.git', '.symphony') for part in parts)):
        raise PackageError('unsafe-repository-path')
    return parts


def _identity(info):
    return info.st_dev, info.st_ino, info.st_mode


def _file_identity(info):
    return (_identity(info), info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def _check_links(links):
    for parent, name, descriptor, initial in links:
        if (_identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) != _identity(initial)
                or _identity(os.fstat(descriptor)) != _identity(initial)):
            raise PackageError('repository-input-raced')


@contextmanager
def _root_directory(root):
    """Anchor every absolute component with Feature 003's no-follow traversal."""
    descriptors, links = [], []
    try:
        root = Path(root)
        if not root.is_absolute() or str(root) != os.path.normpath(str(root)):
            raise PackageError('unsafe-repository-root')
        current = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        descriptors.append(current)
        for name in root.parts[1:]:
            initial = os.stat(name, dir_fd=current, follow_symlinks=False)
            child = state._directory(current, name)
            descriptors.append(child)
            links.append((current, name, child, initial))
            current = child
        _check_links(links)
        yield root, current, links
        _check_links(links)
    except (OSError, ValueError, TypeError, state.InvalidState) as error:
        if isinstance(error, PackageError):
            raise
        raise PackageError('unsafe-repository-root') from None
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def _git(root, *args):
    environment = {key: value for key, value in os.environ.items()
                   if not key.startswith('GIT_')}
    environment.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                       GIT_NO_REPLACE_OBJECTS='1', GIT_NO_LAZY_FETCH='1',
                       GIT_TERMINAL_PROMPT='0', GIT_ALLOW_PROTOCOL='', GIT_OPTIONAL_LOCKS='0')
    try:
        result = subprocess.run(['git', '--no-replace-objects', '--no-lazy-fetch',
                                 '-c', 'core.fsmonitor=false', *args],
                                cwd=root, env=environment, capture_output=True, timeout=10)
        if result.returncode or len(result.stdout) > MAX_JSON_BYTES:
            raise PackageError('git-object-unavailable')
        return result.stdout
    except (OSError, subprocess.SubprocessError):
        raise PackageError('git-object-unavailable') from None


def _repository(root):
    if _git(root, 'rev-parse', '--show-toplevel').rstrip(b'\n') != os.fsencode(root):
        raise PackageError('unsafe-repository-root')
    if _git(root, 'for-each-ref', '--format=%(refname)', 'refs/replace/'):
        raise PackageError('git-object-substitution')
    common = Path(os.fsdecode(_git(root, 'rev-parse', '--path-format=absolute',
                                  '--git-common-dir').rstrip(b'\n')))
    if (common / 'info').is_symlink() or os.path.lexists(common / 'info/grafts'):
        raise PackageError('git-object-substitution')


def _index_gate(root, parts):
    """Query exact path ancestors, excluding each directory's descendants.

    Literal pathspecs prevent task text from becoming pathspec instructions. The
    separate exclusion keeps each response bounded by one index pathname.
    """
    inventory = []
    for count in range(1, len(parts) + 1):
        path = '/'.join(parts[:count])
        entries = _git_index_entry(root, path)
        for entry in entries.split(b'\0'):
            if not entry:
                continue
            header, name = entry.split(b'\t', 1)
            mode, _object_id, stage = header.split(b' ')
            if name == path.encode('utf-8'):
                if mode not in (b'100644', b'100755') or stage != b'0':
                    raise PackageError('unsafe-repository-path')
                inventory.append(entry)
    return tuple(inventory)


def _git_index_entry(root, path):
    pattern = ''.join('\\' + char if char in '*?[]' else char for char in path)
    return _git(root, 'ls-files', '--stage', '-z', '--',
                ':(top,literal)' + path, ':(top,glob,exclude)' + pattern + '/**')


def read_repository_file(root, path):
    """Read a bounded, regular single-link file through contained no-follow descriptors."""
    parts = _path_parts(path)
    descriptors = []
    try:
        with _root_directory(root) as (root, parent, links):
            _repository(root)
            before_index = _index_gate(root, parts)
            for name in parts[:-1]:
                initial = os.stat(name, dir_fd=parent, follow_symlinks=False)
                child = state._directory(parent, name)
                descriptors.append(child)
                links.append((parent, name, child, initial))
                parent = child
            name = parts[-1]
            state._regular(parent, name, required=True)
            initial = os.stat(name, dir_fd=parent, follow_symlinks=False)
            if initial.st_size > MAX_JSON_BYTES:
                raise PackageError('input-limit-exceeded')
            descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK,
                                 dir_fd=parent)
            descriptors.append(descriptor)
            opened = os.fstat(descriptor)
            if (not stat.S_ISREG(opened.st_mode) or opened.st_nlink != 1
                    or _file_identity(initial) != _file_identity(opened)):
                raise PackageError('repository-input-raced')
            chunks, size = [], 0
            while True:
                chunk = os.read(descriptor, min(65536, MAX_JSON_BYTES + 1 - size))
                if not chunk:
                    break
                chunks.append(chunk)
                size += len(chunk)
                if size > MAX_JSON_BYTES:
                    raise PackageError('input-limit-exceeded')
            if _index_gate(root, parts) != before_index:
                raise PackageError('repository-input-raced')
            if (_file_identity(os.fstat(descriptor)) != _file_identity(initial)
                    or _file_identity(os.stat(name, dir_fd=parent, follow_symlinks=False))
                    != _file_identity(initial) or size != initial.st_size):
                raise PackageError('repository-input-raced')
            _check_links(links)
            return b''.join(chunks)
    except (OSError, ValueError, TypeError, state.InvalidState) as error:
        if isinstance(error, PackageError):
            raise
        raise PackageError('unsafe-repository-path') from None
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def load_json(root, path, *, required, optional=()):
    return require_keys(parse_json(read_repository_file(root, path)), required, optional)


def resolve_git_object(root, object_id, *, expected_type='commit'):
    """Resolve an exact local object without ref expansion, replacement or lazy fetch."""
    if (type(object_id) is not str or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', object_id)
            or expected_type not in ('commit', 'tree', 'blob', 'tag')):
        raise PackageError('invalid-git-object')
    with _root_directory(root) as (root, _descriptor, _links):
        _repository(root)
        algorithm = _git(root, 'rev-parse', '--show-object-format')
        if len(object_id) != {b'sha1\n': 40, b'sha256\n': 64}.get(algorithm):
            raise PackageError('invalid-git-object')
        if _git(root, 'cat-file', '-t', object_id) != expected_type.encode('ascii') + b'\n':
            raise PackageError('invalid-git-object')
        return object_id


# Capability evidence is supplied by trusted host code, never instantiated from
# package text. These protocols describe missing enhanced Feature 003/provider
# APIs; the legacy single_task CLI is deliberately not an adapter.
@dataclass(frozen=True)
class CapabilityEvidence:
    available: bool
    implementation_revision: str | None
    fixture_result: bool | None
    live_observation_ref: str | None


class GuardPort(Protocol):
    capabilities: Mapping[str, CapabilityEvidence]

    def validate_context(self, context, evidence): ...
    def compare_artifacts(self, approved_snapshot, current_snapshot, evidence): ...
    def validate_scope(self, repository_snapshot, context, evidence): ...
    def validate_acceptance(self, context, evidence): ...


class TrackerPort(Protocol):
    capabilities: Mapping[str, CapabilityEvidence]

    def read_approval(self, reference): ...
    def read_issue(self, identity): ...
    def find_pr(self, identity): ...
    def publish_pr(self, request): ...
    def find_evidence(self, identity): ...
    def write_evidence(self, request): ...
    def replace_labels(self, identity, expected, desired): ...


class RuntimePort(Protocol):
    capabilities: Mapping[str, CapabilityEvidence]

    def observe(self, execution): ...


class UnavailableGuard:
    capabilities = MappingProxyType({})

    def validate_context(self, context, evidence):
        raise PackageError('guard-unavailable')

    def compare_artifacts(self, approved_snapshot, current_snapshot, evidence):
        raise PackageError('guard-unavailable')

    def validate_scope(self, repository_snapshot, context, evidence):
        raise PackageError('guard-unavailable')

    def validate_acceptance(self, context, evidence):
        raise PackageError('guard-unavailable')


class UnavailableTracker:
    capabilities = MappingProxyType({})

    def read_approval(self, reference):
        raise PackageError('tracker-unavailable')

    def read_issue(self, identity):
        raise PackageError('tracker-unavailable')

    def find_pr(self, identity):
        raise PackageError('tracker-unavailable')

    def publish_pr(self, request):
        raise PackageError('tracker-unavailable')

    def find_evidence(self, identity):
        raise PackageError('tracker-unavailable')

    def write_evidence(self, request):
        raise PackageError('tracker-unavailable')

    def replace_labels(self, identity, expected, desired):
        raise PackageError('tracker-unavailable')


class UnavailableRuntime:
    capabilities = MappingProxyType({})

    def observe(self, execution):
        raise PackageError('runtime-unavailable')


_ENTRY = frozenset(('guard.context', 'guard.dependencies', 'guard.artifact-comparison',
                    'guard.scope-current', 'guard.scope-index', 'guard.scope-history'))
_COMPLETION = frozenset(('guard.acceptance', 'guard.content-seal', 'guard.review-seal',
                         'guard.commit-binding'))
_RECOVERY = frozenset(('guard.recovery',))
_TRACKER = frozenset(('tracker.approval-authentication', 'tracker.issue-ownership',
                      'tracker.pr-publication', 'tracker.evidence', 'tracker.safe-labels'))
_RUNTIME = frozenset(('runtime.provenance', 'runtime.workflow', 'runtime.configuration',
                      'runtime.host-isolation'))
_MODES = ('implement', 'handoff-only', 'owner-authorized rework')


def required_capabilities(action, mode):
    """Derive requirements per step; entry alone never requires completion APIs.

    Readiness checks the entire proposed execution before making it eligible.
    Preview/structural commands expose entry requirements without invoking them.
    """
    by_action = {
        'prepare': _ENTRY, 'validate': _ENTRY, 'effective': _ENTRY,
        'entry': _ENTRY, 'completion': _COMPLETION, 'resume': _ENTRY | _RECOVERY,
        'handoff': _COMPLETION | _RECOVERY | _TRACKER,
        'ready': _ENTRY | _COMPLETION | _RECOVERY | _TRACKER | _RUNTIME,
    }
    if type(action) is not str or action not in by_action or mode not in _MODES:
        raise PackageError('invalid-capability-request')
    required = by_action[action]
    if mode == 'owner-authorized rework':
        required |= {'guard.rework'}
    if mode == 'handoff-only' and action in ('prepare', 'validate', 'effective', 'entry'):
        required |= _RECOVERY
    return tuple(sorted(required))


def _capability_evidence(name, observation, live):
    """Return sanitized observations; invalid supplied references never escape."""
    empty = dict(capability=name, available=False, implementation_revision=None,
                 fixture_result=None, live_observation_ref=None)
    if observation is None:
        return empty, 'capability-unavailable'
    if (not isinstance(observation, CapabilityEvidence)
            or type(observation.available) is not bool
            or observation.fixture_result is not None and type(observation.fixture_result) is not bool
            or observation.implementation_revision is not None and (
                type(observation.implementation_revision) is not str
                or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', observation.implementation_revision))
            or observation.live_observation_ref is not None and (
                type(observation.live_observation_ref) is not str
                or not re.fullmatch(r'sha256:[0-9a-f]{64}', observation.live_observation_ref))):
        return empty, 'invalid-capability-evidence'
    value = dict(capability=name, available=observation.available,
                 implementation_revision=observation.implementation_revision,
                 fixture_result=observation.fixture_result,
                 live_observation_ref=observation.live_observation_ref)
    if not observation.available:
        return value, 'capability-unavailable'
    if observation.implementation_revision is None or observation.fixture_result is not True:
        return value, 'capability-unverified'
    if live and observation.live_observation_ref is None:
        return value, 'live-capability-unobserved'
    return value, None


def assess_capabilities(action, mode, *, guard=None, tracker=None, runtime=None, live=False):
    """Inspect host-supplied evidence only; never call guard/tracker/runtime operations.

    Even complete evidence is no owner approval, review, actual execution or
    execution permission. A later readiness validator owns those separate gates.
    Live references here are safe SHA-256 evidence identifiers, not raw URLs or logs.
    """
    if type(live) is not bool:
        raise PackageError('invalid-capability-request')
    required = required_capabilities(action, mode)
    ports = {'guard': guard if guard is not None else UnavailableGuard(),
             'tracker': tracker if tracker is not None else UnavailableTracker(),
             'runtime': runtime if runtime is not None else UnavailableRuntime()}
    snapshots = {}
    for category, port in ports.items():
        try:
            snapshots[category] = dict(port.capabilities)
        except Exception:
            # Host adapter diagnostics may contain credentials. Expose no raw error.
            snapshots[category] = {}
    evidence, blockers = [], []
    for name in required:
        observation = snapshots[name.split('.', 1)[0]].get(name)
        value, blocker = _capability_evidence(name, observation, live)
        evidence.append(value)
        if blocker:
            blockers.append(dict(capability=name, code=blocker))
    return dict(structural_valid=True, execution_eligible=False,
                capabilities_satisfied=not blockers, required_capabilities=list(required),
                evidence=evidence, blockers=blockers, authenticated_approval=None,
                independent_review=None, live_execution=None)
