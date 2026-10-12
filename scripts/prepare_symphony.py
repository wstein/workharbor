#!/usr/bin/env python3
"""Render non-secret operator settings; no daemon, tracker mutation or network."""
import json
import os
import re
import shutil
import shlex
import subprocess
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
required = ('WORKHARBOR_REPO_URL', 'WORKHARBOR_REPO_REVISION', 'WORKHARBOR_WORKSPACE_ROOT', 'WORKHARBOR_GITHUB_REPO', 'GITHUB_TOKEN')
missing = [key for key in required if not os.environ.get(key)]
if missing:
    sys.exit('Missing settings: ' + ', '.join(missing) + '; see docs/development/symphony.md')
if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', os.environ['WORKHARBOR_GITHUB_REPO']):
    sys.exit('WORKHARBOR_GITHUB_REPO must be owner/repository, not a URL.')
revision = os.environ['WORKHARBOR_REPO_REVISION']
if not re.fullmatch(r'[0-9a-f]{40}', revision):
    sys.exit('WORKHARBOR_REPO_REVISION must be a full lowercase commit SHA.')
feature = os.environ.get('WORKHARBOR_FEATURE')
if feature and (not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', feature)
                or not (root/'specs'/feature/'spec.md').is_file()):
    sys.exit('WORKHARBOR_FEATURE must name an existing direct specs/ feature.')
branch = os.environ.get('WORKHARBOR_ISSUE_BRANCH')
if branch and subprocess.run(['git', 'check-ref-format', '--branch', branch],
                             capture_output=True).returncode:
    sys.exit('WORKHARBOR_ISSUE_BRANCH must be a valid Git branch.')
if not shutil.which('codex'):
    sys.exit('Codex CLI missing; install and authenticate it before activation.')
workspace = Path(os.environ['WORKHARBOR_WORKSPACE_ROOT']).expanduser()
if not workspace.is_absolute():
    sys.exit('WORKHARBOR_WORKSPACE_ROOT must be absolute.')
workspace = workspace.resolve()
if workspace == root or workspace in root.parents or root in workspace.parents:
    sys.exit('Issue workspace root must be separate from this source checkout.')
text = (root/'WORKFLOW.md').read_text()
_, front, body = text.split('---', 2)
config = json.loads(front)
config['tracker']['provider']['repo'] = os.environ['WORKHARBOR_GITHUB_REPO']
config['workspace']['root'] = str(workspace)
hook = ('set -eu\n'
        ': "${WORKHARBOR_REPO_URL:?Set a trusted WorkHarbor repository URL or absolute local path}"\n'
        'git clone --no-local --no-checkout -- "$WORKHARBOR_REPO_URL" .\n')
if branch:
    hook += f'git switch -c {shlex.quote(branch)} {revision}\n'
else:
    hook += ('key=$(basename "$PWD")\n'
             'case "$key" in \'\'|*[!A-Za-z0-9._-]*) exit 1 ;; esac\n'
             f'git switch -c "work/$key" {revision}\n')
hook += f'test "$(git rev-parse HEAD)" = {revision}\n'
if feature:
    hook += f'python3 scripts/select_feature.py {shlex.quote(feature)}\n'
config['hooks']['after_create'] = hook
output = root/'WORKFLOW.local.md'
output.write_text('---\n' + json.dumps(config, indent=2) + '\n---' + body)
output.chmod(0o600)
print('Prepared WORKFLOW.local.md; no service started and no credentials written.')
