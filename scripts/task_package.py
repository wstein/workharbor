"""Bounded local JSON and repository primitives; no execution or guard authority.

Readers reuse Feature 003's no-follow traversal/type checks without opening or
modifying its state. Returned bytes are a checked snapshot, not a filesystem lock
or a hostile-code sandbox. Callers must revalidate inputs at their action boundary.
"""
from contextlib import contextmanager
from dataclasses import dataclass
import hashlib
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
import approval_binding

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
    # Offline provenance lookups need no credentials or interpreter settings.
    environment = {key: os.environ[key] for key in ('PATH', 'LANG', 'LC_ALL', 'TMPDIR')
                   if key in os.environ}
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


# Package envelope schema. The context stays opaque: Feature 003 alone owns its
# keys, dependency semantics, exclusions, approval comparison and scope rules.

_PACKAGE_KEYS = frozenset(('schema_version', 'repository', 'issue', 'feature', 'task_id',
                           'mode', 'context', 'source', 'execution', 'outcomes', 'approval_refs'))
_SOURCE_KEYS = frozenset(('accepted_commit', 'accepted_tree', 'proposal_commit', 'proposal_tree',
                          'proposal_commits', 'artifact_revisions', 'equivalence_evidence_digest'))
_EXECUTION_KEYS = frozenset(('symphony_revision', 'build_provenance', 'workflow_digest',
                             'template_digest', 'configuration_digest', 'guard_revision',
                             'capabilities', 'external_actions', 'publication_scope'))
_APPROVAL_KEYS = frozenset(('spec', 'plan', 'execution', 'matrix', 'rework'))
_EXTERNAL_ACTIONS = frozenset(('publish-pr', 'write-evidence', 'replace-labels'))


def _matches(value, pattern):
    return type(value) is str and re.fullmatch(pattern, value) is not None


def _revision(value):
    return _matches(value, r'[0-9a-f]{40}|[0-9a-f]{64}')


def _digest(value):
    return _matches(value, r'[0-9a-f]{64}')


def _reference(value):
    # Inert content-addressed/local evidence or a credential-free GitHub object
    # URL. Query strings, userinfo, arbitrary URLs and raw output are not stored.
    if value is None or _matches(value, r'sha256:[0-9a-f]{64}'):
        return True
    if _matches(value, r'https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/(?:pull|issues)/[1-9][0-9]*(?:#(?:issuecomment|discussion_r|pullrequestreview)-[1-9][0-9]*)?'):
        return True
    try:
        _path_parts(value)
        return True
    except PackageError:
        return False


def _package_shape(value):
    require_keys(value, _PACKAGE_KEYS, {'package_digest'})
    if (type(value['schema_version']) is not int or value['schema_version'] != 1
            or not _matches(value['repository'], r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+')
            or not _matches(value['feature'], r'[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*')
            or value['mode'] not in _MODES or type(value['context']) is not dict
            or type(value['outcomes']) is not list or not value['outcomes']):
        raise PackageError('invalid-package')
    issue = value['issue']
    if issue is not None:
        require_keys(issue, {'repository', 'number'})
        if (issue['repository'] != value['repository'] or type(issue['number']) is not int
                or issue['number'] <= 0 or not _matches(value['task_id'], r'T[0-9]{3}')):
            raise PackageError('invalid-package-issue')
    elif not _matches(value['task_id'], r'T[0-9]{3}|fixture:[a-z0-9][a-z0-9-]{0,63}'):
        raise PackageError('invalid-package-task')
    source = require_keys(value['source'], _SOURCE_KEYS)
    if (any(not _revision(source[key]) for key in
            ('accepted_commit', 'accepted_tree', 'proposal_commit', 'proposal_tree'))
            or type(source['proposal_commits']) is not list
            or any(not _revision(item) for item in source['proposal_commits'])
            or len(set(source['proposal_commits'])) != len(source['proposal_commits'])
            or source['equivalence_evidence_digest'] is not None
            and not _digest(source['equivalence_evidence_digest'])):
        raise PackageError('invalid-package-source')
    artifacts = source['artifact_revisions']
    if type(artifacts) is not list or len(artifacts) != 3:
        raise PackageError('invalid-package-artifacts')
    paths = []
    for artifact in artifacts:
        require_keys(artifact, {'path', 'commit', 'sha256'})
        _path_parts(artifact['path'])
        if not _revision(artifact['commit']) or not _digest(artifact['sha256']):
            raise PackageError('invalid-package-artifacts')
        paths.append(artifact['path'])
    expected = {f"specs/{value['feature']}/{name}" for name in ('spec.md', 'plan.md', 'tasks.md')}
    if len(set(paths)) != 3 or set(paths) != expected:
        raise PackageError('invalid-package-artifacts')
    execution = require_keys(value['execution'], _EXECUTION_KEYS)
    if (any(execution[key] is not None and not _revision(execution[key])
            for key in ('symphony_revision', 'guard_revision'))
            or any(execution[key] is not None and not _digest(execution[key])
                   for key in ('build_provenance', 'workflow_digest', 'template_digest', 'configuration_digest'))
            or type(execution['capabilities']) is not dict
            or type(execution['external_actions']) is not list
            or any(type(action) is not str or action not in _EXTERNAL_ACTIONS
                   for action in execution['external_actions'])
            or len(set(execution['external_actions'])) != len(execution['external_actions'])):
        raise PackageError('invalid-package-execution')
    for action, names in execution['capabilities'].items():
        required_capabilities(action, value['mode'])
        if (type(names) is not list or any(not _matches(name, r'(?:guard|tracker|runtime)\.[a-z][a-z-]{0,63}')
                                         for name in names) or len(set(names)) != len(names)):
            raise PackageError('invalid-package-capabilities')
    publication = execution['publication_scope']
    if publication is not None:
        require_keys(publication, {'repository', 'base_ref', 'head_ref'})
        if (publication['repository'] != value['repository'] or publication['base_ref'] != 'refs/heads/main'
                or not _matches(publication['head_ref'], r'refs/heads/[a-zA-Z0-9][a-zA-Z0-9/_-]{0,127}')
                or '..' in publication['head_ref']):
            raise PackageError('invalid-publication-scope')
    approvals = require_keys(value['approval_refs'], _APPROVAL_KEYS)
    if any(not _reference(reference) for reference in approvals.values()):
        raise PackageError('invalid-approval-reference')


def _package_identity(value):
    # Do not recalculate or remove the guard's context self-digest: its owning
    # implementation validates that documented exclusion. The exact opaque
    # supplied context is preserved, including its existing identity field.
    immutable = {key: item for key, item in value.items()
                 if key not in ('approval_refs', 'package_digest')}
    return hashlib.sha256(canonical_json(immutable)).hexdigest()


def build_package(value, *, timestamp=None, output_path=None, attempt_id=None):
    """Return a detached canonical proposal; transient run metadata is never stored.

    No file is created/overwritten here. T007's writer uses revision bytes for
    immutable output reuse. Runtime actions, references and assertions grant no
    permission. Outcome/context schemas remain delegated to their owning validators.
    """
    snapshot = parse_json(canonical_json(value))
    _package_shape(snapshot)
    identity = _package_identity(snapshot)
    if 'package_digest' in snapshot and snapshot['package_digest'] != identity:
        raise PackageError('package-digest-mismatch')
    snapshot['package_digest'] = identity
    return snapshot


def package_bytes(value):
    if type(value) is not dict or 'package_digest' not in value:
        raise PackageError('package-digest-missing')
    return canonical_json(build_package(value))


def package_revision_digest(value):
    """Raw immutable serialization identity, distinct from content approval/package identity."""
    return hashlib.sha256(package_bytes(value)).hexdigest()


@dataclass(frozen=True)
class GuardValidation:
    """Port result only; this does not define or implement a TaskContext schema."""
    accepted: bool
    unmet_prerequisites: tuple[str, ...] = ()


def _validate_source_identities(root, source):
    # Only exact provenance objects/artifact bytes are checked here. Ancestry,
    # scope, rebase equivalence and wrapper/history policy belong to T009/003.
    for prefix in ('accepted', 'proposal'):
        commit = source[prefix + '_commit']
        tree = source[prefix + '_tree']
        resolve_git_object(root, commit)
        resolve_git_object(root, tree, expected_type='tree')
        if _git(root, 'rev-parse', commit + '^{tree}').strip() != tree.encode('ascii'):
            raise PackageError('source-tree-mismatch')
    for commit in source['proposal_commits']:
        resolve_git_object(root, commit)
    for artifact in source['artifact_revisions']:
        resolve_git_object(root, artifact['commit'])
        entries = _git(root, 'ls-tree', '-z', artifact['commit'], '--',
                       ':(literal)' + artifact['path']).split(b'\0')
        entries = [entry for entry in entries if entry]
        if len(entries) != 1:
            raise PackageError('artifact-identity-mismatch')
        header, name = entries[0].split(b'\t', 1)
        mode, kind, identity = header.split(b' ')
        if (name != artifact['path'].encode('utf-8') or mode not in (b'100644', b'100755')
                or kind != b'blob' or int(_git(root, 'cat-file', '-s', identity.decode('ascii')))
                > MAX_JSON_BYTES):
            raise PackageError('artifact-identity-mismatch')
        data = _git(root, 'cat-file', 'blob', identity.decode('ascii'))
        if hashlib.sha256(data).hexdigest() != artifact['sha256']:
            raise PackageError('artifact-identity-mismatch')


def _guard_verdict(value):
    if (not isinstance(value, GuardValidation) or type(value.accepted) is not bool
            or type(value.unmet_prerequisites) is not tuple
            or len(value.unmet_prerequisites) > 1000
            or any(not _matches(item, r'T[0-9]{3}|fixture:[a-z0-9][a-z0-9-]{0,63}')
                   for item in value.unmet_prerequisites)
            or len(set(value.unmet_prerequisites)) != len(value.unmet_prerequisites)
            or value.accepted and value.unmet_prerequisites):
        raise PackageError('invalid-guard-result')
    return value


def assess_content_approval(approved, current, repository_snapshot, evidence, *,
                            guard=None, authority=None, action='execution', live=False):
    """Bridge immutable packages to the existing content-binding adapter.

    The injected guard's context API returns approval_binding.ValidatedContract;
    it owns dependencies and comparison semantics. Its scope API returns
    GuardValidation and owns current/index/history scope. This enhanced API is
    deliberately separate from the structural validator's older fixture port.
    Missing demonstrated APIs block; no legacy CLI or local schema substitutes.

    Retention authenticates the original package/revision/binding, never the
    current raw snapshot. Even successful live-port validation grants no entry,
    independent review, publication or eligibility; T013 composes those gates.
    """
    result = dict(schema_version=1, content_approval_retained=False,
                  guard_entry_valid=False, execution_eligible=False,
                  approval_assurance=None, authenticated_approval=False,
                  independent_review=None, live_execution=None,
                  original_approval=None, current_snapshot=None, comparison=None,
                  capabilities=None, unmet_prerequisites=[], blockers=[])
    try:
        # Keep every directory identity anchored through adapter authentication
        # and guard scope validation. The adapter resolves paths itself; a
        # replacement observed on exit invalidates the bridge's entry result.
        with _root_directory(repository_snapshot['root']) as (root, _descriptor, _links):
            return _assess_content_approval(approved, current, repository_snapshot, evidence,
                root, result, guard=guard, authority=authority, action=action, live=live)
    except PackageError as error:
        result['guard_entry_valid'] = False
        result['blockers'].append(dict(code=error.code))
    except Exception:
        result['guard_entry_valid'] = False
        result['blockers'].append(dict(code='approval-validation-unavailable'))
    return result


def _assess_content_approval(approved, current, repository_snapshot, evidence, root, result, *,
                             guard, authority, action, live):

    def block(code):
        result['blockers'].append(dict(code=code))

    try:
        if type(action) is not str or action not in _APPROVAL_KEYS:
            raise PackageError('invalid-approval-action')
        package_bytes(approved)
        package_bytes(current)
        approved, current = build_package(approved), build_package(current)
        identity = ('repository', 'issue', 'feature', 'task_id')
        if (any(approved[key] != current[key] for key in identity)
                or current['repository'] != repository_snapshot['repository']):
            raise PackageError('approval-package-mismatch')
        reference = approved['approval_refs'][action]
        if reference is None:
            raise PackageError('approval-reference-missing')
        _validate_source_identities(root, approved['source'])
        _validate_source_identities(root, current['source'])
        result['original_approval'] = dict(action=action, reference=reference,
            reviewed_revision=approved['source']['proposal_commit'],
            package_digest=approved['package_digest'],
            revision_digest=package_revision_digest(approved), binding_digest=None)
        result['current_snapshot'] = dict(revision=current['source']['proposal_commit'],
            package_digest=current['package_digest'],
            revision_digest=package_revision_digest(current), binding_digest=None)
        assessment = assess_capabilities('entry', current['mode'], guard=guard, live=live)
        result['capabilities'] = assessment
        result['blockers'].extend(assessment['blockers'])
        if not assessment['capabilities_satisfied']:
            return result
        revision = guard.revision
        if (revision != current['execution']['guard_revision']
                or any(item['implementation_revision'] != revision
                       for item in assessment['evidence'])):
            raise PackageError('guard-revision-mismatch')
    except PackageError as error:
        block(error.code)
        return result
    except Exception:
        block('approval-validation-unavailable')
        return result

    request = dict(schema_version=1,
        approved_revision=approved['source']['proposal_commit'],
        current_revision=current['source']['proposal_commit'],
        approved_context=approved['context'], current_context=current['context'],
        evidence=evidence, approval_ref=reference, approval_action=action,
        approved_package_digest=approved['package_digest'])
    try:
        comparison = approval_binding.assess_retention(root, request, guard=guard, authority=authority)
        # Adapter evidence can contain provider diagnostics. Accept only inert
        # references before exposing its otherwise sanitized result.
        for key in ('comparison_evidence_ref', 'approval_evidence_ref'):
            if comparison[key] is not None and not _reference(comparison[key]):
                raise ValueError
        result['comparison'] = comparison
        result['original_approval']['binding_digest'] = comparison['binding_digest']
        result['current_snapshot']['binding_digest'] = comparison['current_binding_digest']
        if (comparison['retained'] is not True or comparison['blocker'] is not None
                or comparison['result'] not in ('identical', 'progress-only')):
            block('content-approval-blocked')
            return result
        result['content_approval_retained'] = True
        result['approval_assurance'] = comparison['approval_assurance']
        result['authenticated_approval'] = comparison['approval_assurance'] == 'authenticated-provider'
        if live and not result['authenticated_approval']:
            block('authenticated-approval-required')
            return result
    except Exception:
        block('content-approval-blocked')
        return result

    try:
        # Pass a detached package and provenance snapshot; guard checks must not
        # silently adopt another baseline or mutate the caller's proposal.
        scope_snapshot = dict(repository_snapshot, approved=approved, current=current)
        verdict = _guard_verdict(guard.validate_scope(scope_snapshot, current['context'], evidence))
        result['unmet_prerequisites'] = list(verdict.unmet_prerequisites)
        if not verdict.accepted:
            raise ValueError
        result['guard_entry_valid'] = True
    except Exception:
        block('guard-scope-blocked')
    return result


def validate(value, repository_snapshot, evidence, capabilities, *, guard=None, outcome_validator=None):
    """Side-effect-free structural/provenance validation with opaque guard delegation.

    Host supplies repository/root and approved/current snapshots; package text
    never selects an adapter. No checks, prerequisite work, publication, provider
    lookup or guard mutation is performed. All execution remains ineligible.
    """
    result = dict(schema_version=1, package_digest=None, revision_digest=None,
                  structural_valid=False, execution_eligible=False, approval_assurance=None,
                  authenticated_approval=None, independent_review=None, live_execution=None,
                  raw_exits=[], unmet_prerequisites=[], blockers=[])
    def block(code):
        result['blockers'].append(dict(code=code))
    try:
        package_bytes(value)
        value = build_package(value)
        result['package_digest'] = value['package_digest']
        result['revision_digest'] = package_revision_digest(value)
        if value['repository'] != repository_snapshot['repository']:
            raise PackageError('repository-mismatch')
        with _root_directory(repository_snapshot['root']) as (root, _descriptor, _links):
            _validate_source_identities(root, value['source'])
        result['structural_valid'] = True
    except PackageError as error:
        # These errors are from the fixed local validators, never adapter text.
        block(error.code)
        return result
    except Exception:
        block('package-validation-unavailable')
        return result
    approvals = value['approval_refs']
    if any(reference is not None for reference in approvals.values()):
        result['approval_assurance'] = 'supplied-local'
    for action in ('spec', 'plan', 'execution'):
        if approvals[action] is None:
            block(action + '-approval-reference-missing')
    if value['mode'] == 'owner-authorized rework' and approvals['rework'] is None:
        block('rework-approval-reference-missing')
    if any(value['execution'][key] is None for key in
           ('symphony_revision', 'build_provenance', 'workflow_digest',
            'template_digest', 'configuration_digest', 'guard_revision')):
        block('execution-identity-unavailable')
    if outcome_validator is None:
        block('outcome-validator-unavailable')
    else:
        try:
            # T006 validates shape/outcomes only. This never calls its runner.
            checked = outcome_validator(parse_json(canonical_json(value['outcomes'])))
            if checked != value['outcomes']:
                raise ValueError
            if (any(case['expected'] == 'assertion' for check in checked for case in check['cases'])
                    and approvals['matrix'] is None):
                block('matrix-approval-reference-missing')
        except Exception:
            block('invalid-outcomes')
            result['structural_valid'] = False
    guard = guard if guard is not None else UnavailableGuard()
    try:
        verdict = _guard_verdict(guard.validate_context(value['context'], evidence))
        result['unmet_prerequisites'] = list(verdict.unmet_prerequisites)
        if not verdict.accepted:
            block('guard-context-blocked')
        else:
            try:
                comparison = guard.compare_artifacts(repository_snapshot['approved'],
                                                     repository_snapshot['current'], evidence)
                if comparison not in ('identical', 'progress-only'):
                    block('guard-comparison-blocked')
            except Exception:
                block('guard-comparison-blocked')
            for step, operation, arguments in (
                    ('scope', guard.validate_scope, (repository_snapshot, value['context'], evidence)),
                    ('acceptance', guard.validate_acceptance, (value['context'], evidence))):
                try:
                    if not _guard_verdict(operation(*arguments)).accepted:
                        block('guard-' + step + '-blocked')
                except Exception:
                    block('guard-' + step + '-blocked')
    except Exception:
        block('guard-unavailable' if isinstance(guard, UnavailableGuard) else 'guard-context-blocked')
    ports = capabilities if type(capabilities) is dict else {}
    assessment = assess_capabilities('validate', value['mode'], guard=guard,
                                    tracker=ports.get('tracker'), runtime=ports.get('runtime'))
    result['capabilities'] = assessment
    result['blockers'].extend(assessment['blockers'])
    return result


def write_preparation(root, output, value, summary):
    """Create/reuse one immutable serialization; preserve conflicts/partial writes.

    The caller explicitly chooses a contained output directory. Exclusive directory
    and file creation never overwrites existing evidence. An interrupted incomplete
    revision is a blocker, not permission to repair or adopt another writer's work.
    """
    parts = _path_parts(output)
    revision = package_revision_digest(value)
    content = {'package.json': package_bytes(value), 'summary.json': canonical_json(summary)}
    descriptors = []
    try:
        with _root_directory(root) as (_root, parent, links):
            try:
                _repository(root)
                gate_paths = [parts + [revision, name] for name in content]
                before_index = tuple(_index_gate(root, path) for path in gate_paths)
                if any(before_index):
                    raise PackageError('unsafe-output')
                for name in parts:
                    try:
                        os.mkdir(name, mode=0o700, dir_fd=parent)
                        os.fsync(parent)
                    except FileExistsError:
                        pass
                    initial = os.stat(name, dir_fd=parent, follow_symlinks=False)
                    child = state._directory(parent, name)
                    descriptors.append(child)
                    links.append((parent, name, child, initial))
                    parent = child
                _check_links(links)
                try:
                    os.mkdir(revision, mode=0o700, dir_fd=parent)
                    reused = False
                except FileExistsError:
                    reused = True
                initial = os.stat(revision, dir_fd=parent, follow_symlinks=False)
                directory = state._directory(parent, revision)
                descriptors.append(directory)
                links.append((parent, revision, directory, initial))
                _check_links(links)
                if reused:
                    names = set(os.listdir(directory))
                    if names < content.keys():
                        raise PackageError('output-incomplete')
                    if names != content.keys():
                        raise PackageError('output-conflict')
                files = []
                for name, data in content.items():
                    _check_links(links)
                    if reused:
                        state._regular(directory, name, required=True)
                        descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                                             dir_fd=directory)
                    else:
                        descriptor = os.open(name, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                             0o600, dir_fd=directory)
                    descriptors.append(descriptor)
                    info = os.fstat(descriptor)
                    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                        raise PackageError('unsafe-output')
                    files.append((name, data, descriptor, info))
                    if not reused:
                        remaining = memoryview(data)
                        while remaining:
                            written = os.write(descriptor, remaining)
                            if written <= 0:
                                raise PackageError('output-unavailable')
                            remaining = remaining[written:]
                        os.fsync(descriptor)
                if tuple(_index_gate(root, path) for path in gate_paths) != before_index:
                    raise PackageError('repository-input-raced')
                os.fsync(directory)
                os.fsync(parent)
                # Verify the complete output after all writes, index probes and
                # durability work. Keep both descriptors until the snapshot ends.
                observed = []
                for name, data, descriptor, initial in files:
                    checked = os.fstat(descriptor)
                    if (not stat.S_ISREG(checked.st_mode) or checked.st_nlink != 1
                            or _identity(checked) != _identity(initial)):
                        raise PackageError('unsafe-output')
                    os.lseek(descriptor, 0, os.SEEK_SET)
                    if checked.st_size != len(data) or os.read(descriptor, len(data) + 1) != data:
                        raise PackageError('output-conflict')
                    observed.append(checked)
                for (name, _data, descriptor, _initial), checked in zip(files, observed):
                    if (_file_identity(checked) != _file_identity(os.fstat(descriptor))
                            or _file_identity(checked) != _file_identity(
                                os.stat(name, dir_fd=directory, follow_symlinks=False))):
                        raise PackageError('unsafe-output')
                if set(os.listdir(directory)) != content.keys():
                    raise PackageError('output-conflict')
                _check_links(links)
                return dict(reused=reused, package=revision + '/package.json',
                            summary=revision + '/summary.json')
            except OSError:
                raise PackageError('output-unavailable') from None
    except PackageError:
        raise
    except state.InvalidState:
        raise PackageError('unsafe-output') from None
    except OSError:
        raise PackageError('output-unavailable') from None
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)
