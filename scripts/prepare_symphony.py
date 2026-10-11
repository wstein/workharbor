#!/usr/bin/env python3
"""Render non-secret operator settings; no daemon, tracker mutation or network."""
import json
import os
import shutil
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
required = ('ANCHORAGE_REPO_URL', 'ANCHORAGE_WORKSPACE_ROOT', 'ANCHORAGE_LINEAR_PROJECT', 'LINEAR_API_KEY')
missing = [key for key in required if not os.environ.get(key)]
if missing:
    sys.exit('Missing settings: ' + ', '.join(missing) + '; see docs/development/symphony.md')
if not shutil.which('codex'):
    sys.exit('Codex CLI missing; install and authenticate it before activation.')
workspace = Path(os.environ['ANCHORAGE_WORKSPACE_ROOT']).expanduser()
if not workspace.is_absolute():
    sys.exit('ANCHORAGE_WORKSPACE_ROOT must be absolute.')
workspace = workspace.resolve()
if workspace == root or workspace in root.parents or root in workspace.parents:
    sys.exit('Issue workspace root must be separate from this source checkout.')
text = (root/'WORKFLOW.md').read_text()
_, front, body = text.split('---', 2)
config = json.loads(front)
config['tracker']['provider']['project_slug'] = os.environ['ANCHORAGE_LINEAR_PROJECT']
config['workspace']['root'] = str(workspace)
output = root/'WORKFLOW.local.md'
output.write_text('---\n' + json.dumps(config, indent=2) + '\n---' + body)
output.chmod(0o600)
print('Prepared WORKFLOW.local.md; no service started and no credentials written.')
