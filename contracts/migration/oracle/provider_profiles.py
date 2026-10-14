"""Inert Feature 005 parsing, canonical identities and action requirements.

These primitives neither assess permission nor supply capabilities/authority.
Typed-record validators must call require_keys and validate every field; generic
JSON parsing deliberately does not invent a profile or evidence schema.
"""
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import stat
from types import MappingProxyType

MAX_JSON_BYTES = 1024 * 1024
MAX_PROFILES = 100
MAX_EVIDENCE_REFS = 100
MAX_RETAINED_EVENTS = 1000
MAX_DATABASE_BYTES = 16 * 1024 * 1024
TASK_MODES = ('implement', 'handoff-only', 'owner-authorized rework')


class ProfileError(ValueError):
    """Fixed, non-input-bearing diagnostic code; never expose raw exceptions."""

    def __init__(self, code):
        self.code = code
        super().__init__(code)


def _json_value(value):
    if value is None or type(value) in (bool, int):
        return
    if type(value) is str:
        value.encode('utf-8', errors='strict')
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


def _unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError
        result[key] = value
    return result


def _reject_number(_text):
    raise ValueError


def parse_json(data):
    """Strict UTF-8 bytes, unique keys at every depth; no floats or BOM."""
    if type(data) is not bytes:
        raise ProfileError('invalid-json')
    if len(data) > MAX_JSON_BYTES:
        raise ProfileError('input-limit-exceeded')
    try:
        value = json.loads(data.decode('utf-8', errors='strict'),
                           object_pairs_hook=_unique_object,
                           parse_float=_reject_number, parse_constant=_reject_number)
        _json_value(value)
        return value
    except (ValueError, UnicodeError, RecursionError, OverflowError):
        raise ProfileError('invalid-json') from None


def canonical_bytes(value):
    """Compact sorted UTF-8 JSON, without normalization or a trailing LF."""
    try:
        _json_value(value)
        return json.dumps(value, sort_keys=True, separators=(',', ':'),
                          ensure_ascii=False, allow_nan=False).encode('utf-8')
    except (ValueError, TypeError, UnicodeError, RecursionError, OverflowError):
        raise ProfileError('invalid-json') from None


def canonical_digest(value):
    return hashlib.sha256(canonical_bytes(value)).hexdigest()


def require_keys(value, required, optional=()):
    """Require exactly a caller-owned typed-record schema (including nested use)."""
    if (type(value) is not dict or any(type(key) is not str for key in value)
            or not set(required) <= value.keys()
            or not value.keys() <= set(required) | set(optional)):
        raise ProfileError('invalid-object-keys')
    return value


def require_integer(value):
    if type(value) is not int:
        raise ProfileError('invalid-integer')
    return value


def require_timestamp(value):
    """A signed integer Unix second denotes UTC, never a local/float timestamp."""
    if type(value) is not int:
        raise ProfileError('invalid-timestamp')
    return value


def require_interval(start, end):
    require_timestamp(start)
    require_timestamp(end)
    if start >= end:
        raise ProfileError('invalid-interval')
    return start, end


def require_identifier(value):
    if type(value) is not str or re.fullmatch(r'[A-Za-z0-9._:-]{1,128}', value) is None:
        raise ProfileError('invalid-identifier')
    return value


def require_digest(value):
    if type(value) is not str or re.fullmatch(r'[0-9a-f]{64}', value) is None:
        raise ProfileError('invalid-digest')
    return value


def _bounded_list(value, limit):
    if type(value) is not list or len(value) > limit:
        raise ProfileError('input-limit-exceeded')
    return value


def require_profiles(value):
    """Bound the list only; a later typed profile validator owns its contents."""
    return _bounded_list(value, MAX_PROFILES)


def require_evidence_refs(value):
    """Bound the list only; evidence validation/authority is separate."""
    return _bounded_list(value, MAX_EVIDENCE_REFS)


def _bounded_count(value, limit):
    if type(value) is not int or not 0 <= value <= limit:
        raise ProfileError('input-limit-exceeded')
    return value


def require_event_count(value):
    """Size predicate only: does not append, archive or grant storage authority."""
    return _bounded_count(value, MAX_RETAINED_EVENTS)


def require_database_size(value):
    return _bounded_count(value, MAX_DATABASE_BYTES)


def require_artifact_path(value):
    """Canonical POSIX-relative names; never normalize aliases into acceptance."""
    if (type(value) is not str or not value or value.startswith('/')
            or '\\' in value or '\x00' in value or ':' in value
            or any(part in ('', '.', '..') for part in value.split('/'))):
        raise ProfileError('invalid-artifact-path')
    try:
        value.encode('utf-8', errors='strict')
    except UnicodeError:
        raise ProfileError('invalid-artifact-path') from None
    return value


def _directory_at(parent, component):
    return os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                   dir_fd=parent)


def read_artifact(root, relative):
    """Read a bounded regular single-link snapshot under a trusted absolute root.

    Walk from / with no-follow directory descriptors, including root ancestors.
    No symlink/nonregular input is opened for reading; no writes or path repair
    occur. This is a checked local snapshot, not a hostile-owner sandbox or lease:
    callers must freshly revalidate it at their own protected action boundary.
    """
    require_artifact_path(relative)
    try:
        root = Path(root)
        if not root.is_absolute() or any(part in ('.', '..') for part in root.parts):
            raise ProfileError('invalid-artifact-path')
        directory = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            for component in (*root.parts[1:], *relative.split('/')[:-1]):
                child = _directory_at(directory, component)
                os.close(directory)
                directory = child
            name = relative.split('/')[-1]
            before = os.stat(name, dir_fd=directory, follow_symlinks=False)
            if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1:
                raise ProfileError('unsafe-artifact')
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                         dir_fd=directory)
            try:
                opened = os.fstat(fd)
                if (not stat.S_ISREG(opened.st_mode) or opened.st_nlink != 1
                        or (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino)):
                    raise ProfileError('unsafe-artifact')
                if opened.st_size > MAX_JSON_BYTES:
                    raise ProfileError('input-limit-exceeded')
                chunks = []
                total = 0
                while True:
                    chunk = os.read(fd, min(65536, MAX_JSON_BYTES + 1 - total))
                    if not chunk:
                        break
                    chunks.append(chunk)
                    total += len(chunk)
                    if total > MAX_JSON_BYTES:
                        raise ProfileError('input-limit-exceeded')
                after = os.fstat(fd)
                current = os.stat(name, dir_fd=directory, follow_symlinks=False)
                fields = ('st_dev', 'st_ino', 'st_mode', 'st_nlink', 'st_size',
                          'st_mtime_ns', 'st_ctime_ns')
                if (any(getattr(opened, field) != getattr(after, field) for field in fields)
                        or any(getattr(after, field) != getattr(current, field) for field in fields)
                        or total != after.st_size):
                    raise ProfileError('artifact-changed')
                return b''.join(chunks)
            finally:
                os.close(fd)
        finally:
            os.close(directory)
    except (OSError, TypeError, ValueError, UnicodeError) as error:
        if isinstance(error, ProfileError):
            raise
        raise ProfileError('unsafe-artifact') from None


@dataclass(frozen=True)
class ActionSpec:
    action_class: str
    boundary: str | None
    guard_operation: str | None
    capabilities: tuple[str, ...]
    package_action: str | None


# G(a,m) is intentionally deferred to unchanged task_package.required_capabilities;
# duplicating that implementation here would risk divergent requirements.
ACTIONS = MappingProxyType({
    'begin': ActionSpec('execution', 'entry', 'begin', ('bridge.begin',), 'entry'),
    'inspect': ActionSpec('observation', None, 'inspect', ('bridge.inspect',), None),
    'verify': ActionSpec('execution', 'completion', 'verify', ('bridge.verify',), 'completion'),
    'complete': ActionSpec('execution', 'completion', 'complete', ('bridge.complete',), 'completion'),
    'record-commit': ActionSpec('execution', 'completion', 'record-commit', ('bridge.record-commit',), 'completion'),
    'resume': ActionSpec('execution', 'resume', 'resume', ('bridge.resume',), 'resume'),
    'rework': ActionSpec('execution', 'entry', 'rework', ('bridge.rework',), 'entry'),
    'status': ActionSpec('observation', None, None, ('ledger.read',), None),
    'lookup': ActionSpec('observation', None, None, ('bridge.lookup',), None),
    'transfer': ActionSpec('execution', 'eligibility', None, ('claim.fenced-transfer',), None),
    'ready': ActionSpec('execution', 'eligibility', None, (), 'ready'),
    'publish': ActionSpec('execution', 'handoff', None, (), 'handoff'),
    'final-labels': ActionSpec('execution', 'handoff', None, (), 'handoff'),
    'provider-execute': ActionSpec('execution', 'provider', None, ('provider.execution',), None),
    'safe-stop': ActionSpec('safety', None, None, ('runtime.safe-stop',), None),
})


@dataclass(frozen=True)
class ActionRequirements:
    capabilities: tuple[str, ...]
    package_action: str | None
    package_mode: str
    downstream_transfer_required: bool
    reference_executor_available: bool


def action_spec(action):
    if type(action) is not str or action not in ACTIONS:
        raise ProfileError('invalid-action')
    return ACTIONS[action]


def require_actions(value, *, execution_only=False):
    if type(value) is not list or not value or type(execution_only) is not bool:
        raise ProfileError('invalid-actions')
    for action in value:
        spec = action_spec(action)
        if execution_only and spec.action_class != 'execution':
            raise ProfileError('invalid-actions')
    if len(set(value)) != len(value):
        raise ProfileError('invalid-actions')
    return tuple(value)


def action_for_operation(operation):
    spec = action_spec(operation)
    if spec.guard_operation is None:
        raise ProfileError('invalid-operation')
    return spec.guard_operation


def action_for_command(command, *, operation=None):
    if type(command) is not str:
        raise ProfileError('invalid-command')
    if command == 'guard':
        return action_for_operation(operation)
    if type(command) is str and command in ('status', 'transfer', 'lookup_creation') and operation is None:
        return 'lookup' if command == 'lookup_creation' else command
    raise ProfileError('invalid-command')


def action_requirements(action, mode, *, guard_attempt_retained=False):
    """Return a requirement recipe, never an available-capability verdict.

    Trusted composition unions capabilities with the unchanged G(package_action,
    package_mode) whenever package_action is non-null. Transfer additionally
    requires fresh new-profile revalidation and *every* downstream transfer/owner
    constraint. Other profile/task/inspection/safety/publication approvals remain
    mandatory independently. Missing composition/observations cannot mean success.
    """
    spec = action_spec(action)
    if type(mode) is not str or mode not in TASK_MODES or type(guard_attempt_retained) is not bool:
        raise ProfileError('invalid-capability-request')
    capabilities = spec.capabilities
    if action == 'status' and guard_attempt_retained:
        capabilities = ('bridge.status', 'ledger.read')
    return ActionRequirements(capabilities, spec.package_action,
                              'owner-authorized rework' if action == 'rework' else mode,
                              action == 'transfer', action not in ('provider-execute', 'safe-stop'))
