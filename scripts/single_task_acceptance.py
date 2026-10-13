"""Ordered local acceptance with fresh content binding and safe durable evidence.

The caller holds the worktree store lock throughout. Reviewed commands are trusted
execution, not a sandbox; ordinary descendants share a bounded process group.
Raw subprocess streams are discarded and never become journal or CLI evidence.
"""
import hashlib
import os
import signal
import subprocess
from pathlib import Path

import single_task_scope as scope
from single_task_context import canonical_bytes


class AcceptanceFailure(Exception):
    """A fixed safe category; never include subprocess output or command text."""
    def __init__(self, category):
        self.category = category
        super().__init__('Local acceptance did not pass')


def _observed_group_states(pid):
    """Resolve signal permission races without treating EPERM as disappearance."""
    try:
        snapshot = subprocess.run(['ps', '-axo', 'pgid=,stat='], capture_output=True,
                                  text=True, check=True, timeout=1)
        return [state for group, state in
                (line.split() for line in snapshot.stdout.splitlines()) if int(group) == pid]
    except (OSError, ValueError, subprocess.SubprocessError):
        raise AcceptanceFailure('process_cleanup') from None


def _group_exists(pid):
    try:
        os.killpg(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        # Retain background-process rejection even for a zombie-only group.
        return bool(_observed_group_states(pid))


def _signal_group(pid, signum):
    try:
        os.killpg(pid, signum)
    except ProcessLookupError:
        pass
    except PermissionError:
        if any(not state.startswith('Z') for state in _observed_group_states(pid)):
            raise AcceptanceFailure('process_cleanup') from None


def _stop_group(process):
    if _group_exists(process.pid):
        _signal_group(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=.2)
        except subprocess.TimeoutExpired:
            pass
        _signal_group(process.pid, signal.SIGKILL)
    try:
        process.wait(timeout=1)
    except subprocess.TimeoutExpired:
        raise AcceptanceFailure('process_cleanup') from None


def _run_check(root, check):
    descriptor = scope._path_fd(root, check['cwd'], directory=True)
    process = None
    result = {'exit': None, 'category': 'passed', 'timed_out': False}
    try:
        try:
            # This POSIX command process is single-threaded. fchdir uses the
            # already-validated directory inode; /dev/fd directory traversal is
            # unavailable on macOS, and spelling cwd again would follow races.
            process = subprocess.Popen(check['argv'], preexec_fn=lambda: os.fchdir(descriptor),
                                       pass_fds=(descriptor,), start_new_session=True,
                                       stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                       stderr=subprocess.DEVNULL, shell=False)
        except FileNotFoundError:
            result['category'] = 'missing_executable'
            return result
        except OSError:
            result['category'] = 'execution_error'
            return result
        try:
            result['exit'] = process.wait(timeout=check['timeout_seconds'])
            if result['exit'] != 0:
                result['category'] = 'nonzero_exit'
            elif _group_exists(process.pid):
                result['category'] = 'background_process'
        except subprocess.TimeoutExpired:
            result.update(category='timeout', timed_out=True)
        finally:
            _stop_group(process)
        return result
    finally:
        os.close(descriptor)


def execute_acceptance(root, attempt, review, store):
    """Execute every approved check against one current manifest under the lock."""
    root = Path(root)
    inspection = scope.prepare_inspection(root, attempt)
    manifest = inspection['content_manifest']
    review = scope.validate_semantic_review(review, attempt, manifest)
    context = attempt['context']
    task_path = f"specs/{context['feature']}/tasks.md"
    tasks_before = scope.read_regular(root, task_path)
    head = scope.git(root, 'rev-parse', 'HEAD').stdout.strip().decode('ascii')
    index = scope._git_inventory(root)
    review_digest = hashlib.sha256(canonical_bytes(review)).hexdigest()
    results = []
    for check in context['checks']:
        result = _run_check(root, check)
        try:
            after = scope.prepare_inspection(root, attempt)
            if (scope.read_regular(root, task_path) != tasks_before or
                    scope.git(root, 'rev-parse', 'HEAD').stdout.strip().decode('ascii') != head or
                    scope._git_inventory(root) != index):
                result['category'] = 'scope_changed'
            elif after['content_manifest_digest'] != inspection['content_manifest_digest']:
                result['category'] = 'content_changed'
        except ValueError:
            result['category'] = 'scope_changed'
        payload = {'schema_version': 1, 'id': check['id'], 'argv': check['argv'],
                   'cwd': check['cwd'], 'expected_exit': check['expected_exit'],
                   **result, 'context_digest': inspection['context_digest'],
                   'content_manifest_digest': inspection['content_manifest_digest'],
                   'semantic_review_digest': review_digest}
        store.append_event(attempt['attempt_id'], 'check-result', payload)
        results.append(payload)
        if result['category'] != 'passed':
            raise AcceptanceFailure(result['category'])
    evidence = {'schema_version': 1, 'attempt_id': attempt['attempt_id'],
                'task_id': context['task_id'], 'context_digest': inspection['context_digest'],
                'content_manifest': manifest,
                'content_manifest_digest': inspection['content_manifest_digest'],
                'semantic_review': review, 'semantic_review_digest': review_digest,
                'verification_head': head, 'checks': results}
    progress = store.read_attempt(attempt['attempt_id'])['progress']
    store.append_event(attempt['attempt_id'], 'acceptance', evidence,
                       progress='acceptance-recorded' if progress in
                       ('active', 'acceptance-recorded') else None)
    return evidence
