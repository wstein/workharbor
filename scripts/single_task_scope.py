"""Read-only entry and execution scope inventories for Feature 003."""
import hashlib
import os
import re
import shutil
import stat
import subprocess
from pathlib import Path

from single_task_context import ARTIFACTS, ContextError, canonical_path, validate_context


class ScopeError(ContextError):
    """Safe inventory blocker with only already-validated relative paths."""
    def __init__(self, paths=()):
        super().__init__('scope_blocker')
        self.affected_paths = list(paths)


def reject(code='scope_context'):
    raise ContextError(code)


def git(root, *args, allowed=(0,)):
    """Local plumbing with no caller credentials, hooks, transports or lazy fetch."""
    executable = shutil.which('git', path=os.defpath)
    if executable is None:
        reject()
    env = {'PATH': os.defpath, 'LC_ALL': 'C', 'GIT_CONFIG_NOSYSTEM': '1',
           'GIT_CONFIG_SYSTEM': os.devnull, 'GIT_CONFIG_GLOBAL': os.devnull,
           'GIT_ATTR_NOSYSTEM': '1', 'GIT_TERMINAL_PROMPT': '0',
           'GIT_ALLOW_PROTOCOL': '', 'GIT_NO_LAZY_FETCH': '1',
           'GIT_NO_REPLACE_OBJECTS': '1', 'GIT_OPTIONAL_LOCKS': '0'}
    try:
        result = subprocess.run([executable, '-c', 'core.fsmonitor=false',
                                 '-c', f'core.hooksPath={os.devnull}',
                                 '-c', 'credential.helper=', '-c', 'core.quotePath=false',
                                 *args], cwd=root, env=env, capture_output=True, timeout=15)
    except (OSError, subprocess.TimeoutExpired):
        reject()
    if result.returncode not in allowed:
        reject()
    return result


def _path_fd(root, relative, *, directory=False, absent=False):
    """Open each canonical component without following links or path aliases."""
    canonical_path(relative, dot=directory)
    descriptors = []
    try:
        current = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        descriptors.append(current)
        parts = [] if relative == '.' else relative.split('/')
        for index, part in enumerate(parts):
            names = os.listdir(current)
            if part not in names:
                # Missing paths are allowed only for future exact output files.
                # A filesystem alias must never be mistaken for absence.
                try:
                    os.stat(part, dir_fd=current, follow_symlinks=False)
                except FileNotFoundError:
                    if absent:
                        return None
                    raise
                reject()
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK
            if index < len(parts) - 1 or directory:
                flags |= os.O_DIRECTORY
            current = os.open(part, flags, dir_fd=current)
            descriptors.append(current)
        mode = os.fstat(current).st_mode
        if not (stat.S_ISDIR(mode) if directory else stat.S_ISREG(mode)):
            reject()
        return os.dup(current)
    except (OSError, ValueError):
        reject()
    finally:
        for descriptor in reversed(descriptors):
            os.close(descriptor)


def read_regular(root, relative):
    descriptor = _path_fd(root, relative)
    with os.fdopen(descriptor, 'rb') as stream:
        return stream.read()


def task_lines(data):
    """Return real task markers/byte offsets, excluding Markdown fenced examples."""
    result = {}
    fence = None
    offset = 0
    try:
        data.decode('utf-8')
    except UnicodeError:
        reject()
    for line in data.splitlines(keepends=True):
        marker = re.match(rb' {0,3}(`{3,}|~{3,})([^\r\n]*)', line)
        if fence:
            if marker and marker[1][:1] == fence[0] and len(marker[1]) >= fence[1] and not marker[2].strip():
                fence = None
        elif marker and not (marker[1][:1] == b'`' and b'`' in marker[2]):
            fence = (marker[1][:1], len(marker[1]))
        else:
            task = re.match(rb' {0,3}- \[([ xX])\] (T[0-9]{3})(?=[ \t\r\n]|$)', line)
            if task:
                identity = task[2].decode('ascii')
                if identity in result:
                    reject()
                result[identity] = {'checked': task[1] != b' ',
                                    'marker_offset': offset + task.start(1)}
        offset += len(line)
    return result


def _clean_baseline(root, context=None):
    # Configured filters/includes can run arbitrary programs or read external
    # configuration during inventory. Reject before any content-aware command.
    config = git(root, 'config', '--local', '--no-includes', '--name-only', '--list').stdout
    if any(name.lower().startswith((b'filter.', b'include.', b'includeif.'))
           for name in config.splitlines()):
        reject()
    head = git(root, 'rev-parse', '--verify', 'HEAD^{commit}').stdout.strip().decode('ascii')
    index = {}
    for record in git(root, 'ls-files', '--stage', '-z').stdout.split(b'\0'):
        if not record:
            continue
        metadata, path = record.split(b'\t', 1)
        mode, identity, stage = metadata.split()
        if stage != b'0' or mode not in (b'100644', b'100755', b'120000', b'160000'):
            reject('dirty_baseline')
        index[path] = (mode, identity)
    tree = {}
    for record in git(root, 'ls-tree', '-r', '-z', head).stdout.split(b'\0'):
        if record:
            metadata, path = record.split(b'\t', 1)
            mode, kind, identity = metadata.split()
            tree[path] = (mode, identity)
    if index != tree or git(root, 'ls-files', '--others', '--exclude-standard', '-z').stdout:
        reject('dirty_baseline')
    algorithm = git(root, 'rev-parse', '--show-object-format').stdout.strip().decode('ascii')
    if algorithm not in ('sha1', 'sha256'):
        reject()
    if context:
        scoped = context['allowed_paths'] + [check['cwd'] for check in context['checks']]
        scoped += [record['path'] for record in context['reviewed_artifacts'].values()]
        for path, (mode, _) in index.items():
            if mode == b'160000':
                try:
                    prefix = path.decode('utf-8').casefold()
                except UnicodeError:
                    reject()
                if any(item.casefold() == prefix or item.casefold().startswith(prefix + '/') for item in scoped):
                    reject()
    for path, (mode, identity) in index.items():
        try:
            relative = path.decode('utf-8')
            if mode == b'160000':
                descriptor = _path_fd(root, relative, directory=True, absent=True)
                if descriptor is None:
                    continue  # Uninitialized, absent baseline submodule.
                try:
                    names = os.listdir(descriptor)
                finally:
                    os.close(descriptor)
                if not names:
                    continue  # Git also permits an empty uninitialized directory.
                submodule = root / relative
                if '.git' not in names or (submodule / '.git').is_symlink():
                    reject('dirty_baseline')
                top = git(submodule, 'rev-parse', '--show-toplevel').stdout.strip()
                if Path(os.fsdecode(top)).resolve() != submodule.resolve():
                    reject('dirty_baseline')
                if _clean_baseline(submodule).encode('ascii') != identity:
                    reject('dirty_baseline')
                continue
            if mode == b'120000':
                # Unrelated baseline symlinks are inventoried as link bytes;
                # approved output/artifact paths are checked separately.
                parent = str(Path(relative).parent)
                parent_fd = _path_fd(root, parent, directory=True)
                try:
                    link = os.stat(Path(relative).name, dir_fd=parent_fd, follow_symlinks=False)
                    if not stat.S_ISLNK(link.st_mode):
                        reject('dirty_baseline')
                    content = os.fsencode(os.readlink(Path(relative).name, dir_fd=parent_fd))
                finally:
                    os.close(parent_fd)
            else:
                descriptor = _path_fd(root, relative)
                with os.fdopen(descriptor, 'rb') as stream:
                    actual_mode = b'100755' if os.fstat(stream.fileno()).st_mode & 0o111 else b'100644'
                    content = stream.read()
                if actual_mode != mode:
                    reject('dirty_baseline')
            actual = hashlib.new(algorithm, b'blob ' + str(len(content)).encode() + b'\0' + content).hexdigest().encode()
            if actual != identity:
                reject('dirty_baseline')
        except (OSError, UnicodeError, ContextError):
            reject('dirty_baseline')
    return head


def validate_entry_scope(root, value):
    """Resolve entry identities and return an immutable clean baseline view."""
    try:
        context = validate_context(value)
        root = Path(root)
        if root.is_symlink() or not root.is_dir():
            reject()
        # Verify the supplied root is the worktree root, never a nested cwd.
        if Path(os.fsdecode(git(root, 'rev-parse', '--show-toplevel').stdout.strip())).resolve() != root.resolve():
            reject()
        feature = context['feature']
        for name in ARTIFACTS:
            read_regular(root, f'specs/{feature}/{name}')
        protected = {f'specs/{feature}/{name}'.casefold() for name in ARTIFACTS}
        for path in context['allowed_paths']:
            if any(part.casefold() in ('.git', '.symphony') for part in path.split('/')) or path.casefold() in protected:
                reject()
            descriptor = _path_fd(root, path, absent=True)
            if descriptor is not None:
                os.close(descriptor)
        for check in context['checks']:
            descriptor = _path_fd(root, check['cwd'], directory=True)
            os.close(descriptor)
        tasks_path = f'specs/{feature}/tasks.md'
        tasks_bytes = read_regular(root, tasks_path)
        tasks = task_lines(tasks_bytes)
        selected = tasks.get(context['task_id'])
        if selected is None or selected['checked'] != (context['mode'] == 'owner-authorized rework'):
            reject()
    except (ContextError, OSError, ValueError):
        reject()
    baseline = _clean_baseline(root, context)
    return {'baseline_commit': baseline, 'tasks_path': tasks_path,
            'tasks_bytes': tasks_bytes, 'tasks': tasks}


def validate_manifest(value):
    """Validate the exact typed, ordered ContentManifest schema."""
    import copy
    if (type(value) is not dict or set(value) != {'schema_version', 'entries'} or
            type(value['schema_version']) is not int or value['schema_version'] != 1 or
            type(value['entries']) is not list or not value['entries']):
        reject()
    paths = []
    for entry in value['entries']:
        if type(entry) is not dict or set(entry) != {'path', 'type', 'mode', 'sha256'}:
            reject()
        canonical_path(entry['path'])
        try:
            entry['path'].encode('utf-8')
        except UnicodeError:
            reject()
        paths.append(entry['path'])
        if entry['type'] == 'absent':
            if entry['mode'] is not None or entry['sha256'] is not None:
                reject()
        elif entry['type'] == 'regular':
            if (entry['mode'] not in ('100644', '100755') or
                    type(entry['sha256']) is not str or
                    re.fullmatch('[0-9a-f]{64}', entry['sha256']) is None):
                reject()
        else:
            reject()
    if paths != sorted(set(paths)):
        reject()
    return copy.deepcopy(value)


def _normalized_selected(data, task_id):
    selected = task_lines(data).get(task_id)
    if selected is None:
        reject()
    offset = selected['marker_offset']
    return data[:offset] + b' ' + data[offset + 1:]


def _inventory_config(root):
    names = git(root, 'config', '--local', '--no-includes', '--name-only', '--list').stdout
    if any(name.lower().startswith((b'filter.', b'include.', b'includeif.'))
           for name in names.splitlines()):
        reject()


def _git_inventory(root, revision=None):
    """Read raw stage/tree modes and object IDs without text conversion."""
    result = {}
    records = (git(root, 'ls-files', '--stage', '-z').stdout if revision is None else
               git(root, 'ls-tree', '-r', '-z', revision).stdout)
    for record in records.split(b'\0'):
        if not record:
            continue
        metadata, path = record.split(b'\t', 1)
        mode, middle, last = metadata.split()
        identity = middle if revision is None else last
        if revision is None and last != b'0':
            reject()
        relative = path.decode('utf-8')
        canonical_path(relative)
        if mode not in (b'100644', b'100755', b'120000', b'160000') or relative in result:
            reject()
        result[relative] = (mode.decode('ascii'), identity.decode('ascii'))
    return result


def _blob(root, identity):
    return git(root, 'cat-file', 'blob', identity).stdout


def _physical(root, relative, algorithm):
    """Return mode/object ID/raw bytes, without following any link component."""
    parent, name = relative.rsplit('/', 1) if '/' in relative else ('.', relative)
    # Inspect a final link as link bytes; directory traversal remains no-follow.
    descriptor = _path_fd(root, parent, directory=True, absent=True)
    if descriptor is None:
        return None
    try:
        if name not in os.listdir(descriptor):
            try:
                os.stat(name, dir_fd=descriptor, follow_symlinks=False)
            except FileNotFoundError:
                return None
            reject()  # A case/Unicode alias is not a canonical absent path.
        info = os.stat(name, dir_fd=descriptor, follow_symlinks=False)
        if stat.S_ISLNK(info.st_mode):
            mode, content = '120000', os.fsencode(os.readlink(name, dir_fd=descriptor))
        elif stat.S_ISREG(info.st_mode):
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=descriptor)
            with os.fdopen(fd, 'rb') as stream:
                opened = os.fstat(stream.fileno())
                if not stat.S_ISREG(opened.st_mode):
                    reject()
                mode = '100755' if opened.st_mode & 0o111 else '100644'
                content = stream.read()
        else:
            reject()
    finally:
        os.close(descriptor)
    identity = hashlib.new(algorithm, b'blob ' + str(len(content)).encode() + b'\0' + content).hexdigest()
    return mode, identity, content


def validate_scope(root, attempt):
    """Validate every repository plane and return the current approved manifest."""
    try:
        root = Path(root)
        context = validate_context(attempt['context'])
        if root.is_symlink() or not root.is_dir():
            reject()
        if Path(os.fsdecode(git(root, 'rev-parse', '--show-toplevel').stdout.strip())).resolve() != root.resolve():
            reject()
        _inventory_config(root)
        baseline = attempt['baseline']
        if type(baseline) is not str or re.fullmatch('[0-9a-f]{40}|[0-9a-f]{64}', baseline) is None:
            reject()
        head = git(root, 'rev-parse', '--verify', 'HEAD^{commit}').stdout.strip().decode('ascii')
        git(root, 'merge-base', '--is-ancestor', baseline, head)
        allowed = set(context['allowed_paths'])
        task_path = f"specs/{context['feature']}/tasks.md"
        task_id = context['task_id']
        entry_tasks = attempt['tasks_bytes']
        normalized_tasks = _normalized_selected(entry_tasks, task_id)
        original = _git_inventory(root, baseline)
        if task_path not in original or _blob(root, original[task_path][1]) != entry_tasks:
            reject()
        algorithm = git(root, 'rev-parse', '--show-object-format').stdout.strip().decode('ascii')
        if algorithm not in ('sha1', 'sha256'):
            reject()

        def validate_plane(inventory, reader):
            for path in original.keys() | inventory.keys() | allowed | {task_path}:
                before, current = original.get(path), inventory.get(path)
                if path in allowed:
                    if current is not None and current[0] not in ('100644', '100755'):
                        reject()
                elif path == task_path:
                    if (current is None or current[0] not in ('100644', '100755') or
                            current[0] != before[0] or
                            _normalized_selected(reader(path, current), task_id) != normalized_tasks):
                        reject()
                elif before != current:
                    raise ScopeError([path])

        for revision in [head] + git(root, 'rev-list', head, '^' + baseline).stdout.decode('ascii').splitlines():
            validate_plane(_git_inventory(root, revision), lambda path, entry: _blob(root, entry[1]))
        validate_plane(_git_inventory(root), lambda path, entry: _blob(root, entry[1]))

        paths = original.keys() | allowed | {task_path}
        untracked = git(root, 'ls-files', '--others', '--exclude-standard', '-z').stdout
        paths |= {path.decode('utf-8') for path in untracked.split(b'\0') if path}
        physical, contents = {}, {}
        for path in paths:
            canonical_path(path)
            if original.get(path, (None,))[0] == '160000':
                # Preserve safe unchanged baseline gitlinks, never enter outputs.
                if path in allowed:
                    reject()
                fd = _path_fd(root, path, directory=True, absent=True)
                if fd is not None:
                    try:
                        names = os.listdir(fd)
                    finally:
                        os.close(fd)
                    if names:
                        submodule = root/path
                        if '.git' not in names or (submodule/'.git').is_symlink():
                            reject()
                        top = git(submodule, 'rev-parse', '--show-toplevel').stdout.strip()
                        if (Path(os.fsdecode(top)).resolve() != submodule.resolve() or
                                _clean_baseline(submodule) != original[path][1]):
                            reject()
                physical[path] = original[path]
                continue
            entry = _physical(root, path, algorithm)
            if entry is not None:
                physical[path] = entry[:2]
                contents[path] = entry[2]
        validate_plane(physical, lambda path, entry: contents[path])
        entries = []
        for path in sorted(allowed | {task_path}):
            if path not in physical:
                entries.append({'path': path, 'type': 'absent', 'mode': None, 'sha256': None})
            else:
                content = normalized_tasks if path == task_path else contents[path]
                entries.append({'path': path, 'type': 'regular', 'mode': physical[path][0],
                                'sha256': hashlib.sha256(content).hexdigest()})
        return validate_manifest({'schema_version': 1, 'entries': entries})
    except ScopeError:
        raise
    except (OSError, ValueError, UnicodeError, KeyError, TypeError):
        raise ScopeError() from None


def normalize_tasks_bytes(data, task_id):
    """Normalize only the selected real checkbox byte; preserve every other byte."""
    if (type(data) is not bytes or type(task_id) is not str or
            re.fullmatch('T[0-9]{3}', task_id) is None):
        reject('scope_blocker')
    return _normalized_selected(data, task_id)


def canonical_manifest_bytes(value):
    """Serialize an exact manifest as canonical UTF-8, without normalization."""
    from single_task_context import canonical_bytes
    return canonical_bytes(validate_manifest(value))


def validate_semantic_review(value, attempt, manifest):
    """Bind a strict positive review to this persisted attempt and current bytes."""
    import copy
    fields = {'schema_version', 'attempt_id', 'task_id', 'context_digest',
              'content_manifest_digest', 'reviewer_ref', 'evidence_ref', 'approved'}
    if (type(value) is not dict or set(value) != fields or
            type(value['schema_version']) is not int or value['schema_version'] != 1 or
            value['approved'] is not True):
        reject('scope_blocker')
    for field in fields - {'schema_version', 'approved'}:
        if type(value[field]) is not str or not value[field] or '\0' in value[field]:
            reject('scope_blocker')
    context = validate_context(attempt['context'])
    identity = hashlib.sha256(canonical_manifest_bytes(manifest)).hexdigest()
    if (value['attempt_id'] != attempt['attempt_id'] or
            value['task_id'] != context['task_id'] or
            value['context_digest'] != context['approval']['context_digest'] or
            value['content_manifest_digest'] != identity):
        reject('scope_blocker')
    # Ensure references themselves are representable by the versioned UTF-8 format.
    from single_task_context import canonical_bytes
    canonical_bytes(value)
    return copy.deepcopy(value)


def prepare_inspection(root, attempt):
    """Return current identity without running checks or publishing any state."""
    manifest = validate_scope(root, attempt)
    return {'attempt_id': attempt['attempt_id'], 'task_id': attempt['context']['task_id'],
            'context_digest': attempt['context']['approval']['context_digest'],
            'content_manifest': manifest,
            'content_manifest_digest': hashlib.sha256(canonical_manifest_bytes(manifest)).hexdigest()}
