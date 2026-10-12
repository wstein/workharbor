"""Bounded local JSON and repository primitives; no execution or guard authority.

Readers reuse Feature 003's no-follow traversal/type checks without opening or
modifying its state. Returned bytes are a checked snapshot, not a filesystem lock
or a hostile-code sandbox. Callers must revalidate inputs at their action boundary.
"""
from contextlib import contextmanager
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess

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
