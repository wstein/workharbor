"""Separate T003 orchestration persistence; no guard or external-write authority.

Hold open_journal for the entire command, including any future external operation.
Only local Linux/macOS filesystems supported by single_task_state are accepted.
SQLite works in bounded memory; committed snapshots are saved through retained
O_NOFOLLOW directory/file descriptors, fsync and atomic relative replacement.
No SQLite absolute-path open can follow a replaced directory into another tree.
Interrupted candidates are retained and never promoted to committed evidence.
An external action may start only AFTER its enclosing intent transaction returns.
Ownership ports are trusted host injections, never loaded from package data. A fresh
exclusive observation is mandatory: local locks and stored observations prove no
cross-host exclusion. Callers supply non-secret, bounded evidence only.
"""
from contextlib import closing, contextmanager
from dataclasses import asdict, dataclass
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import uuid

import single_task_state as durable
from single_task_state import InvalidState, PersistenceFailure, StateConflict


LIMIT = 1024 * 1024
MAX_DATABASE = 16 * LIMIT
SCHEMA = """
CREATE TABLE issue (repository TEXT NOT NULL, issue TEXT NOT NULL);
CREATE TABLE owner (identity BLOB NOT NULL);
CREATE TABLE records (key TEXT PRIMARY KEY, identity BLOB NOT NULL);
CREATE TABLE events (
    key TEXT NOT NULL REFERENCES records(key),
    sequence INTEGER NOT NULL CHECK(sequence > 0),
    kind TEXT NOT NULL,
    payload BLOB NOT NULL,
    digest TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (key, sequence)
);
PRAGMA user_version = 1;
"""
for _table in ('issue', 'owner', 'records', 'events'):
    for _action in ('update', 'delete'):
        SCHEMA += (f'CREATE TRIGGER immutable_{_table}_{_action} BEFORE {_action.upper()} '
                   f'ON {_table} BEGIN SELECT RAISE(ABORT, "append-only evidence"); END;\n')


def _text(value):
    if (not isinstance(value, str) or not value or len(value) > 1024
            or any(ord(character) < 32 or ord(character) == 127 for character in value)):
        raise InvalidState('Invalid journal identifier')
    try:
        value.encode('utf-8')
    except UnicodeError:
        raise InvalidState('Invalid journal identifier') from None
    return value


def _encode(value):
    encoded = durable._canonical(value)
    if len(encoded) > LIMIT:
        raise InvalidState('Journal evidence limit exceeded')
    return encoded


def _digest(value):
    return hashlib.sha256(value).hexdigest()


@dataclass(frozen=True)
class HandoffKey:
    repository: str
    issue: str
    package_digest: str
    guard_attempt_id: str

    def encoded(self):
        for value in asdict(self).values():
            _text(value)
        if not re.fullmatch('[0-9a-f]{64}', self.package_digest):
            raise InvalidState('Invalid package digest')
        return _encode(asdict(self))


@dataclass(frozen=True)
class Owner:
    checkout: str
    host: str
    process: str

    def encoded(self):
        for value in asdict(self).values():
            _text(value)
        return _encode(asdict(self))


@dataclass(frozen=True)
class OwnershipEvidence:
    repository: str
    issue: str
    owner: Owner
    state: str
    verifier: str
    reference: str


class UnavailableOwnership:
    def observe(self, repository, issue, owner):
        return None


def _schema(connection):
    return [tuple(row) for row in connection.execute(
        "SELECT type,name,tbl_name,sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' "
        'ORDER BY type,name')]


class Journal:
    def __init__(self, connection, directory, root, repository, issue):
        self._db = connection
        self._directory = directory
        self._depth = 0
        self.root = str(root)
        self.repository = repository
        self.issue = issue
        self._failed = False
        self._file_identity = self._identity('journal.sqlite3')

    def _identity(self, name):
        try:
            info = os.stat(name, dir_fd=self._directory, follow_symlinks=False)
        except FileNotFoundError:
            return None
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise InvalidState('Unsafe journal file retained')
        return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns

    def _available(self):
        if self._failed:
            raise PersistenceFailure('Persistence uncertain; close and reopen retained journal')

    def _location(self):
        path = Path(self.root) / '.symphony/task-handoff' / _digest(
            _encode([self.repository, self.issue]))
        current = path.lstat()
        pinned = os.fstat(self._directory)
        if (not stat.S_ISDIR(current.st_mode)
                or (current.st_dev, current.st_ino) != (pinned.st_dev, pinned.st_ino)
                or path.resolve() != path):
            raise InvalidState('Journal directory identity changed; retain evidence')

    def _persist(self):
        self._location()
        if self._identity('journal.sqlite3') != self._file_identity:
            raise InvalidState('Committed journal identity changed; retain evidence')
        snapshot = self._db.serialize()
        if len(snapshot) > MAX_DATABASE:
            raise PersistenceFailure('Journal growth limit reached; retain evidence')
        candidate = 'candidate-' + uuid.uuid4().hex + '.sqlite3'
        fd = os.open(candidate, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                     | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=self._directory)
        try:
            with os.fdopen(fd, 'wb', closefd=False) as stream:
                stream.write(snapshot)
                stream.flush()
                os.fsync(fd)
            info = os.fstat(fd)
            identity = info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns
            if self._identity(candidate) != identity:
                raise InvalidState('Checkpoint identity changed; retain evidence')
            os.fsync(self._directory)
            self._location()
            if self._identity('journal.sqlite3') != self._file_identity:
                raise InvalidState('Committed journal identity changed; retain evidence')
            os.replace(candidate, 'journal.sqlite3', src_dir_fd=self._directory,
                       dst_dir_fd=self._directory)
            os.fsync(self._directory)
            if self._identity('journal.sqlite3') != identity:
                raise InvalidState('Checkpoint replacement changed; retain evidence')
            self._location()
            self._file_identity = identity
        finally:
            os.close(fd)

    @contextmanager
    def transaction(self):
        self._available()
        depth = self._depth
        savepoint = 'handoff_' + str(depth)
        prior = self._db.serialize() if depth == 0 else None
        committed = False
        try:
            self._db.execute('BEGIN IMMEDIATE' if depth == 0 else 'SAVEPOINT ' + savepoint)
            self._depth += 1
            try:
                yield self
                self._db.execute('COMMIT' if depth == 0 else 'RELEASE ' + savepoint)
                if depth == 0:
                    committed = True
                    self._persist()
            except BaseException:
                if self._db.in_transaction:
                    if depth == 0:
                        self._db.execute('ROLLBACK')
                    else:
                        self._db.execute('ROLLBACK TO ' + savepoint)
                        self._db.execute('RELEASE ' + savepoint)
                if committed:
                    # The disk may contain old or new state. Restore memory and
                    # prohibit continued calls until a fresh open validates disk.
                    self._db.deserialize(prior)
                    self._failed = True
                raise
            finally:
                self._depth = depth
        except (sqlite3.Error, OSError) as error:
            raise PersistenceFailure('Journal transaction failed; retain evidence') from error

    def _key(self, key):
        self._available()
        if not isinstance(key, HandoffKey):
            raise InvalidState('Invalid handoff key')
        encoded = key.encoded()
        if (key.repository, key.issue) != (self.repository, self.issue):
            raise InvalidState('Handoff key belongs to another issue')
        return _digest(encoded), encoded

    def create_record(self, key):
        identity, encoded = self._key(key)
        with self.transaction():
            row = self._db.execute('SELECT identity FROM records WHERE key=?', (identity,)).fetchone()
            if row is not None:
                if row[0] != encoded:
                    raise StateConflict('Handoff identity conflict; retain original evidence')
                return
            self._db.execute('INSERT INTO records VALUES (?,?)', (identity, encoded))

    def events(self, key):
        identity, encoded = self._key(key)
        row = self._db.execute('SELECT identity FROM records WHERE key=?', (identity,)).fetchone()
        if row is None or row[0] != encoded:
            raise InvalidState('Handoff record missing; retain evidence for recovery')
        return [dict(sequence=row['sequence'], kind=row['kind'], payload=json.loads(row['payload']),
                     digest=row['digest'], created_at=row['created_at'])
                for row in self._db.execute('SELECT * FROM events WHERE key=? ORDER BY sequence',
                                            (identity,))]

    def append(self, key, kind, payload):
        if kind in ('intent', 'result'):
            raise InvalidState('Operation events require the intent/result API')
        return self._append(key, kind, payload)

    def _append(self, key, kind, payload):
        _text(kind)
        encoded = _encode(payload)
        identity, _ = self._key(key)
        with self.transaction():
            sequence = len(self.events(key)) + 1
            self._db.execute('INSERT INTO events VALUES (?,?,?,?,?,?)',
                             (identity, sequence, kind, encoded, _digest(encoded), durable._time()))
        return sequence

    def operation(self, key, operation_key):
        _text(operation_key)
        current = None
        for event in self.events(key):
            payload = event['payload']
            if event['kind'] not in ('intent', 'result') or payload['operation_key'] != operation_key:
                continue
            if event['kind'] == 'intent':
                current = dict(intent=payload['content'], status='intent', result=None,
                               sequence=event['sequence'])
            else:
                current.update(status=payload['status'], result=payload['content'])
        return current

    def intent(self, key, operation_key, content):
        _text(operation_key)
        _encode(content)
        with self.transaction():
            previous = self.operation(key, operation_key)
            if previous is not None:
                if _encode(previous['intent']) != _encode(content):
                    raise StateConflict('Changed operation intent; retain original evidence')
                return previous['sequence']
            return self._append(key, 'intent', {'operation_key': operation_key, 'content': content})

    def result(self, key, operation_key, status, content):
        _encode(content)
        if status not in ('succeeded', 'write-unknown', 'safe-retry', 'ambiguous'):
            raise InvalidState('Invalid operation result status')
        with self.transaction():
            previous = self.operation(key, operation_key)
            if previous is None:
                raise StateConflict('Durable operation intent required before result')
            if previous['status'] == status and _encode(previous['result']) == _encode(content):
                return
            if previous['status'] in ('succeeded', 'ambiguous'):
                raise StateConflict('Terminal operation result retained; explicit recovery required')
            self._append(key, 'result', dict(operation_key=operation_key, status=status, content=content))

    def owner(self):
        self._available()
        row = self._db.execute('SELECT identity FROM owner').fetchone()
        return Owner(**json.loads(row[0])) if row is not None else None

    def claim_owner(self, owner):
        if not isinstance(owner, Owner):
            raise InvalidState('Invalid local owner identity')
        encoded = owner.encoded()
        if owner.checkout != self.root:
            raise StateConflict('Owner checkout must match canonical journal checkout')
        with self.transaction():
            previous = self.owner()
            if previous is not None:
                if previous != owner:
                    raise StateConflict('Competing owner; retain evidence for reconciliation')
                return
            self._db.execute('INSERT INTO owner VALUES (?)', (encoded,))

    def require_ownership(self, owner, port=None):
        """Check freshly on every call; no cached observation grants eligibility."""
        if self.owner() != owner or not isinstance(owner, Owner):
            raise StateConflict('Local ownership absent or conflicting')
        try:
            evidence = (port or UnavailableOwnership()).observe(self.repository, self.issue, owner)
            valid = (isinstance(evidence, OwnershipEvidence) and evidence.state == 'exclusive'
                     and evidence.repository == self.repository and evidence.issue == self.issue
                     and evidence.owner == owner and _text(evidence.verifier)
                     and _text(evidence.reference))
        except Exception:
            valid = False
        if not valid:
            raise StateConflict('Trusted cross-host ownership unavailable or conflicting')
        return evidence

    def _validate(self):
        with closing(sqlite3.connect(':memory:')) as expected:
            expected.executescript(SCHEMA)
            if _schema(self._db) != _schema(expected):
                raise InvalidState('Unsupported journal schema; retain evidence')
        if (self._db.execute('PRAGMA user_version').fetchone()[0] != 1
                or self._db.execute('PRAGMA integrity_check').fetchall()[0][0] != 'ok'
                or self._db.execute('PRAGMA foreign_key_check').fetchone() is not None):
            raise InvalidState('Corrupt journal retained; operator recovery required')
        if [tuple(row) for row in self._db.execute('SELECT * FROM issue')] != [
                (self.repository, self.issue)]:
            raise InvalidState('Journal issue identity conflict')
        if self._db.execute('SELECT count(*) FROM owner').fetchone()[0] > 1:
            raise InvalidState('Conflicting owner evidence retained')
        owner = self.owner()
        if owner is not None:
            owner.encoded()
            if owner.checkout != self.root:
                raise InvalidState('Journal checkout changed; retain ownership evidence')
        for record in self._db.execute('SELECT * FROM records'):
            key = HandoffKey(**json.loads(record['identity']))
            identity, encoded = self._key(key)
            if record['key'] != identity or record['identity'] != encoded:
                raise InvalidState('Corrupt handoff identity retained')
            operations = {}
            for sequence, event in enumerate(self.events(key), 1):
                _text(event['kind'])
                if event['sequence'] != sequence or _digest(_encode(event['payload'])) != event['digest']:
                    raise InvalidState('Corrupt event evidence retained')
                payload = event['payload']
                if event['kind'] == 'intent':
                    if (not isinstance(payload, dict) or set(payload) != {'operation_key', 'content'}
                            or _text(payload['operation_key']) in operations):
                        raise InvalidState('Conflicting operation intent retained')
                    operations[payload['operation_key']] = 'intent'
                elif event['kind'] == 'result':
                    if (not isinstance(payload, dict)
                            or set(payload) != {'operation_key', 'status', 'content'}
                            or _text(payload['operation_key']) not in operations
                            or operations[payload['operation_key']] in ('succeeded', 'ambiguous')
                            or payload['status'] not in ('succeeded', 'write-unknown', 'safe-retry',
                                                         'ambiguous')):
                        raise InvalidState('Conflicting operation result retained')
                    operations[payload['operation_key']] = payload['status']


@contextmanager
def open_journal(root, repository, issue):
    """Open one separate ignored per-issue journal under a whole-command lock.

    Never repairs/deletes unknown evidence or accesses Feature 003's journal.
    A missing database in an existing issue directory requires operator recovery.
    """
    _text(repository)
    _text(issue)
    name = _digest(_encode([repository, issue]))
    descriptors = []
    connection = None
    try:
        root = Path(root)
        if (not root.is_absolute() or root != root.resolve()
                or root.is_symlink() or not root.is_dir()):
            raise InvalidState('Journal root must be an existing unsymlinked directory')
        root = root.resolve()
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        descriptors.append(root_fd)
        durable._local_filesystem(root_fd)
        parent = durable._directory(root_fd, '.symphony', create=True)
        descriptors.append(parent)
        parent = durable._directory(parent, 'task-handoff', create=True)
        descriptors.append(parent)
        new = name not in os.listdir(parent)
        directory = durable._directory(parent, name, create=True)
        descriptors.append(directory)
        for child in os.listdir(directory):
            if (child not in ('lock', 'journal.sqlite3')
                    and not re.fullmatch(r'candidate-[0-9a-f]{32}\.sqlite3', child)):
                raise InvalidState('Unknown journal evidence retained; operator recovery required')
            durable._regular(directory, child, required=True)
        durable._regular(directory, 'lock')
        lock = os.open('lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC,
                       0o600, dir_fd=directory)
        descriptors.append(lock)
        durable._regular(directory, 'lock', required=True)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            if error.errno in (errno.EACCES, errno.EAGAIN):
                raise StateConflict('Another command owns this issue journal lock') from error
            raise PersistenceFailure('Local process locking unavailable') from error
        os.fsync(lock)
        os.fsync(directory)
        existing = durable._regular(directory, 'journal.sqlite3')
        if not existing and not new:
            raise InvalidState('Journal missing; retain ownership evidence for recovery')
        connection = sqlite3.connect(':memory:', isolation_level=None)
        if not all(callable(getattr(connection, method, None))
                   for method in ('serialize', 'deserialize')):
            raise PersistenceFailure('Bounded SQLite snapshot APIs unavailable; Python 3.11+ required')
        connection.row_factory = sqlite3.Row
        loaded_identity = None
        if existing:
            fd = os.open('journal.sqlite3', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC,
                         dir_fd=directory)
            try:
                info = os.fstat(fd)
                if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                        or info.st_size > MAX_DATABASE):
                    raise InvalidState('Unsafe or oversized journal retained')
                with os.fdopen(fd, 'rb', closefd=False) as stream:
                    snapshot = stream.read(MAX_DATABASE + 1)
                if len(snapshot) > MAX_DATABASE:
                    raise InvalidState('Journal growth limit exceeded; retain evidence')
                loaded_identity = info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns
                after = os.fstat(fd)
                if loaded_identity != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns):
                    raise InvalidState('Journal changed during read; retain evidence')
                connection.deserialize(snapshot)
            finally:
                os.close(fd)
        else:
            connection.executescript(SCHEMA)
            connection.execute('INSERT INTO issue VALUES (?,?)', (repository, issue))
        connection.execute('PRAGMA foreign_keys=ON')
        page_size = connection.execute('PRAGMA page_size').fetchone()[0]
        connection.execute('PRAGMA max_page_count=' + str(MAX_DATABASE // page_size))
        journal = Journal(connection, directory, root, repository, issue)
        journal._validate()
        if existing and (journal._file_identity != loaded_identity
                         or journal._identity('journal.sqlite3') != loaded_identity):
            raise InvalidState('Journal file changed during validation; retain evidence')
        if not existing:
            journal._persist()
        yield journal
    except sqlite3.DatabaseError as error:
        raise InvalidState('Journal unavailable or corrupt; retain evidence for recovery') from error
    except (ValueError, TypeError, KeyError, UnicodeError) as error:
        raise InvalidState('Invalid journal evidence retained; operator recovery required') from error
    except OSError as error:
        raise PersistenceFailure('Journal durability unavailable; retain evidence') from error
    finally:
        if connection is not None:
            connection.close()
        for descriptor in reversed(descriptors):
            os.close(descriptor)
