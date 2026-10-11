#!/usr/bin/env python3
"""Local single-task scope/acceptance guard; not a sandbox or approval authority."""
import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

STATE = '.symphony/single-task.json'
TASK = re.compile(r'^(- \[)([ xX])(\] (T\d{3}) .+)$', re.M)


def git(root, *args):
    return subprocess.check_output(['git', *args], cwd=root).decode()


def path_in_repo(root, value):
    path = Path(value)
    if (path.is_absolute() or not value or '..' in path.parts
            or path.as_posix() != value or path.parts[0] in ('.git', '.symphony')):
        raise ValueError(f'Invalid repository path: {value}')
    if not (root/path).resolve().is_relative_to(root):
        raise ValueError(f'Path escapes repository: {value}')
    return value


def task_lines(text):
    rows = list(TASK.finditer(text))
    ids = [row[4] for row in rows]
    if len(ids) != len(set(ids)):
        raise ValueError('Duplicate task IDs')
    return {row[4]: row for row in rows}


def write_state(root, state):
    path = root/STATE
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix('.tmp')
    temporary.write_text(json.dumps(state, indent=2) + '\n')
    temporary.replace(path)


def begin(root, args):
    if (root/STATE).exists():
        raise ValueError('Existing task state: resume with verify/complete; do not reset it')
    if git(root, 'status', '--porcelain').strip():
        raise ValueError('Begin requires a clean checkout, including untracked files')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', args.feature):
        raise ValueError('Feature must be a direct specs/ directory name')
    feature = root/'specs'/args.feature
    if feature.is_symlink() or not feature.is_dir():
        raise ValueError('Feature directory missing or symlinked')
    for name in ('spec.md', 'plan.md', 'tasks.md'):
        if not (feature/name).is_file() or (feature/name).is_symlink():
            raise ValueError(f'Missing regular feature artifact: {name}')
    tasks = f'specs/{args.feature}/tasks.md'
    text = (root/tasks).read_text()
    rows = task_lines(text)
    if args.task not in rows or rows[args.task][2] != ' ':
        raise ValueError('Select exactly one existing pending task')
    protected = {f'specs/{args.feature}/{name}' for name in ('spec.md', 'plan.md', 'tasks.md')}
    allowed = sorted({path_in_repo(root, value) for value in args.allow})
    if protected.intersection(allowed):
        raise ValueError('Feature artifacts cannot be allowed implementation paths')
    checks = [json.loads(value) for value in args.check]
    if not all(isinstance(cmd, list) and cmd and all(isinstance(x, str) and x for x in cmd) for cmd in checks):
        raise ValueError('Each check must be a JSON array of command arguments')
    state = dict(feature=args.feature, task=args.task, baseline=git(root, 'rev-parse', 'HEAD').strip(),
                 tasks_path=tasks, tasks_before=text, allowed=allowed, checks=checks,
                 phase='implement', results=[])
    write_state(root, state)
    print(f'Prepared {args.feature}/{args.task}; scope guard active, approvals not verified.')


def scope(root, state):
    baseline = state['baseline']
    git(root, 'merge-base', '--is-ancestor', baseline, 'HEAD')
    changed = set(filter(None, git(root, 'diff', '--name-only', '-z', baseline, '--').split('\0')))
    changed.update(filter(None, git(root, 'ls-files', '--others', '--exclude-standard', '-z').split('\0')))
    allowed = set(state['allowed']) | {state['tasks_path']}
    extra = changed - allowed
    if extra:
        raise ValueError('Out-of-scope changes: ' + ', '.join(sorted(extra)))
    for value in changed:
        path_in_repo(root, value)
        if (root/value).is_symlink():
            raise ValueError(f'Symlink change not allowed: {value}')
    current = (root/state['tasks_path']).read_text()
    before = state['tasks_before']
    rows = task_lines(current)
    if state['task'] not in rows:
        raise ValueError('Selected task missing')
    row = rows[state['task']]
    normalized = current[:row.start(2)] + ' ' + current[row.end(2):]
    if normalized != before:
        raise ValueError('Only the selected task checkbox may change in tasks.md')
    if row[2] != ' ' and (len(state['results']) != len(state['checks'])
                         or any(result['exit_code'] for result in state['results'])):
        raise ValueError('Completed checkbox has no passing acceptance evidence')
    signature = {}
    for value in sorted(allowed):
        path = root/value
        data = normalized.encode() if value == state['tasks_path'] else (path.read_bytes() if path.is_file() else b'<absent>')
        signature[value] = hashlib.sha256(data).hexdigest()
    if (row[2] != ' ' or state['phase'] == 'handoff-only') and signature != state.get('verified_files'):
        raise ValueError('Completed task content changed; owner-authorized rework required')
    return current, row, signature


def verify(root, state):
    _, _, signature = scope(root, state)
    previous = list(state['results'])
    results = []
    write_state(root, state)
    for command in state['checks']:
        run = subprocess.run(command, cwd=root)
        results.append(dict(command=command, exit_code=run.returncode))
        if run.returncode:
            state['last_failure'] = results
            write_state(root, state)
            raise ValueError(f'Acceptance failed: {command}')
    # Checks may themselves mutate files: inspect again before accepting completion.
    # Retain previous completed-task evidence while validating current checks.
    state['results'] = previous
    _, _, after = scope(root, state)
    if after != signature:
        raise ValueError('Acceptance commands changed task content; rerun verification')
    state['results'] = results
    state['verified_files'] = signature
    state.pop('last_failure', None)
    state['verified_head'] = git(root, 'rev-parse', 'HEAD').strip()
    write_state(root, state)
    print('Scope and acceptance passed; live Symphony behavior not evaluated.')


def complete(root, state):
    verify(root, state)
    text, row, _ = scope(root, state)
    (root/state['tasks_path']).write_text(text[:row.start(2)] + 'x' + text[row.end(2):])
    state['phase'] = 'handoff-only'
    write_state(root, state)
    print('Selected task complete; finish evidence/commit/PR before final label switch.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    start = sub.add_parser('begin')
    start.add_argument('--feature', required=True)
    start.add_argument('--task', required=True)
    start.add_argument('--allow', action='append', required=True, help='Exact approved file path; repeat as needed')
    start.add_argument('--check', action='append', required=True, help='Approved argv JSON, run at repo root; repeat')
    sub.add_parser('verify')
    sub.add_parser('complete')
    args = parser.parse_args()
    root = Path(git(Path.cwd(), 'rev-parse', '--show-toplevel').strip()).resolve()
    if args.command == 'begin':
        begin(root, args)
    else:
        state = json.loads((root/STATE).read_text())
        if args.command == 'verify':
            verify(root, state)
        else:
            complete(root, state)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        sys.exit(str(exc))
