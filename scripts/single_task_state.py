"""Durable local attempt primitives; no CLI, approval or completion authority.

The caller holds open_store across the entire command, including subprocesses.
State is supported only on local Linux/macOS filesystems with working POSIX locks,
SQLite synchronous journaling and file/directory fsync. Rejected evidence is retained.
"""
import ctypes
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import stat
import sys
from contextlib import closing, contextmanager
from datetime import datetime, timezone


class StateConflict(Exception):
    """Another command owns the lock, or an unfinished attempt already exists."""


class InvalidState(Exception):
    """Unsafe, missing, corrupt or unsupported evidence; operator recovery required."""


class PersistenceFailure(Exception):
    """Required locking, transaction or durability guarantees are unavailable."""


PROGRESS = ('active', 'acceptance-recorded', 'completion-prepared',
            'checkbox-applied', 'completed-awaiting-commit', 'handoff-ready')
SCHEMA = """
CREATE TABLE attempts (
    attempt_id TEXT PRIMARY KEY,
    context BLOB NOT NULL,
    context_copy_sha256 TEXT NOT NULL,
    approval_context_digest TEXT,
    baseline TEXT NOT NULL,
    tasks_bytes BLOB NOT NULL,
    entry_mode TEXT NOT NULL CHECK(entry_mode IN ('implement','owner-authorized rework')),
    predecessor TEXT REFERENCES attempts(attempt_id),
    created_at TEXT NOT NULL
);
CREATE TABLE progress (
    attempt_id TEXT PRIMARY KEY REFERENCES attempts(attempt_id),
    value TEXT NOT NULL CHECK(value IN ('active','acceptance-recorded',
      'completion-prepared','checkbox-applied','completed-awaiting-commit','handoff-ready'))
);
CREATE UNIQUE INDEX one_unfinished_attempt ON progress ((1)) WHERE value <> 'handoff-ready';
CREATE TABLE events (
    attempt_id TEXT NOT NULL REFERENCES attempts(attempt_id),
    sequence INTEGER NOT NULL CHECK(sequence > 0),
    kind TEXT NOT NULL,
    payload_digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (attempt_id, sequence)
);
CREATE TRIGGER immutable_attempt_update BEFORE UPDATE ON attempts
BEGIN SELECT RAISE(ABORT, 'immutable attempt'); END;
CREATE TRIGGER immutable_attempt_delete BEFORE DELETE ON attempts
BEGIN SELECT RAISE(ABORT, 'immutable attempt'); END;
CREATE TRIGGER immutable_event_update BEFORE UPDATE ON events
BEGIN SELECT RAISE(ABORT, 'append-only events'); END;
CREATE TRIGGER immutable_event_delete BEFORE DELETE ON events
BEGIN SELECT RAISE(ABORT, 'append-only events'); END;
PRAGMA user_version = 1;
"""


def _canonical(value):
    try:
        return json.dumps(value, sort_keys=True, separators=(',', ':'),
                          ensure_ascii=False, allow_nan=False).encode('utf-8')
    except (ValueError, TypeError, UnicodeError) as error:
        raise InvalidState('Evidence must be canonical UTF-8 JSON') from error


def _approval_identity(context):
    try:
        value = json.loads(_canonical(context))
        claimed = value['approval'].pop('context_digest')
        if claimed != hashlib.sha256(_canonical(value)).hexdigest():
            raise InvalidState('Approval context identity does not match retained context')
        return claimed
    except (KeyError, TypeError, ValueError):
        raise InvalidState('Invalid retained approval context identity') from None


def _time():
    return datetime.now(timezone.utc).isoformat()


def _regular(parent, name, required=False):
    try:
        info = os.stat(name, dir_fd=parent, follow_symlinks=False)
    except FileNotFoundError:
        if required:
            raise InvalidState('Required state evidence is missing; operator recovery required')
        return False
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
        raise InvalidState('State evidence must be a regular, unlinked file')
    return True


def _directory(parent, name, create=False):
    try:
        info = os.stat(name, dir_fd=parent, follow_symlinks=False)
    except FileNotFoundError:
        if not create:
            raise InvalidState('State directory is missing; operator recovery required')
        os.mkdir(name, mode=0o700, dir_fd=parent)
        os.fsync(parent)
    else:
        if not stat.S_ISDIR(info.st_mode):
            raise InvalidState('State path components must be unsymlinked directories')
    return os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC,
                   dir_fd=parent)


def _local_filesystem(fd):
    """Reject remote/unknown filesystems rather than approximate their semantics."""
    libc = ctypes.CDLL(None, use_errno=True)
    # Darwin includes two 1024-byte mount paths; Linux statfs is much smaller.
    buffer = ctypes.create_string_buffer(4096)
    libc.fstatfs.argtypes = (ctypes.c_int, ctypes.c_void_p)
    libc.fstatfs.restype = ctypes.c_int
    if libc.fstatfs(fd, ctypes.byref(buffer)) != 0:
        raise PersistenceFailure('Cannot establish local filesystem support')
    if sys.platform == 'darwin':
        # Darwin statfs: bsize/iosize, five uint64 counters, fsid, owner, type, flags.
        flags = ctypes.c_uint32.from_buffer(buffer, 64).value
        supported = bool(flags & 0x1000)  # MNT_LOCAL
    elif sys.platform.startswith('linux'):
        magic = ctypes.c_long.from_buffer(buffer).value & 0xffffffff
        supported = magic in {0xef53, 0x58465342, 0x9123683e, 0x01021994,
                              0x794c7630, 0x2fc12fc1, 0x858458f6}
        # ext*, XFS, btrfs, tmpfs, overlay, ZFS, ramfs (including disposable fixtures).
    else:
        supported = False
    if not supported:
        raise PersistenceFailure('Unsupported filesystem; use a local Linux/macOS worktree')


class _Store:
    def __init__(self, connection, directory, contexts):
        self._db = connection
        self._directory = directory
        self._contexts = contexts
        self._depth = 0

    @contextmanager
    def transaction(self):
        """Nest using savepoints; an append cannot commit its caller's transaction."""
        depth = self._depth
        savepoint = 'state_' + str(depth)
        try:
            self._db.execute('BEGIN IMMEDIATE' if depth == 0 else 'SAVEPOINT ' + savepoint)
            self._depth += 1
            try:
                yield self
                if depth == 0:
                    # Probe directory durability before SQLite's synchronous commit.
                    os.fsync(self._directory)
                    self._db.execute('COMMIT')
                else:
                    self._db.execute('RELEASE ' + savepoint)
            except BaseException:
                if self._db.in_transaction:
                    if depth == 0:
                        self._db.execute('ROLLBACK')
                    else:
                        self._db.execute('ROLLBACK TO ' + savepoint)
                        self._db.execute('RELEASE ' + savepoint)
                raise
            finally:
                self._depth = depth
        except (sqlite3.Error, OSError) as error:
            raise PersistenceFailure('State transaction failed; retain evidence for recovery') from error

    def _copy_name(self, attempt_id):
        if not isinstance(attempt_id, str) or not attempt_id:
            raise InvalidState('A nonempty attempt ID is required')
        try:
            return hashlib.sha256(attempt_id.encode('utf-8')).hexdigest() + '.json'
        except UnicodeError as error:
            raise InvalidState('Attempt ID must be UTF-8') from error

    def create_attempt(self, *, attempt_id, context, baseline, tasks_bytes,
                       entry_mode, predecessor=None, approval_context_digest=None):
        name = self._copy_name(attempt_id)
        if (not isinstance(context, dict) or not isinstance(baseline, str) or not baseline
                or not isinstance(tasks_bytes, bytes)
                or entry_mode not in ('implement', 'owner-authorized rework')):
            raise InvalidState('Invalid immutable attempt input')
        encoded = _canonical(context)
        digest = hashlib.sha256(encoded).hexdigest()
        if approval_context_digest is not None and _approval_identity(context) != approval_context_digest:
            raise InvalidState('Approval context identity does not match retained context')
        with self.transaction():
            if self._db.execute('SELECT 1 FROM attempts WHERE attempt_id=?', (attempt_id,)).fetchone():
                raise StateConflict('Attempt ID already exists; retain the original attempt')
            if self._db.execute("SELECT 1 FROM progress WHERE value <> 'handoff-ready'").fetchone():
                raise StateConflict('Only one unfinished attempt is permitted per worktree')
            if predecessor is not None and not self._db.execute(
                    'SELECT 1 FROM attempts WHERE attempt_id=?', (predecessor,)).fetchone():
                raise InvalidState('Predecessor evidence is missing')
            if _regular(self._contexts, name):
                raise InvalidState('Context copy already exists; operator recovery required')
            fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW
                         | os.O_CLOEXEC, 0o400, dir_fd=self._contexts)
            try:
                with os.fdopen(fd, 'wb', closefd=False) as stream:
                    stream.write(encoded)
                    stream.flush()
                    os.fsync(fd)
            finally:
                os.close(fd)
            os.fsync(self._contexts)
            self._db.execute('INSERT INTO attempts VALUES (?,?,?,?,?,?,?,?,?)',
                             (attempt_id, encoded, digest, approval_context_digest, baseline, tasks_bytes,
                              entry_mode, predecessor, _time()))
            self._db.execute('INSERT INTO progress VALUES (?,?)', (attempt_id, 'active'))

    def read_attempt(self, attempt_id):
        name = self._copy_name(attempt_id)
        try:
            row = self._db.execute('SELECT a.*, p.value AS progress FROM attempts a '
                                   'JOIN progress p USING(attempt_id) WHERE attempt_id=?',
                                   (attempt_id,)).fetchone()
            if row is None:
                raise InvalidState('Attempt evidence is missing; operator recovery required')
            _regular(self._contexts, name, required=True)
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                         dir_fd=self._contexts)
            with os.fdopen(fd, 'rb') as stream:
                copied = stream.read()
            if (copied != row['context'] or
                    hashlib.sha256(copied).hexdigest() != row['context_copy_sha256']):
                raise InvalidState('Immutable context copy does not match journal evidence')
            value = dict(row)
            value['context'] = json.loads(copied)
            if _canonical(value['context']) != copied:
                raise InvalidState('Context evidence is not canonical')
            if (value['approval_context_digest'] is not None and
                    _approval_identity(value['context']) != value['approval_context_digest']):
                raise InvalidState('Approval context identity does not match retained context')
            return value
        except (sqlite3.Error, ValueError, UnicodeError) as error:
            raise InvalidState('Unreadable attempt evidence; operator recovery required') from error
        except OSError as error:
            raise PersistenceFailure('Cannot read immutable context evidence') from error

    def active_attempt(self):
        row = self._db.execute("SELECT attempt_id FROM progress WHERE value <> 'handoff-ready'").fetchone()
        return None if row is None else self.read_attempt(row[0])

    def read_events(self, attempt_id):
        self.read_attempt(attempt_id)
        try:
            return [dict(row) for row in self._db.execute(
                'SELECT * FROM events WHERE attempt_id=? ORDER BY sequence', (attempt_id,))]
        except sqlite3.Error as error:
            raise InvalidState('Unreadable event evidence; operator recovery required') from error

    def append_event(self, attempt_id, kind, payload, progress=None):
        if not isinstance(kind, str) or not kind or (progress is not None and progress not in PROGRESS):
            raise InvalidState('Invalid event kind or progress')
        digest = hashlib.sha256(_canonical(payload)).hexdigest()
        with self.transaction():
            attempt = self.read_attempt(attempt_id)
            if progress is not None and PROGRESS.index(progress) < PROGRESS.index(attempt['progress']):
                raise InvalidState('Attempt progress cannot be reopened')
            sequence = self._db.execute(
                'SELECT COALESCE(MAX(sequence),0)+1 FROM events WHERE attempt_id=?',
                (attempt_id,)).fetchone()[0]
            self._db.execute('INSERT INTO events VALUES (?,?,?,?,?)',
                             (attempt_id, sequence, kind, digest, _time()))
            if progress is not None:
                self._db.execute('UPDATE progress SET value=? WHERE attempt_id=?',
                                 (progress, attempt_id))

    def _validate(self):
        if self._db.execute('PRAGMA quick_check').fetchall()[0][0] != 'ok':
            raise InvalidState('Corrupt journal; operator recovery required')
        if self._db.execute('PRAGMA foreign_key_check').fetchone():
            raise InvalidState('Inconsistent journal references; operator recovery required')
        # Version alone does not prove that immutable/append-only constraints exist.
        with closing(sqlite3.connect(':memory:')) as expected:
            expected.executescript(SCHEMA)
            query = "SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY name"
            if [tuple(row) for row in self._db.execute(query)] != expected.execute(query).fetchall():
                raise InvalidState('Unsupported journal schema; operator recovery required')
        if self._db.execute('SELECT COUNT(*) FROM attempts').fetchone()[0] != self._db.execute(
                'SELECT COUNT(*) FROM progress').fetchone()[0]:
            raise InvalidState('Missing attempt progress; operator recovery required')
        names = set()
        for row in self._db.execute('SELECT attempt_id FROM attempts'):
            attempt = self.read_attempt(row[0])
            if attempt['progress'] not in PROGRESS:
                raise InvalidState('Unsupported attempt progress')
            names.add(self._copy_name(row[0]))
            events = self.read_events(row[0])
            if [event['sequence'] for event in events] != list(range(1, len(events) + 1)):
                raise InvalidState('Inconsistent event sequence; operator recovery required')
        if set(os.listdir(self._contexts)) != names:
            raise InvalidState('Unbound context evidence; operator recovery required')


@contextmanager
def open_store(worktree_root, *, preflight=None, require_existing=False):
    """Own the command lock; preflight precedes creation, existing-only reads are RO.

    The callback is trusted caller code, never a context-supplied executable.
    Existing-only callers cannot create a journal or write through this store.
    """
    descriptors = []
    connection = None
    try:
        if sys.platform != 'darwin' and not sys.platform.startswith('linux'):
            raise PersistenceFailure('POSIX Linux/macOS persistence is required')
        root = Path(worktree_root)
        if root.is_symlink() or not root.is_dir():
            raise InvalidState('Worktree root must be an existing unsymlinked directory')
        root = root.resolve()
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        descriptors.append(root_fd)
        _local_filesystem(root_fd)
        symphony = _directory(root_fd, '.symphony', create=True)
        descriptors.append(symphony)
        if os.path.lexists(root/'.symphony/single-task.json'):
            raise InvalidState('Legacy evidence retained; explicit operator migration required')
        directory = _directory(symphony, 'single-task', create=True)
        descriptors.append(directory)
        # Inspect all existing children before creating lock/database sidecars.
        for name in os.listdir(directory):
            if name == 'contexts':
                info = os.stat(name, dir_fd=directory, follow_symlinks=False)
                if not stat.S_ISDIR(info.st_mode):
                    raise InvalidState('Unsafe context directory')
            elif name in ('lock', 'journal.sqlite3', 'journal.sqlite3-journal'):
                _regular(directory, name, required=True)
            else:
                raise InvalidState('Unsupported state evidence retained; operator recovery required')
        _regular(directory, 'lock')
        lock = os.open('lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC,
                       0o600, dir_fd=directory)
        descriptors.append(lock)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            if error.errno in (errno.EACCES, errno.EAGAIN):
                raise StateConflict('Another command owns this worktree lock') from error
            raise PersistenceFailure('Nonblocking POSIX locking unavailable') from error
        os.fsync(lock)
        os.fsync(directory)
        existing = _regular(directory, 'journal.sqlite3')
        if require_existing and not existing:
            raise InvalidState('Attempt journal missing; operator recovery required')
        if not existing and os.path.lexists(root/'.symphony/single-task/contexts'):
            raise InvalidState('Journal missing from existing state; operator recovery required')
        if preflight is not None:
            preflight()
        contexts = _directory(directory, 'contexts', create=not existing)
        descriptors.append(contexts)
        for name in os.listdir(contexts):
            _regular(contexts, name, required=True)
        journal = root/'.symphony/single-task/journal.sqlite3'
        if existing:
            # Read-only inspection rejects corrupt/versioned input without rewriting it.
            with closing(sqlite3.connect(journal.as_uri() + '?mode=ro', uri=True)) as probe:
                probe.row_factory = sqlite3.Row
                if probe.execute('PRAGMA user_version').fetchone()[0] != 1:
                    raise InvalidState('Unsupported journal version; operator recovery required')
                try:
                    _Store(probe, directory, contexts)._validate()
                except sqlite3.Error as error:
                    raise InvalidState('Invalid journal schema; operator recovery required') from error
                if require_existing:
                    yield _Store(probe, directory, contexts)
                    return
        connection = sqlite3.connect(journal, isolation_level=None, timeout=0)
        connection.row_factory = sqlite3.Row
        connection.execute('PRAGMA foreign_keys = ON')
        mode = connection.execute('PRAGMA journal_mode = DELETE').fetchone()[0]
        connection.execute('PRAGMA synchronous = EXTRA')
        if mode != 'delete' or connection.execute('PRAGMA synchronous').fetchone()[0] != 3:
            raise PersistenceFailure('Required synchronous SQLite journaling unavailable')
        if not existing:
            connection.executescript('BEGIN IMMEDIATE;\n' + SCHEMA + '\nCOMMIT;')
            fd = os.open('journal.sqlite3', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                         dir_fd=directory)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
            os.fsync(directory)
        store = _Store(connection, directory, contexts)
        try:
            store._validate()
        except sqlite3.Error as error:
            raise InvalidState('Invalid journal schema; operator recovery required') from error
        yield store
    except sqlite3.Error as error:
        if isinstance(error, sqlite3.DatabaseError) and not isinstance(error, sqlite3.OperationalError):
            raise InvalidState('Corrupt journal retained; operator recovery required') from error
        raise PersistenceFailure('SQLite persistence unavailable; retain evidence') from error
    except OSError as error:
        raise PersistenceFailure('State durability unavailable; retain evidence') from error
    finally:
        if connection is not None:
            connection.close()
        for fd in reversed(descriptors):
            os.close(fd)
