"""Content-bound approval comparison; unavailable guard/provider ports fail closed.

This library grants no execution, review or publication permission. Adapters are
trusted host code, never selected by request data. See approval-binding.md.
"""
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
from typing import Protocol

from single_task import path_in_repo
from single_task_state import _directory

HEX = re.compile(r'[0-9a-f]{64}')
REVISION = re.compile(r'[0-9a-f]{40}')
REQUEST_KEYS = {'schema_version', 'approved_revision', 'current_revision',
                'approved_context', 'current_context', 'evidence', 'approval_ref',
                'approval_action', 'approved_package_digest'}


class BindingBlocked(ValueError):
    """Safe identifier only: never retain raw adapter or subprocess diagnostics."""


@dataclass(frozen=True)
class ValidatedContract:
    contract_digest: str
    protected_paths: tuple[str, ...]


@dataclass(frozen=True)
class Artifact:
    path: str
    mode: str
    data: bytes

    def identity(self):
        return dict(path=self.path, mode=self.mode,
                    sha256=hashlib.sha256(self.data).hexdigest())


@dataclass(frozen=True)
class Snapshot:
    revision: str
    tree: str
    artifacts: tuple[Artifact, ...]

    def identity(self):
        return dict(revision=self.revision, tree=self.tree,
                    artifacts=[item.identity() for item in self.artifacts])


@dataclass(frozen=True)
class ApprovalTarget:
    action: str
    reviewed_revision: str
    reviewed_package_digest: str
    binding_digest: str


@dataclass(frozen=True)
class ArtifactComparison:
    result: str
    evidence_ref: str


@dataclass(frozen=True)
class ApprovalVerification:
    approved: bool
    assurance: str
    target: ApprovalTarget
    evidence_ref: str


class GuardPort(Protocol):
    revision: str

    def validate_context(self, context, evidence) -> ValidatedContract: ...

    def compare_artifacts(self, approved: Snapshot, current: Snapshot,
                          evidence) -> ArtifactComparison: ...


class ApprovalPort(Protocol):
    def verify(self, approval_ref: str,
               target: ApprovalTarget) -> ApprovalVerification: ...


class Unavailable003:
    revision = None

    def validate_context(self, context, evidence):
        raise BindingBlocked('feature003-context-comparator-unavailable')

    def compare_artifacts(self, approved, current, evidence):
        raise BindingBlocked('feature003-context-comparator-unavailable')


class UnavailableAuthority:
    def verify(self, approval_ref, target):
        raise BindingBlocked('approval-authority-unavailable')


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'),
                      ensure_ascii=False, allow_nan=False).encode('utf-8')


def binding_digest(contract, snapshot):
    """Only validated semantic contract and protected content enter this digest."""
    return hashlib.sha256(_canonical(dict(
        schema_version=1, contract_digest=contract.contract_digest,
        artifacts=[item.identity() for item in snapshot.artifacts]))).hexdigest()


def _git(root, *args):
    environment = {key: value for key, value in os.environ.items()
                   if not key.startswith('GIT_')}
    environment.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull,
                       GIT_TERMINAL_PROMPT='0', GIT_ALLOW_PROTOCOL='', GIT_OPTIONAL_LOCKS='0',
                       GIT_NO_REPLACE_OBJECTS='1')
    command = ['git', '--no-replace-objects', '-c', 'core.fsmonitor=false']
    for probe in (['rev-parse', '--path-format=absolute', '--git-path', 'info/grafts'],
                  ['for-each-ref', '--format=%(refname)', 'refs/replace/']):
        result = subprocess.run(command + probe, cwd=root, capture_output=True,
                                env=environment, timeout=10)
        if result.returncode:
            raise BindingBlocked('source-object-unavailable')
        if probe[0] == 'rev-parse':
            graft = Path(os.fsdecode(result.stdout.strip()))
            if graft.exists() or graft.is_symlink():
                raise BindingBlocked('git-object-substitution')
        elif result.stdout:
            raise BindingBlocked('git-object-substitution')
    run = subprocess.run(command + list(args),
                         cwd=root, capture_output=True, env=environment, timeout=10)
    if run.returncode:
        raise BindingBlocked('source-object-unavailable')
    return run.stdout


def _contract(root, value):
    if (not isinstance(value, ValidatedContract)
            or not isinstance(value.contract_digest, str)
            or not HEX.fullmatch(value.contract_digest)
            or not isinstance(value.protected_paths, tuple)
            or not 1 <= len(value.protected_paths) <= 1000
            or any(not isinstance(path, str) for path in value.protected_paths)
            or tuple(sorted(set(value.protected_paths))) != value.protected_paths):
        raise BindingBlocked('invalid-guard-contract')
    for path in value.protected_paths:
        try:
            path_in_repo(root, path)
        except ValueError:
            raise BindingBlocked('invalid-protected-path') from None
    return value


def _snapshot(root, revision, paths):
    if not isinstance(revision, str) or not REVISION.fullmatch(revision):
        raise BindingBlocked('invalid-source-revision')
    commit = _git(root, 'rev-parse', '--verify', revision + '^{commit}').decode().strip()
    if commit != revision:
        raise BindingBlocked('invalid-source-revision')
    tree = _git(root, 'rev-parse', revision + '^{tree}').decode().strip()
    artifacts = []
    for path in paths:
        entries = _git(root, 'ls-tree', '-z', revision, '--', path).split(b'\0')
        entries = [entry for entry in entries if entry]
        if len(entries) != 1:
            raise BindingBlocked('protected-artifact-missing')
        header, name = entries[0].split(b'\t', 1)
        mode, kind, object_id = header.decode().split(' ')
        if name.decode('utf-8') != path or mode not in ('100644', '100755') or kind != 'blob':
            raise BindingBlocked('unsafe-protected-artifact')
        if int(_git(root, 'cat-file', '-s', object_id)) > 1024 * 1024:
            raise BindingBlocked('protected-artifact-limit-exceeded')
        data = _git(root, 'cat-file', 'blob', object_id)
        artifacts.append(Artifact(path, mode, data))
    return Snapshot(revision, tree, tuple(artifacts))


def _current(root, revision, paths):
    if _git(root, 'rev-parse', 'HEAD').decode().strip() != revision:
        raise BindingBlocked('current-head-mismatch')
    _inert_status_inputs(root, revision)
    if _git(root, 'status', '--porcelain=v1', '-z', '--untracked-files=all',
            '--ignore-submodules=all'):
        raise BindingBlocked('dirty-current-source')
    snapshot = _snapshot(root, revision, paths)
    for item in snapshot.artifacts:
        metadata, data = _read_regular(root, item.path, len(item.data))
        mode = '100755' if metadata.st_mode & stat.S_IXUSR else '100644'
        if mode != item.mode or data != item.data:
            raise BindingBlocked('worktree-artifact-drift')
    return snapshot


def _inert_status_inputs(root, revision):
    """Refuse executable conversions and submodules before inspecting status.

    Config/object/index reads do not convert worktree content. Includes and
    worktree config participate in this effective-config query. Global/system
    settings and inherited Git overrides are already excluded by _git.
    """
    configuration = _git(root, 'config', '--null', '--list', '--includes')
    for record in configuration.split(b'\0'):
        key = record.split(b'\n', 1)[0].lower()
        if key.startswith(b'filter.') and key.endswith((b'.clean', b'.smudge', b'.process')):
            raise BindingBlocked('executable-git-filter-unsupported')
    tree = _git(root, 'ls-tree', '-r', '-z', revision)
    index = _git(root, 'ls-files', '--stage', '-z')
    if any(record.startswith(b'160000 ') for source in (tree, index)
           for record in source.split(b'\0')):
        raise BindingBlocked('submodule-source-unsupported')


def _read_regular(root, path, expected_size):
    """Reuse guard directory traversal; never follow a swapped component/file."""
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        parts = Path(path).parts
        for part in parts[:-1]:
            child = _directory(directory, part)
            os.close(directory)
            directory = child
        descriptor = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC
                             | os.O_NONBLOCK, dir_fd=directory)
        with os.fdopen(descriptor, 'rb') as stream:
            before = os.fstat(stream.fileno())
            if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                    or before.st_size != expected_size):
                raise BindingBlocked('worktree-artifact-drift')
            data = stream.read(expected_size + 1)
            after = os.fstat(stream.fileno())
            stable = ('st_dev', 'st_ino', 'st_mode', 'st_nlink', 'st_size',
                      'st_mtime_ns', 'st_ctime_ns')
            if any(getattr(before, key) != getattr(after, key) for key in stable):
                raise BindingBlocked('worktree-artifact-drift')
            return after, data
    finally:
        os.close(directory)


def assess_retention(root, request, *, guard=None, authority=None):
    """Return inert approval comparison evidence, including fail-closed blockers.

    The guard owns complete context validation and progress semantics. The
    authority port owns authentication and freshness. Neither is inferred here.
    """
    result = dict(schema_version=1, result='ambiguous', retained=False,
                  approval_assurance=None, binding_digest=None,
                  current_binding_digest=None, comparison_evidence_ref=None,
                  comparator_revision=None, approval_evidence_ref=None,
                  approved_snapshot=None, current_snapshot=None, blocker=None)
    guard = guard if guard is not None else Unavailable003()
    authority = authority if authority is not None else UnavailableAuthority()
    try:
        root = Path(root).resolve(strict=True)
        if (not isinstance(request, dict) or set(request) != REQUEST_KEYS
                or type(request['schema_version']) is not int
                or request['schema_version'] != 1
                or not isinstance(request['approval_ref'], str)
                or not request['approval_ref']
                or not isinstance(request['approval_action'], str)
                or not re.fullmatch(r'[a-z][a-z0-9-]{0,63}', request['approval_action'])
                or not isinstance(request['approved_package_digest'], str)
                or not HEX.fullmatch(request['approved_package_digest'])):
            raise BindingBlocked('invalid-binding-request')
        approved_contract = _contract(root, guard.validate_context(
            request['approved_context'], request['evidence']))
        current_contract = _contract(root, guard.validate_context(
            request['current_context'], request['evidence']))
        if (not isinstance(guard.revision, str)
                or not REVISION.fullmatch(guard.revision)):
            raise BindingBlocked('invalid-comparator-revision')
        result['comparator_revision'] = guard.revision
        if approved_contract != current_contract:
            result['result'] = 'renewal-required'
            raise BindingBlocked('task-contract-changed')
        paths = approved_contract.protected_paths
        approved = _snapshot(root, request['approved_revision'], paths)
        current = _current(root, request['current_revision'], paths)
        result['approved_snapshot'] = approved.identity()
        result['current_snapshot'] = current.identity()
        result['binding_digest'] = binding_digest(approved_contract, approved)
        result['current_binding_digest'] = binding_digest(current_contract, current)
        if tuple((item.path, item.mode) for item in approved.artifacts) != tuple(
                (item.path, item.mode) for item in current.artifacts):
            result['result'] = 'renewal-required'
            raise BindingBlocked('protected-artifact-mode-changed')
        if approved.artifacts == current.artifacts:
            comparison = 'identical'
        else:
            proof = guard.compare_artifacts(approved, current, request['evidence'])
            if (not isinstance(proof, ArtifactComparison)
                    or proof.result not in ('progress-only', 'renewal-required', 'ambiguous')
                    or not isinstance(proof.evidence_ref, str) or not proof.evidence_ref):
                raise BindingBlocked('invalid-artifact-comparison')
            comparison = proof.result
            result['comparison_evidence_ref'] = proof.evidence_ref
        result['result'] = comparison
        if comparison not in ('identical', 'progress-only'):
            raise BindingBlocked('artifact-approval-renewal-required')
        target = ApprovalTarget(request['approval_action'], approved.revision,
                                request['approved_package_digest'], result['binding_digest'])
        verification = authority.verify(request['approval_ref'], target)
        if (not isinstance(verification, ApprovalVerification)
                or verification.approved is not True
                or verification.assurance not in ('supplied-local', 'authenticated-provider')
                or verification.target != target
                or not isinstance(verification.evidence_ref, str)
                or not verification.evidence_ref):
            raise BindingBlocked('approval-not-verified')
        if _current(root, request['current_revision'], paths) != current:
            raise BindingBlocked('current-source-changed-during-verification')
        result['approval_assurance'] = verification.assurance
        result['approval_evidence_ref'] = verification.evidence_ref
        result['retained'] = True
    except BindingBlocked as exc:
        # Only local safe codes are public. Adapters may throw with sensitive text.
        known = {'feature003-context-comparator-unavailable', 'approval-authority-unavailable',
                 'source-object-unavailable', 'invalid-guard-contract', 'invalid-protected-path',
                 'invalid-source-revision', 'protected-artifact-missing', 'unsafe-protected-artifact',
                 'protected-artifact-limit-exceeded',
                 'current-head-mismatch', 'dirty-current-source', 'unsafe-worktree-artifact',
                 'worktree-artifact-drift', 'invalid-binding-request', 'invalid-comparator-revision',
                 'task-contract-changed', 'protected-artifact-mode-changed',
                 'invalid-artifact-comparison', 'artifact-approval-renewal-required',
                 'approval-not-verified', 'current-source-changed-during-verification'}
        known.update(('executable-git-filter-unsupported', 'submodule-source-unsupported'))
        result['blocker'] = str(exc) if str(exc) in known else 'adapter-rejected'
    except Exception:
        result['blocker'] = 'validation-unavailable'
    return result
