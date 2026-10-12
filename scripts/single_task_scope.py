"""Read-only local entry scope, task identity and clean baseline validation."""
import hashlib
import os
import re
import shutil
import stat
import subprocess
from pathlib import Path

from single_task_context import ARTIFACTS, ContextError, canonical_path, validate_context


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
