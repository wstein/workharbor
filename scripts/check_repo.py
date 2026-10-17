#!/usr/bin/env python3
"""Portable scaffold checks; never presented as product acceptance."""
import json
import re
import subprocess
import sys
from pathlib import Path

WORKSPACE_MANIFEST = 'examples/workspace/workharbor.json'

REQUIRED = [WORKSPACE_MANIFEST, 'README.md', 'AGENTS.md', '.specify/memory/constitution.md',
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

SCHEMA_PATH = 'contracts/workspace/workharbor.schema.json'
VECTORS_PATH = 'contracts/workspace/vectors.json'
MANIFEST_MAX_BYTES = 65536
MANIFEST_MAX_DEPTH = 128
V1_FIELDS = ('schema_version', 'devcontainer_path', 'projects_path', 'bundles')
V2_OPTIONAL = ('name', 'start_dir', 'human_shell', 'console_mode', 'shortcuts', 'project')
SCHEMA_ROOT = Path(__file__).resolve().parents[1]


class _Duplicate(Exception):
    pass


def _schema(root=None):
    return json.loads((Path(root or SCHEMA_ROOT)/SCHEMA_PATH).read_text(encoding='utf-8'))


def _too_deep(text):
    """Iterative nesting scan: no recursion, so a depth bomb cannot exhaust the stack."""
    depth = 0
    in_string = escaped = False
    for char in text:
        if in_string:
            if escaped:
                escaped = False
            elif char == '\\':
                escaped = True
            elif char == '"':
                in_string = False
        elif char == '"':
            in_string = True
        elif char in '[{':
            depth += 1
            if depth > MANIFEST_MAX_DEPTH:
                return True
        elif char in ']}':
            depth -= 1
    return False


def _no_constant(name):
    raise ValueError(name)


def _unique_pairs(pairs):
    keys = [key for key, _ in pairs]
    if len(keys) != len(set(keys)):
        raise _Duplicate()
    return dict(pairs)


def manifest_codes(content, schema=None):
    """Finding codes (contracts/cli/validate-findings.json) for the raw bytes of a manifest.

    An empty list means valid. Version comparison is type-exact: True, 1.0 and 1e0 are not 1.
    """
    if len(content) > MANIFEST_MAX_BYTES:
        return ['manifest.too_large']
    if content.startswith(b'\xef\xbb\xbf') or b'\x00' in content:
        return ['manifest.encoding']
    try:
        text = content.decode('utf-8')
    except UnicodeDecodeError:
        return ['manifest.encoding']
    if _too_deep(text):
        return ['manifest.syntax']
    try:
        data = json.loads(text, object_pairs_hook=_unique_pairs, parse_constant=_no_constant)
    except _Duplicate:
        return ['manifest.duplicate_key']
    except (ValueError, RecursionError):
        return ['manifest.syntax']
    return list(dict.fromkeys(code for code, _, _ in manifest_document_findings(data, schema)))


def _is_int(value, expected):
    return type(value) is int and value == expected


def manifest_document_findings(data, schema=None):
    """(code, field, detail) tuples for a parsed manifest value."""
    schema = schema or _schema()
    defs = schema['$defs']
    v2 = defs['v2']['properties']
    findings = []

    def add(code, field, detail=''):
        findings.append((code, field, detail))

    if not isinstance(data, dict):
        return [('manifest.schema', '', 'require a JSON object')]
    version = data.get('schema_version')
    if not (_is_int(version, 1) or _is_int(version, 2)):
        return [('manifest.schema', 'schema_version', 'require the integer 1 or 2')]
    allowed = set(V1_FIELDS) | (set(V2_OPTIONAL) if version == 2 else set())
    if not set(V1_FIELDS) <= set(data) or set(data) - allowed:
        add('manifest.schema', '', 'unexpected or missing fields')
    path_pattern = re.compile(defs['canonicalPath']['pattern'])

    def canonical(value):
        return (isinstance(value, str) and path_pattern.fullmatch(value) is not None
                and '.git' not in (part.casefold() for part in value.split('/')))

    for field in ('devcontainer_path', 'projects_path'):
        if field in data:
            if not isinstance(data[field], str):
                add('manifest.schema', field, 'require a string')
            elif not canonical(data[field]):
                add('manifest.path_invalid', field)
    if 'bundles' in data:
        if not isinstance(data['bundles'], list):
            add('manifest.schema', 'bundles', 'require a list')
        elif data['bundles']:
            add('manifest.bundles_deferred', 'bundles')
    if version != 2:
        return findings
    if 'name' in data:
        if not (isinstance(data['name'], str) and re.fullmatch(v2['name']['pattern'], data['name'])):
            add('manifest.schema', 'name')
    if 'start_dir' in data:
        value = data['start_dir']
        if not isinstance(value, str):
            add('manifest.schema', 'start_dir', 'require a string')
        elif value != '.' and not canonical(value):
            add('manifest.path_invalid', 'start_dir')
    for field in ('human_shell', 'console_mode', 'shortcuts'):
        if field in data and not (isinstance(data[field], str) and data[field] in v2[field]['enum']):
            add('manifest.schema', field)
    if 'project' in data:
        findings.extend(_project_findings(data['project'], data.get('projects_path'), v2['project']))
    return findings


def _project_findings(project, projects_path, project_schema):
    if not isinstance(project, dict) or set(project) != {'url', 'path'}:
        return [('manifest.schema', 'project', 'require exactly url and path')]
    url, path = project['url'], project['path']
    if not isinstance(url, str) or not isinstance(path, str):
        return [('manifest.schema', 'project', 'require strings')]
    userinfo = re.match(r'(?:https|ssh)://([^/?#]*)@', url)
    if userinfo and (url.startswith('https://') or ':' in userinfo.group(1)):
        return [('manifest.url_credentials', 'project.url', '')]
    problems = []
    pattern = project_schema['properties']['url']['pattern']
    if (not re.fullmatch(pattern, url) or len(url) > project_schema['properties']['url']['maxLength']
            or re.search(r'%(?![0-9A-Fa-f]{2})', url)):
        problems.append(('manifest.schema', 'project.url', ''))
    last = url.rstrip('/').rsplit('/', 1)[-1] if not url.endswith('/') else ''
    name = last[:-4] if last.endswith('.git') else last
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._-]*', name) or path != f'{projects_path}/{name}':
        problems.append(('manifest.schema', 'project.path', ''))
    return problems


def vector_bytes(case):
    """Materialize the raw manifest bytes of one shared vector case."""
    if 'document' in case:
        return json.dumps(case['document']).encode('utf-8')
    if 'bytes_hex' in case:
        return bytes.fromhex(case['bytes_hex'])
    if 'raw_text' in case:
        return case['raw_text'].encode('utf-8')
    spec = case['generate_bytes']
    template = spec['template'].encode('utf-8')
    return template + b' ' * (spec['total'] - len(template))


def vector_case_errors(cases):
    errors = []
    seen = set()
    for case in cases:
        if case['id'] in seen:
            errors.append(f'{VECTORS_PATH}: duplicate case id {case["id"]}; ids are unique')
        seen.add(case['id'])
        actual = manifest_codes(vector_bytes(case))
        if actual != case['expect_codes']:
            errors.append(f'{VECTORS_PATH}: case {case["id"]} expects {case["expect_codes"]} but check_repo.py '
                          f'yields {actual}; align the rule and the vector')
    return errors


def vector_errors(root):
    path = Path(root)/VECTORS_PATH
    if not path.is_file():
        return []
    try:
        return vector_case_errors(json.loads(path.read_text(encoding='utf-8'))['cases'])
    except (ValueError, KeyError, TypeError, OSError) as exc:
        return [f'{VECTORS_PATH}: malformed vector file; repair it ({exc})']


def workspace_errors(data):
    errors = []
    for code, field, detail in manifest_document_findings(data):
        if code == 'manifest.path_invalid':
            errors.append(f'workspace example: unsafe {field}; use a relative path without traversal')
        elif code == 'manifest.bundles_deferred':
            errors.append('workspace example: attachments are deferred; retain the empty bundle list')
        else:
            errors.append(f'workspace example: {code} {field} {detail}'.rstrip() + '; update the reviewed contract')
    return errors

def boundary_errors(relative, text):
    errors = []
    path = str(relative)
    if path.startswith('apps/control_plane/') and re.search(r'docker\.sock|github\.com/(docker|moby)/|DockerProvider', text):
        errors.append(f'{path}: provider implementation leaked into control plane; call a port')
    if path.startswith('apps/host_agent/') and '/providers/' not in path:
        # These exact pure input-grammar imports implement the reviewed config
        # capability gate; they expose no daemon/provider access. No module-prefix
        # exception, alias, comment, mixed statement or other file is admitted.
        grammar = 'github.com/moby/buildkit/frontend/dockerfile/'
        pure_imports = {
            'apps/host_agent/internal/config/dockerfile_plan.go': {'parser'},
            'apps/host_agent/internal/config/trusted.go': {'parser', 'instructions', 'linter'},
        }
        allowed_lines = {'"' + grammar + name + '"' for name in pure_imports.get(path, set())}
        import_scan = ''.join(line for line in text.splitlines(keepends=True)
                              if line.strip() not in allowed_lines)
        if re.search(r'"github\.com/(docker|moby)/', import_scan) or re.search(r'docker\.sock', text):
            errors.append(f'{path}: Docker access outside provider adapter; move it under providers/')
    if path.startswith('apps/') and re.search(r'openai/symphony|codex app-server', text):
        errors.append(f'{path}: development orchestrator entered product code; remove the dependency')
    return errors

def check(root):
    errors = [f'{name}: required artifact missing; restore it' for name in REQUIRED if not (root/name).is_file()]
    manifest = root/WORKSPACE_MANIFEST
    if manifest.is_file():
        try:
            errors.extend(workspace_errors(json.loads(manifest.read_text())))
        except (ValueError, UnicodeError) as exc:
            errors.append(f'{WORKSPACE_MANIFEST}: invalid JSON; repair syntax ({exc})')
    for example in sorted((root/'examples/workspace').glob('*.json')):
        codes = manifest_codes(example.read_bytes(), _schema(root))
        if codes:
            errors.append(f'{example.relative_to(root)}: manifest findings {codes}; repair the example')
    errors.extend(vector_errors(root))
    tracked = subprocess.check_output(['git', 'ls-files', '-z'], cwd=root).decode().split('\0')
    for name in filter(None, tracked):
        path = root/name
        if not path.is_file():
            errors.append(f'{name}: tracked file missing; restore or remove deliberately')
            continue
        if name.endswith('.json'):
            try:
                data = json.loads(path.read_text())
            except (ValueError, UnicodeError) as exc:
                errors.append(f'{name}: invalid JSON; repair syntax ({exc})')
        if name.endswith('.md') and (name.startswith(('docs/', 'specs/', 'contracts/', 'examples/')) or name in ('README.md','AGENTS.md')):
            errors.extend(document_errors(root, path))
        if name.startswith('apps/') and path.suffix in ('.go', '.ex', '.exs'):
            errors.extend(boundary_errors(Path(name), path.read_text()))
        if re.search(r'(^|/)(\.env|auth\.json)$', name):
            errors.append(f'{name}: credential file tracked; remove it and rotate exposed credentials')
    if (root/'WORKFLOW.md').exists():
        try:
            _, front, body = (root/'WORKFLOW.md').read_text().split('---', 2)
            workflow = json.loads(front)
            if workflow['tracker']['provider']['token'] != '$GITHUB_TOKEN':
                errors.append('WORKFLOW.md: use a secret reference, never an inline credential')
            tracker = workflow['tracker']
            if (tracker['kind'] != 'github' or tracker['active_states'] != ['open']
                    or tracker['terminal_states'] != ['closed']
                    or tracker['required_labels'] != ['workharbor-ready']):
                errors.append('WORKFLOW.md: dispatch only open GitHub issues labeled workharbor-ready')
            if workflow['agent']['max_concurrent_agents'] > 2:
                errors.append('WORKFLOW.md: baseline concurrency exceeds two; review capacity first')
            if not body.strip():
                errors.append('WORKFLOW.md: missing agent policy')
        except (ValueError, KeyError, TypeError) as exc:
            errors.append(f'WORKFLOW.md: malformed baseline contract; restore JSON YAML front matter ({exc})')
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
