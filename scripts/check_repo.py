#!/usr/bin/env python3
"""Portable scaffold checks; never presented as product acceptance."""
import json
import re
import subprocess
import sys
from pathlib import Path

REQUIRED = ['README.md', 'AGENTS.md', '.specify/memory/constitution.md',
            'WORKFLOW.md', 'docs/index.md', 'contracts/README.md',
            'specs/001-workspace-lifecycle/spec.md',
            'specs/001-workspace-lifecycle/plan.md',
            'specs/001-workspace-lifecycle/tasks.md']

def document_errors(root, path):
    errors = []
    text = path.read_text()
    # Code fences can contain illustrative links that are not document navigation.
    text = re.sub(r'```.*?```', '', text, flags=re.S)
    for target in re.findall(r'\[[^\]]+\]\(([^)]+)\)', text):
        if re.match(r'^(https?://|mailto:|#)', target):
            continue
        target = target.split('#', 1)[0]
        if target and not (path.parent / target).exists():
            errors.append(f'{path.relative_to(root)}: broken link {target}; fix the path or create its target')
    return errors

def workspace_errors(data):
    errors = []
    if not isinstance(data, dict) or data.get('schema_version') != 1:
        return ['workspace example: require schema_version 1']
    if set(data) != {'schema_version', 'devcontainer_path', 'projects_path', 'bundles'}:
        errors.append('workspace example: unexpected or missing fields; update the reviewed contract')
    for field in ('devcontainer_path', 'projects_path'):
        value = data.get(field)
        if (not isinstance(value, str) or not value or value.startswith('/')
                or '..' in value.split('/') or '\\' in value):
            errors.append(f'workspace example: unsafe {field}; use a relative path without traversal')
    if data.get('bundles') != []:
        errors.append('workspace example: attachments are deferred; retain the empty bundle list')
    return errors

def boundary_errors(relative, text):
    errors = []
    path = str(relative)
    if path.startswith('apps/control_plane/') and re.search(r'docker\.sock|github\.com/(docker|moby)/|DockerProvider', text):
        errors.append(f'{path}: provider implementation leaked into control plane; call a port')
    if path.startswith('apps/host_agent/') and '/providers/' not in path:
        if re.search(r'"github\.com/(docker|moby)/|docker\.sock', text):
            errors.append(f'{path}: Docker access outside provider adapter; move it under providers/')
    if path.startswith('apps/') and re.search(r'openai/symphony|codex app-server', text):
        errors.append(f'{path}: development orchestrator entered product code; remove the dependency')
    return errors

def check(root):
    errors = [f'{name}: required artifact missing; restore it' for name in REQUIRED if not (root/name).is_file()]
    tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=root).decode().split('\0')
    for name in filter(None, tracked):
        path = root/name
        if not path.is_file():
            errors.append(f'{name}: tracked file missing; restore or remove deliberately')
            continue
        if name.endswith('.json'):
            try:
                data = json.loads(path.read_text())
                if name == 'examples/workspace/anchorage.json':
                    errors.extend(workspace_errors(data))
            except (ValueError, UnicodeError) as exc:
                errors.append(f'{name}: invalid JSON; repair syntax ({exc})')
        if name.endswith('.md') and (name.startswith(('docs/', 'specs/', 'contracts/', 'examples/')) or name in ('README.md','AGENTS.md')):
            errors.extend(document_errors(root, path))
        if name.startswith('apps/') and path.suffix in ('.go', '.ex', '.exs'):
            errors.extend(boundary_errors(Path(name), path.read_text()))
        if re.search(r'(^|/)(\.env|auth\.json)$', name):
            errors.append(f'{name}: credential file tracked; remove it and rotate exposed credentials')
    if (root/'AGENTS.md').exists() and len((root/'AGENTS.md').read_text().splitlines()) > 100:
        errors.append('AGENTS.md: exceeds 100 lines; move detail into linked documents')
    return errors

if __name__ == '__main__':
    errors = check(Path(__file__).resolve().parents[1])
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        sys.exit(1)
    print('Repository bootstrap checks passed (product runtime not evaluated).')
